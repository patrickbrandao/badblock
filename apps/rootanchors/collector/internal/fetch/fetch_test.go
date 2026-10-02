package fetch

import (
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

const hash = "3ccaab38830025ee0a0f6c1f25769427544f81ea2865aa860468f3ef5278b908"

func newFetcher() *Fetcher {
	return &Fetcher{Client: http.DefaultClient, UserAgent: "test", Retries: 2, RetryDelay: time.Millisecond}
}

func TestFindSHA256RealFile(t *testing.T) {
	b, err := os.ReadFile("../../testdata/checksums-sha256.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got := FindSHA256(string(b), "root-anchors.xml"); got != hash {
		t.Errorf("root-anchors.xml = %q", got)
	}
	// A primeira linha é de outro arquivo: pegar o primeiro hash seria errado.
	if got := FindSHA256(string(b), "icannbundle.pem"); got != "18ce7215812d1a2cad8d9d4d3d7c26f7235a9b5ec6f0c1e214e15230fd4f9e24" {
		t.Errorf("icannbundle.pem = %q", got)
	}
	if got := FindSHA256(string(b), "root-anchors.xml.bak"); got != "" {
		t.Errorf("arquivo ausente = %q", got)
	}
}

func TestFindSHA256Formats(t *testing.T) {
	upper := strings.ToUpper(hash)
	for name, body := range map[string]string{
		"gnu binário": hash + " *root-anchors.xml\n",
		"gnu com caminho e CRLF": "0000000000000000000000000000000000000000000000000000000000000000  outro.xml\r\n" +
			hash + "  ./root-anchors/root-anchors.xml\r\n",
		"bsd":       "SHA256 (root-anchors.xml) = " + hash,
		"maiúsculo": upper + "  root-anchors.xml",
	} {
		if got := FindSHA256(body, "root-anchors.xml"); got != hash {
			t.Errorf("%s: hash = %q", name, got)
		}
	}
	for name, body := range map[string]string{
		"html":           "<html>not found</html>",
		"hash curto":     "3ccaab38  root-anchors.xml",
		"hash sem nome":  hash,
		"nome no começo": "root-anchors.xml " + hash,
	} {
		if got := FindSHA256(body, "root-anchors.xml"); got != "" {
			t.Errorf("%s: hash = %q", name, got)
		}
	}
}

func TestPublishedSHA256(t *testing.T) {
	b, _ := os.ReadFile("../../testdata/checksums-sha256.txt")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/checksums-sha256.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(b)
	}))
	defer srv.Close()
	f := newFetcher()
	got, err := f.PublishedSHA256(context.Background(), srv.URL+"/checksums-sha256.txt", "root-anchors.xml")
	if err != nil || got != hash {
		t.Errorf("hash = %q, err = %v", got, err)
	}
	if _, err := f.PublishedSHA256(context.Background(), srv.URL+"/checksums-sha256.txt", "outro.xml"); err == nil ||
		!strings.Contains(err.Error(), "sem hash SHA-256 de outro.xml") {
		t.Errorf("err = %v", err)
	}
	if _, err := f.PublishedSHA256(context.Background(), srv.URL+"/x", "root-anchors.xml"); err == nil ||
		err.Error() != "HTTP 404" {
		t.Errorf("err = %v", err)
	}
}

func TestPublishedSHA256TooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("#\n", 3000)))
	}))
	defer srv.Close()
	f := newFetcher()
	f.Retries = 0
	if _, err := f.PublishedSHA256(context.Background(), srv.URL, "root-anchors.xml"); err == nil ||
		!strings.Contains(err.Error(), "resposta maior que 4096 bytes") {
		t.Errorf("err = %v", err)
	}
}

// O Cloudflare da IANA comprime com gzip e manda um ETag fraco; o transporte
// do Go descomprime sozinho, e o ETag fraco reenviado dá 304.
func TestDownloadGzipWeakETag(t *testing.T) {
	body, _ := os.ReadFile("../../testdata/root-anchors.xml")
	const etag = `W/"745-6262f56bfe940-br"`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			t.Errorf("Accept-Encoding = %q", r.Header.Get("Accept-Encoding"))
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Encoding", "gzip")
		zw := gzip.NewWriter(w)
		_, _ = zw.Write(body)
		_ = zw.Close()
	}))
	defer srv.Close()
	f := newFetcher()
	d, err := f.Download(context.Background(), srv.URL, Validators{})
	if err != nil {
		t.Fatal(err)
	}
	if d.SHA256 != hash || len(d.Body) != 1861 || d.ETag != etag {
		t.Errorf("download = %d bytes, %s, %s", len(d.Body), d.SHA256, d.ETag)
	}
	d, err = f.Download(context.Background(), srv.URL, Validators{ETag: etag})
	if err != nil || !d.NotModified {
		t.Errorf("d = %+v, err = %v", d, err)
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
