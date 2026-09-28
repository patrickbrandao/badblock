// Package fetch baixa as fontes do catálogo com GET condicional, fallback para
// espelhos e conferência do MD5 publicado pelos RIRs.
package fetch

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/source"
)

// Previous é o que se sabe da última resposta aplicada de uma fonte.
type Previous struct {
	URL          string
	ETag         string
	LastModified string
	SHA256       string
}

// Result descreve um download.
type Result struct {
	URL          string
	Fallback     bool // a URL usada não é a primária
	Status       int
	NotModified  bool // 304, ou conteúdo idêntico ao último aplicado
	Body         []byte
	SHA256       string
	ETag         string
	LastModified string
	Failures     []string // erros das URLs tentadas antes da que funcionou
}

// Fetcher baixa fontes por HTTP ou, no modo fixture, lê de um diretório.
type Fetcher struct {
	Client     *http.Client
	UserAgent  string
	SourcesDir string // se definido, lê <dir>/<id> em vez de usar a rede
	MaxBytes   int64
	Retries    int           // tentativas extras por URL em erro de rede ou 5xx
	RetryDelay time.Duration // espera entre tentativas
}

// NewHTTPClient cria o cliente com timeouts de conexão. O limite total de cada
// download vem do contexto.
func NewHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   2,
	}
	return &http.Client{Transport: transport}
}

// Fetch baixa a fonte. Tenta as URLs em ordem e para na primeira que responder
// 200 (com MD5 válido, quando a fonte publica um) ou 304.
func (f *Fetcher) Fetch(ctx context.Context, src source.Source, prev Previous) (*Result, error) {
	if f.SourcesDir != "" {
		return f.fetchFile(src, prev)
	}
	var failures []string
	for i, url := range src.URLs {
		res, err := f.fetchURLWithRetry(ctx, src, url, prev)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", url, err))
			if ctx.Err() != nil {
				break
			}
			continue
		}
		res.Fallback = i > 0
		res.Failures = failures
		return res, nil
	}
	return nil, fmt.Errorf("nenhuma URL respondeu: %s", strings.Join(failures, " | "))
}

// errRetryable marca falhas que valem nova tentativa (rede, 5xx).
type errRetryable struct{ error }

func (f *Fetcher) fetchURLWithRetry(ctx context.Context, src source.Source, url string, prev Previous) (*Result, error) {
	var err error
	for attempt := 0; attempt <= f.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(f.RetryDelay):
			}
		}
		var res *Result
		res, err = f.fetchURL(ctx, src, url, prev)
		if err == nil {
			return res, nil
		}
		var retry errRetryable
		if !errors.As(err, &retry) {
			return nil, err
		}
	}
	return nil, err
}

func (f *Fetcher) fetchURL(ctx context.Context, src source.Source, url string, prev Previous) (*Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.UserAgent)
	// Os validadores só valem para a mesma URL: o ETag de um espelho não diz
	// nada sobre a URL primária.
	if prev.URL == url {
		if prev.ETag != "" {
			req.Header.Set("If-None-Match", prev.ETag)
		}
		if prev.LastModified != "" {
			req.Header.Set("If-Modified-Since", prev.LastModified)
		}
	}

	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, errRetryable{err}
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotModified:
		return &Result{
			URL: url, Status: resp.StatusCode, NotModified: true,
			ETag: prev.ETag, LastModified: prev.LastModified, SHA256: prev.SHA256,
		}, nil
	case resp.StatusCode >= 500:
		return nil, errRetryable{fmt.Errorf("HTTP %d", resp.StatusCode)}
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := readLimited(resp.Body, f.MaxBytes)
	if err != nil {
		return nil, errRetryable{err}
	}
	if src.MD5 {
		if err := f.checkMD5(ctx, url+".md5", body); err != nil {
			return nil, err
		}
	}
	res := &Result{
		URL:          url,
		Status:       resp.StatusCode,
		Body:         body,
		SHA256:       sha256Hex(body),
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}
	res.NotModified = prev.SHA256 != "" && res.SHA256 == prev.SHA256
	return res, nil
}

// md5Re acha o hash nos dois formatos publicados pelos RIRs:
// "MD5 (arquivo) = <hash>" (BSD) e "<hash>  arquivo" (GNU).
var md5Re = regexp.MustCompile(`\b[0-9a-fA-F]{32}\b`)

func (f *Fetcher) checkMD5(ctx context.Context, url string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", f.UserAgent)
	resp, err := f.Client.Do(req)
	if err != nil {
		return errRetryable{fmt.Errorf("md5: %w", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("md5: HTTP %d", resp.StatusCode)
	}
	raw, err := readLimited(resp.Body, 4096)
	if err != nil {
		return fmt.Errorf("md5: %w", err)
	}
	return verifyMD5(raw, body)
}

// verifyMD5 compara o hash publicado com o do conteúdo baixado. Uma
// divergência costuma ser corrida entre a publicação do arquivo e a do .md5;
// a próxima checagem resolve.
func verifyMD5(published, body []byte) error {
	want := strings.ToLower(md5Re.FindString(string(published)))
	if want == "" {
		return fmt.Errorf("md5: arquivo sem hash reconhecível")
	}
	sum := md5.Sum(body)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("md5 divergente: publicado %s, baixado %s", want, got)
	}
	return nil
}

// fetchFile é o modo fixture: lê <dir>/<id> e, se houver, <dir>/<id>.md5.
func (f *Fetcher) fetchFile(src source.Source, prev Previous) (*Result, error) {
	path := filepath.Join(f.SourcesDir, src.ID)
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if src.MD5 {
		if published, err := os.ReadFile(path + ".md5"); err == nil {
			if err := verifyMD5(published, body); err != nil {
				return nil, err
			}
		}
	}
	sum := sha256Hex(body)
	return &Result{
		URL:         "file://" + path,
		Status:      http.StatusOK,
		Body:        body,
		SHA256:      sum,
		NotModified: prev.SHA256 != "" && sum == prev.SHA256,
	}, nil
}

func readLimited(r io.Reader, max int64) ([]byte, error) {
	if max <= 0 {
		max = 512 << 20
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

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
