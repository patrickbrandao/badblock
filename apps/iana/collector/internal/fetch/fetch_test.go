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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Sat, 26 Sep 2026 03:06:50 GMT")
		_, _ = w.Write([]byte("abc"))
	}))
	defer srv.Close()

	f := newFetcher()
	d, err := f.Download(context.Background(), srv.URL, Validators{})
	if err != nil {
		t.Fatal(err)
	}
	if d.NotModified || string(d.Body) != "abc" || d.ETag != `"v1"` || d.LastModified == "" {
		t.Errorf("primeiro download = %+v", d)
	}
	if d.SHA256 != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("sha256 = %s", d.SHA256)
	}

	d, err = f.Download(context.Background(), srv.URL, Validators{ETag: d.ETag, LastModified: d.LastModified})
	if err != nil {
		t.Fatal(err)
	}
	if !d.NotModified || d.ETag != `"v1"` {
		t.Errorf("segundo download deveria ser 304: %+v", d)
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

// www.iana.org responde só Last-Modified (sem ETag): o condicional vai por
// If-Modified-Since e o 304 mantém os validadores anteriores.
func TestDownloadLastModifiedOnly(t *testing.T) {
	const lm = "Sat, 19 Sep 2026 00:44:44 GMT"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "" {
			t.Errorf("If-None-Match sem ETag anterior: %q", r.Header.Get("If-None-Match"))
		}
		if r.Header.Get("If-Modified-Since") == lm {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Last-Modified", lm)
		_, _ = w.Write([]byte("csv"))
	}))
	defer srv.Close()

	f := newFetcher()
	d, err := f.Download(context.Background(), srv.URL, Validators{})
	if err != nil || d.ETag != "" || d.LastModified != lm {
		t.Fatalf("d = %+v, err = %v", d, err)
	}
	d, err = f.Download(context.Background(), srv.URL, Validators{LastModified: d.LastModified})
	if err != nil || !d.NotModified || d.LastModified != lm || d.Body != nil {
		t.Fatalf("segundo download deveria ser 304: %+v, err = %v", d, err)
	}
}

func TestDownloadGivesUpAfterRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if _, err := newFetcher().Download(context.Background(), srv.URL, Validators{}); err == nil {
		t.Fatal("esperava erro")
	}
	if calls.Load() != 3 {
		t.Errorf("tentativas = %d, quero 3 (1 + Retries)", calls.Load())
	}
}
