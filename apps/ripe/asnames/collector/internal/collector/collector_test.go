package collector

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/patrickbrandao/badblock/apps/ripe/asnames/collector/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/ripe/asnames/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/ripe/asnames/collector/internal/store"
)

type fakeStore struct {
	last     *store.Applied
	lastErr  error
	touched  int
	touchErr error
	applied  []store.Run
	opts     []store.ApplyOptions
	failures []string
	failRuns []store.Run
	applyErr error
}

func (f *fakeStore) LastApplied(context.Context) (*store.Applied, error) { return f.last, f.lastErr }
func (f *fakeStore) TouchCheck(context.Context) error {
	f.touched++
	return f.touchErr
}
func (f *fakeStore) Apply(_ context.Context, ds *parse.Dataset, run store.Run, opt store.ApplyOptions) (store.Changes, string, error) {
	if f.applyErr != nil {
		return store.Changes{}, "", f.applyErr
	}
	f.applied = append(f.applied, run)
	f.opts = append(f.opts, opt)
	f.last = &store.Applied{Version: "v" + run.SHA256[:4], URL: run.URL, ETag: run.ETag, SHA256: run.SHA256}
	return store.Changes{ASNInserted: len(ds.ASNs)}, f.last.Version, nil
}
func (f *fakeStore) RecordFailure(_ context.Context, run store.Run, cause error) error {
	f.failures = append(f.failures, cause.Error())
	f.failRuns = append(f.failRuns, run)
	return nil
}

type fakeSource struct {
	body        []byte
	err         error
	downloads   int
	lastPrev    fetch.Validators
	notModified bool
}

func (f *fakeSource) Download(_ context.Context, url string, prev fetch.Validators) (*fetch.Download, error) {
	f.downloads++
	f.lastPrev = prev
	if f.err != nil {
		return nil, f.err
	}
	if f.notModified {
		return &fetch.Download{URL: url, Status: 304, NotModified: true, ETag: prev.ETag}, nil
	}
	sum := sha256.Sum256(f.body)
	return &fetch.Download{URL: url, Status: 200, Body: f.body, SHA256: hex.EncodeToString(sum[:]),
		ETag: `W/"e1"`, LastModified: "Mon, 28 Sep 2026 10:49:00 GMT"}, nil
}

func sample(t *testing.T) ([]byte, string) {
	t.Helper()
	b, err := os.ReadFile("../../testdata/asn-sample.txt")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:])
}

const srcURL = "https://x/asn.txt"

func newCollector(st Store, src Source) *Collector {
	return New(st, src, Options{URL: srcURL, MinASNs: 5, RemovalThreshold: 0.05},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestFirstRunApplies(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{}
	src := &fakeSource{body: body}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Applied || len(st.applied) != 1 || res.Version != "v"+sum[:4] {
		t.Fatalf("res = %+v", res)
	}
	if src.lastPrev != (fetch.Validators{}) {
		t.Errorf("sem execução anterior não há validadores: %+v", src.lastPrev)
	}
	r := st.applied[0]
	if r.ASNs != 37 || r.SHA256 != sum || r.ETag != `W/"e1"` || r.HTTPStatus != 200 || r.Bytes != int64(len(body)) || r.Forced {
		t.Errorf("run = %+v", r)
	}
	if st.opts[0].Force || st.opts[0].RemovalThreshold != 0.05 {
		t.Errorf("opções = %+v", st.opts[0])
	}
	if st.touched != 0 {
		t.Error("arquivo aplicado não chama TouchCheck (Apply já atualiza jobs)")
	}
}

func TestNotModifiedSendsValidators(t *testing.T) {
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: srcURL, SHA256: "old", ETag: `W/"e0"`, LastModified: "lm"}}
	src := &fakeSource{notModified: true}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || res.Reason != "HTTP 304" || res.Version != "v1" || st.touched != 1 {
		t.Errorf("res = %+v, touched = %d", res, st.touched)
	}
	if src.lastPrev.ETag != `W/"e0"` || src.lastPrev.LastModified != "lm" {
		t.Errorf("validadores = %+v", src.lastPrev)
	}
	if len(st.failures) != 0 || len(st.applied) != 0 {
		t.Error("304 não grava nada em ripe_asnames_run")
	}
}

func TestOtherURLDoesNotSendValidators(t *testing.T) {
	body, _ := sample(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: "https://outro/asn.txt", SHA256: "old", ETag: `"e0"`}}
	src := &fakeSource{body: body}
	if _, err := newCollector(st, src).RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if src.lastPrev != (fetch.Validators{}) {
		t.Errorf("validadores de outra URL não valem: %+v", src.lastPrev)
	}
}

func TestSameContentIsUnchanged(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: srcURL, SHA256: sum}}
	src := &fakeSource{body: body}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || res.Reason != "conteúdo igual ao último aplicado" || len(st.applied) != 0 || st.touched != 1 {
		t.Errorf("res = %+v", res)
	}
}

func TestForceAppliesSameFile(t *testing.T) {
	body, sum := sample(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: srcURL, SHA256: sum, ETag: `W/"e1"`}}
	src := &fakeSource{body: body}
	res, err := newCollector(st, src).RunOnce(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Applied || src.lastPrev != (fetch.Validators{}) || !st.applied[0].Forced || !st.opts[0].Force {
		t.Errorf("res = %+v, prev = %+v, opts = %+v", res, src.lastPrev, st.opts)
	}
}

func TestDownloadErrorIsNotRecorded(t *testing.T) {
	st := &fakeStore{}
	src := &fakeSource{err: errors.New("HTTP 503")}
	_, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "download") {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 0 || st.touched != 0 {
		t.Errorf("falha antes do download só vai para o log: failures = %v", st.failures)
	}
}

func TestLastAppliedError(t *testing.T) {
	st := &fakeStore{lastErr: errors.New("banco fora")}
	src := &fakeSource{}
	if _, err := newCollector(st, src).RunOnce(context.Background(), false); err == nil {
		t.Fatal("esperava erro")
	}
	if src.downloads != 0 {
		t.Error("sem banco não baixa")
	}
}

func TestTouchCheckError(t *testing.T) {
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: srcURL, ETag: "e"}, touchErr: errors.New("banco fora")}
	src := &fakeSource{notModified: true}
	if _, err := newCollector(st, src).RunOnce(context.Background(), false); err == nil || !strings.Contains(err.Error(), "jobs") {
		t.Fatalf("err = %v", err)
	}
}

func TestParserErrorIsRecorded(t *testing.T) {
	st := &fakeStore{}
	src := &fakeSource{body: []byte("<html>manutenção</html>\n")}
	_, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "parser") {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 || len(st.applied) != 0 {
		t.Fatalf("failures = %v", st.failures)
	}
	if r := st.failRuns[0]; r.SHA256 == "" || r.HTTPStatus != 200 || r.ASNs != 0 {
		t.Errorf("a falha registra o download, sem contagens: %+v", r)
	}
}

func TestTooFewASNsIsRecorded(t *testing.T) {
	st := &fakeStore{}
	src := &fakeSource{body: []byte("1 LVLT-1 - Level 3 Parent, LLC, US\n")}
	if _, err := newCollector(st, src).RunOnce(context.Background(), false); err == nil || !strings.Contains(err.Error(), "truncado") {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 || st.failRuns[0].ASNs != 1 {
		t.Errorf("failures = %v, runs = %+v", st.failures, st.failRuns)
	}
}

func TestApplyErrorIsRecorded(t *testing.T) {
	body, _ := sample(t)
	st := &fakeStore{applyErr: &store.RemovalError{Removed: 50, Current: 100, Threshold: 0.05}}
	src := &fakeSource{body: body}
	_, err := newCollector(st, src).RunOnce(context.Background(), false)
	if _, ok := errors.AsType[*store.RemovalError](err); !ok {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 || !strings.Contains(st.failures[0], "--force") {
		t.Errorf("failures = %v", st.failures)
	}
}

func TestBusyIsNotRecorded(t *testing.T) {
	body, _ := sample(t)
	st := &fakeStore{applyErr: store.ErrBusy}
	src := &fakeSource{body: body}
	if _, err := newCollector(st, src).RunOnce(context.Background(), false); !errors.Is(err, store.ErrBusy) {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 0 {
		t.Errorf("concorrência não é falha do arquivo: %v", st.failures)
	}
}

func TestWarningsAreCapped(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 10000; i++ {
		b.WriteString(strconv.Itoa(i) + " H - N, US\n")
	}
	for range parse.MaxWarnings + 10 {
		b.WriteString("x ruim\n") // 60 de 10060 linhas: abaixo de 1%
	}
	st := &fakeStore{}
	src := &fakeSource{body: []byte(b.String())}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	w := res.Run.Warnings
	if len(w) != parse.MaxWarnings+1 || w[len(w)-1] != "... e mais 10 avisos" {
		t.Errorf("avisos = %d, último %q", len(w), w[len(w)-1])
	}
}
