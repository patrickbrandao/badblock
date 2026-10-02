package collector

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/source"
	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/store"
)

// relaxed são limites que os recortes de testdata/ satisfazem.
var relaxed = parse.Limits{MinIPv6Blocks: 5, MinSpecialIPv4: 5, MinSpecialIPv6: 5, MinSpecialASNs: 3,
	MinRDAPASN: 5, MinRDAPIPv4: 5, MinRDAPIPv6: 5}

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
		return nil, "", f.applyErr
	}
	f.applied = append(f.applied, run)
	f.last = &store.Applied{Version: "v" + run.SHA256[:6], SHA256: run.SHA256, Files: run.Files}
	return store.Changes{"iana_asn_block": {Inserted: len(ds.ASNBlocks)}}, f.last.Version, nil
}
func (f *fakeStore) RecordFailure(_ context.Context, _ store.Run, cause error) error {
	f.failures = append(f.failures, cause.Error())
	return nil
}

// server simula www.iana.org e data.iana.org com os recortes de testdata/:
// ETag = hash do conteúdo, 304 quando o If-None-Match bate.
type server struct {
	*httptest.Server
	mu        sync.Mutex
	bodies    map[string][]byte // caminho → conteúdo
	fail      map[string]bool   // caminho → 500 sempre
	failPlain map[string]bool   // caminho → 500 só sem validadores
	no304     bool              // ignora os validadores
	reqs      map[string][]string
}

func path(f source.File) string {
	if f.RDAP {
		return "/rdap/" + f.Path
	}
	return "/assignments/" + f.Path
}

func newServer(t *testing.T) *server {
	t.Helper()
	s := &server{bodies: map[string][]byte{}, fail: map[string]bool{}, failPlain: map[string]bool{}, reqs: map[string][]string{}}
	for _, f := range source.Files {
		b, err := os.ReadFile(filepath.Join("../../testdata", f.Basename()))
		if err != nil {
			t.Fatal(err)
		}
		s.bodies[path(f)] = b
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.Close)
	return s
}

func (s *server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inm := r.Header.Get("If-None-Match")
	kind := "plain"
	if inm != "" || r.Header.Get("If-Modified-Since") != "" {
		kind = "cond"
	}
	s.reqs[r.URL.Path] = append(s.reqs[r.URL.Path], kind)
	body, ok := s.bodies[r.URL.Path]
	switch {
	case !ok:
		http.NotFound(w, r)
		return
	case s.fail[r.URL.Path] || (s.failPlain[r.URL.Path] && kind == "plain"):
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	if !s.no304 && inm == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", "Sat, 19 Sep 2026 00:44:44 GMT")
	_, _ = w.Write(body)
}

func (s *server) set(name string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range source.Files {
		if f.Name == name {
			s.bodies[path(f)] = body
		}
	}
}

func (s *server) requests(name string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range source.Files {
		if f.Name == name {
			return append([]string(nil), s.reqs[path(f)]...)
		}
	}
	return nil
}

func (s *server) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = map[string][]string{}
}

func (s *server) collector(st Store, lim parse.Limits) *Collector {
	src := &fetch.Fetcher{Client: s.Client(), UserAgent: "test", Retries: 1, RetryDelay: time.Millisecond}
	return New(st, src, Options{
		IANABaseURL: s.URL + "/assignments", RDAPBaseURL: s.URL + "/rdap",
		RemovalThreshold: 0.05, Limits: lim,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestFirstRunApplies(t *testing.T) {
	srv := newServer(t)
	st := &fakeStore{}
	res, err := srv.collector(st, relaxed).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Applied || len(st.applied) != 1 || len(st.failures) != 0 {
		t.Fatalf("res = %+v, failures = %v", res, st.failures)
	}
	run := st.applied[0]
	if len(run.Files) != 10 || run.SHA256 != CombinedSHA256(run.Files) || len(run.SHA256) != 64 {
		t.Fatalf("run = %+v", run)
	}
	for i, f := range run.Files {
		if f.Name != source.Files[i].Name || !f.Changed || f.HTTPStatus != 200 || f.ETag == "" || f.LastModified == "" ||
			f.Rows == nil || *f.Rows == 0 || f.Bytes == 0 || !strings.HasPrefix(f.URL, srv.URL) {
			t.Errorf("arquivo %d = %+v", i, f)
		}
		if got := srv.requests(f.Name); len(got) != 1 || got[0] != "plain" {
			t.Errorf("%s: requisições %v", f.Name, got)
		}
	}
	if p := run.Files[7].Publication; p != "2026-06-01T20:00:01Z" {
		t.Errorf("publication rdap-asn = %q", p)
	}
	if len(res.ChangedFiles()) != 10 {
		t.Errorf("changed = %v", res.ChangedFiles())
	}
}

func TestNothingChangedSends304(t *testing.T) {
	srv := newServer(t)
	st := &fakeStore{}
	c := srv.collector(st, relaxed)
	if _, err := c.RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	srv.reset()
	res, err := c.RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || st.touched != 1 || len(st.applied) != 1 || res.Version != st.last.Version {
		t.Errorf("res = %+v, touched = %d", res, st.touched)
	}
	if !strings.Contains(res.Reason, "10 com HTTP 304") {
		t.Errorf("reason = %q", res.Reason)
	}
	for _, f := range source.Files {
		if got := srv.requests(f.Name); len(got) != 1 || got[0] != "cond" {
			t.Errorf("%s: requisições %v, quero um GET condicional", f.Name, got)
		}
	}
}

func TestSameContentWithout304(t *testing.T) {
	srv := newServer(t)
	st := &fakeStore{}
	c := srv.collector(st, relaxed)
	if _, err := c.RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	srv.no304 = true
	res, err := c.RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || !strings.Contains(res.Reason, "10 com conteúdo igual") || len(st.applied) != 1 {
		t.Errorf("res = %+v", res)
	}
}

func TestOneChangedDownloadsAll(t *testing.T) {
	srv := newServer(t)
	st := &fakeStore{}
	c := srv.collector(st, relaxed)
	if _, err := c.RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	first := st.applied[0]

	body, _ := os.ReadFile("../../testdata/special-purpose-as-numbers.csv")
	srv.set(source.SpecialASN, append(body, []byte("112,Used by the AS112 project to sink misdirected DNS queries; see [RFC7534],[RFC7534]\r\n")...))
	srv.reset()
	res, err := c.RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Applied || len(st.applied) != 2 {
		t.Fatalf("res = %+v", res)
	}
	if got := res.ChangedFiles(); len(got) != 1 || got[0] != source.SpecialASN {
		t.Errorf("changed = %v", got)
	}
	run := st.applied[1]
	if run.SHA256 == first.SHA256 {
		t.Error("hash combinado deveria mudar")
	}
	for i, f := range source.Files {
		got := srv.requests(f.Name)
		want := []string{"cond", "plain"} // 304 e depois o download completo
		if f.Name == source.SpecialASN {
			want = []string{"cond"} // mudou: veio 200 no condicional
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: requisições %v, quero %v", f.Name, got, want)
		}
		if run.Files[i].HTTPStatus != 200 || run.Files[i].Bytes == 0 || run.Files[i].Rows == nil {
			t.Errorf("%s: %+v", f.Name, run.Files[i])
		}
	}
	if rows := *run.Files[6].Rows; rows != 6 {
		t.Errorf("special ASN = %d, quero 6", rows)
	}
}

func TestDownloadFailureAppliesNothing(t *testing.T) {
	srv := newServer(t)
	srv.fail["/rdap/ipv4.json"] = true
	st := &fakeStore{}
	_, err := srv.collector(st, relaxed).RunOnce(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "rdap-ipv4") {
		t.Fatalf("err = %v", err)
	}
	if len(st.applied) != 0 || len(st.failures) != 0 || st.touched != 0 {
		t.Errorf("falha de download não aplica nem registra: %+v", st)
	}
	if got := srv.requests(source.RDAPIPv4); len(got) != 2 {
		t.Errorf("tentativas = %v, quero 2 (Retries = 1)", got)
	}
}

func TestRedownloadFailureAppliesNothing(t *testing.T) {
	srv := newServer(t)
	st := &fakeStore{}
	c := srv.collector(st, relaxed)
	if _, err := c.RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	// Um arquivo muda; outro responde 304 ao condicional mas falha no
	// download completo: nada é aplicado.
	srv.set(source.SpecialASN, []byte("AS Number,Reason for Reservation,Reference\r\n0,Reserved,[RFC7607]\r\n1,x,y\r\n2,x,y\r\n"))
	srv.failPlain["/assignments/as-numbers/as-numbers-2.csv"] = true
	_, err := c.RunOnce(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "as-numbers-2") {
		t.Fatalf("err = %v", err)
	}
	if len(st.applied) != 1 || len(st.failures) != 0 {
		t.Errorf("applied = %d, failures = %v", len(st.applied), st.failures)
	}
}

func TestForceAppliesSameDataset(t *testing.T) {
	srv := newServer(t)
	st := &fakeStore{}
	c := srv.collector(st, relaxed)
	if _, err := c.RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	srv.reset()
	res, err := c.RunOnce(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Applied || len(st.applied) != 2 || !st.applied[1].Forced {
		t.Fatalf("res = %+v", res)
	}
	for _, f := range source.Files {
		if got := srv.requests(f.Name); len(got) != 1 || got[0] != "plain" {
			t.Errorf("%s: --force não envia validadores: %v", f.Name, got)
		}
	}
	if len(res.ChangedFiles()) != 0 {
		t.Errorf("nada mudou de fato: %v", res.ChangedFiles())
	}
}

func TestURLChangeDropsValidators(t *testing.T) {
	srv := newServer(t)
	st := &fakeStore{}
	if _, err := srv.collector(st, relaxed).RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	for i := range st.last.Files {
		st.last.Files[i].URL = "https://outra.example/" + st.last.Files[i].Name
	}
	srv.reset()
	res, err := srv.collector(st, relaxed).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged || !strings.Contains(res.Reason, "10 com conteúdo igual") {
		t.Errorf("res = %+v", res)
	}
	if got := srv.requests(source.ASNumbers1); len(got) != 1 || got[0] != "plain" {
		t.Errorf("URL diferente não usa validadores: %v", got)
	}
}

func TestParserFailureIsRecorded(t *testing.T) {
	srv := newServer(t)
	srv.set(source.RDAPIPv4, []byte("<html>manutenção</html>"))
	st := &fakeStore{}
	_, err := srv.collector(st, relaxed).RunOnce(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "parser") {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 || len(st.applied) != 0 {
		t.Errorf("failures = %v", st.failures)
	}
}

func TestSanityFailureIsRecorded(t *testing.T) {
	// Os recortes de testdata/ não passam nos limites de produção.
	srv := newServer(t)
	st := &fakeStore{}
	_, err := srv.collector(st, parse.DefaultLimits()).RunOnce(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "sanidade") || !strings.Contains(err.Error(), "256") {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 1 || len(st.applied) != 0 {
		t.Errorf("failures = %v", st.failures)
	}
}

func TestApplyErrorIsRecordedButBusyIsNot(t *testing.T) {
	srv := newServer(t)
	st := &fakeStore{applyErr: &store.RemovalError{Table: "iana_asn_block", Removed: 10, Current: 20, Threshold: 0.05}}
	if _, err := srv.collector(st, relaxed).RunOnce(context.Background(), false); err == nil {
		t.Fatal("esperava erro")
	}
	if len(st.failures) != 1 {
		t.Errorf("trava de remoção deveria ser registrada: %v", st.failures)
	}

	st = &fakeStore{applyErr: store.ErrBusy}
	if _, err := srv.collector(st, relaxed).RunOnce(context.Background(), false); !errors.Is(err, store.ErrBusy) {
		t.Fatalf("err = %v", err)
	}
	if len(st.failures) != 0 {
		t.Errorf("concorrência não é falha do dataset: %v", st.failures)
	}
}

func TestCombinedSHA256(t *testing.T) {
	a := []store.File{{Name: "a", SHA256: "1"}, {Name: "b", SHA256: "2"}}
	b := []store.File{{Name: "b", SHA256: "2"}, {Name: "a", SHA256: "1"}}
	if CombinedSHA256(a) == CombinedSHA256(b) {
		t.Error("a ordem dos arquivos faz parte do hash")
	}
	sum := sha256.Sum256([]byte("a:1\nb:2\n"))
	if CombinedSHA256(a) != hex.EncodeToString(sum[:]) {
		t.Error("hash combinado fora do formato documentado")
	}
}
