package fetch

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// hash é o conteúdo de testdata/named.root.md5, publicado pela InterNIC para o
// testdata/named.root.
const hash = "d0732825a760fee171258b4890ca5243"

func newFetcher() *Fetcher {
	return &Fetcher{Client: NewHTTPClient(), UserAgent: "test", Retries: 2, RetryDelay: time.Millisecond}
}

func TestParseMD5Formats(t *testing.T) {
	real, err := os.ReadFile("../../testdata/named.root.md5")
	if err != nil {
		t.Fatal(err)
	}
	good := map[string]string{
		"real (só o hash)":  string(real),
		"sem newline":       hash,
		"bsd":               "MD5 (named.root) = " + hash + "\n",
		"gnu":               hash + "  named.root\n",
		"gnu binário":       hash + " *named.root\n",
		"linha vazia antes": "\n" + hash + "\n",
		"maiúsculo":         strings.ToUpper(hash),
		"crlf":              hash + "\r\n",
	}
	for name, body := range good {
		if got, err := ParseMD5(body); err != nil || got != hash {
			t.Errorf("%s: hash = %q, err = %v", name, got, err)
		}
	}
	bad := map[string]string{
		"html":       "<html><body>" + hash + "</body></html>",
		"vazio":      "\n\n",
		"sha256":     "SHA256 (x) = 18f27fc4801c9a16337047cb2e18419a42623cfd15f53b73cb37c98f496e7730",
		"hash curto": hash[:31],
		"hash longo": hash + "0",
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

// Um .md5 acima de 4 KiB é erro de leitura (repetido) e não vira hash.
func TestPublishedMD5TooBig(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(hash + "\n" + strings.Repeat("x", 5000)))
	}))
	defer srv.Close()
	_, err := newFetcher().PublishedMD5(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "resposta maior que 4096 bytes") {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("tentativas = %d, quero 3", calls.Load())
	}
}

func TestDownloadConditional(t *testing.T) {
	body, err := os.ReadFile("../../testdata/named.root")
	if err != nil {
		t.Fatal(err)
	}
	var gotUA atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA.Store(r.Header.Get("User-Agent"))
		if r.Header.Get("If-None-Match") == `"cf3-65ca377d2eb00"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"cf3-65ca377d2eb00"`)
		w.Header().Set("Last-Modified", "Tue, 29 Sep 2026 18:37:00 GMT")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	f := newFetcher()
	d, err := f.Download(context.Background(), srv.URL, Validators{})
	if err != nil {
		t.Fatal(err)
	}
	if d.NotModified || len(d.Body) != 3315 || d.ETag != `"cf3-65ca377d2eb00"` || d.LastModified == "" || d.Status != 200 {
		t.Errorf("primeiro download = %+v", d)
	}
	if d.MD5 != hash || d.SHA256 != "18f27fc4801c9a16337047cb2e18419a42623cfd15f53b73cb37c98f496e7730" {
		t.Errorf("md5 = %s, sha256 = %s", d.MD5, d.SHA256)
	}
	if gotUA.Load() != "test" {
		t.Errorf("User-Agent = %v", gotUA.Load())
	}

	d, err = f.Download(context.Background(), srv.URL, Validators{ETag: d.ETag, LastModified: d.LastModified})
	if err != nil {
		t.Fatal(err)
	}
	if !d.NotModified || d.ETag != `"cf3-65ca377d2eb00"` || d.Status != 304 || d.Body != nil {
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
		`"cf3-65ca377d2eb00-gzip"`:   `"cf3-65ca377d2eb00-gzip", "cf3-65ca377d2eb00"`,
		`W/"cf3-65ca377d2eb00-gzip"`: `W/"cf3-65ca377d2eb00-gzip", W/"cf3-65ca377d2eb00"`,
		`"cf3-65ca377d2eb00"`:        `"cf3-65ca377d2eb00"`,
		`W/"x"`:                      `W/"x"`,
		`-gzip"`:                     `-gzip"`,
	}
	for in, want := range cases {
		if got := IfNoneMatch(in); got != want {
			t.Errorf("IfNoneMatch(%s) = %s, quero %s", in, got, want)
		}
	}
}

// Imita o Apache da InterNIC com gzip (medido no named.cache, cópia do
// named.root no mesmo servidor, em 2026-09-30): o ETag ganha "-gzip", e o
// servidor só reconhece no If-None-Match o ETag sem o sufixo — com o
// If-None-Match presente, o If-Modified-Since é ignorado. O corpo e os hashes
// são os do arquivo descomprimido, e a segunda requisição dá 304.
func TestDownloadApacheGzipETag(t *testing.T) {
	plain, err := os.ReadFile("../../testdata/named.root")
	if err != nil {
		t.Fatal(err)
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write(plain)
	_ = zw.Close()
	const lm = "Tue, 29 Sep 2026 18:37:00 GMT"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			t.Errorf("cliente deveria aceitar gzip")
		}
		if inm := r.Header.Get("If-None-Match"); inm != "" {
			for tag := range strings.SplitSeq(inm, ",") {
				if strings.TrimSpace(tag) == `"cf3-65ca377d2eb00"` {
					w.WriteHeader(http.StatusNotModified)
					return
				}
			}
		} else if r.Header.Get("If-Modified-Since") == lm {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		w.Header().Set("ETag", `"cf3-65ca377d2eb00-gzip"`)
		w.Header().Set("Last-Modified", lm)
		_, _ = w.Write(gz.Bytes())
	}))
	defer srv.Close()

	f := newFetcher()
	d, err := f.Download(context.Background(), srv.URL, Validators{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(d.Body, plain) || d.MD5 != hash || d.ETag != `"cf3-65ca377d2eb00-gzip"` {
		t.Fatalf("corpo descomprimido = %d bytes, md5 %s, etag %q", len(d.Body), d.MD5, d.ETag)
	}
	d, err = f.Download(context.Background(), srv.URL, Validators{ETag: d.ETag, LastModified: d.LastModified})
	if err != nil || !d.NotModified || d.ETag != `"cf3-65ca377d2eb00-gzip"` {
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
