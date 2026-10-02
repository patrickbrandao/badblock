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

	"github.com/patrickbrandao/badblock/apps/cgibr/collector/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/cgibr/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/cgibr/collector/internal/store"
)

type fakeStore struct {
	last     *store.Applied
	touched  int
	applied  []store.Run
	failures []string
	applyErr error
}

func (f *fakeStore) LastApplied(context.Context) (*store.Applied, error) { return f.last, nil }
func (f *fakeStore) TouchCheck(context.Context) error                    { f.touched++; return nil }
func (f *fakeStore) Apply(_ context.Context, ds *parse.Dataset, run store.Run, _ store.ApplyOptions) (store.Changes, string, error) {
	if f.applyErr != nil {
		return store.Changes{}, "", f.applyErr
	}
	f.applied = append(f.applied, run)
	f.last = &store.Applied{Version: "v" + run.SHA256[:4], URL: run.URL, ETag: run.ETag, SHA256: run.SHA256}
	return store.Changes{ASNInserted: len(ds.ASNs)}, f.last.Version, nil
}
func (f *fakeStore) RecordFailure(_ context.Context, _ store.Run, cause error) error {
	f.failures = append(f.failures, cause.Error())
	return nil
}

type fakeSource struct {
	body        []byte
	published   string
	shaErr      error
	downloads   int
	lastPrev    fetch.Validators
	notModified bool
}

func (f *fakeSource) PublishedSHA256(context.Context, string) (string, error) {
	return f.published, f.shaErr
}

func (f *fakeSource) Download(_ context.Context, url string, prev fetch.Validators) (*fetch.Download, error) {
	f.downloads++
	f.lastPrev = prev
	if f.notModified {
		return &fetch.Download{URL: url, Status: 304, NotModified: true}, nil
	}
	sum := sha256.Sum256(f.body)
	return &fetch.Download{URL: url, Status: 200, Body: f.body, SHA256: hex.EncodeToString(sum[:]), ETag: `"e1"`}, nil
}

func sample(t *testing.T) ([]byte, string) {
	t.Helper()
	b, err := os.ReadFile("../../testdata/nicbr-asn-blk-sample.txt")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:])
}

func newCollector(st Store, src Source) *Collector {
	return New(st, src, Options{URL: "https://x/f.txt", SHA256URL: "https://x/f.txt.sha256", MinASNs: 5, RemovalThreshold: 0.05},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestFirstRunApplies(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{}
	src := &fakeSource{body: body, published: sum}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Applied || len(st.applied) != 1 {
		t.Fatalf("res = %+v", res)
	}
	if r := st.applied[0]; r.ASNs != 11 || r.PrefixesV4 != 52 || r.PrefixesV6 != 7 || r.SHA256 != sum || r.ETag != `"e1"` {
		t.Errorf("run = %+v", r)
	}
}

func TestSameSHA256SkipsDownload(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: "https://x/f.txt", SHA256: sum}}
	src := &fakeSource{body: body, published: sum}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || src.downloads != 0 || st.touched != 1 || res.Version != "v1" {
		t.Errorf("res = %+v, downloads = %d, touched = %d", res, src.downloads, st.touched)
	}
}

func TestNotModifiedSendsValidators(t *testing.T) {
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: "https://x/f.txt", SHA256: "old", ETag: `"e0"`}}
	src := &fakeSource{shaErr: errors.New("fora do ar"), notModified: true}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || src.lastPrev.ETag != `"e0"` || st.touched != 1 {
		t.Errorf("res = %+v, prev = %+v", res, src.lastPrev)
	}
}

func TestSameContentIsUnchanged(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: "https://x/f.txt", SHA256: sum}}
	src := &fakeSource{body: body, shaErr: errors.New("fora do ar")}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || len(st.applied) != 0 {
		t.Errorf("res = %+v", res)
	}
}

func TestForceAppliesSameFile(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: "https://x/f.txt", SHA256: sum, ETag: `"e1"`}}
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
	if len(st.failures) != 1 || len(st.applied) != 0 {
		t.Errorf("failures = %v", st.failures)
	}
}

func TestTooFewASNsIsRecorded(t *testing.T) {
	st := &fakeStore{}
	src := &fakeSource{body: []byte("AS1|x|y|192.0.2.0/24\n")}
	c := newCollector(st, src)
	c.opt.SHA256URL = ""
	if _, err := c.RunOnce(context.Background(), false); err == nil || !strings.Contains(err.Error(), "truncado") {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 {
		t.Errorf("failures = %v", st.failures)
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
