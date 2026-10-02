//go:build integration

package httpapi

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/store"
	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/testdb"
)

// fixtureFile é o recorte real do root.zone de 2026-09-29 (ápice e 7 TLDs
// completos, com o glue), na fixture do coletor.
const fixtureFile = "../../../collector/testdata/root-zone-sample.zone"

// Limites de tempo com o arquivo real inteiro (handler e store reais, sem
// cache); medidos bem abaixo disso (api.md, "Medições").
const (
	realMaxTLDs = 500 * time.Millisecond
	realMaxTLD  = 100 * time.Millisecond
)

// TestRealFile carrega o root.zone com o binário do collector-rootzone
// (compilado aqui, com o arquivo e o .md5 servidos por um httptest.Server)
// num PG18 descartável e confere /meta, /tlds e o /tld de todos os TLDs
// contra as tabelas, com tempos no log. Usa ROOTZONE_REAL_FILE se definida
// (make test-real); senão, a fixture do coletor, com MIN_TLDS=0 — por isso
// roda em todo make test-int, sem rede.
func TestRealFile(t *testing.T) {
	path := os.Getenv("ROOTZONE_REAL_FILE")
	full := path != ""
	if !full {
		path = fixtureFile
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum(raw)
	sha := sha256.Sum256(raw)
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
	run.Env = append(os.Environ(), "POSTGRES_URL="+pgURL, "SOURCE_URL="+files.URL+"/root.zone", "LOG_FORMAT=text")
	if !full {
		run.Env = append(run.Env, "MIN_TLDS=0") // o recorte tem 7 TLDs
	}
	start := time.Now()
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("collector --once: %v\n%s", err, out)
	}
	t.Logf("carga pelo collector: %v (%d bytes, MD5 %x)", time.Since(start).Round(time.Millisecond), len(raw), sum)
	if _, err := admin.Exec(ctx, "ANALYZE"); err != nil {
		t.Fatal(err)
	}

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

	var slowest time.Duration
	var slowestPath string
	get := func(path string, verbose bool) *httptest.ResponseRecorder {
		t.Helper()
		start := time.Now()
		rec := do(h, "GET", base+path)
		d := time.Since(start)
		if rec.Code != 200 {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
		if d > slowest && strings.HasPrefix(path, "/tld/") {
			slowest, slowestPath = d, path
		}
		if verbose {
			t.Logf("%-32s %3d %7d bytes %v", path, rec.Code, rec.Body.Len(), d.Round(time.Microsecond))
		}
		return rec
	}

	// /meta bate com a execução gravada pelo collector e com o arquivo.
	meta := decode[MetaResponse](t, get("/meta", true))
	var serial int64
	var tlds, records, rrsigs, tldRows int
	if err := admin.QueryRow(ctx, `SELECT serial, tlds, records, rrsigs, (SELECT count(*) FROM rootzone_tld)
		FROM rootzone_run WHERE status = 1`).Scan(&serial, &tlds, &records, &rrsigs, &tldRows); err != nil {
		t.Fatal(err)
	}
	if meta.Dataset == nil || *meta.Dataset.Serial != serial || *meta.Dataset.TLDs != tlds || tldRows != tlds ||
		*meta.Dataset.Records != records || *meta.Dataset.RRSIGs != rrsigs || meta.Dataset.SHA256 != hex.EncodeToString(sha[:]) ||
		meta.Dataset.SOA == nil || meta.Dataset.SOA.MName != "a.root-servers.net" ||
		meta.Collector == nil || meta.Collector.LastSyncAt == nil {
		t.Errorf("meta = %+v %+v", meta.Dataset, meta.Collector)
	}
	t.Logf("zona: serial %d, %d TLDs, %d RRs, %d RRSIG", serial, tlds, records, rrsigs)

	// /tlds: todos, na ordem da tabela, com o serial da versão.
	start = time.Now()
	rec := get("/tlds", true)
	if d := time.Since(start); full && d > realMaxTLDs {
		t.Errorf("/tlds levou %v (limite %v)", d, realMaxTLDs)
	}
	list := decode[TLDsResponse](t, rec)
	if list.Count != tlds || len(list.TLDs) != tlds || *list.Zone.Serial != serial || list.Dataset.Version != w.Current().Version {
		t.Fatalf("tlds: count %d, zone %+v", list.Count, list.Zone)
	}
	var names []string
	if err := admin.QueryRow(ctx, `SELECT array_agg(tld ORDER BY tld) FROM rootzone_tld`).Scan(&names); err != nil {
		t.Fatal(err)
	}
	var idn, noDS, noV6 int
	for i, x := range list.TLDs {
		if x.TLD != names[i] {
			t.Fatalf("tlds[%d] = %s, tabela %s", i, x.TLD, names[i])
		}
	}

	// /tld de todos os TLDs: as listas batem com as contagens de rootzone_tld;
	// um IDN também pela forma Unicode em maiúsculas e com o ponto final.
	start = time.Now()
	biggest, biggestTLD := 0, ""
	for _, x := range list.TLDs {
		rec := get("/tld/"+x.TLD, false)
		if rec.Body.Len() > biggest {
			biggest, biggestTLD = rec.Body.Len(), x.TLD
		}
		got := decode[TLDResponse](t, rec)
		v4, v6 := 0, 0
		for _, ns := range got.Nameservers {
			if len(ns.IPv4) > 0 {
				v4++
			}
			if len(ns.IPv6) > 0 {
				v6++
			}
			if ns.TTL <= 0 || (len(ns.IPv4) == 0 && len(ns.IPv6) == 0) {
				t.Errorf("%s: NS %+v sem TTL ou sem glue", x.TLD, ns)
			}
		}
		if got.TLD != x.TLD || got.TLDUnicode != x.TLDUnicode || len(got.Nameservers) != x.Nameservers ||
			v4 != x.NameserversIPv4 || v6 != x.NameserversIPv6 || len(got.DS) != x.DSRecords || *got.Zone.Serial != serial {
			t.Errorf("tld/%s = %d NS (%d v4, %d v6), %d DS; lista %+v", x.TLD, len(got.Nameservers), v4, v6, len(got.DS), x)
		}
		if strings.HasPrefix(x.TLD, "xn--") {
			idn++
			byUnicode := get("/v1/tld/"+url.PathEscape(strings.ToUpper(x.TLDUnicode))+".", false)
			if byUnicode.Body.String() != rec.Body.String() {
				t.Errorf("tld/%s pela forma Unicode %q: %s", x.TLD, x.TLDUnicode, byUnicode.Body)
			}
		}
		if x.DSRecords == 0 {
			noDS++
		}
		if x.NameserversIPv6 == 0 {
			noV6++
		}
	}
	t.Logf("/tld de %d TLDs (%d IDN também em Unicode): %v no total; mais lento %s em %v; maior %s com %d bytes",
		len(list.TLDs), idn, time.Since(start).Round(time.Millisecond), slowestPath, slowest.Round(time.Microsecond), biggestTLD, biggest)
	t.Logf("%d sem DS, %d sem servidor IPv6", noDS, noV6)
	if full && slowest > realMaxTLD {
		t.Errorf("%s levou %v (limite %v)", slowestPath, slowest, realMaxTLD)
	}
	for _, p := range []string{"/tld/br", "/tld/com", "/tld/%D1%80%D1%84"} {
		get(p, true)
	}
	if rec := do(h, "GET", base+"/tld/nada-disso"); rec.Code != 404 {
		t.Errorf("/tld/nada-disso = %d", rec.Code)
	}

	// O recorte da fixture: os valores de fonte.md.
	if !full {
		got := decode[TLDResponse](t, get("/tld/br", false))
		if serial != 2026092901 || tlds != 7 || records != 195 || rrsigs != 17 || idn != 1 || noDS != 2 ||
			len(got.DS) != 1 || got.DS[0].KeyTag != 38298 || got.Nameservers[0].IPv6[0].Address != "2001:12f8:6::10" {
			t.Errorf("fixture: serial %d, %d TLDs, %d RRs, %d RRSIG, br = %+v", serial, tlds, records, rrsigs, got)
		}
	}

	// Índices das consultas de /tld (com a zona inteira, o planejador os
	// escolhe sozinho; no recorte, pequeno, só sem seq scan).
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if !full {
		if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
			t.Fatal(err)
		}
	}
	for q, idx := range map[string]string{
		`SELECT * FROM rootzone_tld WHERE tld = 'br'`:                                                                   "uq_rootzone_tld_tld",
		`SELECT type, rdata, ttl FROM rootzone_record WHERE owner = 'br' AND type IN ('NS', 'DS') ORDER BY type, rdata`: "uq_rootzone_record_owner_type_rdata",
		`SELECT g.owner, g.type, g.rdata, g.ttl FROM rootzone_record ns JOIN rootzone_record g ON g.owner = ns.rdata AND g.type IN ('A', 'AAAA')
		  WHERE ns.owner = 'com' AND ns.type = 'NS' ORDER BY g.owner, g.type, g.rdata`: "uq_rootzone_record_owner_type_rdata",
	} {
		rows, err := tx.Query(ctx, "EXPLAIN "+q)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, line)
		}
		rows.Close()
		text := strings.Join(plan, "\n")
		if !strings.Contains(text, idx) || slices.ContainsFunc(plan, func(l string) bool { return strings.Contains(l, "Seq Scan") }) {
			t.Errorf("%s: plano sem %s ou com Seq Scan:\n%s", q, idx, text)
		}
	}
}
