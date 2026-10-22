//go:build integration

package store

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/rdap"
)

// setup sobe um PG18, aplica as migrations de central/, cgibr/ e
// registrobr_rdapnum/ (seção migrate:up) e devolve um Store e um pool
// administrativo no mesmo banco.
func setup(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pg, err := postgres.Run(ctx, "postgres:18-trixie",
		postgres.WithDatabase("badblock"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("pg"),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, pg)
	if err != nil {
		t.Fatal(err)
	}
	url, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	for _, dir := range []string{"central", "cgibr", "registrobr_rdapnum"} {
		files, _ := filepath.Glob(filepath.Join("..", "..", "..", "..", "..", "..", "database", "postgres", dir, "*.sql"))
		if len(files) == 0 {
			t.Fatalf("nenhuma migration em database/postgres/%s", dir)
		}
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			up, _, _ := strings.Cut(string(raw), "-- migrate:down")
			if _, err := admin.Exec(ctx, up); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
		}
	}
	st, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st, admin
}

func exec(t *testing.T, db *pgxpool.Pool, sql string, args ...any) int64 {
	t.Helper()
	tag, err := db.Exec(context.Background(), sql, args...)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return tag.RowsAffected()
}

func count(t *testing.T, db *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func text(t *testing.T, db *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	var s *string
	if err := db.QueryRow(context.Background(), sql, args...).Scan(&s); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	if s == nil {
		return "<nil>"
	}
	return *s
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newWorker(t *testing.T, st *Store, id string) {
	t.Helper()
	if err := st.Heartbeat(context.Background(), Worker{UUID: id, Host: "h", Version: "test", RateLimit: 32}); err != nil {
		t.Fatal(err)
	}
}

const (
	w1 = "01990000-0000-7000-8000-000000000001"
	w2 = "01990000-0000-7000-8000-000000000002"
)

// cgibrVersion grava uma versão do cgibr com estes ASNs e blocos (bloco →
// ASN) e devolve o uuid da versão.
func cgibrVersion(t *testing.T, db *pgxpool.Pool, asns []int64, prefixes map[string]int64) string {
	t.Helper()
	exec(t, db, "DELETE FROM cgibr_asn")
	for _, asn := range asns {
		exec(t, db, `INSERT INTO cgibr_asn (asn, name, document) VALUES ($1, 'X', '00.000.000/0001-00')`, asn)
	}
	for p, asn := range prefixes {
		exec(t, db, `INSERT INTO cgibr_prefix (prefix, asn_uuid) SELECT $1::cidr, uuid FROM cgibr_asn WHERE asn = $2`, p, asn)
	}
	var v string
	if err := db.QueryRow(context.Background(), `INSERT INTO cgibr_run (status, url, sha256, started_at)
		VALUES (1, 'https://example.test/f.txt', repeat('a', 64), NOW()) RETURNING uuid::text`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestDerive(t *testing.T) {
	st, admin := setup(t)
	ctx := context.Background()

	// Sem dataset do cgibr: nada a derivar, mas jobs.last_check_at anda.
	res, err := st.Derive(ctx, []string{"cgibr"}, false)
	if err != nil || !res.Locked || res.Runs[0].Applied || res.Runs[0].Reason != "cgibr sem dataset aplicado" {
		t.Fatalf("sem cgibr: %+v %v", res, err)
	}

	exec(t, admin, "INSERT INTO registrobr_rdapnum_target (kind, asn) VALUES ('asn', 61610)")
	exec(t, admin, "INSERT INTO registrobr_rdapnum_target (kind, prefix) VALUES ('prefix', '200.225.48.0/24')")
	v1 := cgibrVersion(t, admin, []int64{61610, 22548},
		map[string]int64{"200.225.48.0/21": 61610, "2804:8ae0::/32": 61610, "200.160.0.0/20": 22548})
	res, err = st.Derive(ctx, []string{"cgibr"}, false)
	if err != nil {
		t.Fatal(err)
	}
	run := res.Runs[0]
	if !run.Applied || run.Version != v1 || run.ASNs != 2 || run.Prefixes != 3 || run.Inserted != 4 || run.NotInSource != 0 {
		t.Errorf("primeira = %+v", run)
	}
	// O estático fica estático; o /24 estático e o /21 derivado convivem.
	if s := text(t, admin, "SELECT source FROM registrobr_rdapnum_target WHERE asn = 61610"); s != "static" {
		t.Errorf("61610 = %s", s)
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_target WHERE source = 'cgibr'"); n != 4 {
		t.Errorf("derivados = %d", n)
	}
	if n := count(t, admin, "SELECT count(*) FROM jobs WHERE app = $1 AND last_sync_at IS NOT NULL AND consolidated = 0", AppName); n != 1 {
		t.Error("jobs sem last_sync_at")
	}

	// A mesma versão: sem mudança e sem linha em _run.
	res, _ = st.Derive(ctx, []string{"cgibr"}, false)
	if res.Runs[0].Applied || res.Runs[0].Reason != "versão do cgibr já derivada" {
		t.Errorf("repetida = %+v", res.Runs[0])
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_run"); n != 1 {
		t.Errorf("_run = %d", n)
	}

	// Versão nova sem o 22548 e o bloco dele: continuam, contados.
	cgibrVersion(t, admin, []int64{61610}, map[string]int64{"200.225.48.0/21": 61610, "2804:8ae0::/32": 61610})
	res, _ = st.Derive(ctx, []string{"cgibr"}, false)
	if r := res.Runs[0]; !r.Applied || r.Inserted != 0 || r.NotInSource != 2 {
		t.Errorf("sem o 22548 = %+v", r)
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_target"); n != 6 {
		t.Errorf("alvos = %d", n)
	}
	// --force deriva a mesma versão de novo.
	res, _ = st.Derive(ctx, []string{"cgibr"}, true)
	if r := res.Runs[0]; !r.Applied || !r.Forced {
		t.Errorf("force = %+v", r)
	}

	// A trava: outra sessão segurando, a passada não faz nada.
	conn, _ := admin.Acquire(ctx)
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext($1))", AppName); err != nil {
		t.Fatal(err)
	}
	res, err = st.Derive(ctx, []string{"cgibr"}, true)
	if err != nil || res.Locked {
		t.Errorf("com a trava de outro: %+v %v", res, err)
	}
	_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock(hashtext($1))", AppName)
}

func TestQueue(t *testing.T) {
	st, admin := setup(t)
	ctx := context.Background()
	newWorker(t, st, w1)
	newWorker(t, st, w2)
	exec(t, admin, `INSERT INTO registrobr_rdapnum_target (kind, asn, last_scan_at) VALUES ('asn', 1, NOW() - interval '30 days')`)
	exec(t, admin, `INSERT INTO registrobr_rdapnum_target (kind, asn) VALUES ('asn', 2)`)
	exec(t, admin, `INSERT INTO registrobr_rdapnum_target (kind, asn, rescan, last_scan_at) VALUES ('asn', 3, true, NOW())`)
	exec(t, admin, `INSERT INTO registrobr_rdapnum_target (kind, asn, rescan, retry_at) VALUES ('asn', 4, true, NOW() + interval '1 hour')`)
	exec(t, admin, `INSERT INTO registrobr_rdapnum_target (kind, asn, last_scan_at) VALUES ('asn', 5, NOW())`)
	exec(t, admin, `INSERT INTO registrobr_rdapnum_target (kind, asn, active) VALUES ('asn', 6, false)`)
	exec(t, admin, `INSERT INTO registrobr_rdapnum_target (kind, prefix, last_scan_at) VALUES ('prefix', '2001:db8::/32', NOW() - interval '8 days')`)

	lease, interval := 2*time.Minute, 7*24*time.Hour
	var got []string
	for {
		c, err := st.Claim(ctx, w1, lease, interval)
		if err != nil {
			t.Fatal(err)
		}
		if c == nil {
			break
		}
		got = append(got, c.Target())
	}
	// rescan (respeitando retry_at) → nunca → mais antigo; o recente, o
	// inativo e o em espera ficam.
	if want := []string{"3", "2", "1", "2001:db8::/32"}; !slices.Equal(got, want) {
		t.Errorf("ordem = %v, quero %v", got, want)
	}

	// Lease vencido: outra instância retoma.
	exec(t, admin, "UPDATE registrobr_rdapnum_target SET scan_started_at = NOW() - interval '3 minutes' WHERE asn = 2")
	c, _ := st.Claim(ctx, w2, lease, interval)
	if c == nil || c.ASN != 2 {
		t.Fatalf("lease vencido = %+v", c)
	}
	// O lease da w1 venceu: a gravação dela é descartada.
	stale := &Claim{UUID: c.UUID, Kind: "asn", ASN: 2, StartedAt: time.Now()}
	_, err := st.SaveAttempt(ctx, w1, stale, Attempt{Resp: rdap.Response{Result: rdap.NotFound, HTTPStatus: 404, URL: "u"}})
	if !errors.Is(err, ErrLeaseLost) {
		t.Errorf("lease perdido = %v", err)
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_response"); n != 0 {
		t.Errorf("resposta gravada com o lease perdido: %d", n)
	}

	// Duas instâncias ao mesmo tempo nunca pegam o mesmo alvo.
	exec(t, admin, "DELETE FROM registrobr_rdapnum_target")
	for i := range 40 {
		exec(t, admin, "INSERT INTO registrobr_rdapnum_target (kind, asn) VALUES ('asn', $1)", 1000+i)
	}
	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for _, w := range []string{w1, w2} {
		wg.Go(func() {
			for {
				c, err := st.Claim(ctx, w, lease, interval)
				if err != nil || c == nil {
					return
				}
				mu.Lock()
				seen[c.UUID]++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(seen) != 40 {
		t.Errorf("alvos pegos = %d", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("%s pego %d vezes", id, n)
		}
	}

	// Desligar a instância devolve os alvos dela.
	if err := st.DeleteWorker(ctx, w1); err != nil {
		t.Fatal(err)
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_target WHERE scan_worker = $1::uuid", w1); n != 0 {
		t.Errorf("presos na instância apagada: %d", n)
	}
}

// grab pega um alvo específico para a instância (sem passar pela ordem da
// fila).
func grab(t *testing.T, db *pgxpool.Pool, worker, target string) *Claim {
	t.Helper()
	c := &Claim{}
	var asn *int64
	var prefix *netip.Prefix
	err := db.QueryRow(context.Background(), `
		UPDATE registrobr_rdapnum_target SET scan_worker = $1::uuid, scan_started_at = NOW()
		 WHERE uuid = $2::uuid
		RETURNING uuid::text, kind, asn, prefix, rescan, scan_started_at, failures`, worker, target).
		Scan(&c.UUID, &c.Kind, &asn, &prefix, &c.Rescan, &c.StartedAt, &c.Failures)
	if err != nil {
		t.Fatal(err)
	}
	if asn != nil {
		c.ASN = *asn
	}
	if prefix != nil {
		c.Prefix = *prefix
	}
	return c
}

func complete(body []byte) rdap.Response {
	r := rdap.Response{Result: rdap.Complete, HTTPStatus: 200, URL: "https://rdap.registro.br/x", Body: body, Bytes: len(body)}
	r.ServerTime, r.EgressIP, _ = rdap.ReadNotice(body)
	return r
}

// collect grava uma tentativa e, se conclusiva, processa.
func collect(t *testing.T, st *Store, db *pgxpool.Pool, target string, r rdap.Response) (Saved, Processed) {
	t.Helper()
	ctx := context.Background()
	c := grab(t, db, w1, target)
	a := Attempt{Resp: r, RetryIn: time.Minute}
	if r.Result == rdap.Error {
		a.ErrorMsg = "HTTP 503"
	}
	s, err := st.SaveAttempt(ctx, w1, c, a)
	if err != nil {
		t.Fatal(err)
	}
	var p Processed
	if r.Result.Conclusive() {
		if p, err = st.Process(ctx, s.ResponseUUID); err != nil {
			t.Fatal(err)
		}
	}
	return s, p
}

func TestResponsesAndProcessing(t *testing.T) {
	st, admin := setup(t)
	newWorker(t, st, w1)
	var target string
	_ = admin.QueryRow(context.Background(),
		"INSERT INTO registrobr_rdapnum_target (kind, asn, rescan) VALUES ('asn', 61610, true) RETURNING uuid::text").Scan(&target)
	full := fixture(t, "autnum-61610.json")

	// Completa → linha e processadas.
	s1, p := collect(t, st, admin, target, complete(full))
	if !s1.Changed || !p.Changed || p.Kind != "autnum" {
		t.Fatalf("primeira = %+v %+v", s1, p)
	}
	if v := text(t, admin, "SELECT response_uuid::text FROM registrobr_rdapnum_target WHERE uuid = $1::uuid", target); v != s1.ResponseUUID {
		t.Errorf("response_uuid = %s", v)
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_target WHERE rescan OR failures > 0 OR last_result <> 1 OR last_scan_at IS NULL"); n != 0 {
		t.Error("estado do alvo depois da completa")
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_response WHERE egress_ip = '192.0.2.1' AND server_time IS NOT NULL AND parser_version = $1", parse.Version); n != 1 {
		t.Error("resposta sem aviso ou sem processar")
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_autnum a JOIN registrobr_rdapnum_entity e ON e.uuid = a.registrant_uuid WHERE a.asn = 61610 AND e.document_digits = '35980592000130'"); n != 1 {
		t.Error("autnum sem titular")
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_link"); n != 4 {
		t.Errorf("ligações = %d", n)
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_link l JOIN registrobr_rdapnum_entity v ON v.uuid = l.via_uuid WHERE v.handle = '35980592000130'"); n != 1 {
		t.Errorf("contato aninhado = %d", n)
	}
	updated := text(t, admin, "SELECT updated_at::text FROM registrobr_rdapnum_entity WHERE handle = 'ROBEL308'")

	// A mesma resposta noutra hora e noutro IP: seen_count 2, sem linha nova,
	// e o processamento não muda nada (só o response_uuid andaria).
	other := []byte(strings.Replace(string(full), "2026-10-21T17:36:01-03:00 - 192.0.2.1", "2026-10-21T18:00:00-03:00 - 198.51.100.9", 1))
	s2, p := collect(t, st, admin, target, complete(other))
	if s2.Changed || s2.ResponseUUID != s1.ResponseUUID || p.Changed {
		t.Errorf("igual = %+v %+v", s2, p)
	}
	if n := count(t, admin, "SELECT seen_count FROM registrobr_rdapnum_response WHERE uuid = $1::uuid", s1.ResponseUUID); n != 2 {
		t.Errorf("seen_count = %d", n)
	}
	if u := text(t, admin, "SELECT updated_at::text FROM registrobr_rdapnum_entity WHERE handle = 'ROBEL308'"); u != updated {
		t.Errorf("updated_at andou sem mudança: %s → %s", updated, u)
	}

	// Truncada: outra linha, sem processar, sem mudar response_uuid nem o
	// e-mail; falhas e espera.
	trunc := fixture(t, "autnum-61610-truncated.json")
	tr := complete(trunc)
	tr.Result = rdap.Truncated
	s3, _ := collect(t, st, admin, target, tr)
	if !s3.Changed || s3.ResponseUUID == s1.ResponseUUID {
		t.Errorf("truncada = %+v", s3)
	}
	if v := text(t, admin, "SELECT response_uuid::text FROM registrobr_rdapnum_target WHERE uuid = $1::uuid", target); v != s1.ResponseUUID {
		t.Error("a truncada virou a resposta atual")
	}
	if n := count(t, admin, `SELECT count(*) FROM registrobr_rdapnum_target WHERE failures = 1 AND last_result = 2
		AND retry_at BETWEEN NOW() + interval '50 seconds' AND NOW() + interval '70 seconds' AND scan_worker IS NULL`); n != 1 {
		t.Error("espera depois da truncada")
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_response WHERE result = 2 AND processed_at IS NULL AND parser_version IS NULL"); n != 1 {
		t.Error("truncada processada")
	}
	if e := text(t, admin, "SELECT email FROM registrobr_rdapnum_entity WHERE handle = 'ROBEL308'"); e != "contato@example.com" {
		t.Errorf("e-mail = %s", e)
	}
	// Erro: sem linha em _response, last_error, falhas 2.
	collect(t, st, admin, target, rdap.Response{Result: rdap.Error, HTTPStatus: 503})
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_target WHERE failures = 2 AND last_error = 'HTTP 503' AND last_http_status = 503"); n != 1 {
		t.Error("estado depois do erro")
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_response"); n != 2 {
		t.Errorf("respostas = %d", n)
	}

	// A → B → A: três linhas completas; o contato trocado regrava as ligações
	// e o ROBEL308 fica órfão (sai na manutenção).
	b := []byte(strings.ReplaceAll(string(full), "ROBEL308", "OUTRO1"))
	sB, p := collect(t, st, admin, target, complete(b))
	if !sB.Changed || !p.Changed {
		t.Errorf("B = %+v %+v", sB, p)
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_target WHERE failures = 0 AND last_error IS NULL AND retry_at IS NULL"); n != 1 {
		t.Error("a completa não zerou as falhas")
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_link l JOIN registrobr_rdapnum_entity e ON e.uuid = l.entity_uuid WHERE e.handle = 'OUTRO1'"); n != 3 {
		t.Errorf("ligações do OUTRO1 = %d", n)
	}
	res, err := st.Derive(context.Background(), nil, false)
	if err != nil || res.EntitiesRemoved != 1 {
		t.Errorf("órfã = %+v %v", res, err)
	}
	sA, _ := collect(t, st, admin, target, complete(full))
	if !sA.Changed || sA.ResponseUUID == s1.ResponseUUID {
		t.Errorf("A de novo = %+v", sA)
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_response WHERE result = 1"); n != 3 {
		t.Errorf("completas = %d", n)
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_entity WHERE handle = 'ROBEL308'"); n != 1 {
		t.Error("ROBEL308 não voltou")
	}

	// Processar uma resposta que não é mais a atual: marca e não grava.
	p, err = st.Process(context.Background(), sB.ResponseUUID)
	if err != nil || !p.Stale {
		t.Errorf("velha = %+v %v", p, err)
	}

	// Subir o PARSER_VERSION: a atual aparece para reprocessar.
	exec(t, admin, "UPDATE registrobr_rdapnum_response SET parser_version = 0")
	ids, err := st.PendingProcess(context.Background(), parse.Version, 100)
	if err != nil || !slices.Equal(ids, []string{sA.ResponseUUID}) {
		t.Errorf("pendentes = %v %v", ids, err)
	}

	// 404: apaga o ASN e as ligações.
	s4, p := collect(t, st, admin, target, rdap.Response{Result: rdap.NotFound, HTTPStatus: 404, URL: "u"})
	if !s4.Changed || !p.Changed || p.Kind != "not_found" {
		t.Errorf("404 = %+v %+v", s4, p)
	}
	if n := count(t, admin, "SELECT (SELECT count(*) FROM registrobr_rdapnum_autnum) + (SELECT count(*) FROM registrobr_rdapnum_link)"); n != 0 {
		t.Errorf("sobrou dado do ASN: %d", n)
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_response WHERE result = 3 AND body IS NULL AND sha256 = 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'"); n != 1 {
		t.Error("404 cru")
	}
}

func TestNetworkProcessing(t *testing.T) {
	st, admin := setup(t)
	newWorker(t, st, w1)
	var t24, t21 string
	_ = admin.QueryRow(context.Background(),
		"INSERT INTO registrobr_rdapnum_target (kind, prefix) VALUES ('prefix', '200.225.48.0/24') RETURNING uuid::text").Scan(&t24)
	_ = admin.QueryRow(context.Background(),
		"INSERT INTO registrobr_rdapnum_target (kind, prefix, source) VALUES ('prefix', '200.225.48.0/21', 'cgibr') RETURNING uuid::text").Scan(&t21)
	body := fixture(t, "ip-200.225.48.0-21.json")

	collect(t, st, admin, t24, complete(body))
	if v := text(t, admin, "SELECT prefix::text FROM registrobr_rdapnum_network WHERE target_uuid = $1::uuid", t24); v != "200.225.48.0/21" {
		t.Errorf("bloco = %s", v)
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_reverse WHERE prefix IS NOT NULL AND jsonb_array_length(nameservers) > 0"); n != 1 {
		t.Error("reverso")
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_network WHERE prefix >>= '200.225.49.7'::cidr AND asn = 61610 AND family = 4 AND status = '{active}'"); n != 1 {
		t.Error("busca por IP")
	}
	// O /21 do cgibr devolve o mesmo bloco: a mesma linha, agora dele.
	_, p := collect(t, st, admin, t21, complete(body))
	if p.Changed {
		t.Errorf("mesmo bloco por outro alvo mudou dado: %+v", p)
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_network WHERE target_uuid = $1::uuid", t21); n != 1 {
		t.Error("o bloco não passou para o último alvo")
	}

	// Bloco cuja faixa não é CIDR: recusado, o dado anterior continua.
	_, p = collect(t, st, admin, t21, complete(fixture(t, "synthetic-ip-block-not-cidr.json")))
	if p.Refused == nil {
		t.Errorf("não recusou: %+v", p)
	}
	if v := text(t, admin, "SELECT process_error FROM registrobr_rdapnum_response WHERE process_error IS NOT NULL"); v != "faixa não é CIDR: 203.0.113.0-203.0.113.191" {
		t.Errorf("process_error = %s", v)
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_network"); n != 1 {
		t.Error("o dado anterior saiu")
	}

	// IPv6 com a faixa reversa sintética que não é CIDR.
	var t6 string
	_ = admin.QueryRow(context.Background(),
		"INSERT INTO registrobr_rdapnum_target (kind, prefix) VALUES ('prefix', '198.51.100.0/24') RETURNING uuid::text").Scan(&t6)
	collect(t, st, admin, t6, complete(fixture(t, "synthetic-ip-reverse-not-cidr.json")))
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_reverse WHERE prefix IS NULL AND host(end_address) = '198.51.100.191'"); n != 1 {
		t.Error("reverso não CIDR")
	}
	// O mesmo de novo: os reversos não mudam.
	_, p = collect(t, st, admin, t6, complete(fixture(t, "synthetic-ip-reverse-not-cidr.json")))
	if p.Changed {
		t.Errorf("reprocessar igual mudou: %+v", p)
	}
}

func TestMarkRescan(t *testing.T) {
	st, admin := setup(t)
	exec(t, admin, "INSERT INTO registrobr_rdapnum_target (kind, asn) VALUES ('asn', 61610)")
	exec(t, admin, "INSERT INTO registrobr_rdapnum_target (kind, prefix, active) VALUES ('prefix', '200.225.48.0/21', false)")
	ctx := context.Background()
	if ok, err := st.MarkRescan(ctx, "asn", "61610"); !ok || err != nil {
		t.Errorf("asn = %v %v", ok, err)
	}
	if ok, _ := st.MarkRescan(ctx, "prefix", "200.225.48.0/21"); ok {
		t.Error("inativo marcado")
	}
	if ok, _ := st.MarkRescan(ctx, "asn", "1"); ok {
		t.Error("inexistente marcado")
	}
	if n := count(t, admin, "SELECT count(*) FROM registrobr_rdapnum_target WHERE rescan"); n != 1 {
		t.Errorf("rescan = %d", n)
	}
}
