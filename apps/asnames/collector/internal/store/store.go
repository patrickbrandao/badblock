// Package store grava o dataset de nomes de AS na tabela asnames_asn, registra
// as execuções em asnames_run e mantém a linha do app na tabela central jobs.
//
// O schema é de database/postgres/asnames/ e database/postgres/central/; este
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

	"github.com/patrickbrandao/badblock/apps/asnames/collector/internal/parse"
)

// AppName é o nome gravado em jobs.app.
const AppName = "collector-asnames"

// ErrBusy indica outra execução aplicando dados ao mesmo tempo.
var ErrBusy = errors.New("outra execução do collector-asnames está aplicando dados")

// RemovalError é a trava contra remoção em massa: um arquivo truncado ou um
// formato novo não pode apagar boa parte da tabela.
type RemovalError struct {
	Removed   int
	Current   int
	Threshold float64
}

func (e *RemovalError) Error() string {
	return fmt.Sprintf("o arquivo removeria %d de %d ASNs (%.1f%%, limite %.1f%%); use --force se for legítimo",
		e.Removed, e.Current, 100*float64(e.Removed)/float64(e.Current), 100*e.Threshold)
}

// Applied descreve o último arquivo aplicado (a versão atual do dataset).
type Applied struct {
	Version      string // asnames_run.uuid
	URL          string
	ETag         string
	LastModified string
	SHA256       string
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
	SHA256       string
	Bytes        int64
	ASNs         int
	Warnings     []string
}

// Changes conta as linhas alteradas por uma aplicação.
type Changes struct {
	ASNInserted int
	ASNUpdated  int
	ASNDeleted  int
}

// Total é a soma de todas as alterações.
func (c Changes) Total() int { return c.ASNInserted + c.ASNUpdated + c.ASNDeleted }

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
	err := s.pool.QueryRow(ctx, `
		SELECT uuid::text, url, coalesce(etag, ''), coalesce(last_modified, ''), sha256, created_at
		  FROM asnames_run
		 WHERE status = 1
		 ORDER BY created_at DESC
		 LIMIT 1`).Scan(&a.Version, &a.URL, &a.ETag, &a.LastModified, &a.SHA256, &a.AppliedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
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

// RecordFailure grava uma execução recusada; a tabela de dados não muda.
func (s *Store) RecordFailure(ctx context.Context, run Run, cause error) error {
	_, err := s.insertRun(ctx, s.pool, run, 0, nil, cause.Error())
	return err
}

// Apply substitui o conteúdo de asnames_asn pelo do dataset numa transação
// única: carrega tudo numa tabela temporária com COPY, confere a trava de
// remoção e aplica as diferenças com um MERGE. Grava a execução em
// asnames_run e atualiza jobs (consolidated = 0 se alguma linha mudou).
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

	// Novos, alterados (qualquer campo, inclusive os derivados: uma mudança
	// nas regras do parser reaplicada com --force atualiza as linhas) e
	// removidos. As contagens saem agregadas do próprio servidor.
	actions, err := mergeActions(ctx, tx, `
		WITH m AS (
			MERGE INTO asnames_asn t
			USING stage_asn s ON t.asn = s.asn
			WHEN MATCHED AND (t.description, t.handle, t.name, t.country)
			                 IS DISTINCT FROM (s.description, s.handle, s.name, s.country) THEN
				UPDATE SET description = s.description, handle = s.handle, name = s.name, country = s.country
			WHEN NOT MATCHED BY TARGET THEN
				INSERT (asn, description, handle, name, country)
				VALUES (s.asn, s.description, s.handle, s.name, s.country)
			WHEN NOT MATCHED BY SOURCE THEN
				DELETE
			RETURNING merge_action() AS action
		)
		SELECT action, count(*) FROM m GROUP BY action`)
	if err != nil {
		return ch, "", fmt.Errorf("merge asnames_asn: %w", err)
	}
	ch.ASNInserted, ch.ASNUpdated, ch.ASNDeleted = actions["INSERT"], actions["UPDATE"], actions["DELETE"]

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

// stage cria a tabela temporária da carga e a preenche com COPY. Campos
// derivados vazios viram NULL.
func stage(ctx context.Context, tx pgx.Tx, ds *parse.Dataset) error {
	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE stage_asn (
			asn          bigint PRIMARY KEY,
			description  text   NOT NULL,
			handle       text,
			name         text,
			country      text
		) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("staging: %w", err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"stage_asn"},
		[]string{"asn", "description", "handle", "name", "country"},
		pgx.CopyFromSlice(len(ds.ASNs), func(i int) ([]any, error) {
			a := ds.ASNs[i]
			return []any{a.Number, a.Description, nullStr(a.Handle), nullStr(a.Name), nullStr(a.Country)}, nil
		})); err != nil {
		return fmt.Errorf("copy stage_asn: %w", err)
	}
	_, err := tx.Exec(ctx, "ANALYZE stage_asn")
	return err
}

func checkRemoval(ctx context.Context, tx pgx.Tx, threshold float64) error {
	var cur, removed int
	err := tx.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM asnames_asn),
		       (SELECT count(*) FROM asnames_asn a
		         WHERE NOT EXISTS (SELECT 1 FROM stage_asn s WHERE s.asn = a.asn))`).
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
	counts := make([]any, 3)
	if ch != nil {
		counts = []any{ch.ASNInserted, ch.ASNUpdated, ch.ASNDeleted}
	}
	args := []any{
		status, run.Forced, run.URL, nullInt(run.HTTPStatus), nullStr(run.ETag), nullStr(run.LastModified),
		nullStr(run.SHA256), nullInt64(run.Bytes), nullInt(run.ASNs),
	}
	args = append(args, counts...)
	args = append(args, string(wj), nullStr(errMsg), run.StartedAt)

	var version string
	err = q.QueryRow(ctx, `
		INSERT INTO asnames_run (
			status, forced, url, http_status, etag, last_modified,
			sha256, bytes, asns, asn_inserted, asn_updated, asn_deleted,
			warnings, error, started_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, $14, $15)
		RETURNING uuid::text`, args...).Scan(&version)
	if err != nil {
		return "", fmt.Errorf("asnames_run: %w", err)
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
