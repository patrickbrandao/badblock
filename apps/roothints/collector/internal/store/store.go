// Package store grava os servidores raiz na tabela roothints_server, registra
// as execuções em roothints_run e mantém a linha do app na tabela central jobs.
//
// O schema é de database/postgres/roothints/ e database/postgres/central/;
// este pacote só lê e escreve dados.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/patrickbrandao/badblock/apps/roothints/collector/internal/parse"
)

// AppName é o nome gravado em jobs.app.
const AppName = "collector-roothints"

// ErrBusy indica outra execução aplicando dados ao mesmo tempo.
var ErrBusy = errors.New("outra execução do collector-roothints está aplicando dados")

// RemovalError é a trava contra remoção em massa. Com 13 servidores e o
// limite padrão de 5%, qualquer remoção (1 de 13 = 7,7%) é recusada sem
// --force.
type RemovalError struct {
	Removed   int
	Current   int
	Threshold float64
}

func (e *RemovalError) Error() string {
	return fmt.Sprintf("o arquivo removeria %d de %d servidores (%.1f%%, limite %.1f%%); use --force se for legítimo",
		e.Removed, e.Current, 100*float64(e.Removed)/float64(e.Current), 100*e.Threshold)
}

// Applied descreve o último arquivo aplicado (a versão atual do dataset).
type Applied struct {
	Version      string // roothints_run.uuid
	URL          string
	ETag         string
	LastModified string
	MD5          string
	SHA256       string
	ZoneSerial   *int64 // nil = desconhecido
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

	Parsed        bool // o parser aceitou o arquivo: cabeçalho e contagens valem
	Header        parse.Header
	Servers       int
	IPv4Addresses int
	IPv6Addresses int
	Warnings      []string
}

// Changes conta as linhas alteradas por uma aplicação.
type Changes struct {
	ServerInserted int
	ServerUpdated  int
	ServerDeleted  int
}

// Total é a soma de todas as alterações.
func (c Changes) Total() int { return c.ServerInserted + c.ServerUpdated + c.ServerDeleted }

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
		SELECT uuid::text, url, coalesce(etag, ''), coalesce(last_modified, ''),
		       coalesce(md5, ''), sha256, zone_serial, created_at
		  FROM roothints_run
		 WHERE status = 1
		 ORDER BY created_at DESC
		 LIMIT 1`).Scan(&a.Version, &a.URL, &a.ETag, &a.LastModified, &a.MD5, &a.SHA256, &a.ZoneSerial, &a.AppliedAt)
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

// Apply substitui o conteúdo de roothints_server pelo do dataset numa
// transação única: carrega tudo numa tabela temporária com COPY, confere a
// trava de remoção e aplica as diferenças com um MERGE. Grava a execução em
// roothints_run e atualiza jobs (consolidated = 0 se alguma linha mudou).
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

	// Novos, alterados (qualquer coluna de dado) e removidos, pela chave
	// natural name. As contagens saem agregadas do próprio servidor.
	actions, err := mergeActions(ctx, tx, `
		WITH m AS (
			MERGE INTO roothints_server t
			USING stage_server s ON t.name = s.name
			WHEN MATCHED AND (t.letter, t.ipv4, t.ipv6, t.ns_ttl, t.ipv4_ttl, t.ipv6_ttl, t.note)
			                 IS DISTINCT FROM (s.letter, s.ipv4, s.ipv6, s.ns_ttl, s.ipv4_ttl, s.ipv6_ttl, s.note) THEN
				UPDATE SET letter = s.letter, ipv4 = s.ipv4, ipv6 = s.ipv6, ns_ttl = s.ns_ttl,
				           ipv4_ttl = s.ipv4_ttl, ipv6_ttl = s.ipv6_ttl, note = s.note
			WHEN NOT MATCHED BY TARGET THEN
				INSERT (name, letter, ipv4, ipv6, ns_ttl, ipv4_ttl, ipv6_ttl, note)
				VALUES (s.name, s.letter, s.ipv4, s.ipv6, s.ns_ttl, s.ipv4_ttl, s.ipv6_ttl, s.note)
			WHEN NOT MATCHED BY SOURCE THEN
				DELETE
			RETURNING merge_action() AS action
		)
		SELECT action, count(*) FROM m GROUP BY action`)
	if err != nil {
		return ch, "", fmt.Errorf("merge roothints_server: %w", err)
	}
	ch.ServerInserted, ch.ServerUpdated, ch.ServerDeleted = actions["INSERT"], actions["UPDATE"], actions["DELETE"]

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

// stage cria a tabela temporária da carga e a preenche com COPY. Endereço
// ausente (e o TTL dele) e comentário vazio viram NULL.
func stage(ctx context.Context, tx pgx.Tx, ds *parse.Dataset) error {
	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE stage_server (
			name      text    PRIMARY KEY,
			letter    text    NOT NULL,
			ipv4      inet,
			ipv6      inet,
			ns_ttl    integer NOT NULL,
			ipv4_ttl  integer,
			ipv6_ttl  integer,
			note      text
		) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("staging: %w", err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"stage_server"},
		[]string{"name", "letter", "ipv4", "ipv6", "ns_ttl", "ipv4_ttl", "ipv6_ttl", "note"},
		pgx.CopyFromSlice(len(ds.Servers), func(i int) ([]any, error) {
			sv := ds.Servers[i]
			v4, v4ttl := nullAddr(sv.IPv4, sv.IPv4TTL)
			v6, v6ttl := nullAddr(sv.IPv6, sv.IPv6TTL)
			return []any{sv.Name, sv.Letter, v4, v6, sv.NSTTL, v4ttl, v6ttl, nullStr(sv.Note)}, nil
		})); err != nil {
		return fmt.Errorf("copy stage_server: %w", err)
	}
	return nil
}

func checkRemoval(ctx context.Context, tx pgx.Tx, threshold float64) error {
	var cur, removed int
	err := tx.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM roothints_server),
		       (SELECT count(*) FROM roothints_server r
		         WHERE NOT EXISTS (SELECT 1 FROM stage_server s WHERE s.name = r.name))`).
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
	// Cabeçalho e contagens só existem quando o parser aceitou o arquivo.
	parsed := make([]any, 5)
	if run.Parsed {
		parsed = []any{run.Header.LastUpdate, run.Header.ZoneSerial, run.Servers, run.IPv4Addresses, run.IPv6Addresses}
	}
	counts := make([]any, 3)
	if ch != nil {
		counts = []any{ch.ServerInserted, ch.ServerUpdated, ch.ServerDeleted}
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
		INSERT INTO roothints_run (
			status, forced, url, http_status, etag, last_modified,
			md5, sha256, bytes,
			last_update, zone_serial, servers, ipv4_addresses, ipv6_addresses,
			server_inserted, server_updated, server_deleted,
			warnings, error, started_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::date, $11, $12, $13, $14, $15, $16, $17,
		          $18::jsonb, $19, $20)
		RETURNING uuid::text`, args...).Scan(&version)
	if err != nil {
		return "", fmt.Errorf("roothints_run: %w", err)
	}
	return version, nil
}

// nullAddr devolve o endereço e o TTL, ou NULL nos dois quando o registro não
// existe no arquivo.
func nullAddr(a netip.Addr, ttl int64) (any, any) {
	if !a.IsValid() {
		return nil, nil
	}
	return a, ttl
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
