//go:build integration

package syncer

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/source"
	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/store"
	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/testdb"
)

// env reúne o banco, uma cópia editável das fixtures e o syncer.
type env struct {
	t     *testing.T
	ctx   context.Context
	db    testdb.DB
	dir   string
	store *store.Store
	sync  *Syncer
	api   *pgx.Conn
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	db := testdb.Start(t)

	dir := t.TempDir()
	copyDir(t, "../../testdata/sources", dir)

	st, err := store.Open(ctx, db.SyncURL, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)

	api, err := pgx.Connect(ctx, db.APIURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { api.Close(ctx) })

	e := &env{t: t, ctx: ctx, db: db, dir: dir, store: st, api: api}
	e.newSyncer(time.Hour, false)
	return e
}

func (e *env) newSyncer(grace time.Duration, force bool) {
	e.sync = New(Config{
		Sources:          source.Catalog(),
		DefaultInterval:  time.Hour,
		RemovalThreshold: 0.05,
		RemovalGrace:     grace,
		RawDir:           filepath.Join(e.dir, "raw"),
		RawKeep:          2,
		Force:            force,
		SkipMinRecords:   true,
	}, e.store, &fetch.Fetcher{SourcesDir: e.dir}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func (e *env) cycle() CycleResult {
	e.t.Helper()
	res, err := e.sync.Cycle(e.ctx, true)
	if err != nil {
		e.t.Fatalf("ciclo: %v", err)
	}
	return res
}

// query lê um valor pelas views do schema api, como a API faz.
func (e *env) query(sql string, args ...any) string {
	e.t.Helper()
	var v *string
	if err := e.api.QueryRow(e.ctx, sql, args...).Scan(&v); err != nil {
		e.t.Fatalf("%s: %v", sql, err)
	}
	if v == nil {
		return "<null>"
	}
	return *v
}

// editDelegated reescreve um arquivo delegated da fixture aplicando edit às
// linhas de registro e recalcula cabeçalho, summary e .md5.
func (e *env) editDelegated(id string, edit func(records []string) []string) {
	e.t.Helper()
	path := filepath.Join(e.dir, id)
	raw, err := os.ReadFile(path)
	if err != nil {
		e.t.Fatal(err)
	}
	var header []string
	var records []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		f := strings.Split(line, "|")
		switch {
		case header == nil:
			header = f
		case len(f) >= 6 && f[5] == "summary":
		default:
			records = append(records, line)
		}
	}
	records = edit(records)
	counts := map[string]int{}
	for _, r := range records {
		counts[strings.Split(r, "|")[2]]++
	}
	header[3] = fmt.Sprint(len(records))
	rir := header[1]
	lines := []string{strings.Join(header, "|")}
	for _, typ := range []string{"asn", "ipv4", "ipv6"} {
		lines = append(lines, fmt.Sprintf("%s|*|%s|*|%d|summary", rir, typ, counts[typ]))
	}
	lines = append(lines, records...)
	content := strings.Join(lines, "\n") + "\n"
	e.write(id, content)
	sum := md5.Sum([]byte(content))
	e.write(id+".md5", "MD5 ("+id+") = "+hex.EncodeToString(sum[:])+"\n")
}

// setHeaderDate troca o enddate do cabeçalho de um arquivo delegated.
func (e *env) setHeaderDate(id, date string) {
	e.editDelegated(id, func(r []string) []string { return r })
	path := filepath.Join(e.dir, id)
	raw, _ := os.ReadFile(path)
	lines := strings.SplitN(string(raw), "\n", 2)
	f := strings.Split(lines[0], "|")
	f[5] = date
	content := strings.Join(f, "|") + "\n" + lines[1]
	e.write(id, content)
	sum := md5.Sum([]byte(content))
	e.write(id+".md5", "MD5 ("+id+") = "+hex.EncodeToString(sum[:])+"\n")
}

func (e *env) write(name, content string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.dir, name), []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func without(needle string) func([]string) []string {
	return func(records []string) []string {
		var out []string
		for _, r := range records {
			if !strings.Contains(r, needle) {
				out = append(out, r)
			}
		}
		return out
	}
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSyncLifecycle(t *testing.T) {
	e := newEnv(t)

	// 1. Primeira carga: baseline, sem eventos de inserção no histórico.
	res := e.cycle()
	if len(res.Applied) != len(source.Catalog()) || res.Build == nil || !res.Build.Baseline {
		t.Fatalf("primeira carga = %+v", res)
	}
	if got := e.query(`SELECT count(*)::text FROM api.change_log`); got != "0" {
		t.Errorf("baseline não deveria gerar eventos, gerou %s", got)
	}
	if got := e.query(`SELECT name || '|' || name_source || '|' || holder_key || '|' || holder_document FROM api.asn WHERE asn = 61613`); got !=
		"TMSoft Solucoes em Informatica Ltda|nicbr|lacnic:258500|08.030.063/0001-00" {
		t.Errorf("AS61613 = %s", got)
	}
	// ASN estrangeiro listado pelo NIC.br mantém o próprio nome.
	if got := e.query(`SELECT name || '|' || rir FROM api.asn WHERE asn = 8075`); got != "Microsoft Corporation|arin" {
		t.Errorf("AS8075 = %s", got)
	}
	if got := e.query(`SELECT name FROM api.holder WHERE holder_key = 'lacnic:287080'`); got != "Microsoft 272945 Brasil LTDA" {
		t.Errorf("titular dos blocos da Microsoft Brasil = %s", got)
	}
	if got := e.query(`SELECT string_agg(level || ' ' || prefix::text, ', ' ORDER BY masklen(prefix) DESC)
	                     FROM api.prefix WHERE prefix >>= '45.171.60.1' AND removed_at IS NULL`); got !=
		"rir 45.171.60.0/22, iana 45.0.0.0/8" {
		t.Errorf("cadeia de 45.171.60.1 = %s", got)
	}

	// 2. Nada mudou: nenhuma fonte aplicada, nenhuma versão nova.
	res = e.cycle()
	if len(res.Applied) != 0 || res.Build != nil {
		t.Fatalf("segunda carga sem mudança = %+v", res)
	}
	if got := e.query(`SELECT count(*)::text FROM api.dataset`); got != "1" {
		t.Errorf("versões = %s", got)
	}

	// 3. O NIC.br muda o nome do titular: evento update no titular e no ASN.
	nicbr, _ := os.ReadFile(filepath.Join(e.dir, "nicbr"))
	e.write("nicbr", strings.Replace(string(nicbr), "TMSoft Solucoes em Informatica Ltda", "TMSoft Tecnologia Ltda", 1))
	res = e.cycle()
	if len(res.Applied) != 1 || res.Applied[0] != "nicbr" || res.Build == nil {
		t.Fatalf("mudança no NIC.br = %+v", res)
	}
	if got := e.query(`SELECT before->>'name' || ' -> ' || (after->>'name') FROM api.change_log
	                    WHERE entity = 'holder' AND key = 'lacnic:258500' AND action = 'update'`); got !=
		"TMSoft Solucoes em Informatica Ltda -> TMSoft Tecnologia Ltda" {
		t.Errorf("evento do titular = %s", got)
	}
	if got := e.query(`SELECT count(*)::text FROM api.change_log WHERE entity = 'asn' AND key = '61613' AND action = 'update'`); got != "1" {
		t.Errorf("eventos do AS61613 = %s", got)
	}

	// 4. Um bloco some da LACNIC: dentro da carência continua ativo e sem evento.
	e.editDelegated("rir-lacnic", without("|200.192.152.0|"))
	res = e.cycle()
	if res.Build == nil {
		t.Fatal("remoção deveria reconstruir o central")
	}
	var missing bool
	if err := pgxQueryRow(e, `SELECT missing_since IS NOT NULL FROM registry.prefix
	                           WHERE level = 'rir' AND prefix = '200.192.152.0/22'`, &missing); err != nil || !missing {
		t.Errorf("bloco ausente deveria ganhar missing_since (%v)", err)
	}
	if got := e.query(`SELECT (removed_at IS NULL)::text FROM api.prefix WHERE level = 'rir' AND prefix = '200.192.152.0/22'`); got != "true" {
		t.Errorf("dentro da carência o bloco deveria seguir ativo: removed_at IS NULL = %s", got)
	}
	if got := e.query(`SELECT count(*)::text FROM api.change_log WHERE key = '200.192.152.0/22' AND level = 'rir'`); got != "0" {
		t.Errorf("marcar ausência não deveria gerar evento, gerou %s", got)
	}
	// O NIC.br continua listando o bloco para o AS61613: ele passa a existir no
	// nível nicbr, que não depende da delegação da LACNIC.
	if got := e.query(`SELECT action || '|' || (after->>'nicbr_asns') FROM api.change_log
	                    WHERE key = '200.192.152.0/22' AND level = 'nicbr'`); got != "insert|[61613]" {
		t.Errorf("bloco ainda listado pelo NIC.br = %s", got)
	}

	// Vencida a carência, a próxima reconstrução remove e registra.
	e.newSyncer(0, false)
	if _, err := e.sync.Rebuild(e.ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.query(`SELECT action || '|' || (before->>'status') || '|' || coalesce(after::text, 'null') FROM api.change_log
	                    WHERE key = '200.192.152.0/22' AND level = 'rir'`); got != "remove|allocated|null" {
		t.Errorf("evento de remoção = %s", got)
	}
	if got := e.query(`SELECT count(*)::text FROM api.prefix WHERE prefix >>= '200.192.152.1' AND removed_at IS NULL AND level = 'rir'`); got != "0" {
		t.Errorf("bloco removido não deveria aparecer no lookup")
	}

	// 5. O bloco volta: evento restore.
	e.editDelegated("rir-lacnic", func(r []string) []string {
		return append(r, "lacnic|BR|ipv4|200.192.152.0|1024|20031125|allocated|258500")
	})
	e.newSyncer(time.Hour, false)
	e.cycle()
	if got := e.query(`SELECT action FROM api.change_log WHERE key = '200.192.152.0/22' AND level = 'rir'
	                    ORDER BY id DESC LIMIT 1`); got != "restore" {
		t.Errorf("evento ao voltar = %s", got)
	}

	// 6. Trava de remoção: uma carga que apaga metade da fonte é abortada.
	e.editDelegated("rir-lacnic", func(r []string) []string { return r[:len(r)/2] })
	res, err := e.sync.Cycle(e.ctx, true)
	if err == nil || len(res.Failed) != 1 || res.Failed[0] != "rir-lacnic" {
		t.Fatalf("carga destrutiva deveria falhar: %+v %v", res, err)
	}
	var outcome string
	if err := pgxQueryRow(e, `SELECT outcome FROM ingest.source_run WHERE source_id = 'rir-lacnic' ORDER BY id DESC LIMIT 1`, &outcome); err != nil {
		t.Fatal(err)
	}
	if outcome != "aborted" {
		t.Errorf("resultado da carga destrutiva = %s", outcome)
	}
	if got := e.query(`SELECT count(*)::text FROM api.prefix WHERE level = 'rir' AND rir = 'lacnic' AND removed_at IS NULL`); got == "0" {
		t.Error("a carga abortada não pode ter mexido nos dados")
	}
	// Com --force, a mesma carga passa.
	e.newSyncer(time.Hour, true)
	if res := e.cycle(); len(res.Applied) != 1 {
		t.Errorf("com --force a carga deveria passar: %+v", res)
	}
}

func TestTransferAndStaleFile(t *testing.T) {
	e := newEnv(t)
	e.cycle()

	// AS61613 aparece também na ARIN, cujo arquivo é mais novo: a ARIN vence.
	e.editDelegated("rir-arin", func(r []string) []string {
		return append(r, "arin|US|asn|61613|1|20260928|assigned|feedfacefeedfacefeedfacefeedface")
	})
	e.cycle()
	if got := e.query(`SELECT rir FROM api.asn WHERE asn = 61613`); got != "arin" {
		t.Errorf("em transferência vence o arquivo mais novo; rir = %s", got)
	}
	if got := e.query(`SELECT (before->>'rir') || ' -> ' || (after->>'rir') FROM api.change_log
	                    WHERE entity = 'asn' AND key = '61613' AND action = 'update'`); got != "lacnic -> arin" {
		t.Errorf("evento da transferência = %s", got)
	}

	// Um arquivo com data anterior à aplicada é recusado (espelho atrasado).
	e.setHeaderDate("rir-lacnic", "20260101")
	res, err := e.sync.Cycle(e.ctx, true)
	if err == nil || len(res.Failed) != 1 {
		t.Fatalf("arquivo antigo deveria ser recusado: %+v %v", res, err)
	}
	var outcome string
	if err := pgxQueryRow(e, `SELECT outcome FROM ingest.source_run WHERE source_id = 'rir-lacnic' ORDER BY id DESC LIMIT 1`, &outcome); err != nil {
		t.Fatal(err)
	}
	if outcome != "stale" {
		t.Errorf("resultado = %s", outcome)
	}
}

func TestAPIRoleCannotReadTables(t *testing.T) {
	e := newEnv(t)
	e.cycle()
	for _, sql := range []string{
		`SELECT 1 FROM registry.asn LIMIT 1`,
		`SELECT 1 FROM ingest.delegation LIMIT 1`,
		`INSERT INTO registry.dataset DEFAULT VALUES`,
	} {
		if _, err := e.api.Exec(e.ctx, sql); err == nil {
			t.Errorf("badblock_api não deveria conseguir: %s", sql)
		}
	}
}

// pgxQueryRow lê um valor com o role do sync (tabelas de ingest).
func pgxQueryRow(e *env, sql string, dst any) error {
	conn, err := pgx.Connect(e.ctx, e.db.SyncURL)
	if err != nil {
		return err
	}
	defer conn.Close(e.ctx)
	return conn.QueryRow(e.ctx, sql).Scan(dst)
}
