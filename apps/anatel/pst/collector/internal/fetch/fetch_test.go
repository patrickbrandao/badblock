package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newFetcher() *Fetcher {
	return &Fetcher{Client: http.DefaultClient, UserAgent: "test", Retries: 2, RetryDelay: time.Millisecond}
}

func TestDownloadConditional(t *testing.T) {
	const etag = `"3714464ca50dd1:1a98"`
	const lm = "Wed, 30 Sep 2026 10:56:48 GMT"
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		// A Anatel responde 304 a qualquer um dos dois validadores.
		if r.Header.Get("If-None-Match") == etag || r.Header.Get("If-Modified-Since") == lm {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", lm)
		w.Header().Set("Content-Type", "application/x-zip-compressed")
		_, _ = w.Write([]byte("abc"))
	}))
	defer srv.Close()

	f := newFetcher()
	d, err := f.Download(context.Background(), srv.URL, Validators{})
	if err != nil {
		t.Fatal(err)
	}
	if d.NotModified || d.Status != 200 || string(d.Body) != "abc" || d.ETag != etag || d.LastModified != lm || gotUA != "test" {
		t.Errorf("primeiro download = %+v", d)
	}
	if d.SHA256 != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("sha256 = %s", d.SHA256)
	}

	for _, prev := range []Validators{{ETag: etag}, {LastModified: lm}} {
		d, err = f.Download(context.Background(), srv.URL, prev)
		if err != nil {
			t.Fatal(err)
		}
		if !d.NotModified || d.Status != 304 || d.Body != nil {
			t.Errorf("com %+v deveria ser 304: %+v", prev, d)
		}
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
	_, err := newFetcher().Download(context.Background(), srv.URL, Validators{})
	if err == nil || err.Error() != "HTTP 503" {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("tentativas = %d, quero 3", calls.Load())
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
	if _, err := f.Download(context.Background(), srv.URL, Validators{}); err == nil || err.Error() != "resposta maior que 10 bytes" {
		t.Fatalf("err = %v", err)
	}
}
