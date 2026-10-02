package collector

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/patrickbrandao/badblock/apps/rootanchors/collector/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/rootanchors/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/rootanchors/collector/internal/store"
)

type fakeStore struct {
	last       *store.Applied
	touched    int
	applied    []store.Run
	failures   []string
	failedRuns []store.Run
	applyErr   error
}

func (f *fakeStore) LastApplied(context.Context) (*store.Applied, error) { return f.last, nil }
func (f *fakeStore) TouchCheck(context.Context) error                    { f.touched++; return nil }
func (f *fakeStore) Apply(_ context.Context, ds *parse.Dataset, run store.Run, _ store.ApplyOptions) (store.Changes, string, error) {
	if f.applyErr != nil {
		return store.Changes{}, "", f.applyErr
	}
	f.applied = append(f.applied, run)
	f.last = &store.Applied{Version: "v" + run.SHA256[:4], URL: run.URL, ETag: run.ETag, SHA256: run.SHA256}
	return store.Changes{KeyInserted: len(ds.Keys)}, f.last.Version, nil
}
func (f *fakeStore) RecordFailure(_ context.Context, run store.Run, cause error) error {
	f.failures = append(f.failures, cause.Error())
	f.failedRuns = append(f.failedRuns, run)
	return nil
}

type fakeSource struct {
	body        []byte
	published   string
	shaErr      error
	shaName     string
	downloads   int
	lastPrev    fetch.Validators
	notModified bool
	dlErr       error
}

func (f *fakeSource) PublishedSHA256(_ context.Context, _, name string) (string, error) {
	f.shaName = name
	return f.published, f.shaErr
}

func (f *fakeSource) Download(_ context.Context, url string, prev fetch.Validators) (*fetch.Download, error) {
	f.downloads++
	f.lastPrev = prev
	if f.dlErr != nil {
		return nil, f.dlErr
	}
	if f.notModified {
		return &fetch.Download{URL: url, Status: 304, NotModified: true}, nil
	}
	sum := sha256.Sum256(f.body)
	return &fetch.Download{URL: url, Status: 200, Body: f.body, SHA256: hex.EncodeToString(sum[:]), ETag: `W/"e1"`}, nil
}

func fixture(t *testing.T, name string) ([]byte, string) {
	t.Helper()
	b, err := os.ReadFile("../../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:])
}

func sample(t *testing.T) ([]byte, string) { return fixture(t, "root-anchors.xml") }

const srcURL = "https://x/root-anchors/root-anchors.xml"

func newCollector(st Store, src Source) *Collector {
	return New(st, src, Options{URL: srcURL, SHA256URL: "https://x/root-anchors/checksums-sha256.txt",
		SHA256Name: "root-anchors.xml", MinKeys: 1, RemovalThreshold: 0.05},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestFirstRunApplies(t *testing.T) {
	body, sum := sample(t)
	if sum != "3ccaab38830025ee0a0f6c1f25769427544f81ea2865aa860468f3ef5278b908" {
		t.Fatalf("fixture mudou: %s", sum)
	}
	st := &fakeStore{}
	src := &fakeSource{body: body, published: sum}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Applied || len(st.applied) != 1 || src.shaName != "root-anchors.xml" || src.lastPrev != (fetch.Validators{}) {
		t.Fatalf("res = %+v, name = %q, prev = %+v", res, src.shaName, src.lastPrev)
	}
	r := st.applied[0]
	if !r.Parsed || r.Keys != 3 || r.AnchorID != "0C05FDD6-422C-4910-8ED6-430ED15E11C2" || r.Zone != "." ||
		r.AnchorSource != "http://data.iana.org/root-anchors/root-anchors.xml" ||
		r.SHA256 != sum || r.Bytes != 1861 || r.ETag != `W/"e1"` || len(r.Warnings) != 0 {
		t.Errorf("run = %+v", r)
	}
	if res.Changes.KeyInserted != 3 {
		t.Errorf("changes = %+v", res.Changes)
	}
}

func TestSameSHA256SkipsDownload(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: srcURL, SHA256: sum}}
	src := &fakeSource{body: body, published: sum}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || res.Reason != "sha256 publicado igual ao último aplicado" ||
		src.downloads != 0 || st.touched != 1 || res.Version != "v1" {
		t.Errorf("res = %+v, downloads = %d, touched = %d", res, src.downloads, st.touched)
	}
}

func TestNotModifiedSendsValidators(t *testing.T) {
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: srcURL, SHA256: "old", ETag: `W/"e0"`, LastModified: "Tue, 05 Nov 2024 19:23:41 GMT"}}
	src := &fakeSource{shaErr: errors.New("fora do ar"), notModified: true}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || res.Reason != "HTTP 304" || src.lastPrev.ETag != `W/"e0"` ||
		src.lastPrev.LastModified == "" || st.touched != 1 {
		t.Errorf("res = %+v, prev = %+v", res, src.lastPrev)
	}
}

func TestOtherURLSendsNoValidators(t *testing.T) {
	body, _ := sample(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: "https://espelho/root-anchors.xml", SHA256: "old", ETag: `"e0"`}}
	src := &fakeSource{body: body, shaErr: errors.New("fora do ar")}
	if _, err := newCollector(st, src).RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if src.lastPrev != (fetch.Validators{}) {
		t.Errorf("validadores de outra URL: %+v", src.lastPrev)
	}
}

func TestSameContentIsUnchanged(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: srcURL, SHA256: sum}}
	src := &fakeSource{body: body, shaErr: errors.New("fora do ar")}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || res.Reason != "conteúdo igual ao último aplicado" || len(st.applied) != 0 {
		t.Errorf("res = %+v", res)
	}
}

func TestForceAppliesSameFile(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: srcURL, SHA256: sum, ETag: `W/"e1"`}}
	src := &fakeSource{body: body, published: sum}
	res, err := newCollector(st, src).RunOnce(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Applied || src.lastPrev.ETag != "" || !st.applied[0].Forced {
		t.Errorf("res = %+v, prev = %+v", res, src.lastPrev)
	}
}

func TestSHA256MismatchIsRecorded(t *testing.T) {
	body, _ := sample(t)
	st := &fakeStore{}
	src := &fakeSource{body: body, published: strings.Repeat("0", 64)}
	_, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "sha256 divergente") {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 || len(st.applied) != 0 || st.failedRuns[0].Parsed || st.failedRuns[0].SHA256 == "" {
		t.Errorf("failures = %v, runs = %+v", st.failures, st.failedRuns)
	}
}

func TestParserRefusalIsRecorded(t *testing.T) {
	for file, want := range map[string]string{
		"root-anchors-bad-digest.xml": "parser: KeyDigest Klajeyz: digest não bate",
		"root-anchors-bad-keytag.xml": "parser: KeyDigest Klajeyz: key tag 20327 não bate",
		"root-anchors-bad-zone.xml":   `parser: zona "com."`,
		"root-anchors-truncated.xml":  "parser: XML: ",
	} {
		body, sum := fixture(t, file)
		st := &fakeStore{}
		src := &fakeSource{body: body, published: sum} // o hash confere: a recusa é do parser
		_, err := newCollector(st, src).RunOnce(context.Background(), false)
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("%s: err = %v", file, err)
			continue
		}
		if len(st.failures) != 1 || len(st.applied) != 0 || st.failedRuns[0].Parsed || st.failedRuns[0].Bytes != int64(len(body)) {
			t.Errorf("%s: failures = %v, runs = %+v", file, st.failures, st.failedRuns)
		}
	}
}

func TestTooFewKeysIsRecorded(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{}
	src := &fakeSource{body: body, published: sum}
	c := newCollector(st, src)
	c.opt.MinKeys = 4
	if _, err := c.RunOnce(context.Background(), false); err == nil || err.Error() != "só 3 chaves no arquivo (mínimo 4): arquivo truncado?" {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 || !st.failedRuns[0].Parsed || st.failedRuns[0].Keys != 3 {
		t.Errorf("failures = %v, runs = %+v", st.failures, st.failedRuns)
	}
}

func TestWarningsAreCapped(t *testing.T) {
	body, _ := sample(t)
	body = []byte(strings.Replace(string(body), "<Zone>.</Zone>", "<Zone>.</Zone>"+strings.Repeat("<X/>", 55), 1))
	st := &fakeStore{}
	c := newCollector(st, &fakeSource{body: body})
	c.opt.SHA256URL = ""
	if _, err := c.RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	w := st.applied[0].Warnings
	if len(w) != parse.MaxWarnings+1 || w[len(w)-1] != "... e mais 5 avisos" {
		t.Errorf("warnings = %d, último %q", len(w), w[len(w)-1])
	}
}

func TestBusyIsNotRecorded(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{applyErr: store.ErrBusy}
	src := &fakeSource{body: body, published: sum}
	if _, err := newCollector(st, src).RunOnce(context.Background(), false); !errors.Is(err, store.ErrBusy) {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 0 {
		t.Errorf("concorrência não é falha do arquivo: %v", st.failures)
	}
}

func TestRemovalIsRecorded(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{applyErr: &store.RemovalError{Removed: 1, Current: 3, Threshold: 0.05}}
	src := &fakeSource{body: body, published: sum}
	if _, err := newCollector(st, src).RunOnce(context.Background(), false); err == nil ||
		err.Error() != "o arquivo removeria 1 de 3 chaves (33.3%, limite 5.0%); use --force se for legítimo" {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 || !st.failedRuns[0].Parsed {
		t.Errorf("failures = %v", st.failures)
	}
}

func TestDownloadFailureIsNotRecorded(t *testing.T) {
	st := &fakeStore{}
	src := &fakeSource{dlErr: errors.New("HTTP 404")}
	if _, err := newCollector(st, src).RunOnce(context.Background(), false); err == nil || err.Error() != "download: HTTP 404" {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 0 || st.touched != 0 {
		t.Errorf("falha antes do download fica só no log: %v", st.failures)
	}
}
