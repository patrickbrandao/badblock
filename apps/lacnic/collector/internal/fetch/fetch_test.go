package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const hash = "1d11010d5e9cc31ce507817ac4fae1bf"

func newFetcher() *Fetcher {
	return &Fetcher{Client: http.DefaultClient, UserAgent: "test", Retries: 2, RetryDelay: time.Millisecond}
}

func TestParseMD5Formats(t *testing.T) {
	good := map[string]string{
		"bsd":             "MD5 (delegated-rir-extended-latest) = " + hash + "\n",
		"bsd sem newline": "MD5 (delegated-rir-extended-latest) = " + hash,
		"gnu":             hash + "  delegated-rir-extended-20260928\n",
		"gnu binário":     hash + " *delegated-rir-extended-latest\n",
		"só o hash":       "\n" + hash + "\n",
		"maiúsculo":       "MD5 (x) = 1D11010D5E9CC31CE507817AC4FAE1BF",
	}
	for name, body := range good {
		if got, err := ParseMD5(body); err != nil || got != hash {
			t.Errorf("%s: hash = %q, err = %v", name, got, err)
		}
	}
	bad := map[string]string{
		"html":       "<html><body>" + hash + "</body></html>",
		"vazio":      "\n\n",
		"sha256":     "SHA256 (x) = 0a474b20ea017ffeebfc069f378acf41872a682c423856afc94f6ecfa3bb65c2",
		"hash curto": "MD5 (x) = 1d11010d5e9cc31ce507817ac4fae1b",
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
		_, _ = w.Write([]byte("MD5 (delegated-rir-extended-latest) = " + hash + "\n"))
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

func TestDownloadConditional(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` || r.Header.Get("If-Modified-Since") == "Mon, 28 Sep 2026 02:56:19 GMT" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Mon, 28 Sep 2026 02:56:19 GMT")
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
	if d.MD5 != "900150983cd24fb0d6963f7d28e17f72" {
		t.Errorf("md5 = %s", d.MD5)
	}
	if d.SHA256 != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("sha256 = %s", d.SHA256)
	}

	for _, prev := range []Validators{{ETag: d.ETag}, {LastModified: d.LastModified}} {
		d2, err := f.Download(context.Background(), srv.URL, prev)
		if err != nil {
			t.Fatal(err)
		}
		if !d2.NotModified || d2.Status != 304 || d2.Body != nil {
			t.Errorf("com %+v deveria ser 304: %+v", prev, d2)
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
	if _, err := newFetcher().Download(context.Background(), srv.URL, Validators{}); err == nil {
		t.Fatal("esperava erro")
	}
	if calls.Load() != 3 {
		t.Errorf("tentativas = %d, quero 1 + 2 retries", calls.Load())
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
