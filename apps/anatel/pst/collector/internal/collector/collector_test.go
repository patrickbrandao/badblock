package collector

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/anatel/pst/collector/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/collector/internal/store"
)

type fakeStore struct {
	last     *store.Applied
	touched  int
	applied  []store.Run
	failures []string
	failRuns []store.Run
	applyErr error
}

func (f *fakeStore) LastApplied(context.Context) (*store.Applied, error) { return f.last, nil }
func (f *fakeStore) TouchCheck(context.Context) error                    { f.touched++; return nil }
func (f *fakeStore) Apply(_ context.Context, ds *parse.Dataset, run store.Run, _ store.ApplyOptions) (store.Changes, string, error) {
	if f.applyErr != nil {
		return store.Changes{}, "", f.applyErr
	}
	f.applied = append(f.applied, run)
	f.last = &store.Applied{Version: "v" + run.SHA256[:4], URL: run.URL, ETag: run.ETag, SHA256: run.SHA256,
		CSVSHA256: run.CSVSHA256, CSVModifiedAt: run.CSVModifiedAt}
	return store.Changes{ProviderInserted: len(ds.Providers), ServiceInserted: ds.Services()}, f.last.Version, nil
}
func (f *fakeStore) RecordFailure(_ context.Context, run store.Run, cause error) error {
	f.failures = append(f.failures, cause.Error())
	f.failRuns = append(f.failRuns, run)
	return nil
}

type fakeSource struct {
	body        []byte
	downloads   int
	lastPrev    fetch.Validators
	notModified bool
	err         error
}

func (f *fakeSource) Download(_ context.Context, url string, prev fetch.Validators) (*fetch.Download, error) {
	f.downloads++
	f.lastPrev = prev
	if f.err != nil {
		return nil, f.err
	}
	if f.notModified {
		return &fetch.Download{URL: url, Status: 304, NotModified: true}, nil
	}
	return &fetch.Download{URL: url, Status: 200, Body: f.body, SHA256: sum(f.body), ETag: `"e1"`,
		LastModified: "Wed, 30 Sep 2026 10:56:48 GMT"}, nil
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

const sampleURL = "https://x/pst.zip"

// csvModified é a data da entrada do pst-sample.zip (06:15:08 em Brasília).
var csvModified = time.Date(2026, 9, 30, 9, 15, 8, 0, time.UTC)

func sampleZip(t *testing.T) ([]byte, string) {
	t.Helper()
	b, err := os.ReadFile("../../testdata/pst-sample.zip")
	if err != nil {
		t.Fatal(err)
	}
	c, err := os.ReadFile("../../testdata/pst-sample.csv")
	if err != nil {
		t.Fatal(err)
	}
	return b, sum(c)
}

// rezip põe o CSV num ZIP novo com outra data (MS-DOS, hora de Brasília) e outras entradas.
func rezip(t *testing.T, csv []byte, modified time.Time, extra ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range extra {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte("x"))
	}
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "prestadoras_servicos_telecomunicacoes.csv", Method: zip.Deflate, Modified: modified})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(csv)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newCollector(st Store, src Source) *Collector {
	return New(st, src, Options{URL: sampleURL, MinProviders: 5, RemovalThreshold: 0.05},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestFirstRunApplies(t *testing.T) {
	body, csvSum := sampleZip(t)
	st := &fakeStore{}
	src := &fakeSource{body: body}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Applied || len(st.applied) != 1 || src.lastPrev != (fetch.Validators{}) {
		t.Fatalf("res = %+v", res)
	}
	r := st.applied[0]
	if r.Rows != 158 || r.RowsCNPJ != 155 || r.RowsCPF != 3 || r.Duplicates != 103 || r.Skipped != 0 ||
		r.Providers != 8 || r.Services != 52 || !r.Parsed || r.SHA256 != sum(body) || r.ETag != `"e1"` ||
		r.HTTPStatus != 200 || r.Bytes != int64(len(body)) {
		t.Errorf("run = %+v", r)
	}
	if r.CSVName != "prestadoras_servicos_telecomunicacoes.csv" || r.CSVSHA256 != csvSum || r.CSVBytes != 64421 ||
		!r.CSVModifiedAt.Equal(csvModified) {
		t.Errorf("csv da execução = %s %s %d %v", r.CSVName, r.CSVSHA256, r.CSVBytes, r.CSVModifiedAt)
	}
}

func TestNotModifiedSendsValidators(t *testing.T) {
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: sampleURL, SHA256: "old", ETag: `"e0"`, LastModified: "lm"}}
	src := &fakeSource{notModified: true}
	res, err := newCollector(st, src).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || res.Reason != "HTTP 304" || res.Version != "v1" ||
		src.lastPrev != (fetch.Validators{ETag: `"e0"`, LastModified: "lm"}) || st.touched != 1 {
		t.Errorf("res = %+v, prev = %+v", res, src.lastPrev)
	}
}

func TestOtherURLSendsNoValidators(t *testing.T) {
	body, _ := sampleZip(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: "https://antiga/pst.zip", SHA256: "old", ETag: `"e0"`}}
	src := &fakeSource{body: body}
	if _, err := newCollector(st, src).RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if src.lastPrev != (fetch.Validators{}) {
		t.Errorf("validadores de outra URL: %+v", src.lastPrev)
	}
}

func TestSameZipIsUnchanged(t *testing.T) {
	body, _ := sampleZip(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: sampleURL, SHA256: sum(body)}}
	res, err := newCollector(st, &fakeSource{body: body}).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || res.Reason != "conteúdo igual ao último aplicado" || len(st.applied) != 0 || st.touched != 1 {
		t.Errorf("res = %+v", res)
	}
}

func TestSameCSVInNewZipIsUnchanged(t *testing.T) {
	body, csvSum := sampleZip(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: sampleURL, SHA256: sum(body), CSVSHA256: csvSum, CSVModifiedAt: csvModified}}
	csvData, _ := os.ReadFile("../../testdata/pst-sample.csv")
	newZip := rezip(t, csvData, time.Date(2026, 10, 1, 6, 15, 0, 0, time.FixedZone("", -3*3600)))
	res, err := newCollector(st, &fakeSource{body: newZip}).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || res.Reason != "csv igual ao último aplicado" || len(st.applied) != 0 || len(st.failures) != 0 {
		t.Errorf("res = %+v", res)
	}
}

func TestOlderCSVIsUnchanged(t *testing.T) {
	csvData, _ := os.ReadFile("../../testdata/pst-sample.csv")
	changed := append(append([]byte(nil), csvData...), []byte("CPF;***00000**;PESSOA FISICA EXEMPLO\r\n")...)
	brt := time.FixedZone("", -3*3600)
	old := rezip(t, changed, time.Date(2026, 9, 29, 6, 15, 8, 0, brt))
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: sampleURL, SHA256: "x", CSVSHA256: "y", CSVModifiedAt: csvModified}}
	res, err := newCollector(st, &fakeSource{body: old}).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	want := "csv mais antigo que o aplicado (2026-09-29T06:15:08-03:00 < 2026-09-30T06:15:08-03:00)"
	if res.Outcome != Unchanged || res.Reason != want || len(st.applied) != 0 || len(st.failures) != 0 || st.touched != 1 {
		t.Errorf("res = %+v", res)
	}

	// --force aplica a cópia velha de propósito.
	res, err = newCollector(st, &fakeSource{body: old}).RunOnce(context.Background(), true)
	if err != nil || res.Outcome != Applied || !st.applied[0].Forced {
		t.Errorf("--force: res = %+v, err = %v", res, err)
	}

	// Mais novo: aplica.
	st = &fakeStore{last: &store.Applied{Version: "v1", URL: sampleURL, SHA256: "x", CSVSHA256: "y", CSVModifiedAt: csvModified}}
	newer := rezip(t, changed, time.Date(2026, 10, 1, 6, 15, 8, 0, brt), "LEIAME.txt")
	if res, err := newCollector(st, &fakeSource{body: newer}).RunOnce(context.Background(), false); err != nil || res.Outcome != Applied {
		t.Errorf("mais novo: res = %+v, err = %v", res, err)
	}
}

func TestForceAppliesSameFile(t *testing.T) {
	body, csvSum := sampleZip(t)
	st := &fakeStore{last: &store.Applied{Version: "v1", URL: sampleURL, SHA256: sum(body), CSVSHA256: csvSum, ETag: `"e1"`}}
	src := &fakeSource{body: body}
	res, err := newCollector(st, src).RunOnce(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Applied || src.lastPrev != (fetch.Validators{}) || !st.applied[0].Forced {
		t.Errorf("res = %+v, prev = %+v", res, src.lastPrev)
	}
}

func TestBadZipIsRecorded(t *testing.T) {
	st := &fakeStore{}
	_, err := newCollector(st, &fakeSource{body: []byte("<html>manutenção</html>")}).RunOnce(context.Background(), false)
	if err == nil || !strings.HasPrefix(err.Error(), "ZIP inválido") {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 || st.failRuns[0].SHA256 == "" || st.failRuns[0].CSVName != "" || st.failRuns[0].Parsed {
		t.Errorf("falhas = %v, %+v", st.failures, st.failRuns)
	}
}

func TestParserErrorIsRecorded(t *testing.T) {
	st := &fakeStore{}
	z := rezip(t, []byte("a;b;c\r\n"), time.Now())
	_, err := newCollector(st, &fakeSource{body: z}).RunOnce(context.Background(), false)
	if err == nil || err.Error() != "parser: cabeçalho com 3 colunas (esperadas 24)" {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 || st.failRuns[0].CSVSHA256 == "" || st.failRuns[0].Parsed {
		t.Errorf("falhas = %v", st.failures)
	}
}

func TestTooFewProvidersIsRecorded(t *testing.T) {
	body, _ := sampleZip(t)
	st := &fakeStore{}
	c := newCollector(st, &fakeSource{body: body})
	c.opt.MinProviders = 9
	_, err := c.RunOnce(context.Background(), false)
	if err == nil || err.Error() != "só 8 prestadoras no arquivo (mínimo 9): arquivo truncado?" {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 || !st.failRuns[0].Parsed || st.failRuns[0].Providers != 8 {
		t.Errorf("falhas = %v", st.failures)
	}
}

func TestRemovalErrorIsRecorded(t *testing.T) {
	body, _ := sampleZip(t)
	st := &fakeStore{applyErr: &store.RemovalError{Entity: "prestadoras", Removed: 10, Current: 100, Threshold: 0.05}}
	if _, err := newCollector(st, &fakeSource{body: body}).RunOnce(context.Background(), false); err == nil {
		t.Fatal("esperava erro")
	}
	if len(st.failures) != 1 || !strings.HasPrefix(st.failures[0], "o arquivo removeria 10 de 100 prestadoras") {
		t.Errorf("falhas = %v", st.failures)
	}
}

func TestBusyIsNotRecorded(t *testing.T) {
	body, _ := sampleZip(t)
	st := &fakeStore{applyErr: store.ErrBusy}
	if _, err := newCollector(st, &fakeSource{body: body}).RunOnce(context.Background(), false); !errors.Is(err, store.ErrBusy) {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 0 {
		t.Errorf("concorrência não é falha do arquivo: %v", st.failures)
	}
}

func TestDownloadErrorIsNotRecorded(t *testing.T) {
	st := &fakeStore{}
	_, err := newCollector(st, &fakeSource{err: errors.New("HTTP 503")}).RunOnce(context.Background(), false)
	if err == nil || err.Error() != "download: HTTP 503" || len(st.failures) != 0 || st.touched != 0 {
		t.Fatalf("err = %v, falhas = %v", err, st.failures)
	}
}
