// Package store grava o dataset do RIR nas tabelas apnic_* e mantém a linha
// do app na tabela central jobs.
//
// O schema é de database/postgres/apnic/ e database/postgres/central/; este
// pacote só lê e escreve dados.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/patrickbrandao/badblock/apps/apnic/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/apnic/collector/internal/rir"
)

// AppName é o nome gravado em jobs.app.
const AppName = rir.App

// ErrBusy indica outra execução aplicando dados ao mesmo tempo.
var ErrBusy = errors.New("outra execução do " + AppName + " está aplicando dados")

// RemovalError é a trava contra remoção em massa: um arquivo truncado ou um
// formato novo não pode apagar boa parte das tabelas.
type RemovalError struct {
	Entity    string // "registros de ASN" ou "blocos"
	Removed   int
	Current   int
	Threshold float64
}

func (e *RemovalError) Error() string {
	return fmt.Sprintf("o arquivo removeria %d de %d %s (%.1f%%, limite %.1f%%); use --force se for legítimo",
		e.Removed, e.Current, e.Entity, 100*float64(e.Removed)/float64(e.Current), 100*e.Threshold)
}

// Applied descreve o último arquivo aplicado (a versão atual do dataset).
type Applied struct {
	Version      string // apnic_run.uuid
	URL          string
	ETag         string
	LastModified string
	MD5          string
	SHA256       string
	Serial       string    // serial do cabeçalho
	EndDate      time.Time // enddate do cabeçalho; zero = sem data
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

	// Preenchidos só se o parser aceitou o arquivo.
	Parsed      bool
	Header      parse.Header
	ASNRecords  int
	IPv4Records int
	IPv6Records int
	PrefixesV4  int
	PrefixesV6  int
	Warnings    []string
}

// Changes conta as linhas alteradas por uma aplicação.
type Changes struct {
	ASNInserted    int
	ASNUpdated     int
	ASNDeleted     int
	PrefixInserted int
	PrefixUpdated  int
	PrefixDeleted  int
}

// Total é a soma de todas as alterações.
func (c Changes) Total() int {
	return c.ASNInserted + c.ASNUpdated + c.ASNDeleted + c.PrefixInserted + c.PrefixUpdated + c.PrefixDeleted
}

// ApplyOptions controla as travas da aplicação.
type ApplyOptions struct {
	RemovalThreshold float64 // fração máxima de remoção sem --force
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
	var endDate *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT uuid::text, url, coalesce(etag, ''), coalesce(last_modified, ''),
		       coalesce(md5, ''), coalesce(sha256, ''), coalesce(serial, ''), end_date, created_at
		  FROM apnic_run
		 WHERE status = 1
		 ORDER BY created_at DESC
		 LIMIT 1`).Scan(&a.Version, &a.URL, &a.ETag, &a.LastModified, &a.MD5, &a.SHA256, &a.Serial, &endDate, &a.AppliedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if endDate != nil {
		a.EndDate = *endDate
	}
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

// Apply substitui o conteúdo de apnic_asn e apnic_prefix pelo do dataset
// numa transação única: carrega tudo em tabelas temporárias com COPY, confere
// a trava de remoção e aplica as diferenças com MERGE. Grava a execução em
// apnic_run e atualiza jobs (consolidated = 0 se alguma linha mudou).
// Devolve as alterações e a nova versão do dataset.
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

	// Registros de ASN: novos, alterados e removidos. As duas tabelas são
	// independentes (sem FK): o vínculo entre ASNs e blocos é o opaque_id.
	actions, err := mergeActions(ctx, tx, `
		MERGE INTO apnic_asn t
		USING stage_asn s ON t.asn_start = s.asn_start
		WHEN MATCHED AND (t.asn_count, t.cc, t.reg_date, t.status, t.opaque_id)
		      IS DISTINCT FROM (s.asn_count, s.cc, s.reg_date, s.status, s.opaque_id) THEN
			UPDATE SET asn_count = s.asn_count, cc = s.cc, reg_date = s.reg_date,
			           status = s.status, opaque_id = s.opaque_id
		WHEN NOT MATCHED BY TARGET THEN
			INSERT (asn_start, asn_count, cc, reg_date, status, opaque_id)
			VALUES (s.asn_start, s.asn_count, s.cc, s.reg_date, s.status, s.opaque_id)
		WHEN NOT MATCHED BY SOURCE THEN
			DELETE
		RETURNING merge_action()`)
	if err != nil {
		return ch, "", fmt.Errorf("merge apnic_asn: %w", err)
	}
	ch.ASNInserted, ch.ASNUpdated, ch.ASNDeleted = actions["INSERT"], actions["UPDATE"], actions["DELETE"]

	actions, err = mergeActions(ctx, tx, `
		MERGE INTO apnic_prefix t
		USING stage_prefix s ON t.prefix = s.prefix
		WHEN MATCHED AND (t.cc, t.reg_date, t.status, t.opaque_id, t.record_start, t.record_value)
		      IS DISTINCT FROM (s.cc, s.reg_date, s.status, s.opaque_id, s.record_start, s.record_value) THEN
			UPDATE SET cc = s.cc, reg_date = s.reg_date, status = s.status, opaque_id = s.opaque_id,
			           record_start = s.record_start, record_value = s.record_value
		WHEN NOT MATCHED BY TARGET THEN
			INSERT (prefix, cc, reg_date, status, opaque_id, record_start, record_value)
			VALUES (s.prefix, s.cc, s.reg_date, s.status, s.opaque_id, s.record_start, s.record_value)
		WHEN NOT MATCHED BY SOURCE THEN
			DELETE
		RETURNING merge_action()`)
	if err != nil {
		return ch, "", fmt.Errorf("merge apnic_prefix: %w", err)
	}
	ch.PrefixInserted, ch.PrefixUpdated, ch.PrefixDeleted = actions["INSERT"], actions["UPDATE"], actions["DELETE"]

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
		CREATE TEMP TABLE stage_asn (
			asn_start     bigint PRIMARY KEY,
			asn_count     bigint NOT NULL,
			cc            text,
			reg_date      date,
			status        text   NOT NULL,
			opaque_id     text
		) ON COMMIT DROP;
		CREATE TEMP TABLE stage_prefix (
			prefix        cidr   PRIMARY KEY,
			cc            text,
			reg_date      date,
			status        text   NOT NULL,
			opaque_id     text,
			record_start  inet   NOT NULL,
			record_value  bigint NOT NULL
		) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("staging: %w", err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"stage_asn"},
		[]string{"asn_start", "asn_count", "cc", "reg_date", "status", "opaque_id"},
		pgx.CopyFromSlice(len(ds.ASNs), func(i int) ([]any, error) {
			a := ds.ASNs[i]
			return []any{a.Start, a.Count, nullStr(a.CC), nullDate(a.Date), a.Status, nullStr(a.OpaqueID)}, nil
		})); err != nil {
		return fmt.Errorf("copy stage_asn: %w", err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"stage_prefix"},
		[]string{"prefix", "cc", "reg_date", "status", "opaque_id", "record_start", "record_value"},
		pgx.CopyFromSlice(len(ds.Prefixes), func(i int) ([]any, error) {
			p := ds.Prefixes[i]
			return []any{p.Prefix, nullStr(p.CC), nullDate(p.Date), p.Status, nullStr(p.OpaqueID), p.RecordStart, p.RecordValue}, nil
		})); err != nil {
		return fmt.Errorf("copy stage_prefix: %w", err)
	}
	_, err := tx.Exec(ctx, "ANALYZE stage_asn; ANALYZE stage_prefix")
	return err
}

func checkRemoval(ctx context.Context, tx pgx.Tx, threshold float64) error {
	var curASN, remASN, curPfx, remPfx int
	err := tx.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM apnic_asn),
		       (SELECT count(*) FROM apnic_asn a
		         WHERE NOT EXISTS (SELECT 1 FROM stage_asn s WHERE s.asn_start = a.asn_start)),
		       (SELECT count(*) FROM apnic_prefix),
		       (SELECT count(*) FROM apnic_prefix p
		         WHERE NOT EXISTS (SELECT 1 FROM stage_prefix s WHERE s.prefix = p.prefix))`).
		Scan(&curASN, &remASN, &curPfx, &remPfx)
	if err != nil {
		return err
	}
	if curASN > 0 && float64(remASN)/float64(curASN) > threshold {
		return &RemovalError{Entity: "registros de ASN", Removed: remASN, Current: curASN, Threshold: threshold}
	}
	if curPfx > 0 && float64(remPfx)/float64(curPfx) > threshold {
		return &RemovalError{Entity: "blocos", Removed: remPfx, Current: curPfx, Threshold: threshold}
	}
	return nil
}

// mergeActions roda um MERGE ... RETURNING merge_action() e conta as ações.
func mergeActions(ctx context.Context, tx pgx.Tx, sql string) (map[string]int, error) {
	rows, err := tx.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			return nil, err
		}
		out[action]++
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
	counts := make([]any, 6)
	if ch != nil {
		counts = []any{ch.ASNInserted, ch.ASNUpdated, ch.ASNDeleted, ch.PrefixInserted, ch.PrefixUpdated, ch.PrefixDeleted}
	}
	// Cabeçalho e contagens do arquivo só existem se o parser aceitou o arquivo.
	header := make([]any, 6)
	sizes := make([]any, 5)
	if run.Parsed {
		h := run.Header
		header = []any{h.Version, h.Serial, h.Records, nullDate(h.StartDate), nullDate(h.EndDate), nullStr(h.UTCOffset)}
		sizes = []any{run.ASNRecords, run.IPv4Records, run.IPv6Records, run.PrefixesV4, run.PrefixesV6}
	}
	args := []any{
		status, run.Forced, run.URL, nullInt(run.HTTPStatus), nullStr(run.ETag), nullStr(run.LastModified),
		nullStr(run.MD5), nullStr(run.SHA256), nullInt64(run.Bytes),
	}
	args = append(args, header...)
	args = append(args, sizes...)
	args = append(args, counts...)
	args = append(args, string(wj), nullStr(errMsg), run.StartedAt)

	var version string
	err = q.QueryRow(ctx, `
		INSERT INTO apnic_run (
			status, forced, url, http_status, etag, last_modified,
			md5, sha256, bytes,
			format_version, serial, header_records, start_date, end_date, utc_offset,
			asn_records, ipv4_records, ipv6_records, prefixes_v4, prefixes_v6,
			asn_inserted, asn_updated, asn_deleted, prefix_inserted, prefix_updated, prefix_deleted,
			warnings, error, started_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
		          $21, $22, $23, $24, $25, $26, $27::jsonb, $28, $29)
		RETURNING uuid::text`, args...).Scan(&version)
	if err != nil {
		return "", fmt.Errorf("apnic_run: %w", err)
	}
	return version, nil
}

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

func nullDate(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
