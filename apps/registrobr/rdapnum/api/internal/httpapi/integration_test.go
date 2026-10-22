//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/refresh"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/store"
)

// root é a raiz do repositório, a partir deste pacote.
var root = filepath.Join("..", "..", "..", "..", "..", "..")

// setupDB sobe um PG18 com as migrations de central/ e registrobr_rdapnum/ e
// devolve o Store da API e um pool administrativo (que faz o papel do
// coletor).
func setupDB(t *testing.T) (*store.Store, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pg, err := postgres.Run(ctx, "postgres:18-trixie",
		postgres.WithDatabase("badblock"), postgres.WithUsername("postgres"), postgres.WithPassword("pg"),
		postgres.BasicWaitStrategies())
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
	for _, dir := range []string{"central", "registrobr_rdapnum"} {
		files, _ := filepath.Glob(filepath.Join(root, "database", "postgres", dir, "*.sql"))
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
	st, err := store.Open(ctx, url, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st, admin
}

func setupValkey(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.Run(ctx, "valkey/valkey:9-alpine",
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("6379/tcp")))
	testcontainers.CleanupContainer(t, c)
	if err != nil {
		t.Fatal(err)
	}
	ep, err := c.PortEndpoint(ctx, "6379/tcp", "")
	if err != nil {
		t.Fatal(err)
	}
	return "redis://" + ep + "/0"
}

func mustExec(t *testing.T, db *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := db.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", strings.Join(strings.Fields(sql), " "), err)
	}
}

func scalar(t *testing.T, db *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	var v string
	if err := db.QueryRow(context.Background(), sql, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return v
}

// fixture lê uma resposta real do coletor (collector/testdata, com a pessoa
// física trocada e o IP do aviso em 192.0.2.1).
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "apps", "registrobr", "rdapnum", "collector", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const sha = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

// load grava o que o coletor gravaria depois de processar as fixtures do
// AS61610 e dos três blocos dele (o mapeamento de dados-processados.md), mais
// alvos de volume para os planos, uma derivação, instâncias (com IP de saída,
// que a API não pode mostrar) e a linha de jobs. As datas são relativas ao
// NOW() do banco onde importa (overdue, alive).
func load(t *testing.T, admin *pgxpool.Pool) map[string]string {
	t.Helper()
	ids := map[string]string{}
	wk := scalar(t, admin, `INSERT INTO registrobr_rdapnum_worker (host, version, rate_limit, proxy, requests, complete,
		egress_ip, egress_ips) VALUES ('vivo', 'test', 32, true, 10, 9, '198.51.100.9', 3) RETURNING uuid::text`)
	mustExec(t, admin, `INSERT INTO registrobr_rdapnum_worker (host, version, rate_limit, proxy, egress_ip, updated_at)
		VALUES ('morto', 'test', 32, false, '198.51.100.10', NOW() - interval '2 days')`)

	// Volume: 3.000 ASNs e 3.000 blocos sem dado processado.
	mustExec(t, admin, `INSERT INTO registrobr_rdapnum_target (kind, asn, source)
		SELECT 'asn', 100000 + i, 'cgibr' FROM generate_series(1, 3000) i`)
	mustExec(t, admin, `INSERT INTO registrobr_rdapnum_target (kind, prefix, source)
		SELECT 'prefix', set_masklen('100.64.0.0'::inet + i * 256, 24)::cidr, 'cgibr' FROM generate_series(1, 3000) i`)

	target := func(name, sql string) {
		ids[name] = scalar(t, admin, sql)
	}
	target("t61610", `INSERT INTO registrobr_rdapnum_target (kind, asn, source, last_scan_at, last_attempt_at, last_result, last_http_status)
		VALUES ('asn', 61610, 'cgibr', '2026-10-21T20:36:01Z', '2026-10-21T20:36:01Z', 1, 200) RETURNING uuid::text`)
	target("t200", `INSERT INTO registrobr_rdapnum_target (kind, prefix, source, last_scan_at, last_attempt_at, last_result)
		VALUES ('prefix', '200.225.48.0/21', 'cgibr', NOW() - interval '1 minute', NOW() - interval '1 minute', 1) RETURNING uuid::text`)
	target("t2804", `INSERT INTO registrobr_rdapnum_target (kind, prefix, source, last_scan_at, last_attempt_at, last_result, retry_at, failures)
		VALUES ('prefix', '2804:8ae0::/32', 'cgibr', '2026-10-21T20:59:37Z', NOW(), 2, NOW() + interval '1 hour', 1) RETURNING uuid::text`)
	target("t187", `INSERT INTO registrobr_rdapnum_target (kind, prefix, source, last_scan_at, last_attempt_at, last_result)
		VALUES ('prefix', '187.87.28.0/22', 'cgibr', '2026-10-21T20:40:00Z', '2026-10-21T20:40:00Z', 1) RETURNING uuid::text`)
	target("t64500", `INSERT INTO registrobr_rdapnum_target (kind, asn) VALUES ('asn', 64500) RETURNING uuid::text`)
	target("t64501", `INSERT INTO registrobr_rdapnum_target (kind, asn, source, active, last_scan_at, last_attempt_at, last_result, last_http_status)
		VALUES ('asn', 64501, 'cgibr', false, NOW() - interval '1 day', NOW() - interval '1 day', 3, 404) RETURNING uuid::text`)

	resp := func(name, target string, result, status int, body *string, fetched string) {
		ids[name] = scalar(t, admin, `INSERT INTO registrobr_rdapnum_response (target_uuid, result, url, http_status, body,
			sha256, bytes, egress_ip, server_time, worker, fetched_at, last_seen_at, parser_version, processed_at)
			VALUES ($1::uuid, $2::smallint, 'https://rdap.registro.br/x', $3, $4::jsonb, $5, coalesce(octet_length($4::text), 0), '192.0.2.1',
			        NOW(), $6::uuid, $7::timestamptz, $7::timestamptz,
			        CASE WHEN $2::smallint IN (1, 3) THEN 1 END, CASE WHEN $2::smallint IN (1, 3) THEN NOW() END)
			RETURNING uuid::text`, target, result, status, body, sha, wk, fetched)
	}
	trunc, b61610 := fixture(t, "autnum-61610-truncated.json"), fixture(t, "autnum-61610.json")
	b200, b2804, b187 := fixture(t, "ip-200.225.48.0-21.json"), fixture(t, "ip-2804-8ae0--32.json"), fixture(t, "ip-187.87.28.0-22.json")
	resp("r61610t", ids["t61610"], 2, 200, &trunc, "2026-10-21T20:35:12Z")
	resp("r61610", ids["t61610"], 1, 200, &b61610, "2026-10-21T20:36:01Z")
	resp("r200", ids["t200"], 1, 200, &b200, "2026-10-21T20:36:08Z")
	resp("r2804", ids["t2804"], 1, 200, &b2804, "2026-10-21T20:59:37Z")
	resp("r187", ids["t187"], 1, 200, &b187, "2026-10-21T20:40:00Z")
	resp("r64501", ids["t64501"], 3, 404, nil, "2026-10-20T10:00:00Z")
	for tg, r := range map[string]string{"t61610": "r61610", "t200": "r200", "t2804": "r2804", "t187": "r187", "t64501": "r64501"} {
		mustExec(t, admin, `UPDATE registrobr_rdapnum_target SET response_uuid = $2::uuid WHERE uuid = $1::uuid`, ids[tg], ids[r])
	}

	// Entidades: o titular e o contato (o mesmo handle em todos os objetos).
	ids["elea"] = scalar(t, admin, `INSERT INTO registrobr_rdapnum_entity (handle, kind, name, country, document, document_type,
		legal_representative, registered_at, changed_at, response_uuid)
		VALUES ('35980592000130', 'org', 'ELEA DATA CENTERS', 'BR', '35.980.592/0001-30', 'cnpj', 'REPRESENTANTE EXEMPLO',
		        '2023-02-06T21:58:30Z', '2026-04-22T15:11:18Z', $1::uuid) RETURNING uuid::text`, ids["r61610"])
	ids["robel"] = scalar(t, admin, `INSERT INTO registrobr_rdapnum_entity (handle, kind, name, email, country, registered_at, changed_at)
		VALUES ('ROBEL308', 'individual', 'CONTATO EXEMPLO', 'contato@example.com', 'BR', '2023-02-08T17:39:14Z', '2026-07-07T18:30:11Z')
		RETURNING uuid::text`)

	ids["a61610"] = scalar(t, admin, `INSERT INTO registrobr_rdapnum_autnum (asn, target_uuid, response_uuid, handle, name, type, country,
		registrant_uuid, registered_at, changed_at)
		VALUES (61610, $1::uuid, $2::uuid, '61610', 'ASN61610', 'DIRECT ALLOCATION', 'BR', $3::uuid,
		        '2023-02-27T15:58:28Z', '2023-02-27T15:58:28Z') RETURNING uuid::text`, ids["t61610"], ids["r61610"], ids["elea"])
	network := func(name, prefix, target, response, handle, nm, reg, chg string) {
		ids[name] = scalar(t, admin, `INSERT INTO registrobr_rdapnum_network (prefix, target_uuid, response_uuid, handle, name, type,
			country, status, asn, registrant_uuid, registered_at, changed_at)
			VALUES ($1::cidr, $2::uuid, $3::uuid, $4, $5, 'ALLOCATED', 'BR', '{active}', 61610, $6::uuid, $7::timestamptz, $8::timestamptz)
			RETURNING uuid::text`, prefix, ids[target], ids[response], handle, nm, ids["elea"], reg, chg)
	}
	network("n200", "200.225.48.0/21", "t200", "r200", "200.225.48.0/21", "462539", "1999-11-18T12:00:00Z", "2023-07-19T11:05:57Z")
	network("n2804", "2804:8ae0::/32", "t2804", "r2804", "2804:8ae0::/32", "452026", "2023-02-24T11:25:13Z", "2023-02-27T16:03:00Z")
	network("n187", "187.87.28.0/22", "t187", "r187", "187.87.28.0/22", "462540", "2007-01-01T00:00:00Z", "2023-07-19T11:05:57Z")

	link := func(entity, col, object, role string, via *string) {
		mustExec(t, admin, `INSERT INTO registrobr_rdapnum_link (entity_uuid, `+col+`, role, via_uuid)
			VALUES ($1::uuid, $2::uuid, $3, $4::uuid)`, ids[entity], ids[object], role, via)
	}
	elea := ids["elea"]
	link("elea", "autnum_uuid", "a61610", "registrant", nil)
	link("robel", "autnum_uuid", "a61610", "administrative", &elea)
	link("robel", "autnum_uuid", "a61610", "administrative", nil)
	link("robel", "autnum_uuid", "a61610", "abuse", nil)
	for _, n := range []string{"n200", "n2804", "n187"} {
		link("elea", "network_uuid", n, "registrant", nil)
		link("robel", "network_uuid", n, "administrative", &elea)
		if n != "n187" {
			link("robel", "network_uuid", n, "technical", nil)
			link("robel", "network_uuid", n, "abuse", nil)
		}
	}
	ns := func(names ...string) string {
		var out []map[string]any
		for _, n := range names {
			out = append(out, map[string]any{"name": n, "status": []string{"ns aa"},
				"checked_at": "2026-10-21T12:00:00Z", "last_ok_at": "2026-10-21T12:00:00Z"})
		}
		b, _ := json.Marshal(out)
		return string(b)
	}
	mustExec(t, admin, `INSERT INTO registrobr_rdapnum_reverse (network_uuid, start_address, end_address, prefix, nameservers, signed,
		registered_at, changed_at) VALUES ($1::uuid, '200.225.48.0', '200.225.48.255', '200.225.48.0/24', $2::jsonb, false,
		'2024-09-27T15:31:56Z', '2024-09-27T15:31:56Z')`, ids["n200"], ns("keenan.ns.cloudflare.com", "sureena.ns.cloudflare.com"))
	mustExec(t, admin, `INSERT INTO registrobr_rdapnum_reverse (network_uuid, start_address, end_address, prefix, nameservers, signed,
		registered_at, changed_at) VALUES ($1::uuid, '2804:8ae0:4000::', '2804:8ae0:40ff:ffff:ffff:ffff:ffff:ffff', '2804:8ae0:4000::/40',
		$2::jsonb, false, '2026-07-07T19:46:53Z', '2026-07-07T19:46:53Z')`, ids["n2804"],
		ns("liberty.ns.cloudflare.com", "steven.ns.cloudflare.com"))

	mustExec(t, admin, `INSERT INTO registrobr_rdapnum_run (status, source, source_version, source_updated_at, asns, prefixes,
		inserted, not_in_source, started_at, created_at)
		VALUES (1, 'cgibr', uuidv7(), NOW() - interval '2 days', 9134, 21991, 31125, 0, NOW(), NOW() - interval '1 day'),
		       (1, 'cgibr', uuidv7(), NOW() - interval '1 day', 9135, 21992, 2, 1, NOW(), NOW()),
		       (0, 'cgibr', uuidv7(), NOW(), NULL, NULL, NULL, NULL, NOW(), NOW() + interval '1 second')`)
	mustExec(t, admin, `INSERT INTO jobs (app, last_sync_at, last_check_at) VALUES ('collector-registrobr-rdapnum', NOW(), NOW())`)
	mustExec(t, admin, "ANALYZE")
	return ids
}

func TestQueriesAndIndexes(t *testing.T) {
	st, admin := setupDB(t)
	ctx := context.Background()
	if ok, err := st.HasTargets(ctx); err != nil || ok {
		t.Fatalf("banco vazio: HasTargets = %v, %v", ok, err)
	}
	ids := load(t, admin)
	if ok, _ := st.HasTargets(ctx); !ok {
		t.Fatal("HasTargets deveria ser true")
	}

	a, err := st.ASN(ctx, 61610)
	if err != nil || a.Autnum == nil || *a.Autnum.Name != "ASN61610" || a.Registrant == nil ||
		*a.Registrant.DocumentDigits != "35980592000130" || len(a.Links) != 4 || len(a.Networks) != 3 ||
		a.Networks[0].Prefix.String() != "187.87.28.0/22" || a.Networks[2].Prefix.String() != "2804:8ae0::/32" ||
		*a.Target.ResponseUUID != ids["r61610"] {
		t.Errorf("ASN = %+v, %v", a, err)
	}
	if a, err := st.ASN(ctx, 64500); err != nil || a.Autnum != nil || a.Target.Source != "static" {
		t.Errorf("ASN sem dado = %+v, %v", a, err)
	}
	if _, err := st.ASN(ctx, 1); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ASN fora = %v", err)
	}

	n, err := st.Network(ctx, netip.MustParsePrefix("200.225.48.10/32"))
	if err != nil || n.Network.Prefix.String() != "200.225.48.0/21" || len(n.Links) != 4 || len(n.Reverses) != 1 ||
		len(n.Reverses[0].Nameservers) != 2 || n.Reverses[0].Start != "200.225.48.0" || n.Target.UUID != ids["t200"] {
		t.Errorf("Network = %+v, %v", n, err)
	}
	if n, err := st.Network(ctx, netip.MustParsePrefix("2804:8ae0:4000::/48")); err != nil || n.Network.Prefix.String() != "2804:8ae0::/32" ||
		n.Reverses[0].Prefix == nil || n.Reverses[0].End != "2804:8ae0:40ff:ffff:ffff:ffff:ffff:ffff" {
		t.Errorf("Network IPv6 = %+v, %v", n, err)
	}
	if _, err := st.Network(ctx, netip.MustParsePrefix("8.8.8.0/24")); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Network fora = %v", err)
	}

	if tg, err := st.TargetPrefix(ctx, netip.MustParsePrefix("200.225.48.0/21")); err != nil || tg.UUID != ids["t200"] {
		t.Errorf("TargetPrefix exato = %+v, %v", tg, err)
	}
	if tg, err := st.TargetPrefix(ctx, netip.MustParsePrefix("200.225.49.0/24")); err != nil || tg.UUID != ids["t200"] {
		t.Errorf("TargetPrefix pelo bloco = %+v, %v", tg, err)
	}
	if tg, err := st.TargetPrefix(ctx, netip.MustParsePrefix("100.64.1.0/24")); err != nil || tg.LastScanAt != nil {
		t.Errorf("TargetPrefix sem bloco = %+v, %v", tg, err)
	}

	d, err := st.Document(ctx, "35980592000130")
	if err != nil || len(d.Entities) != 1 || len(d.ASNs) != 1 || d.ASNs[0].LastScanAt == nil || len(d.Networks) != 3 ||
		d.Networks[0].Prefix.String() != "187.87.28.0/22" {
		t.Errorf("Document = %+v, %v", d, err)
	}
	c, err := st.Contact(ctx, "ROBEL308")
	if err != nil || len(c.Objects) != 10 || c.Objects[0].Kind != "asn" || c.Objects[0].Via != nil || c.Objects[0].Role != "abuse" ||
		c.Objects[2].Via == nil || c.Objects[3].Prefix == nil || c.Objects[3].Prefix.String() != "187.87.28.0/22" {
		t.Errorf("Contact = %+v, %v", c, err)
	}
	r, err := st.Response(ctx, ids["r61610"])
	if err != nil || r.Body == nil || !strings.Contains(*r.Body, "192.0.2.1") || r.Result != 1 {
		t.Errorf("Response = %+v, %v", r, err)
	}
	h, err := st.History(ctx, ids["t61610"])
	if err != nil || len(h) != 2 || h[0].UUID != ids["r61610"] || !h[0].Processed || h[1].Processed {
		t.Errorf("History = %+v, %v", h, err)
	}

	ts, err := st.TargetStats(ctx, 168*time.Hour)
	if err != nil || ts.Total != 6006 || ts.ASN != 3003 || ts.Prefix != 3003 || ts.Active != 6005 || ts.Static != 1 ||
		ts.NeverScanned != 6001 || ts.Waiting != 1 || ts.Complete != 3 || ts.Truncated != 1 || ts.NotFound != 1 || ts.OldestScanAt == nil {
		t.Errorf("TargetStats = %+v, %v", ts, err)
	}
	rs, err := st.ResponseStats(ctx)
	if err != nil || rs.Total != 6 || rs.Truncated != 1 || rs.NotFound != 1 || rs.Pending != 0 {
		t.Errorf("ResponseStats = %+v, %v", rs, err)
	}
	mustExec(t, admin, `UPDATE registrobr_rdapnum_response SET processed_at = NULL, parser_version = NULL WHERE uuid = $1::uuid`, ids["r187"])
	if rs, _ := st.ResponseStats(ctx); rs.Pending != 1 {
		t.Errorf("pending = %d, quero 1", rs.Pending)
	}
	ds, err := st.DataStats(ctx)
	if err != nil || *ds != (store.DataStats{Autnum: 1, Network: 3, Entity: 2, Reverse: 2}) {
		t.Errorf("DataStats = %+v, %v", ds, err)
	}
	ders, err := st.Derivations(ctx)
	if err != nil || len(ders) != 1 || *ders[0].ASNs != 9135 {
		t.Errorf("Derivations = %+v, %v", ders, err)
	}
	ws, err := st.Workers(ctx)
	if err != nil || len(ws) != 1 || !ws[0].Alive || !ws[0].Proxy || ws[0].EgressIPs != 3 {
		t.Errorf("Workers = %+v, %v", ws, err)
	}
	if j, err := st.Job(ctx); err != nil || j == nil || j.LastSyncAt == nil {
		t.Errorf("Job = %+v, %v", j, err)
	}
	mustExec(t, admin, "INSERT INTO registrobr_rdapnum_config (key, value) VALUES ('scan_interval', '12h')")
	if cfg, err := st.Config(ctx); err != nil || cfg["scan_interval"] != "12h" {
		t.Errorf("Config = %v, %v", cfg, err)
	}

	// Cada consulta usa o índice da spec (dados-processados.md), com seq scan
	// desligado (vale com a tabela pequena do teste).
	p := "registrobr_rdapnum_"
	for q, want := range map[string][]string{
		`SELECT t.uuid, a.name FROM registrobr_rdapnum_target t LEFT JOIN registrobr_rdapnum_autnum a ON a.asn = t.asn WHERE t.asn = 61610`:              {"uq_" + p + "target_asn", "uq_" + p + "autnum_asn"},
		`SELECT l.role FROM registrobr_rdapnum_link l WHERE l.autnum_uuid = '` + ids["a61610"] + `'`:                                                     {"ix_" + p + "link_autnum"},
		`SELECT prefix, asn, type, status FROM registrobr_rdapnum_network WHERE asn = 61610 ORDER BY family, prefix`:                                     {"ix_" + p + "network_asn"},
		`SELECT n.uuid FROM registrobr_rdapnum_network n WHERE n.prefix >>= '200.225.48.10/32'::cidr ORDER BY masklen(n.prefix) DESC LIMIT 1`:            {"ix_" + p + "network_prefix_gist"},
		`SELECT l.role FROM registrobr_rdapnum_link l WHERE l.network_uuid = '` + ids["n200"] + `'`:                                                      {"ix_" + p + "link_network"},
		`SELECT prefix FROM registrobr_rdapnum_reverse WHERE network_uuid = '` + ids["n200"] + `' ORDER BY start_address, end_address`:                   {"ix_" + p + "reverse_network"},
		`SELECT e.handle FROM registrobr_rdapnum_entity e WHERE e.document_digits = '35980592000130'`:                                                    {"ix_" + p + "entity_document_digits"},
		`SELECT l.network_uuid FROM registrobr_rdapnum_link l WHERE l.entity_uuid = ANY(ARRAY['` + ids["elea"] + `']::uuid[]) AND l.role = 'registrant'`: {"_link"},
		`SELECT e.uuid FROM registrobr_rdapnum_entity e WHERE e.handle = 'ROBEL308'`:                                                                     {"uq_" + p + "entity_handle"},
		`SELECT t.uuid FROM registrobr_rdapnum_target t WHERE t.prefix = '200.225.48.0/21'::cidr`:                                                        {"uq_" + p + "target_prefix"},
		`SELECT uuid FROM registrobr_rdapnum_response WHERE target_uuid = '` + ids["t61610"] + `' ORDER BY fetched_at DESC, uuid DESC`:                   {"ix_" + p + "response_target"},
		`SELECT body FROM registrobr_rdapnum_response WHERE uuid = '` + ids["r61610"] + `'`:                                                              {p + "response_pkey"},
		`SELECT uuid FROM registrobr_rdapnum_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1`:                                                      {"ix_" + p + "run_applied"},
		`SELECT last_sync_at FROM jobs WHERE app = 'collector-registrobr-rdapnum'`:                                                                       {"uq_jobs_app"},
	} {
		plan := explain(t, admin, q)
		// O vínculo por entidade serve-se do ix_..._link_entity ou do
		// uq_..._link (que começa por entity_uuid); basta não ser seq scan.
		if strings.Contains(plan, "Seq Scan") {
			t.Errorf("seq scan: %s\n→ %s", q, plan)
		}
		for _, idx := range want {
			if !strings.Contains(plan, idx) {
				t.Errorf("sem %s: %s\n→ %s", idx, q, plan)
			}
		}
		t.Logf("%s\n→ %s", strings.Join(strings.Fields(q), " "), plan)
	}
}

// explain devolve o plano numa linha, com seq scan desligado.
func explain(t *testing.T, admin *pgxpool.Pool, q string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(ctx, "RESET enable_seqscan") }()
	rows, err := conn.Query(ctx, "EXPLAIN "+q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		_ = rows.Scan(&line)
		plan = append(plan, strings.TrimSpace(line))
	}
	return strings.Join(plan, " / ")
}

// TestEndToEnd passa pelas rotas com o banco e o Valkey reais: as respostas
// formadas a partir das fixtures, a resposta crua sem o IP de saída, o cache
// sem versão com TTL e o POST pondo o pedido na fila.
func TestEndToEnd(t *testing.T) {
	st, admin := setupDB(t)
	ids := load(t, admin)
	url := setupValkey(t)
	ctx := context.Background()

	opts, _ := redis.ParseURL(url)
	rdb := redis.NewClient(opts)
	defer rdb.Close()
	c, err := cache.NewValkey(url, 300*time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	v, err := refresh.NewValkey(url)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	watcher := dataset.NewWatcher(st, time.Minute, slog.New(slog.DiscardHandler))
	if err := watcher.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	rip, _ := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	h := New(st, watcher, c, refresh.NewRequester(v, 10*time.Minute, 1000), rip,
		slog.New(slog.DiscardHandler), Config{BasePath: base, Version: "test"}).Handler()

	// ASN: o formato de api-rotas.md, com os dados das fixtures.
	rec := do(h, "GET", base+"/asn/61610")
	if rec.Code != 200 || rec.Header().Get("X-Cache") != "MISS" {
		t.Fatalf("asn = %d %s", rec.Code, rec.Body)
	}
	t.Logf("GET /asn/61610 → %s", rec.Body)
	want := compact(t, `{
  "asn": 61610, "handle": "61610", "name": "ASN61610", "type": "DIRECT ALLOCATION", "country": "BR",
  "registered_at": "2023-02-27T15:58:28Z", "changed_at": "2023-02-27T15:58:28Z",
  "registrant": {"handle": "35980592000130", "kind": "org", "name": "ELEA DATA CENTERS", "email": null, "country": "BR",
    "document": "35.980.592/0001-30", "document_type": "cnpj", "document_digits": "35980592000130",
    "legal_representative": "REPRESENTANTE EXEMPLO",
    "registered_at": "2023-02-06T21:58:30Z", "changed_at": "2026-04-22T15:11:18Z"},
  "contacts": [
    {`+robelFields+`, "roles": ["abuse", "administrative"], "via": null},
    {`+robelFields+`, "roles": ["administrative"], "via": "35980592000130"}
  ],
  "networks": [
    {"prefix": "187.87.28.0/22", "type": "ALLOCATED", "status": ["active"]},
    {"prefix": "200.225.48.0/21", "type": "ALLOCATED", "status": ["active"]},
    {"prefix": "2804:8ae0::/32", "type": "ALLOCATED", "status": ["active"]}
  ],
  "scan": {"kind": "asn", "target": "61610", "source": "cgibr", "active": true,
    "checked_at": "2026-10-21T20:36:01Z", "attempted_at": "2026-10-21T20:36:01Z",
    "last_result": "complete", "retry_at": null, "response": "`+ids["r61610"]+`"}
}`)
	if rec.Body.String() != want {
		t.Errorf("asn =\n%s\nquero\n%s", rec.Body, want)
	}
	if ttl := rdb.TTL(ctx, keyPrefix+"asn:61610").Val(); ttl <= 0 || ttl > 300*time.Second {
		t.Errorf("TTL da chave = %v", ttl)
	}
	if rec := do(h, "GET", base+"/v1/asn/AS61610"); rec.Header().Get("X-Cache") != "HIT" {
		t.Errorf("v1 = %s", rec.Header().Get("X-Cache"))
	}

	// Bloco pedido como /24: o /21 registrado, com o reverso.
	rec = do(h, "GET", base+"/prefix/200.225.48.0/24")
	got := decode[NetworkResponse](t, rec)
	if got.Prefix != "200.225.48.0/21" || got.EndAddress != "200.225.55.255" || len(got.Reverse) != 1 ||
		*got.Reverse[0].Nameservers[0].Name != "keenan.ns.cloudflare.com" || len(got.Contacts) != 2 ||
		got.Contacts[0].Via != nil || strings.Join(got.Contacts[0].Roles, ",") != "abuse,technical" {
		t.Errorf("prefix = %s", rec.Body)
	}
	t.Logf("GET /prefix/200.225.48.0/24 → %s", rec.Body)
	if got := decode[NetworkResponse](t, do(h, "GET", base+"/ip/2804:8ae0:4000::1")); got.Prefix != "2804:8ae0::/32" ||
		*got.Scan.LastResult != "truncated" || got.Scan.RetryAt == nil || *got.Scan.CheckedAt != "2026-10-21T20:59:37Z" {
		t.Errorf("ip IPv6 = %+v", got)
	}
	// Documento com a barra crua e contato.
	if d := decode[DocumentResponse](t, do(h, "GET", base+"/document/35.980.592/0001-30")); len(d.ASNs) != 1 || len(d.Networks) != 3 {
		t.Errorf("document = %+v", d)
	}
	if c := decode[ContactResponse](t, do(h, "GET", base+"/contact/ROBEL308")); len(c.Objects) != 7 {
		t.Errorf("contact = %+v", c)
	}

	// Resposta crua: a fixture sem a linha "<hora> - 192.0.2.1".
	rec = do(h, "GET", base+"/asn/61610/raw")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/rdap+json" || rec.Header().Get("X-Response-UUID") != ids["r61610"] ||
		strings.Contains(rec.Body.String(), "192.0.2.1") || !strings.Contains(rec.Body.String(), "expressly prohibited.\"]") ||
		!strings.Contains(rec.Body.String(), `"legalRepresentative":"REPRESENTANTE EXEMPLO"`) {
		t.Errorf("raw = %d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil || raw["objectClassName"] != "autnum" {
		t.Errorf("raw ilegível: %v", err)
	}
	if rec := do(h, "GET", base+"/prefix/200.225.48.0/24/raw"); rec.Code != 200 || rec.Header().Get("X-Response-UUID") != ids["r200"] ||
		strings.Contains(rec.Body.String(), "192.0.2.1") {
		t.Errorf("prefix raw = %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", base+"/asn/64501/raw"); rec.Code != 404 {
		t.Errorf("raw de not_found = %d", rec.Code)
	}
	// Histórico: sem egress_ip; a truncada também, sem a linha do IP.
	rec = do(h, "GET", base+"/asn/61610/history")
	hist := decode[HistoryResponse](t, rec)
	if len(hist.Responses) != 2 || hist.Responses[1].Result != "truncated" || strings.Contains(rec.Body.String(), "192.0.2.1") ||
		strings.Contains(rec.Body.String(), "egress") {
		t.Errorf("history = %s", rec.Body)
	}
	rec = do(h, "GET", base+"/response/"+ids["r61610t"])
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "192.0.2.1") || !strings.Contains(rec.Body.String(), "object truncated due to server policy") {
		t.Errorf("response truncada = %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", base+"/response/"+ids["r64501"]); rec.Body.String() != `{"result":"not_found","http_status":404}`+"\n" {
		t.Errorf("response not_found = %s", rec.Body)
	}

	// Atualização: entra na fila com a marca inflight de 60 s.
	rec = do(h, "POST", base+"/asn/61610/refresh")
	if rec.Code != 202 || !decode[RefreshResponse](t, rec).Queued {
		t.Fatalf("refresh = %d %s", rec.Code, rec.Body)
	}
	if ttl := rdb.TTL(ctx, refresh.InflightPrefix+"asn:61610").Val(); ttl <= 50*time.Second || ttl > 60*time.Second {
		t.Errorf("TTL da marca = %v", ttl)
	}
	vals := rdb.LRange(ctx, refresh.QueueKey, 0, -1).Val()
	var req refresh.Request
	if len(vals) != 1 || json.Unmarshal([]byte(vals[0]), &req) != nil || req.Kind != "asn" || req.Target != "61610" ||
		!strings.HasSuffix(req.At, "Z") {
		t.Errorf("fila = %v", vals)
	}
	if ttl := rdb.TTL(ctx, refresh.QueueKey).Val(); ttl != -1 {
		t.Errorf("a fila não tem TTL: %v", ttl)
	}
	if got := decode[RefreshResponse](t, do(h, "POST", base+"/asn/61610/refresh")); got.Queued || *got.Reason != "already_queued" {
		t.Errorf("segundo pedido = %+v", got)
	}
	// Coletado há 1 min: recent, sem pedido.
	if got := decode[RefreshResponse](t, do(h, "POST", base+"/prefix/200.225.49.0/24/refresh")); got.Queued || *got.Reason != "recent" ||
		got.Target != "200.225.48.0/21" {
		t.Errorf("recent = %+v", got)
	}
	if rec := do(h, "POST", base+"/asn/64501/refresh"); rec.Code != 409 {
		t.Errorf("inativo = %d", rec.Code)
	}
	if rec := do(h, "POST", base+"/prefix/2804:8ae0::/32/refresh"); rec.Code != 202 || !decode[RefreshResponse](t, rec).Queued {
		t.Errorf("IPv6 = %d %s", rec.Code, rec.Body)
	}
	if n := rdb.LLen(ctx, refresh.QueueKey).Val(); n != 2 {
		t.Errorf("fila = %d, quero 2", n)
	}

	// /meta: a fila, sem o IP de saída.
	rec = do(h, "GET", base+"/meta")
	m := decode[MetaResponse](t, rec)
	if m.Refresh.Queue == nil || *m.Refresh.Queue != 2 || m.Targets.Total != 6006 || len(m.Workers) != 1 ||
		m.Data.Reverse != 2 || len(m.Derivations) != 1 {
		t.Errorf("meta = %s", rec.Body)
	}
	for _, ip := range []string{"198.51.100.9", "198.51.100.10", "192.0.2.1", "egress_ip\""} {
		if strings.Contains(rec.Body.String(), ip) {
			t.Errorf("o /meta mostra %s", ip)
		}
	}
	t.Logf("GET /meta → %s", rec.Body)
}
