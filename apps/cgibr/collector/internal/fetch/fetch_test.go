package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const hash = "0a474b20ea017ffeebfc069f378acf41872a682c423856afc94f6ecfa3bb65c2"

func newFetcher() *Fetcher {
	return &Fetcher{Client: http.DefaultClient, UserAgent: "test", Retries: 2, RetryDelay: time.Millisecond}
}

func TestPublishedSHA256Formats(t *testing.T) {
	bodies := map[string]string{
		"bsd":       "SHA256 (nicbr-asn-blk-latest.txt) = " + hash + "\n",
		"gnu":       hash + "  nicbr-asn-blk-latest.txt\n",
		"maiúsculo": "SHA256 (x) = 0A474B20EA017FFEEBFC069F378ACF41872A682C423856AFC94F6ECFA3BB65C2",
	}
	for name, body := range bodies {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		got, err := newFetcher().PublishedSHA256(context.Background(), srv.URL)
		srv.Close()
		if err != nil || got != hash {
			t.Errorf("%s: hash = %q, err = %v", name, got, err)
		}
	}
}

func TestPublishedSHA256Garbage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>not found</html>"))
	}))
	defer srv.Close()
	if _, err := newFetcher().PublishedSHA256(context.Background(), srv.URL); err == nil {
		t.Error("esperava erro para conteúdo sem hash")
	}
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
