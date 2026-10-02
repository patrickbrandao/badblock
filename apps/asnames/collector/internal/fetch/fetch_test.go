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

func newFetcher() *Fetcher {
	return &Fetcher{Client: NewHTTPClient(), UserAgent: "test", Retries: 2, RetryDelay: time.Millisecond}
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
		w.Header().Set("Last-Modified", "Mon, 28 Sep 2026 10:49:00 GMT")
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
	if d.SHA256 != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("sha256 = %s", d.SHA256)
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
	const lm = "Mon, 28 Sep 2026 10:49:00 GMT"
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

// O servidor do RIPE comprime com gzip e devolve um ETag fraco; o corpo e o
// hash têm que ser os do arquivo descomprimido e o ETag fraco volta no
// If-None-Match.
func TestDownloadGzipWeakETag(t *testing.T) {
	plain := []byte(strings.Repeat("15169 GOOGLE - Google LLC, US\n", 100))
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write(plain)
	_ = zw.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			t.Errorf("cliente deveria aceitar gzip")
		}
		if r.Header.Get("If-None-Match") == `W/"g1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("ETag", `W/"g1"`)
		_, _ = w.Write(gz.Bytes())
	}))
	defer srv.Close()

	f := newFetcher()
	d, err := f.Download(context.Background(), srv.URL, Validators{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(d.Body, plain) || d.ETag != `W/"g1"` {
		t.Fatalf("corpo descomprimido = %d bytes, etag %q", len(d.Body), d.ETag)
	}
	d, err = f.Download(context.Background(), srv.URL, Validators{ETag: d.ETag})
	if err != nil || !d.NotModified {
		t.Fatalf("ETag fraco deveria dar 304: %+v, %v", d, err)
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
