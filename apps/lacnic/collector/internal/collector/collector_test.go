package collector

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/lacnic/collector/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/lacnic/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/lacnic/collector/internal/rir"
	"github.com/patrickbrandao/badblock/apps/lacnic/collector/internal/store"
)

const url = "https://x/delegated"

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
	f.last = &store.Applied{Version: "v" + run.SHA256[:4], URL: run.URL, ETag: run.ETag, MD5: run.MD5, SHA256: run.SHA256,
		Serial: run.Header.Serial, EndDate: run.Header.EndDate}
	return store.Changes{ASNInserted: len(ds.ASNs)}, f.last.Version, nil
}
func (f *fakeStore) RecordFailure(_ context.Context, _ store.Run, cause error) error {
	f.failures = append(f.failures, cause.Error())
	return nil
}

type fakeSource struct {
	body        []byte
	published   string
	md5Err      error
	md5Calls    int
	downloads   int
	lastPrev    fetch.Validators
	notModified bool
}

func (f *fakeSource) PublishedMD5(context.Context, string) (string, error) {
	f.md5Calls++
	return f.published, f.md5Err
}

func (f *fakeSource) Download(_ context.Context, url string, prev fetch.Validators) (*fetch.Download, error) {
	f.downloads++
	f.lastPrev = prev
	if f.notModified {
		return &fetch.Download{URL: url, Status: 304, NotModified: true}, nil
	}
	m, s := sums(f.body)
	return &fetch.Download{URL: url, Status: 200, Body: f.body, MD5: m, SHA256: s, ETag: `"e1"`}, nil
}

func sums(b []byte) (string, string) {
	m := md5.Sum(b)
	s := sha256.Sum256(b)
	return hex.EncodeToString(m[:]), hex.EncodeToString(s[:])
}

// sample devolve a fixture real (serial 20260927, enddate 20260925) e o MD5 dela.
func sample(t *testing.T) ([]byte, string) {
	t.Helper()
	b, err := os.ReadFile("../../testdata/delegated-extended-sample.txt")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := sums(b)
	return b, m
}

func newCollector(st Store, src Source) *Collector {
	return New(st, src, Options{URL: url, MD5URL: url + ".md5", Registry: rir.Registry, MinRecords: 5, RemovalThreshold: 0.05},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func day(s string) time.Time {
	t, _ := time.Parse("20060102", s)
	return t
}

func TestFirstRunApplies(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{}
	src := &fakeSource{body: body, published: sum}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Applied || len(st.applied) != 1 || len(st.failures) != 0 {
		t.Fatalf("res = %+v", res)
	}
	r := st.applied[0]
	if !r.Parsed || r.ASNRecords != 7 || r.IPv4Records != 8 || r.IPv6Records != 6 || r.PrefixesV4 != 8 || r.PrefixesV6 != 6 {
		t.Errorf("contagens = %+v", r)
	}
	if r.MD5 != sum || r.ETag != `"e1"` || r.Header.Serial != "20260927" || r.HTTPStatus != 200 || r.Bytes != int64(len(body)) {
		t.Errorf("run = %+v", r)
	}
}

func TestSameMD5SkipsDownload(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: url, MD5: sum}}
	src := &fakeSource{body: body, published: sum}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || src.downloads != 0 || st.touched != 1 || res.Version != "v1" || !strings.Contains(res.Reason, "md5") {
		t.Errorf("res = %+v, downloads = %d, touched = %d", res, src.downloads, st.touched)
	}
}

func TestNotModifiedSendsValidators(t *testing.T) {
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: url, MD5: "old", ETag: `"e0"`, LastModified: "ontem"}}
	src := &fakeSource{md5Err: errors.New("fora do ar"), notModified: true}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || res.Reason != "HTTP 304" || src.lastPrev.ETag != `"e0"` || src.lastPrev.LastModified != "ontem" || st.touched != 1 {
		t.Errorf("res = %+v, prev = %+v", res, src.lastPrev)
	}
}

func TestValidatorsOnlyForSameURL(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: "https://espelho/delegated", MD5: "old", ETag: `"e0"`}}
	src := &fakeSource{body: body, published: sum}
	if _, err := newCollector(st, src).RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if src.lastPrev != (fetch.Validators{}) {
		t.Errorf("validadores de outra URL foram enviados: %+v", src.lastPrev)
	}
}

func TestSameContentIsUnchanged(t *testing.T) {
	body, _ := sample(t)
	_, sha := sums(body)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: url, SHA256: sha}}
	src := &fakeSource{body: body, md5Err: errors.New("fora do ar")}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || len(st.applied) != 0 || !strings.Contains(res.Reason, "conteúdo igual") {
		t.Errorf("res = %+v", res)
	}
}

func TestMD5Off(t *testing.T) {
	body, _ := sample(t)
	st := &fakeStore{}
	src := &fakeSource{body: body, published: strings.Repeat("0", 32)}
	c := newCollector(st, src)
	c.opt.MD5URL = ""
	res, err := c.RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Applied || src.md5Calls != 0 {
		t.Errorf("res = %+v, md5Calls = %d", res, src.md5Calls)
	}
}

func TestForceAppliesSameFile(t *testing.T) {
	body, sum := sample(t)
	_, sha := sums(body)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: url, MD5: sum, SHA256: sha, ETag: `"e1"`}}
	src := &fakeSource{body: body, published: sum}
	res, err := newCollector(st, src).RunOnce(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Applied || src.lastPrev.ETag != "" || !st.applied[0].Forced {
		t.Errorf("res = %+v, prev = %+v", res, src.lastPrev)
	}
}

func TestMD5MismatchIsRecorded(t *testing.T) {
	body, _ := sample(t)
	st := &fakeStore{}
	src := &fakeSource{body: body, published: strings.Repeat("0", 32)}
	_, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "md5 divergente") {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 || len(st.applied) != 0 {
		t.Errorf("failures = %v", st.failures)
	}
}

func TestParserErrorIsRecorded(t *testing.T) {
	body, _ := sample(t)
	st := &fakeStore{}
	src := &fakeSource{body: body}
	c := newCollector(st, src)
	c.opt.MD5URL, c.opt.Registry = "", "outro"
	if _, err := c.RunOnce(context.Background(), false); err == nil || !strings.Contains(err.Error(), "parser") {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 {
		t.Errorf("failures = %v", st.failures)
	}
}

func TestOlderFileIsIgnored(t *testing.T) {
	body, sum := sample(t) // serial 20260927, enddate 20260925
	cases := map[string]*store.Applied{
		"enddate maior":         {Serial: "20260920", EndDate: day("20260926")},
		"mesmo enddate, serial": {Serial: "20260928", EndDate: day("20260925")},
		"sem enddate, serial":   {Serial: "20260928"},
	}
	for name, last := range cases {
		last.Version, last.URL, last.MD5, last.SHA256 = "v1", url, "old", "old"
		st := &fakeStore{last: last}
		src := &fakeSource{body: body, published: sum}
		res, err := newCollector(st, src).RunOnce(context.Background(), false)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.Outcome != Unchanged || !strings.Contains(res.Reason, "mais antigo") || len(st.applied) != 0 ||
			len(st.failures) != 0 || st.touched != 1 || res.Version != "v1" {
			t.Errorf("%s: res = %+v, st = %+v", name, res, st)
		}

		// --force aplica mesmo assim.
		st = &fakeStore{last: last}
		res, err = newCollector(st, &fakeSource{body: body, published: sum}).RunOnce(context.Background(), true)
		if err != nil || res.Outcome != Applied {
			t.Errorf("%s com --force: res = %+v, err = %v", name, res, err)
		}
	}
}

func TestNewerOrIncomparableFileApplies(t *testing.T) {
	body, sum := sample(t)
	cases := map[string]*store.Applied{
		"enddate menor, serial maior": {Serial: "20260928", EndDate: day("20260924")},
		"mesmo cabeçalho":             {Serial: "20260927", EndDate: day("20260925")},
		"serial de outro formato":     {Serial: "1790600421096", EndDate: day("20260925")},
		"serial não numérico":         {Serial: "x", EndDate: day("20260925")},
	}
	for name, last := range cases {
		last.Version, last.URL, last.MD5, last.SHA256 = "v1", url, "old", "old"
		st := &fakeStore{last: last}
		res, err := newCollector(st, &fakeSource{body: body, published: sum}).RunOnce(context.Background(), false)
		if err != nil || res.Outcome != Applied {
			t.Errorf("%s: res = %+v, err = %v", name, res, err)
		}
	}
}

func TestTooFewRecordsIsRecorded(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{}
	c := newCollector(st, &fakeSource{body: body, published: sum})
	c.opt.MinRecords = 22
	if _, err := c.RunOnce(context.Background(), false); err == nil || !strings.Contains(err.Error(), "truncado") {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 || len(st.applied) != 0 {
		t.Errorf("failures = %v", st.failures)
	}
}

func TestApplyErrorIsRecorded(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{applyErr: &store.RemovalError{Entity: "blocos", Removed: 50, Current: 100, Threshold: 0.05}}
	if _, err := newCollector(st, &fakeSource{body: body, published: sum}).RunOnce(context.Background(), false); err == nil {
		t.Fatal("esperava erro")
	}
	if len(st.failures) != 1 || !strings.Contains(st.failures[0], "removeria") {
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

func TestSecondRunIsUnchanged(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{}
	src := &fakeSource{body: body, published: sum}
	c := newCollector(st, src)
	if _, err := c.RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	res, err := c.RunOnce(context.Background(), false)
	if err != nil || res.Outcome != Unchanged || src.downloads != 1 {
		t.Errorf("res = %+v, err = %v, downloads = %d", res, err, src.downloads)
	}
}
