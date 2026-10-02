package collector

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/patrickbrandao/badblock/apps/rootzone/collector/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/rootzone/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/rootzone/collector/internal/store"
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
	f.last = &store.Applied{Version: "v" + run.SHA256[:4], URL: run.URL, ETag: run.ETag, MD5: run.MD5,
		SHA256: run.SHA256, Serial: ds.SOA.Serial}
	return store.Changes{TLDInserted: len(ds.TLDs), RecordInserted: len(ds.Records)}, f.last.Version, nil
}
func (f *fakeStore) RecordFailure(_ context.Context, run store.Run, cause error) error {
	f.failures = append(f.failures, cause.Error())
	f.failRuns = append(f.failRuns, run)
	return nil
}

type fakeSource struct {
	body        []byte
	md5         string // publicado; vazio = o do body
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
	if f.md5 != "" {
		return f.md5, nil
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
	return &fetch.Download{URL: url, Status: 200, Body: f.body, MD5: md5hex(f.body), SHA256: shaHex(f.body),
		ETag: `"225757-65ca377d2eb00-gzip"`, LastModified: "Tue, 29 Sep 2026 18:37:00 GMT"}, nil
}

func md5hex(b []byte) string {
	s := md5.Sum(b)
	return hex.EncodeToString(s[:])
}

func shaHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func sample(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/root-zone-sample.zone")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// withSerial troca o serial do SOA do recorte (2026092901).
func withSerial(t *testing.T, serial string) []byte {
	t.Helper()
	return []byte(strings.Replace(string(sample(t)), "2026092901 1800", serial+" 1800", 1))
}

const (
	srcURL = "https://x/root.zone"
	md5URL = "https://x/root.zone.md5"
)

func newCollector(st Store, src Source) *Collector {
	return New(st, src, Options{URL: srcURL, MD5URL: md5URL, MinTLDs: 5, RemovalThreshold: 0.05},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func applied(sum string, serial uint32) *store.Applied {
	return &store.Applied{Version: "v1", URL: srcURL, ETag: `"e0"`, LastModified: "lm", MD5: "old", SHA256: sum, Serial: serial}
}

func TestFirstRunApplies(t *testing.T) {
	body := sample(t)
	st := &fakeStore{}
	src := &fakeSource{body: body}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Applied || len(st.applied) != 1 || res.Version != "v"+shaHex(body)[:4] {
		t.Fatalf("res = %+v", res)
	}
	if src.lastPrev != (fetch.Validators{}) || src.md5Calls != 1 {
		t.Errorf("sem execução anterior não há validadores: %+v; md5 %d", src.lastPrev, src.md5Calls)
	}
	r := st.applied[0]
	if !r.Parsed || r.SOA.Serial != 2026092901 || r.SOA.MName != "a.root-servers.net" || r.TLDs != 7 || r.Records != 195 ||
		r.RRSIGs != 17 || r.TypeCounts["NS"] != 61 || r.MD5 != md5hex(body) || r.SHA256 != shaHex(body) ||
		r.ETag != `"225757-65ca377d2eb00-gzip"` || r.HTTPStatus != 200 || r.Bytes != int64(len(body)) || r.Forced || len(r.Warnings) != 0 {
		t.Errorf("run = %+v", r)
	}
	if st.opts[0].Force || st.opts[0].RemovalThreshold != 0.05 {
		t.Errorf("opções = %+v", st.opts[0])
	}
	if st.touched != 0 {
		t.Error("arquivo aplicado não chama TouchCheck (Apply já atualiza jobs)")
	}
}

func TestPublishedMD5UnchangedSkipsDownload(t *testing.T) {
	body := sample(t)
	st := &fakeStore{last: applied(shaHex(body), 2026092901)}
	st.last.MD5 = md5hex(body)
	src := &fakeSource{body: body}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || res.Reason != "md5 publicado igual ao último aplicado" || src.downloads != 0 || st.touched != 1 {
		t.Errorf("res = %+v, downloads %d, touched %d", res, src.downloads, st.touched)
	}
}

func TestNotModifiedSendsValidators(t *testing.T) {
	st := &fakeStore{last: applied("old", 2026092901)}
	src := &fakeSource{notModified: true, md5: "novo"}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || res.Reason != "HTTP 304" || res.Version != "v1" || st.touched != 1 {
		t.Errorf("res = %+v", res)
	}
	if src.lastPrev != (fetch.Validators{ETag: `"e0"`, LastModified: "lm"}) {
		t.Errorf("validadores = %+v", src.lastPrev)
	}
}

func TestOtherURLSendsNoValidators(t *testing.T) {
	st := &fakeStore{last: applied("old", 2026092901)}
	st.last.URL = "https://espelho/root.zone"
	src := &fakeSource{body: withSerial(t, "2026092902")}
	if _, err := newCollector(st, src).RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if src.lastPrev != (fetch.Validators{}) {
		t.Errorf("outra URL não recebe validadores: %+v", src.lastPrev)
	}
}

func TestSameContentIsUnchanged(t *testing.T) {
	body := sample(t)
	st := &fakeStore{last: applied(shaHex(body), 2026092901)}
	src := &fakeSource{body: body}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || res.Reason != "conteúdo igual ao último aplicado" || len(st.applied) != 0 || len(st.failures) != 0 {
		t.Errorf("res = %+v", res)
	}
}

func TestMD5Off(t *testing.T) {
	st := &fakeStore{}
	src := &fakeSource{body: sample(t), md5: "outro"}
	c := New(st, src, Options{URL: srcURL, MinTLDs: 5, RemovalThreshold: 0.05}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := c.RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if src.md5Calls != 0 || len(st.applied) != 1 {
		t.Errorf("md5 desligado: chamadas %d, aplicados %d", src.md5Calls, len(st.applied))
	}
}

func TestMD5UnavailableContinues(t *testing.T) {
	st := &fakeStore{}
	src := &fakeSource{body: sample(t), md5Err: errors.New("HTTP 503")}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil || res.Outcome != Applied {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestMD5MismatchIsRecorded(t *testing.T) {
	st := &fakeStore{}
	src := &fakeSource{body: sample(t), md5: strings.Repeat("0", 32)}
	_, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err == nil || !strings.HasPrefix(err.Error(), "md5 divergente: publicado 00000000000000000000000000000000, baixado ") {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 || len(st.applied) != 0 || st.failRuns[0].Parsed || st.failRuns[0].SHA256 == "" {
		t.Errorf("falhas %v, run %+v", st.failures, st.failRuns)
	}
}

func TestForceAppliesSameFile(t *testing.T) {
	body := sample(t)
	st := &fakeStore{last: applied(shaHex(body), 2026092901)}
	st.last.MD5 = md5hex(body)
	src := &fakeSource{body: body}
	res, err := newCollector(st, src).RunOnce(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Applied || !st.opts[0].Force || !st.applied[0].Forced || src.md5Calls != 1 {
		t.Errorf("res = %+v, opts %+v", res, st.opts)
	}
	if src.lastPrev != (fetch.Validators{}) {
		t.Errorf("--force não envia validadores: %+v", src.lastPrev)
	}
}

func TestOlderSerialIsIgnored(t *testing.T) {
	st := &fakeStore{last: applied("old", 2026092902)}
	src := &fakeSource{body: sample(t)} // serial 2026092901
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || res.Reason != "arquivo mais antigo que o aplicado (serial 2026092901 < 2026092902)" ||
		len(st.applied) != 0 || len(st.failures) != 0 || st.touched != 1 {
		t.Errorf("res = %+v, falhas %v", res, st.failures)
	}

	// Com --force, aplica.
	st = &fakeStore{last: applied("old", 2026092902)}
	res, err = newCollector(st, &fakeSource{body: sample(t)}).RunOnce(context.Background(), true)
	if err != nil || res.Outcome != Applied {
		t.Fatalf("--force: res = %+v, err = %v", res, err)
	}
}

func TestNewerSerialApplies(t *testing.T) {
	st := &fakeStore{last: applied("old", 2026092900)}
	res, err := newCollector(st, &fakeSource{body: sample(t)}).RunOnce(context.Background(), false)
	if err != nil || res.Outcome != Applied || len(st.applied[0].Warnings) != 0 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

// Mesmo serial com conteúdo diferente: aplica, com aviso.
func TestEqualSerialDifferentContentApplies(t *testing.T) {
	st := &fakeStore{last: applied("old", 2026092901)}
	res, err := newCollector(st, &fakeSource{body: sample(t)}).RunOnce(context.Background(), false)
	if err != nil || res.Outcome != Applied {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	w := st.applied[0].Warnings
	if len(w) != 1 || !strings.HasPrefix(w[0], "serial 2026092901 igual ao aplicado, com conteúdo diferente (sha256 ") {
		t.Errorf("avisos = %v", w)
	}
}

func TestSerialLess(t *testing.T) {
	cases := []struct {
		a, b uint32
		want bool
	}{
		{2026092901, 2026092902, true},
		{2026092902, 2026092901, false},
		{2026092901, 2026092901, false},
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

func TestParserErrorIsRecorded(t *testing.T) {
	st := &fakeStore{}
	src := &fakeSource{body: []byte("isto não é uma zona\n")}
	_, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err == nil || !strings.HasPrefix(err.Error(), "parser: ") {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 || st.failRuns[0].Parsed || st.failRuns[0].TLDs != 0 {
		t.Errorf("falha = %v, run %+v", st.failures, st.failRuns)
	}
}

func TestTooFewTLDsIsRecorded(t *testing.T) {
	st := &fakeStore{}
	c := New(st, &fakeSource{body: sample(t)}, Options{URL: srcURL, MD5URL: md5URL, MinTLDs: 8, RemovalThreshold: 0.05},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := c.RunOnce(context.Background(), false)
	if err == nil || err.Error() != "só 7 TLDs na zona (mínimo 8): arquivo truncado?" {
		t.Fatalf("err = %v", err)
	}
	if len(st.failRuns) != 1 || !st.failRuns[0].Parsed || st.failRuns[0].TLDs != 7 || st.failRuns[0].SOA.Serial != 2026092901 {
		t.Errorf("run = %+v", st.failRuns)
	}
}

func TestApplyErrorIsRecorded(t *testing.T) {
	st := &fakeStore{applyErr: &store.RemovalError{Removed: 10, Current: 100, Threshold: 0.05}}
	_, err := newCollector(st, &fakeSource{body: sample(t)}).RunOnce(context.Background(), false)
	if err == nil || len(st.failures) != 1 || !strings.Contains(st.failures[0], "use --force") {
		t.Fatalf("err = %v, falhas %v", err, st.failures)
	}
}

func TestBusyIsNotRecorded(t *testing.T) {
	st := &fakeStore{applyErr: store.ErrBusy}
	_, err := newCollector(st, &fakeSource{body: sample(t)}).RunOnce(context.Background(), false)
	if !errors.Is(err, store.ErrBusy) || len(st.failures) != 0 {
		t.Fatalf("err = %v, falhas %v", err, st.failures)
	}
}

func TestFailuresBeforeDownloadAreNotRecorded(t *testing.T) {
	st := &fakeStore{lastErr: errors.New("banco fora")}
	if _, err := newCollector(st, &fakeSource{}).RunOnce(context.Background(), false); err == nil ||
		!strings.HasPrefix(err.Error(), "lendo o último arquivo aplicado: ") {
		t.Errorf("err = %v", err)
	}
	st = &fakeStore{}
	if _, err := newCollector(st, &fakeSource{err: errors.New("HTTP 503"), md5Err: errors.New("x")}).RunOnce(context.Background(), false); err == nil ||
		err.Error() != "download: HTTP 503" {
		t.Errorf("err = %v", err)
	}
	st = &fakeStore{last: applied("old", 1), touchErr: errors.New("jobs fora")}
	if _, err := newCollector(st, &fakeSource{notModified: true}).RunOnce(context.Background(), false); err == nil ||
		!strings.HasPrefix(err.Error(), "jobs: ") {
		t.Errorf("err = %v", err)
	}
	if len(st.failures) != 0 {
		t.Errorf("falhas antes do download não são gravadas: %v", st.failures)
	}
}

func TestWarningsAreCapped(t *testing.T) {
	var b strings.Builder
	b.Write(sample(t))
	for i := range 7000 {
		fmt.Fprintf(&b, "h%d.nic.br. 172800 IN A 192.0.2.%d\n", i, i%256)
	}
	for range 60 {
		b.WriteString("br. 172800 CH NS a.dns.br.\n")
	}
	st := &fakeStore{}
	if _, err := newCollector(st, &fakeSource{body: []byte(b.String())}).RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	w := st.applied[0].Warnings
	if len(w) != parse.MaxWarnings+1 || w[len(w)-1] != "... e mais 10 avisos" {
		t.Errorf("avisos = %d, último %q", len(w), w[len(w)-1])
	}
}

func TestSecondRunUnchanged(t *testing.T) {
	st := &fakeStore{}
	src := &fakeSource{body: sample(t)}
	c := newCollector(st, src)
	if _, err := c.RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	res, err := c.RunOnce(context.Background(), false)
	if err != nil || res.Outcome != Unchanged || res.Reason != "md5 publicado igual ao último aplicado" || src.downloads != 1 {
		t.Errorf("res = %+v, err = %v, downloads %d", res, err, src.downloads)
	}
}
