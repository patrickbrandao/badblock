//go:build integration

package httpapi

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/roothints/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/roothints/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/roothints/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/roothints/api/internal/store"
	"github.com/patrickbrandao/badblock/apps/roothints/api/internal/testdb"
)

// fixtureFile é o named.root inteiro de 2026-09-30, na fixture do coletor.
const fixtureFile = "../../../collector/testdata/named.root"

// TestRealFile carrega o named.root real com o binário do collector-roothints
// (compilado aqui, com o arquivo e o .md5 servidos por um httptest.Server)
// num PG18 descartável e confere as rotas contra as tabelas, com tempos no
// log. Usa ROOTHINTS_REAL_FILE se definida (make test-real FILE=...); senão,
// a fixture do coletor — por isso roda em todo make test-int, sem rede.
func TestRealFile(t *testing.T) {
	path := os.Getenv("ROOTHINTS_REAL_FILE")
	if path == "" {
		path = fixtureFile
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum(raw)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	pgURL, admin := testdb.New(t)

	// Compila o collector irmão e carrega o arquivo com --once.
	bin := filepath.Join(t.TempDir(), store.CollectorApp)
	build := exec.Command("go", "build", "-o", bin, "./cmd/"+store.CollectorApp)
	build.Dir = filepath.Join("..", "..", "..", "collector")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build do collector: %v\n%s", err, out)
	}
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".md5") {
			_, _ = io.WriteString(w, hex.EncodeToString(sum[:])+"\n")
			return
		}
		_, _ = w.Write(raw)
	}))
	defer files.Close()
	run := exec.Command(bin, "--once")
	run.Env = append(os.Environ(), "POSTGRES_URL="+pgURL, "SOURCE_URL="+files.URL+"/named.root", "LOG_FORMAT=text")
	start := time.Now()
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("collector --once: %v\n%s", err, out)
	}
	t.Logf("carga pelo collector: %v (%d bytes, MD5 %x)", time.Since(start).Round(time.Millisecond), len(raw), sum)

	st, err := store.Open(ctx, pgURL, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	w := dataset.NewWatcher(st, time.Minute, log)
	if err := w.Refresh(ctx); err != nil || !w.Current().Ready() {
		t.Fatalf("dataset: %+v, %v", w.Current(), err)
	}
	rip, _ := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	h := New(st, w, cache.Noop{}, rip, log, Config{BasePath: base, Version: "real"}).Handler()

	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		start := time.Now()
		rec := do(h, "GET", base+path)
		if rec.Code != 200 {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
		t.Logf("%-40s %3d %6d bytes %v", path, rec.Code, rec.Body.Len(), time.Since(start).Round(time.Microsecond))
		return rec
	}

	// /meta bate com a execução gravada pelo collector.
	meta := decode[MetaResponse](t, get("/meta"))
	var (
		rows       int
		lastUpdate string
		serial     int64
	)
	if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM roothints_server), last_update::text, zone_serial
		FROM roothints_run WHERE status = 1`).Scan(&rows, &lastUpdate, &serial); err != nil {
		t.Fatal(err)
	}
	if meta.Dataset == nil || meta.Dataset.Servers != rows || meta.Dataset.MD5 != hex.EncodeToString(sum[:]) ||
		*meta.Dataset.LastUpdate != lastUpdate || *meta.Dataset.ZoneSerial != serial ||
		meta.Collector == nil || meta.Collector.LastSyncAt == nil {
		t.Errorf("meta = %+v %+v", meta.Dataset, meta.Collector)
	}
	t.Logf("arquivo: %d servidores, last update %s, serial %d", rows, lastUpdate, serial)

	// /servers: todos, em ordem de letra, iguais às linhas da tabela (host()
	// tira a máscara do inet), com o cabeçalho do arquivo.
	list := decode[ServersResponse](t, get("/servers"))
	if list.Count != rows || len(list.Servers) != rows || *list.Source.LastUpdate != lastUpdate ||
		*list.Source.ZoneSerial != serial || list.Dataset.Version != w.Current().Version {
		t.Fatalf("servers: count %d, source %+v", list.Count, list.Source)
	}
	dbRows, err := admin.Query(ctx, `SELECT name, letter, host(ipv4), host(ipv6), ns_ttl, ipv4_ttl, ipv6_ttl, note
		FROM roothints_server ORDER BY letter`)
	if err != nil {
		t.Fatal(err)
	}
	var want []Server
	for dbRows.Next() {
		var s Server
		if err := dbRows.Scan(&s.Name, &s.Letter, &s.IPv4, &s.IPv6, &s.NSTTL, &s.IPv4TTL, &s.IPv6TTL, &s.Note); err != nil {
			t.Fatal(err)
		}
		want = append(want, s)
	}
	dbRows.Close()
	for i, s := range list.Servers {
		if !sameServer(s, want[i]) {
			t.Errorf("servers[%d] = %+v, tabela %+v", i, s, want[i])
		}
		// Cada servidor pela letra e pelo nome absoluto em maiúsculas.
		one := decode[ServerResponse](t, get("/server/"+strings.ToUpper(s.Letter)))
		byName := decode[ServerResponse](t, get("/v1/server/"+strings.ToUpper(s.Name)+"."))
		if !sameServer(one.Server, s) || !sameServer(byName.Server, s) || one.FirstSeen == "" {
			t.Errorf("server %s = %+v / %+v", s.Letter, one, byName)
		}
	}
	if rec := do(h, "GET", base+"/server/z"); rec.Code != 404 {
		t.Errorf("/server/z = %d", rec.Code)
	}

	// O arquivo do dia 2026-09-30 (a fixture): os valores de fonte.md.
	if path == fixtureFile {
		a, m := list.Servers[0], list.Servers[12]
		if rows != 13 || serial != 2026092401 || lastUpdate != "2026-09-24" ||
			*a.IPv4 != "198.41.0.4" || *a.IPv6 != "2001:503:ba3e::2:30" || *a.Note != "FORMERLY NS.INTERNIC.NET" ||
			*m.IPv4 != "202.12.27.33" || *m.IPv6 != "2001:dc3::35" || *m.Note != "OPERATED BY WIDE" || a.NSTTL != 3600000 {
			t.Errorf("fixture: %d servidores, %s, %d, a = %+v, m = %+v", rows, lastUpdate, serial, a, m)
		}
	}

	// Os dois índices únicos servem às consultas por letra e por nome (com 13
	// linhas o planejador prefere a página inteira; sem seq scan, o índice).
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	for q, idx := range map[string]string{
		`SELECT * FROM roothints_server WHERE letter = 'k'`:                "uq_roothints_server_letter",
		`SELECT * FROM roothints_server WHERE name = 'k.root-servers.net'`: "uq_roothints_server_name",
	} {
		var plan string
		if err := tx.QueryRow(ctx, "EXPLAIN "+q).Scan(&plan); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(plan, idx) {
			t.Errorf("%s: plano sem %s: %s", q, idx, plan)
		}
	}
}

func sameServer(a, b Server) bool {
	eq := func(x, y *string) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	eqi := func(x, y *int) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return a.Name == b.Name && a.Letter == b.Letter && eq(a.IPv4, b.IPv4) && eq(a.IPv6, b.IPv6) && a.NSTTL == b.NSTTL &&
		eqi(a.IPv4TTL, b.IPv4TTL) && eqi(a.IPv6TTL, b.IPv6TTL) && eq(a.Note, b.Note)
}
