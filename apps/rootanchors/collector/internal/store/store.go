// Package store grava as âncoras de confiança da raiz nas tabelas
// rootanchors_* e mantém a linha do app na tabela central jobs.
//
// O schema é de database/postgres/rootanchors/ e database/postgres/central/;
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

	"github.com/patrickbrandao/badblock/apps/rootanchors/collector/internal/parse"
)

// AppName é o nome gravado em jobs.app.
const AppName = "collector-rootanchors"

// ErrBusy indica outra execução aplicando dados ao mesmo tempo.
var ErrBusy = errors.New("outra execução do collector-rootanchors está aplicando dados")

// RemovalError é a trava contra remoção em massa. A IANA mantém no arquivo as
// chaves expiradas, então uma chave que some é inesperada; com poucas linhas,
// qualquer remoção passa do limite padrão e exige --force.
type RemovalError struct {
	Removed   int
	Current   int
	Threshold float64
}

func (e *RemovalError) Error() string {
	return fmt.Sprintf("o arquivo removeria %d de %d chaves (%.1f%%, limite %.1f%%); use --force se for legítimo",
		e.Removed, e.Current, 100*float64(e.Removed)/float64(e.Current), 100*e.Threshold)
}

// Applied descreve o último arquivo aplicado (a versão atual do dataset).
type Applied struct {
	Version      string // rootanchors_run.uuid
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
	Parsed       bool // o parser aceitou o arquivo: AnchorID, AnchorSource, Zone e Keys valem
	AnchorID     string
	AnchorSource string
	Zone         string
	Keys         int
	Warnings     []string
}

// Changes conta as linhas alteradas por uma aplicação.
type Changes struct {
	KeyInserted int
	KeyUpdated  int
	KeyDeleted  int
}

// Total é a soma de todas as alterações.
func (c Changes) Total() int { return c.KeyInserted + c.KeyUpdated + c.KeyDeleted }

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
		  FROM rootanchors_run
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

// RecordFailure grava uma execução recusada; as tabelas de dados não mudam.
func (s *Store) RecordFailure(ctx context.Context, run Run, cause error) error {
	_, err := s.insertRun(ctx, s.pool, run, 0, nil, cause.Error())
	return err
}

// Apply substitui o conteúdo de rootanchors_key pelo do dataset numa
// transação única: carrega as chaves numa tabela temporária com COPY, confere
// a trava de remoção e aplica as diferenças com MERGE. Grava a execução em
// rootanchors_run e atualiza jobs (consolidated = 0 se alguma linha mudou).
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

	actions, err := mergeActions(ctx, tx, `
		MERGE INTO rootanchors_key t
		USING stage_key s ON t.key_id = s.key_id
		WHEN MATCHED AND (t.key_tag, t.algorithm, t.digest_type, t.digest, t.public_key, t.flags, t.valid_from, t.valid_until)
		       IS DISTINCT FROM (s.key_tag, s.algorithm, s.digest_type, s.digest, s.public_key, s.flags, s.valid_from, s.valid_until) THEN
			UPDATE SET key_tag = s.key_tag, algorithm = s.algorithm, digest_type = s.digest_type, digest = s.digest,
			           public_key = s.public_key, flags = s.flags, valid_from = s.valid_from, valid_until = s.valid_until
		WHEN NOT MATCHED BY TARGET THEN
			INSERT (key_id, key_tag, algorithm, digest_type, digest, public_key, flags, valid_from, valid_until)
			VALUES (s.key_id, s.key_tag, s.algorithm, s.digest_type, s.digest, s.public_key, s.flags, s.valid_from, s.valid_until)
		WHEN NOT MATCHED BY SOURCE THEN
			DELETE
		RETURNING merge_action()`)
	if err != nil {
		return ch, "", fmt.Errorf("merge rootanchors_key: %w", err)
	}
	ch.KeyInserted, ch.KeyUpdated, ch.KeyDeleted = actions["INSERT"], actions["UPDATE"], actions["DELETE"]

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

// stage cria a tabela temporária da carga e a preenche com COPY.
func stage(ctx context.Context, tx pgx.Tx, ds *parse.Dataset) error {
	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE stage_key (
			key_id       text        PRIMARY KEY,
			key_tag      integer     NOT NULL,
			algorithm    smallint    NOT NULL,
			digest_type  smallint    NOT NULL,
			digest       text        NOT NULL,
			public_key   text,
			flags        integer,
			valid_from   timestamptz NOT NULL,
			valid_until  timestamptz
		) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("staging: %w", err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"stage_key"},
		[]string{"key_id", "key_tag", "algorithm", "digest_type", "digest", "public_key", "flags", "valid_from", "valid_until"},
		pgx.CopyFromSlice(len(ds.Keys), func(i int) ([]any, error) {
			k := ds.Keys[i]
			return []any{k.ID, int32(k.KeyTag), int16(k.Algorithm), int16(k.DigestType), k.Digest,
				nullStr(k.PublicKey), k.Flags, k.ValidFrom, k.ValidUntil}, nil
		})); err != nil {
		return fmt.Errorf("copy stage_key: %w", err)
	}
	return nil
}

func checkRemoval(ctx context.Context, tx pgx.Tx, threshold float64) error {
	var cur, rem int
	err := tx.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM rootanchors_key),
		       (SELECT count(*) FROM rootanchors_key k
		         WHERE NOT EXISTS (SELECT 1 FROM stage_key s WHERE s.key_id = k.key_id))`).Scan(&cur, &rem)
	if err != nil {
		return err
	}
	if cur > 0 && float64(rem)/float64(cur) > threshold {
		return &RemovalError{Removed: rem, Current: cur, Threshold: threshold}
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
	counts := make([]any, 3)
	if ch != nil {
		counts = []any{ch.KeyInserted, ch.KeyUpdated, ch.KeyDeleted}
	}
	// O que vem do arquivo só existe se o parser o aceitou.
	parsed := []any{nil, nil, nil, nil}
	if run.Parsed {
		parsed = []any{run.AnchorID, nullStr(run.AnchorSource), run.Zone, run.Keys}
	}
	args := []any{
		status, run.Forced, run.URL, nullInt(run.HTTPStatus), nullStr(run.ETag), nullStr(run.LastModified),
		nullStr(run.SHA256), nullInt64(run.Bytes),
	}
	args = append(args, parsed...)
	args = append(args, counts...)
	args = append(args, string(wj), nullStr(errMsg), run.StartedAt)

	var version string
	err = q.QueryRow(ctx, `
		INSERT INTO rootanchors_run (
			status, forced, url, http_status, etag, last_modified, sha256, bytes,
			anchor_id, anchor_source, zone, keys,
			key_inserted, key_updated, key_deleted,
			warnings, error, started_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16::jsonb, $17, $18)
		RETURNING uuid::text`, args...).Scan(&version)
	if err != nil {
		return "", fmt.Errorf("rootanchors_run: %w", err)
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
