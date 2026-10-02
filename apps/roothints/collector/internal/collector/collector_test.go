package collector

import (
	"bytes"
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

	"github.com/patrickbrandao/badblock/apps/roothints/collector/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/roothints/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/roothints/collector/internal/store"
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
	serial := ds.Header.ZoneSerial
	f.last = &store.Applied{Version: "v" + run.SHA256[:4], URL: run.URL, ETag: run.ETag, MD5: run.MD5,
		SHA256: run.SHA256, ZoneSerial: &serial}
	return store.Changes{ServerInserted: len(ds.Servers)}, f.last.Version, nil
}
func (f *fakeStore) RecordFailure(_ context.Context, run store.Run, cause error) error {
	f.failures = append(f.failures, cause.Error())
	f.failRuns = append(f.failRuns, run)
	return nil
}

type fakeSource struct {
	body        []byte
	published   string // "" = usa o MD5 do body
	md5Err      error
	md5Calls    int
	err         error
	downloads   int
	lastPrev    fetch.Validators
	notModified bool
}

func (f *fakeSource) PublishedMD5(context.Context, string) (string, error) {
	f.md5Calls++
	if f.md5Err != nil {
		return "", f.md5Err
	}
	if f.published != "" {
		return f.published, nil
	}
	return md5hex(f.body), nil
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
	return &fetch.Download{URL: url, Status: 200, Body: f.body, MD5: md5hex(f.body), SHA256: sha(f.body),
		ETag: `"cf3-65ca377d2eb00"`, LastModified: "Tue, 29 Sep 2026 18:37:00 GMT"}, nil
}

func md5hex(b []byte) string {
	s := md5.Sum(b)
	return hex.EncodeToString(s[:])
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func sample(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/named.root")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// withSerial troca o serial da zona raiz do cabeçalho da fixture.
func withSerial(t *testing.T, serial string) []byte {
	t.Helper()
	return []byte(strings.Replace(string(sample(t)), "2026092401", serial, 1))
}

const (
	srcURL = "https://x/named.root"
	md5URL = "https://x/named.root.md5"
)

func newCollector(st Store, src Source) *Collector {
	return New(st, src, Options{URL: srcURL, MD5URL: md5URL, MinServers: 13, RemovalThreshold: 0.05},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestFirstRunApplies(t *testing.T) {
	body := sample(t)
	st := &fakeStore{}
	src := &fakeSource{body: body}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Applied || len(st.applied) != 1 || res.Version != "v"+sha(body)[:4] {
		t.Fatalf("res = %+v", res)
	}
	if src.lastPrev != (fetch.Validators{}) {
		t.Errorf("sem execução anterior não há validadores: %+v", src.lastPrev)
	}
	r := st.applied[0]
	if !r.Parsed || r.Servers != 13 || r.IPv4Addresses != 13 || r.IPv6Addresses != 13 || r.Header.ZoneSerial != 2026092401 ||
		r.MD5 != "d0732825a760fee171258b4890ca5243" || r.SHA256 != sha(body) || r.HTTPStatus != 200 || r.Bytes != 3315 || r.Forced {
		t.Errorf("run = %+v", r)
	}
	if st.opts[0].Force || st.opts[0].RemovalThreshold != 0.05 {
		t.Errorf("opções = %+v", st.opts[0])
	}
	if st.touched != 0 {
		t.Error("arquivo aplicado não chama TouchCheck (Apply já atualiza jobs)")
	}
}

func TestSamePublishedMD5DoesNotDownload(t *testing.T) {
	body := sample(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: srcURL, MD5: md5hex(body), SHA256: sha(body)}}
	src := &fakeSource{body: body}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || res.Reason != "md5 publicado igual ao último aplicado" || res.Version != "v1" ||
		src.downloads != 0 || st.touched != 1 {
		t.Errorf("res = %+v, downloads = %d", res, src.downloads)
	}
}

func TestNotModifiedSendsValidators(t *testing.T) {
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: srcURL, MD5: "old", SHA256: "old", ETag: `"e0"`, LastModified: "lm"}}
	src := &fakeSource{notModified: true, published: "d0732825a760fee171258b4890ca5243"}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || res.Reason != "HTTP 304" || res.Version != "v1" || st.touched != 1 {
		t.Errorf("res = %+v, touched = %d", res, st.touched)
	}
	if src.lastPrev.ETag != `"e0"` || src.lastPrev.LastModified != "lm" {
		t.Errorf("validadores = %+v", src.lastPrev)
	}
	if len(st.failures) != 0 || len(st.applied) != 0 {
		t.Error("304 não grava nada em roothints_run")
	}
}

func TestOtherURLDoesNotSendValidators(t *testing.T) {
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: "https://outro/named.root", SHA256: "old", ETag: `"e0"`}}
	src := &fakeSource{body: sample(t)}
	if _, err := newCollector(st, src).RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if src.lastPrev != (fetch.Validators{}) {
		t.Errorf("validadores de outra URL não valem: %+v", src.lastPrev)
	}
}

// O Last-Modified da InterNIC muda sem o arquivo mudar; com o .md5 fora do ar,
// o SHA-256 decide.
func TestSameContentIsUnchanged(t *testing.T) {
	body := sample(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: srcURL, MD5: md5hex(body), SHA256: sha(body), ETag: `"velho"`}}
	src := &fakeSource{body: body, md5Err: errors.New("HTTP 503")}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || res.Reason != "conteúdo igual ao último aplicado" || len(st.applied) != 0 || st.touched != 1 {
		t.Errorf("res = %+v", res)
	}
}

func TestMD5Disabled(t *testing.T) {
	st := &fakeStore{}
	src := &fakeSource{body: sample(t), published: strings.Repeat("0", 32)}
	c := New(st, src, Options{URL: srcURL, MinServers: 13, RemovalThreshold: 0.05}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	res, err := c.RunOnce(context.Background(), false)
	if err != nil || res.Outcome != Applied {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if src.md5Calls != 0 {
		t.Error("com SOURCE_MD5_URL=off o .md5 não é pedido")
	}
}

func TestMD5UnavailableStillApplies(t *testing.T) {
	st := &fakeStore{}
	src := &fakeSource{body: sample(t), md5Err: errors.New("HTTP 404")}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil || res.Outcome != Applied {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestMD5MismatchIsRecorded(t *testing.T) {
	st := &fakeStore{}
	src := &fakeSource{body: sample(t), published: strings.Repeat("a", 32)}
	_, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "md5 divergente") {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 || len(st.applied) != 0 || st.failRuns[0].Parsed || st.failRuns[0].MD5 == "" {
		t.Errorf("failures = %v, runs = %+v", st.failures, st.failRuns)
	}
}

func TestForceAppliesSameFile(t *testing.T) {
	body := sample(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: srcURL, MD5: md5hex(body), SHA256: sha(body), ETag: `"e1"`,
		ZoneSerial: new(int64(2026092401))}}
	src := &fakeSource{body: body}
	res, err := newCollector(st, src).RunOnce(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Applied || src.lastPrev != (fetch.Validators{}) || !st.applied[0].Forced || !st.opts[0].Force {
		t.Errorf("res = %+v, prev = %+v, opts = %+v", res, src.lastPrev, st.opts)
	}
	if src.md5Calls != 1 {
		t.Error("--force ainda confere o .md5")
	}
}

// Serial menor que o aplicado (um cache no caminho com a cópia anterior): sem
// mudança, sem linha em roothints_run, com aviso no log; --force aplica.
func TestOlderSerialIsIgnored(t *testing.T) {
	body := withSerial(t, "2026092400")
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: srcURL, MD5: "old", SHA256: "old", ZoneSerial: new(int64(2026092401))}}
	src := &fakeSource{body: body}
	var logs bytes.Buffer
	c := New(st, src, Options{URL: srcURL, MD5URL: md5URL, MinServers: 13, RemovalThreshold: 0.05},
		slog.New(slog.NewTextHandler(&logs, nil)))
	res, err := c.RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || res.Reason != "arquivo mais antigo que o aplicado (serial 2026092400 < 2026092401)" ||
		res.Version != "v1" || len(st.applied) != 0 || len(st.failures) != 0 || st.touched != 1 {
		t.Fatalf("res = %+v, applied = %d, failures = %v, touched = %d", res, len(st.applied), st.failures, st.touched)
	}
	if l := logs.String(); !strings.Contains(l, "arquivo mais antigo que o aplicado; ignorado") ||
		!strings.Contains(l, "serial=2026092400") || !strings.Contains(l, "applied_serial=2026092401") {
		t.Errorf("log = %s", l)
	}

	// Com --force, aplica.
	res, err = newCollector(st, src).RunOnce(context.Background(), true)
	if err != nil || res.Outcome != Applied || len(st.failures) != 0 {
		t.Fatalf("--force: res = %+v, err = %v, failures = %v", res, err, st.failures)
	}
}

// A comparação é a da RFC 1982: perto da volta de 2³², 0 vem depois de
// 4294967295.
func TestOlderSerialAcrossWrapIsIgnored(t *testing.T) {
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: srcURL, MD5: "old", SHA256: "old", ZoneSerial: new(int64(5))}}
	res, err := newCollector(st, &fakeSource{body: withSerial(t, "4294967295")}).RunOnce(context.Background(), false)
	if err != nil || res.Outcome != Unchanged || len(st.failures) != 0 {
		t.Fatalf("4294967295 é anterior a 5: res = %+v, err = %v", res, err)
	}
	st = &fakeStore{last: &store.Applied{Version: "v1", URL: srcURL, MD5: "old", SHA256: "old", ZoneSerial: new(int64(4294967295))}}
	res, err = newCollector(st, &fakeSource{body: withSerial(t, "5")}).RunOnce(context.Background(), false)
	if err != nil || res.Outcome != Applied {
		t.Fatalf("5 é posterior a 4294967295: res = %+v, err = %v", res, err)
	}
}

func TestSerialLess(t *testing.T) {
	cases := []struct {
		a, b uint32
		want bool
	}{
		{2026092400, 2026092401, true},
		{2026092401, 2026092400, false},
		{2026092401, 2026092401, false},
		{4294967295, 0, true},  // volta de 2³²: 0 é depois de 4294967295
		{0, 4294967295, false}, // e 4294967295 é antes de 0
		{1, 1 + 1<<31, false},  // diferença de exatamente 2³¹: indefinido na RFC, não é "menor"
		{1, 1 << 31, true},
	}
	for _, c := range cases {
		if got := SerialLess(c.a, c.b); got != c.want {
			t.Errorf("SerialLess(%d, %d) = %v", c.a, c.b, got)
		}
	}
}

func TestSameOrNewerSerialApplies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   []byte
		serial *int64
	}{
		{"mesmo serial, conteúdo novo", []byte(strings.Replace(string(sample(t)), "OPERATED BY WIDE", "OPERATED BY WIDE PROJECT", 1)), new(int64(2026092401))},
		{"serial maior", withSerial(t, "2026100101"), new(int64(2026092401))},
		{"aplicado sem serial", sample(t), nil},
	} {
		st := &fakeStore{last: &store.Applied{Version: "v1", URL: srcURL, MD5: "old", SHA256: "old", ZoneSerial: tc.serial}}
		res, err := newCollector(st, &fakeSource{body: tc.body}).RunOnce(context.Background(), false)
		if err != nil || res.Outcome != Applied {
			t.Errorf("%s: res = %+v, err = %v", tc.name, res, err)
		}
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
	if src.downloads != 0 || src.md5Calls != 0 {
		t.Error("sem banco não baixa")
	}
}

func TestTouchCheckError(t *testing.T) {
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: srcURL, ETag: "e"}, touchErr: errors.New("banco fora")}
	src := &fakeSource{notModified: true, published: strings.Repeat("b", 32)}
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
	if r := st.failRuns[0]; r.SHA256 == "" || r.MD5 == "" || r.HTTPStatus != 200 || r.Parsed || r.Servers != 0 {
		t.Errorf("a falha registra o download, sem cabeçalho nem contagens: %+v", r)
	}
}

func TestTooFewServersIsRecorded(t *testing.T) {
	st := &fakeStore{}
	src := &fakeSource{body: sample(t)}
	c := New(st, src, Options{URL: srcURL, MD5URL: md5URL, MinServers: 14, RemovalThreshold: 0.05},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := c.RunOnce(context.Background(), false); err == nil || !strings.Contains(err.Error(), "só 13 servidores no arquivo (mínimo 14)") {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 || st.failRuns[0].Servers != 13 {
		t.Errorf("failures = %v, runs = %+v", st.failures, st.failRuns)
	}
}

func TestApplyErrorIsRecorded(t *testing.T) {
	st := &fakeStore{applyErr: &store.RemovalError{Removed: 1, Current: 13, Threshold: 0.05}}
	src := &fakeSource{body: sample(t)}
	_, err := newCollector(st, src).RunOnce(context.Background(), false)
	if _, ok := errors.AsType[*store.RemovalError](err); !ok {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 || !strings.Contains(st.failures[0], "--force") {
		t.Errorf("failures = %v", st.failures)
	}
}

func TestBusyIsNotRecorded(t *testing.T) {
	st := &fakeStore{applyErr: store.ErrBusy}
	src := &fakeSource{body: sample(t)}
	if _, err := newCollector(st, src).RunOnce(context.Background(), false); !errors.Is(err, store.ErrBusy) {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 0 {
		t.Errorf("concorrência não é falha do arquivo: %v", st.failures)
	}
}

func TestSecondRunIsUnchanged(t *testing.T) {
	st := &fakeStore{}
	src := &fakeSource{body: sample(t)}
	c := newCollector(st, src)
	if _, err := c.RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	res, err := c.RunOnce(context.Background(), false)
	if err != nil || res.Outcome != Unchanged || src.downloads != 1 {
		t.Errorf("res = %+v, err = %v, downloads = %d", res, err, src.downloads)
	}
}

func TestMissingFamilyWarningGoesToRun(t *testing.T) {
	body := strings.Replace(string(sample(t)), "M.ROOT-SERVERS.NET.      3600000      AAAA  2001:dc3::35\n", "", 1)
	st := &fakeStore{}
	res, err := newCollector(st, &fakeSource{body: []byte(body)}).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if w := res.Run.Warnings; len(w) != 1 || w[0] != "servidor m.root-servers.net sem IPv6 (registro AAAA)" || res.Run.IPv6Addresses != 12 {
		t.Errorf("run = %+v", res.Run)
	}
}
