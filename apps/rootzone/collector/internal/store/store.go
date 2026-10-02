// Package store grava a zona raiz nas tabelas rootzone_tld e rootzone_record,
// registra as execuções em rootzone_run e mantém a linha do app na tabela
// central jobs.
//
// O schema é de database/postgres/rootzone/ e database/postgres/central/;
// este pacote só lê e escreve dados.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/patrickbrandao/badblock/apps/rootzone/collector/internal/parse"
)

// AppName é o nome gravado em jobs.app.
const AppName = "collector-rootzone"

// ErrBusy indica outra execução aplicando dados ao mesmo tempo.
var ErrBusy = errors.New("outra execução do collector-rootzone está aplicando dados")

// RemovalError é a trava contra remoção em massa: um arquivo truncado ou um
// formato novo não pode apagar boa parte da zona.
type RemovalError struct {
	Removed   int
	Current   int
	Threshold float64
}

func (e *RemovalError) Error() string {
	return fmt.Sprintf("a zona removeria %d de %d registros (%.1f%%, limite %.1f%%); use --force se for legítimo",
		e.Removed, e.Current, 100*float64(e.Removed)/float64(e.Current), 100*e.Threshold)
}

// Applied descreve o último arquivo aplicado (a versão atual do dataset).
type Applied struct {
	Version      string // rootzone_run.uuid
	URL          string
	ETag         string
	LastModified string
	MD5          string
	SHA256       string
	Serial       uint32
	AppliedAt    time.Time
}

// Run são os dados de uma execução que baixou um arquivo.
type Run struct {
	StartedAt    time.Time
	Forced       bool
	URL          string
	HTTPStatus   int
	ETag         string
	LastModified string
	MD5          string
	SHA256       string
	Bytes        int64

	Parsed     bool // o parser aceitou o arquivo: SOA e contagens valem
	SOA        parse.SOA
	TLDs       int
	Records    int
	RRSIGs     int
	TypeCounts map[string]int
	Warnings   []string
}

// Changes conta as linhas alteradas por uma aplicação.
type Changes struct {
	TLDInserted    int
	TLDUpdated     int
	TLDDeleted     int
	RecordInserted int
	RecordUpdated  int
	RecordDeleted  int
}

// Total é a soma de todas as alterações.
func (c Changes) Total() int {
	return c.TLDInserted + c.TLDUpdated + c.TLDDeleted + c.RecordInserted + c.RecordUpdated + c.RecordDeleted
}

// ApplyOptions controla as travas da aplicação.
type ApplyOptions struct {
	RemovalThreshold float64 // fração máxima de remoção em rootzone_record sem --force
	Force            bool
}

// Store é a conexão com o Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// Open conecta e valida com um SELECT 1.
func Open(ctx context.Context, url string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("POSTGRES_URL inválida: %w", err)
	}
	cfg.MaxConns = 2
	cfg.MaxConnIdleTime = 5 * time.Minute
	if cfg.ConnConfig.RuntimeParams["application_name"] == "" {
		cfg.ConnConfig.RuntimeParams["application_name"] = AppName
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s := &Store{pool: pool}
	if err := s.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// NewFromPool usa um pool existente (testes).
func NewFromPool(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Close fecha o pool.
func (s *Store) Close() { s.pool.Close() }

// Ping confere a conexão.
func (s *Store) Ping(ctx context.Context) error {
	var one int
	return s.pool.QueryRow(ctx, "SELECT 1").Scan(&one)
}

// LastApplied devolve o último arquivo aplicado, ou nil se nunca houve um.
func (s *Store) LastApplied(ctx context.Context) (*Applied, error) {
	var a Applied
	var serial int64
	err := s.pool.QueryRow(ctx, `
		SELECT uuid::text, url, coalesce(etag, ''), coalesce(last_modified, ''),
		       coalesce(md5, ''), sha256, serial, created_at
		  FROM rootzone_run
		 WHERE status = 1
		 ORDER BY created_at DESC
		 LIMIT 1`).Scan(&a.Version, &a.URL, &a.ETag, &a.LastModified, &a.MD5, &a.SHA256, &serial, &a.AppliedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.Serial = uint32(serial)
	return &a, nil
}

// TouchCheck registra em jobs uma verificação sem mudança.
func (s *Store) TouchCheck(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO jobs (app, last_check_at) VALUES ($1, NOW())
		ON CONFLICT (app) DO UPDATE SET last_check_at = NOW()`, AppName)
	return err
}

// RecordFailure grava uma execução recusada; as tabelas de dados não mudam.
func (s *Store) RecordFailure(ctx context.Context, run Run, cause error) error {
	_, err := s.insertRun(ctx, s.pool, run, 0, nil, cause.Error())
	return err
}

// Apply substitui o conteúdo de rootzone_record e rootzone_tld pelo do
// dataset numa transação única: carrega tudo em tabelas temporárias com COPY,
// confere a trava de remoção (sobre rootzone_record) e aplica as diferenças
// com um MERGE por tabela. Grava a execução em rootzone_run e atualiza jobs
// (consolidated = 0 se alguma linha mudou). Devolve as alterações e a nova
// versão do dataset.
func (s *Store) Apply(ctx context.Context, ds *parse.Dataset, run Run, opt ApplyOptions) (Changes, string, error) {
	var ch Changes
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ch, "", err
	}
	defer tx.Rollback(ctx)

	var locked bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtext($1))", AppName).Scan(&locked); err != nil {
		return ch, "", err
	}
	if !locked {
		return ch, "", ErrBusy
	}

	if err := stage(ctx, tx, ds); err != nil {
		return ch, "", err
	}
	if !opt.Force {
		if err := checkRemoval(ctx, tx, opt.RemovalThreshold); err != nil {
			return ch, "", err
		}
	}

	// Chave natural (owner, type, rdata): só o TTL pode mudar numa RR que
	// continua. As contagens saem agregadas do próprio servidor.
	actions, err := mergeActions(ctx, tx, `
		WITH m AS (
			MERGE INTO rootzone_record t
			USING stage_record s ON t.owner = s.owner AND t.type = s.type AND t.rdata = s.rdata
			WHEN MATCHED AND t.ttl IS DISTINCT FROM s.ttl THEN
				UPDATE SET ttl = s.ttl
			WHEN NOT MATCHED BY TARGET THEN
				INSERT (owner, type, rdata, ttl) VALUES (s.owner, s.type, s.rdata, s.ttl)
			WHEN NOT MATCHED BY SOURCE THEN
				DELETE
			RETURNING merge_action() AS action
		)
		SELECT action, count(*) FROM m GROUP BY action`)
	if err != nil {
		return ch, "", fmt.Errorf("merge rootzone_record: %w", err)
	}
	ch.RecordInserted, ch.RecordUpdated, ch.RecordDeleted = actions["INSERT"], actions["UPDATE"], actions["DELETE"]

	actions, err = mergeActions(ctx, tx, `
		WITH m AS (
			MERGE INTO rootzone_tld t
			USING stage_tld s ON t.tld = s.tld
			WHEN MATCHED AND (t.tld_unicode, t.nameservers, t.nameservers_ipv4, t.nameservers_ipv6, t.ds_records)
			                 IS DISTINCT FROM (s.tld_unicode, s.nameservers, s.nameservers_ipv4, s.nameservers_ipv6, s.ds_records) THEN
				UPDATE SET tld_unicode = s.tld_unicode, nameservers = s.nameservers,
				           nameservers_ipv4 = s.nameservers_ipv4, nameservers_ipv6 = s.nameservers_ipv6,
				           ds_records = s.ds_records
			WHEN NOT MATCHED BY TARGET THEN
				INSERT (tld, tld_unicode, nameservers, nameservers_ipv4, nameservers_ipv6, ds_records)
				VALUES (s.tld, s.tld_unicode, s.nameservers, s.nameservers_ipv4, s.nameservers_ipv6, s.ds_records)
			WHEN NOT MATCHED BY SOURCE THEN
				DELETE
			RETURNING merge_action() AS action
		)
		SELECT action, count(*) FROM m GROUP BY action`)
	if err != nil {
		return ch, "", fmt.Errorf("merge rootzone_tld: %w", err)
	}
	ch.TLDInserted, ch.TLDUpdated, ch.TLDDeleted = actions["INSERT"], actions["UPDATE"], actions["DELETE"]

	version, err := s.insertRun(ctx, tx, run, 1, &ch, "")
	if err != nil {
		return ch, "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO jobs (app, last_sync_at, last_check_at, consolidated) VALUES ($1, NOW(), NOW(), 0)
		ON CONFLICT (app) DO UPDATE SET
			last_sync_at  = NOW(),
			last_check_at = NOW(),
			consolidated  = CASE WHEN $2 THEN 0 ELSE jobs.consolidated END`,
		AppName, ch.Total() > 0); err != nil {
		return ch, "", fmt.Errorf("jobs: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ch, "", err
	}
	return ch, version, nil
}

// stage cria as tabelas temporárias da carga e as preenche com COPY.
func stage(ctx context.Context, tx pgx.Tx, ds *parse.Dataset) error {
	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE stage_record (
			owner  text    NOT NULL,
			type   text    NOT NULL,
			rdata  text    NOT NULL,
			ttl    integer NOT NULL,
			PRIMARY KEY (owner, type, rdata)
		) ON COMMIT DROP;
		CREATE TEMP TABLE stage_tld (
			tld               text     PRIMARY KEY,
			tld_unicode       text     NOT NULL,
			nameservers       smallint NOT NULL,
			nameservers_ipv4  smallint NOT NULL,
			nameservers_ipv6  smallint NOT NULL,
			ds_records        smallint NOT NULL
		) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("staging: %w", err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"stage_record"},
		[]string{"owner", "type", "rdata", "ttl"},
		pgx.CopyFromSlice(len(ds.Records), func(i int) ([]any, error) {
			r := ds.Records[i]
			return []any{r.Owner, r.Type, r.RData, int32(r.TTL)}, nil
		})); err != nil {
		return fmt.Errorf("copy stage_record: %w", err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"stage_tld"},
		[]string{"tld", "tld_unicode", "nameservers", "nameservers_ipv4", "nameservers_ipv6", "ds_records"},
		pgx.CopyFromSlice(len(ds.TLDs), func(i int) ([]any, error) {
			t := ds.TLDs[i]
			return []any{t.Name, t.Unicode, int16(t.Nameservers), int16(t.NameserversIPv4),
				int16(t.NameserversIPv6), int16(t.DSRecords)}, nil
		})); err != nil {
		return fmt.Errorf("copy stage_tld: %w", err)
	}
	_, err := tx.Exec(ctx, "ANALYZE stage_record; ANALYZE stage_tld")
	return err
}

func checkRemoval(ctx context.Context, tx pgx.Tx, threshold float64) error {
	var cur, removed int
	err := tx.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM rootzone_record),
		       (SELECT count(*) FROM rootzone_record r
		         WHERE NOT EXISTS (SELECT 1 FROM stage_record s
		                            WHERE s.owner = r.owner AND s.type = r.type AND s.rdata = r.rdata))`).
		Scan(&cur, &removed)
	if err != nil {
		return err
	}
	if cur > 0 && float64(removed)/float64(cur) > threshold {
		return &RemovalError{Removed: removed, Current: cur, Threshold: threshold}
	}
	return nil
}

// mergeActions roda uma consulta que devolve (merge_action, quantidade) e
// monta o mapa ação → quantidade.
func mergeActions(ctx context.Context, tx pgx.Tx, sql string) (map[string]int, error) {
	rows, err := tx.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var action string
		var n int
		if err := rows.Scan(&action, &n); err != nil {
			return nil, err
		}
		out[action] = n
	}
	return out, rows.Err()
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s *Store) insertRun(ctx context.Context, q querier, run Run, status int, ch *Changes, errMsg string) (string, error) {
	warnings := run.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	wj, err := json.Marshal(warnings)
	if err != nil {
		return "", err
	}
	// SOA e contagens só existem quando o parser aceitou o arquivo.
	parsed := make([]any, 11)
	if run.Parsed {
		tc, err := json.Marshal(run.TypeCounts)
		if err != nil {
			return "", err
		}
		soa := run.SOA
		parsed = []any{int64(soa.Serial), soa.MName, soa.RName, int64(soa.Refresh), int64(soa.Retry),
			int64(soa.Expire), int64(soa.Minimum), run.TLDs, run.Records, run.RRSIGs, string(tc)}
	}
	counts := make([]any, 6)
	if ch != nil {
		counts = []any{ch.TLDInserted, ch.TLDUpdated, ch.TLDDeleted, ch.RecordInserted, ch.RecordUpdated, ch.RecordDeleted}
	}
	args := []any{
		status, run.Forced, run.URL, nullInt(run.HTTPStatus), nullStr(run.ETag), nullStr(run.LastModified),
		nullStr(run.MD5), nullStr(run.SHA256), nullInt64(run.Bytes),
	}
	args = append(args, parsed...)
	args = append(args, counts...)
	args = append(args, string(wj), nullStr(errMsg), run.StartedAt)

	var version string
	err = q.QueryRow(ctx, `
		INSERT INTO rootzone_run (
			status, forced, url, http_status, etag, last_modified, md5, sha256, bytes,
			serial, soa_mname, soa_rname, soa_refresh, soa_retry, soa_expire, soa_minimum,
			tlds, records, rrsigs, type_counts,
			tld_inserted, tld_updated, tld_deleted, record_inserted, record_updated, record_deleted,
			warnings, error, started_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
		          $10, $11, $12, $13, $14, $15, $16,
		          $17, $18, $19, $20::jsonb,
		          $21, $22, $23, $24, $25, $26,
		          $27::jsonb, $28, $29)
		RETURNING uuid::text`, args...).Scan(&version)
	if err != nil {
		return "", fmt.Errorf("rootzone_run: %w", err)
	}
	return version, nil
}

// nullStr, nullInt e nullInt64 gravam o valor zero como NULL (campo ausente).
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullInt64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}
