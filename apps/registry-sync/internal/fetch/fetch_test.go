package fetch

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/source"
)

const body = "2|test|20260928|0|19700101|20260928|+0000\n"

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func newFetcher() *Fetcher {
	return &Fetcher{Client: http.DefaultClient, UserAgent: "test", MaxBytes: 1 << 20, Retries: 1, RetryDelay: time.Millisecond}
}

func TestFetchConditionalAndMD5(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/file.md5":
			fmt.Fprintf(w, "MD5 (file) = %s\n", md5Hex(body))
		case "/file":
			if r.Header.Get("If-None-Match") == `"v1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"v1"`)
			w.Header().Set("Last-Modified", "Mon, 28 Sep 2026 00:00:00 GMT")
			fmt.Fprint(w, body)
		}
	}))
	defer srv.Close()

	src := source.Source{ID: "x", URLs: []string{srv.URL + "/file"}, MD5: true}
	f := newFetcher()

	res, err := f.Fetch(context.Background(), src, Previous{})
	if err != nil {
		t.Fatal(err)
	}
	if res.NotModified || string(res.Body) != body || res.ETag != `"v1"` {
		t.Fatalf("primeiro download = %+v", res)
	}

	prev := Previous{URL: res.URL, ETag: res.ETag, LastModified: res.LastModified, SHA256: res.SHA256}
	res2, err := f.Fetch(context.Background(), src, prev)
	if err != nil {
		t.Fatal(err)
	}
	if !res2.NotModified || res2.Status != http.StatusNotModified {
		t.Errorf("segundo download deveria ser 304: %+v", res2)
	}

	// Mesmo conteúdo sem validadores (outra URL): detectado pelo SHA-256.
	res3, err := f.Fetch(context.Background(), src, Previous{SHA256: res.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	if !res3.NotModified || res3.Status != http.StatusOK {
		t.Errorf("conteúdo idêntico deveria ser NotModified: %+v", res3)
	}
}

func TestFetchFallbackOnErrorAndBadMD5(t *testing.T) {
	var primaryHits atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits.Add(1)
		http.Error(w, "fora do ar", http.StatusBadGateway)
	}))
	defer primary.Close()

	corrupt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".md5") {
			fmt.Fprintf(w, "%s  file\n", md5Hex("outro conteúdo"))
			return
		}
		fmt.Fprint(w, body)
	}))
	defer corrupt.Close()

	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".md5") {
			fmt.Fprintf(w, "%s  file\n", md5Hex(body))
			return
		}
		fmt.Fprint(w, body)
	}))
	defer mirror.Close()

	src := source.Source{ID: "x", MD5: true, URLs: []string{primary.URL + "/file", corrupt.URL + "/file", mirror.URL + "/file"}}
	res, err := newFetcher().Fetch(context.Background(), src, Previous{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Fallback || res.URL != mirror.URL+"/file" {
		t.Errorf("deveria usar o espelho válido: %+v", res)
	}
	if len(res.Failures) != 2 {
		t.Errorf("esperadas 2 falhas registradas, vieram %v", res.Failures)
	}
	if primaryHits.Load() != 2 {
		t.Errorf("5xx deveria ter 1 nova tentativa (2 no total), houve %d", primaryHits.Load())
	}
}

func TestFetchAllFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	src := source.Source{ID: "x", URLs: []string{srv.URL + "/a", srv.URL + "/b"}}
	if _, err := newFetcher().Fetch(context.Background(), src, Previous{}); err == nil {
		t.Error("esperado erro quando todas as URLs falham")
	}
}

func TestFetchMaxBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", 2048))
	}))
	defer srv.Close()
	f := newFetcher()
	f.MaxBytes = 1024
	if _, err := f.Fetch(context.Background(), source.Source{ID: "x", URLs: []string{srv.URL}}, Previous{}); err == nil {
		t.Error("resposta acima do limite deveria falhar")
	}
}

func TestFetchFixtureDir(t *testing.T) {
	f := &Fetcher{SourcesDir: "../../testdata/sources"}
	lacnic := source.Source{ID: "rir-lacnic", MD5: true}
	res, err := f.Fetch(context.Background(), lacnic, Previous{})
	if err != nil {
		t.Fatal(err)
	}
	if res.NotModified || len(res.Body) == 0 || !strings.HasPrefix(res.URL, "file://") {
		t.Errorf("fixture = %+v", res)
	}
	again, err := f.Fetch(context.Background(), lacnic, Previous{SHA256: res.SHA256})
	if err != nil || !again.NotModified {
		t.Errorf("mesma fixture deveria ser NotModified: %+v %v", again, err)
	}
}

func TestVerifyMD5Formats(t *testing.T) {
	sum := md5Hex(body)
	for _, published := range []string{
		"MD5 (delegated-x-latest) = " + sum + "\n",
		sum + "  delegated-x-20260928\n",
		strings.ToUpper(sum),
	} {
		if err := verifyMD5([]byte(published), []byte(body)); err != nil {
			t.Errorf("%q: %v", published, err)
		}
	}
	if err := verifyMD5([]byte("sem hash"), []byte(body)); err == nil {
		t.Error("arquivo .md5 sem hash deveria falhar")
	}
}
