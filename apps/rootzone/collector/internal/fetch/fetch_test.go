package fetch

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Hash do root.zone de 2026-09-29 (serial 2026092901), como publicado.
const hash = "0df326149210107d5dcb2cac740ca4b0"

func newFetcher() *Fetcher {
	return &Fetcher{Client: NewHTTPClient(), UserAgent: "test", Retries: 2, RetryDelay: time.Millisecond}
}

func TestParseMD5Formats(t *testing.T) {
	good := map[string]string{
		"internic (só o hash e LF)": hash + "\n",
		"sem LF":                    hash,
		"bsd":                       "MD5 (root.zone) = " + hash + "\n",
		"gnu":                       hash + "  root.zone\n",
		"gnu binário":               hash + " *root.zone\n",
		"linha vazia antes":         "\n" + hash + "\n",
		"maiúsculo":                 strings.ToUpper(hash),
	}
	for name, body := range good {
		if got, err := ParseMD5(body); err != nil || got != hash {
			t.Errorf("%s: hash = %q, err = %v", name, got, err)
		}
	}
	bad := map[string]string{
		"html":       "<html><body>" + hash + "</body></html>",
		"vazio":      "\n\n",
		"sha256":     "SHA256 (x) = 78e2a7f6f7c151534979196f781ba5ce7c5d9b8ce84eee84372d60fd86bda8ec",
		"hash curto": hash[:31],
	}
	for name, body := range bad {
		if got, err := ParseMD5(body); err == nil {
			t.Errorf("%s: esperava erro, veio %q", name, got)
		}
	}
}

func TestPublishedMD5(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "test" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(hash + "\n"))
	}))
	defer srv.Close()
	got, err := newFetcher().PublishedMD5(context.Background(), srv.URL)
	if err != nil || got != hash {
		t.Errorf("hash = %q, err = %v", got, err)
	}
}

func TestPublishedMD5Garbage(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte("<html>not found</html>"))
	}))
	defer srv.Close()
	if _, err := newFetcher().PublishedMD5(context.Background(), srv.URL); err == nil {
		t.Error("esperava erro para conteúdo sem hash")
	}
	if calls.Load() != 1 {
		t.Errorf("conteúdo inválido não deve ser repetido; tentativas = %d", calls.Load())
	}
}

func TestPublishedMD5TooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("a"), 5000))
	}))
	defer srv.Close()
	if _, err := newFetcher().PublishedMD5(context.Background(), srv.URL); err == nil || !strings.Contains(err.Error(), "4096") {
		t.Errorf("err = %v", err)
	}
}

func TestDownloadConditional(t *testing.T) {
	var gotUA atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA.Store(r.Header.Get("User-Agent"))
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Tue, 29 Sep 2026 18:37:00 GMT")
		_, _ = w.Write([]byte("abc"))
	}))
	defer srv.Close()

	f := newFetcher()
	d, err := f.Download(context.Background(), srv.URL, Validators{})
	if err != nil {
		t.Fatal(err)
	}
	if d.NotModified || string(d.Body) != "abc" || d.ETag != `"v1"` || d.LastModified == "" || d.Status != 200 {
		t.Errorf("primeiro download = %+v", d)
	}
	if d.SHA256 != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" || d.MD5 != "900150983cd24fb0d6963f7d28e17f72" {
		t.Errorf("sha256 = %s, md5 = %s", d.SHA256, d.MD5)
	}
	if gotUA.Load() != "test" {
		t.Errorf("User-Agent = %v", gotUA.Load())
	}

	d, err = f.Download(context.Background(), srv.URL, Validators{ETag: d.ETag, LastModified: d.LastModified})
	if err != nil {
		t.Fatal(err)
	}
	if !d.NotModified || d.ETag != `"v1"` || d.Status != 304 || d.Body != nil {
		t.Errorf("segundo download deveria ser 304: %+v", d)
	}
}

func TestDownloadIfModifiedSince(t *testing.T) {
	const lm = "Tue, 29 Sep 2026 18:37:00 GMT"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-Modified-Since") == lm {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()
	d, err := newFetcher().Download(context.Background(), srv.URL, Validators{LastModified: lm})
	if err != nil || !d.NotModified || d.LastModified != lm {
		t.Fatalf("d = %+v, err = %v", d, err)
	}
}

func TestIfNoneMatch(t *testing.T) {
	cases := map[string]string{
		`"225757-65ca377d2eb00-gzip"`:   `"225757-65ca377d2eb00-gzip", "225757-65ca377d2eb00"`,
		`W/"225757-65ca377d2eb00-gzip"`: `W/"225757-65ca377d2eb00-gzip", W/"225757-65ca377d2eb00"`,
		`"225757-65ca377d2eb00"`:        `"225757-65ca377d2eb00"`,
		`W/"x"`:                         `W/"x"`,
		`-gzip"`:                        `-gzip"`,
	}
	for in, want := range cases {
		if got := IfNoneMatch(in); got != want {
			t.Errorf("IfNoneMatch(%s) = %s, quero %s", in, got, want)
		}
	}
}

// Imita o Apache da InterNIC: com gzip o ETag ganha "-gzip", e o servidor só
// reconhece no If-None-Match o ETag sem o sufixo. O corpo e os hashes são os
// do arquivo descomprimido, e a segunda requisição dá 304.
func TestDownloadApacheGzipETag(t *testing.T) {
	plain := []byte(strings.Repeat("br.\t\t\t172800\tIN\tNS\ta.dns.br.\n", 100))
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write(plain)
	_ = zw.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			t.Errorf("cliente deveria aceitar gzip")
		}
		for tag := range strings.SplitSeq(r.Header.Get("If-None-Match"), ",") {
			if strings.TrimSpace(tag) == `"225757-65ca377d2eb00"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("ETag", `"225757-65ca377d2eb00-gzip"`)
		_, _ = w.Write(gz.Bytes())
	}))
	defer srv.Close()

	f := newFetcher()
	d, err := f.Download(context.Background(), srv.URL, Validators{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(d.Body, plain) || d.ETag != `"225757-65ca377d2eb00-gzip"` {
		t.Fatalf("corpo descomprimido = %d bytes, etag %q", len(d.Body), d.ETag)
	}
	d, err = f.Download(context.Background(), srv.URL, Validators{ETag: d.ETag})
	if err != nil || !d.NotModified || d.ETag != `"225757-65ca377d2eb00-gzip"` {
		t.Fatalf("ETag com -gzip deveria dar 304: %+v, %v", d, err)
	}
}
func TestDownloadRetriesServerErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	d, err := newFetcher().Download(context.Background(), srv.URL, Validators{})
	if err != nil || string(d.Body) != "ok" {
		t.Fatalf("d = %+v, err = %v", d, err)
	}
	if calls.Load() != 3 {
		t.Errorf("tentativas = %d, quero 3", calls.Load())
	}
}

func TestDownloadGivesUpAfterRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if _, err := newFetcher().Download(context.Background(), srv.URL, Validators{}); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("tentativas = %d, quero 3 (1 + 2 retries)", calls.Load())
	}
}

// Corpo cortado no meio (Content-Length maior que o enviado) é erro de rede:
// tenta de novo.
func TestDownloadRetriesTruncatedBody(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte("cortado"))
			return
		}
		_, _ = w.Write([]byte("inteiro"))
	}))
	defer srv.Close()
	d, err := newFetcher().Download(context.Background(), srv.URL, Validators{})
	if err != nil || string(d.Body) != "inteiro" {
		t.Fatalf("d = %+v, err = %v", d, err)
	}
}

func TestDownloadDoesNotRetry404(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	if _, err := newFetcher().Download(context.Background(), srv.URL, Validators{}); err == nil {
		t.Fatal("esperava erro")
	}
	if calls.Load() != 1 {
		t.Errorf("404 não deve ser repetido; tentativas = %d", calls.Load())
	}
}

func TestDownloadMaxBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 100))
	}))
	defer srv.Close()
	f := newFetcher()
	f.MaxBytes = 10
	f.Retries = 0
	if _, err := f.Download(context.Background(), srv.URL, Validators{}); err == nil {
		t.Fatal("esperava erro de tamanho")
	}
}

func TestDownloadCanceledContextStopsRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	f := newFetcher()
	f.RetryDelay = time.Hour
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := f.Download(ctx, srv.URL, Validators{}); err == nil {
		t.Fatal("esperava erro")
	}
	if time.Since(start) > 5*time.Second {
		t.Error("o contexto deveria interromper a espera entre tentativas")
	}
}
