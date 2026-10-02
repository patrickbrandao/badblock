// Package fetch baixa o named.root e o MD5 publicado ao lado dele
// (named.root.md5).
//
// O download é condicional (If-None-Match / If-Modified-Since com os
// validadores do último arquivo aplicado): um 304 significa "nada mudou" sem
// transferir o arquivo. O MD5 e o SHA-256 do corpo são calculados aqui para a
// conferência e para a checagem de conteúdo igual. A compressão gzip fica
// ligada (o transporte a pede e a desfaz); corpo, limite de tamanho, MD5 e
// SHA-256 valem para o arquivo descomprimido.
//
// Com gzip, o Apache da InterNIC (mod_deflate) devolve o ETag com o sufixo
// "-gzip" ("cf3-65ca377d2eb00-gzip") e não reconhece esse mesmo valor no
// If-None-Match (responde 200); reconhece o ETag sem o sufixo. Hoje o
// named.root sai sem compressão, mas a cópia named.cache, no mesmo servidor,
// sai com; por isso o If-None-Match leva as duas formas (ver IfNoneMatch).
package fetch

import (
	"context"
	"crypto/md5" // só confere o hash publicado pela InterNIC; não é uso de segurança
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Validators são os cabeçalhos do último arquivo aplicado.
type Validators struct {
	ETag         string
	LastModified string
}

// Download é o resultado de um GET do arquivo.
type Download struct {
	URL          string
	Status       int
	NotModified  bool // 304
	Body         []byte
	MD5          string // hex minúsculo
	SHA256       string // hex minúsculo
	ETag         string
	LastModified string
}

// Fetcher faz as requisições HTTP.
type Fetcher struct {
	Client     *http.Client
	UserAgent  string
	MaxBytes   int64
	Retries    int           // tentativas extras em erro de rede ou 5xx
	RetryDelay time.Duration // espera entre tentativas
}

// NewHTTPClient cria o cliente com timeouts de conexão; o limite total de cada
// requisição vem do contexto.
func NewHTTPClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   2,
	}}
}

// A InterNIC publica só o hash (33 bytes: 32 dígitos hex minúsculos e "\n").
// Os formatos BSD e GNU também são aceitos, como no padrão dos coletores:
//
//	d0732825a760fee171258b4890ca5243                     (só o hash, InterNIC)
//	MD5 (named.root) = d0732825a760fee171258b4890ca5243  (BSD)
//	d0732825a760fee171258b4890ca5243  named.root         (GNU)
var (
	md5BSDRe = regexp.MustCompile(`^MD5 ?\(.*\) ?= ?([0-9a-fA-F]{32})$`)
	md5GNURe = regexp.MustCompile(`^([0-9a-fA-F]{32})(?:[ \t]+\*?.*)?$`)
)

// ParseMD5 extrai o hash (hex minúsculo) do conteúdo de um arquivo .md5, no
// formato BSD ou GNU (maiúsculas aceitas). Olha só a primeira linha não vazia:
// uma página HTML de erro não pode virar um hash.
func ParseMD5(raw string) (string, error) {
	for line := range strings.Lines(raw) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, re := range []*regexp.Regexp{md5BSDRe, md5GNURe} {
			if m := re.FindStringSubmatch(line); m != nil {
				return strings.ToLower(m[1]), nil
			}
		}
		return "", fmt.Errorf("sem hash MD5 reconhecível: %q", truncate(line, 80))
	}
	return "", errors.New("arquivo .md5 vazio")
}

// PublishedMD5 baixa o arquivo .md5 e devolve o hash em hex minúsculo.
func (f *Fetcher) PublishedMD5(ctx context.Context, url string) (string, error) {
	var hash string
	err := f.retry(ctx, func() error {
		resp, err := f.get(ctx, url, Validators{})
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if err := statusError(resp.StatusCode); err != nil {
			return err
		}
		raw, err := readLimited(resp.Body, 4096)
		if err != nil {
			return retryable{err}
		}
		if hash, err = ParseMD5(string(raw)); err != nil {
			return fmt.Errorf("%s: %w", url, err)
		}
		return nil
	})
	return hash, err
}

// Download baixa o arquivo com GET condicional.
func (f *Fetcher) Download(ctx context.Context, url string, prev Validators) (*Download, error) {
	var d *Download
	err := f.retry(ctx, func() error {
		resp, err := f.get(ctx, url, prev)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotModified {
			d = &Download{URL: url, Status: resp.StatusCode, NotModified: true,
				ETag: prev.ETag, LastModified: prev.LastModified}
			return nil
		}
		if err := statusError(resp.StatusCode); err != nil {
			return err
		}
		body, err := readLimited(resp.Body, f.MaxBytes)
		if err != nil {
			return retryable{err}
		}
		sumMD5 := md5.Sum(body)
		sumSHA := sha256.Sum256(body)
		d = &Download{
			URL:          url,
			Status:       resp.StatusCode,
			Body:         body,
			MD5:          hex.EncodeToString(sumMD5[:]),
			SHA256:       hex.EncodeToString(sumSHA[:]),
			ETag:         resp.Header.Get("ETag"),
			LastModified: resp.Header.Get("Last-Modified"),
		}
		return nil
	})
	return d, err
}

func (f *Fetcher) get(ctx context.Context, url string, prev Validators) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.UserAgent)
	if prev.ETag != "" {
		req.Header.Set("If-None-Match", IfNoneMatch(prev.ETag))
	}
	if prev.LastModified != "" {
		req.Header.Set("If-Modified-Since", prev.LastModified)
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, retryable{err}
	}
	return resp, nil
}

// IfNoneMatch monta o If-None-Match a partir do ETag guardado. Um ETag com o
// sufixo "-gzip" do mod_deflate vai também sem o sufixo
// (`"x-gzip", "x"`): o Apache só reconhece a segunda forma e responde 304 a
// ela; um servidor que reconheça a primeira também responde 304.
func IfNoneMatch(etag string) string {
	base, ok := strings.CutSuffix(etag, `-gzip"`)
	if ok && (strings.HasPrefix(base, `"`) || strings.HasPrefix(base, `W/"`)) {
		return etag + ", " + base + `"`
	}
	return etag
}

// retryable marca falhas que valem nova tentativa (rede, 5xx).
type retryable struct{ error }

func (e retryable) Unwrap() error { return e.error }

func statusError(code int) error {
	switch {
	case code == http.StatusOK:
		return nil
	case code >= 500:
		return retryable{fmt.Errorf("HTTP %d", code)}
	default:
		return fmt.Errorf("HTTP %d", code)
	}
}

func (f *Fetcher) retry(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 0; attempt <= f.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(f.RetryDelay):
			}
		}
		if err = fn(); err == nil {
			return nil
		}
		if _, ok := errors.AsType[retryable](err); !ok {
			return err
		}
	}
	return err
}

func readLimited(r io.Reader, max int64) ([]byte, error) {
	if max <= 0 {
		max = 64 << 20
	}
	body, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > max {
		return nil, fmt.Errorf("resposta maior que %d bytes", max)
	}
	return body, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
