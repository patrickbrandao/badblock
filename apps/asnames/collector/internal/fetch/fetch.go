// Package fetch baixa o arquivo da fonte.
//
// O download é condicional (If-None-Match / If-Modified-Since com os
// validadores do último arquivo aplicado): um 304 significa "nada mudou" sem
// transferir o arquivo. O RIPE não publica hash do asn.txt, então esta é a
// checagem barata; o SHA-256 do conteúdo é calculado aqui para a checagem
// seguinte.
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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
	SHA256       string
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
// requisição vem do contexto. A compressão gzip fica ligada (o servidor do
// RIPE a oferece: ~2,3 MB em vez de ~6 MB) e é desfeita pelo transporte; o
// corpo, o limite de tamanho e o SHA-256 valem para o arquivo descomprimido.
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
		sum := sha256.Sum256(body)
		d = &Download{
			URL:          url,
			Status:       resp.StatusCode,
			Body:         body,
			SHA256:       hex.EncodeToString(sum[:]),
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
		req.Header.Set("If-None-Match", prev.ETag)
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

// retryable marca falhas que valem nova tentativa (rede, 5xx, corpo cortado).
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
