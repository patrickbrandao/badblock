// Package store concentra todo o acesso do registry-sync ao Postgres: estado
// das fontes, histórico de execuções, carga das tabelas de ingest e a
// reconstrução das tabelas centrais.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// cycleLockKey identifica o advisory lock que impede dois registry-sync (ou um
// 'registry-sync --once' manual ao lado do serviço) de sincronizar ao mesmo
// tempo.
const cycleLockKey int64 = 0x62616462_6c6f636b // "badblock"

// Store é o acesso ao banco.
type Store struct {
	pool *pgxpool.Pool
}

// Open conecta ao Postgres e confere a conexão.
func Open(ctx context.Context, url string, maxConns int32) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("POSTGRES_URL inválida: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = "badblock-registry-sync"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Store{pool: pool}, nil
}

// Close encerra o pool.
func (s *Store) Close() { s.pool.Close() }

// Ping confere se o banco responde.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// SchemaReady confere se as migrations já criaram as tabelas usadas.
func (s *Store) SchemaReady(ctx context.Context) error {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT to_regclass('ingest.source_state') IS NOT NULL
		   AND to_regclass('registry.change_log') IS NOT NULL`).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("schema ausente: rode as migrations (make -C database/postgresql migrate)")
	}
	return nil
}

// TryLock tenta pegar o lock de ciclo numa conexão dedicada. Devolve ok=false
// se outro processo já está sincronizando. unlock libera o lock e a conexão.
func (s *Store) TryLock(ctx context.Context) (unlock func(), ok bool, err error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, cycleLockKey).Scan(&ok); err != nil {
		conn.Release()
		return nil, false, err
	}
	if !ok {
		conn.Release()
		return nil, false, nil
	}
	return func() {
		// Contexto próprio: o lock precisa ser liberado mesmo se o ciclo foi
		// cancelado.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, cycleLockKey)
		conn.Release()
	}, true, nil
}

// State é uma linha de ingest.source_state.
type State struct {
	SourceID      string
	URL           string
	ETag          string
	LastModified  string
	ContentSHA256 string
	FileDate      *time.Time
	Records       int64
	LastCheckedAt *time.Time
	LastChangedAt *time.Time
	LastSuccessAt *time.Time
	LastError     string
	LastErrorAt   *time.Time
}

// States lê o estado de todas as fontes já vistas.
func (s *Store) States(ctx context.Context) (map[string]State, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT source_id, coalesce(url, ''), coalesce(etag, ''), coalesce(last_modified, ''),
		       coalesce(content_sha256, ''), file_date, coalesce(records, 0),
		       last_checked_at, last_changed_at, last_success_at,
		       coalesce(last_error, ''), last_error_at
		  FROM ingest.source_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]State{}
	for rows.Next() {
		var st State
		if err := rows.Scan(&st.SourceID, &st.URL, &st.ETag, &st.LastModified, &st.ContentSHA256,
			&st.FileDate, &st.Records, &st.LastCheckedAt, &st.LastChangedAt, &st.LastSuccessAt,
			&st.LastError, &st.LastErrorAt); err != nil {
			return nil, err
		}
		out[st.SourceID] = st
	}
	return out, rows.Err()
}

// Run é uma linha de ingest.source_run.
type Run struct {
	SourceID   string
	StartedAt  time.Time
	FinishedAt time.Time
	Outcome    string // unchanged, applied, aborted, failed, stale
	URL        string
	HTTPStatus int
	Bytes      int64
	SHA256     string
	FileDate   *time.Time
	Records    int64
	Inserted   int64
	Updated    int64
	Deleted    int64
	Message    string
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func insertRun(ctx context.Context, db execer, r Run) error {
	_, err := db.Exec(ctx, `
		INSERT INTO ingest.source_run
		       (source_id, started_at, finished_at, outcome, url, http_status, bytes, content_sha256,
		        file_date, records, inserted, updated, deleted, message)
		VALUES ($1::text, $2::timestamptz, $3::timestamptz, $4::text, nullif($5::text, ''),
		        nullif($6::integer, 0), nullif($7::bigint, 0), nullif($8::text, ''), $9::date,
		        nullif($10::bigint, 0), $11::bigint, $12::bigint, $13::bigint, nullif($14::text, ''))`,
		r.SourceID, r.StartedAt, r.FinishedAt, r.Outcome, r.URL, r.HTTPStatus, r.Bytes, r.SHA256,
		r.FileDate, r.Records, r.Inserted, r.Updated, r.Deleted, r.Message)
	return err
}

// RecordCheck grava uma verificação que não aplicou dados (unchanged, failed,
// aborted, stale): a linha de source_run e o last_checked_at da fonte. Em
// falha, guarda também o erro; em sucesso sem mudança, limpa o erro anterior.
// Um 304 renova os validadores (ETag/Last-Modified) da mesma URL.
func (s *Store) RecordCheck(ctx context.Context, r Run, etag, lastModified string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	failed := r.Outcome == "failed" || r.Outcome == "aborted"
	_, err = tx.Exec(ctx, `
		INSERT INTO ingest.source_state (source_id, last_checked_at, last_error, last_error_at, updated_at)
		VALUES ($1::text, $2::timestamptz, CASE WHEN $3::boolean THEN $4::text END,
		        CASE WHEN $3::boolean THEN $2::timestamptz END, now())
		ON CONFLICT (source_id) DO UPDATE SET
		       last_checked_at = excluded.last_checked_at,
		       last_error      = CASE WHEN $3::boolean THEN $4::text END,
		       last_error_at   = CASE WHEN $3::boolean THEN $2::timestamptz ELSE source_state.last_error_at END,
		       last_success_at = CASE WHEN $3::boolean THEN source_state.last_success_at ELSE $2::timestamptz END,
		       etag            = CASE WHEN $5::text <> '' AND source_state.url = $6::text
		                              THEN $5::text ELSE source_state.etag END,
		       last_modified   = CASE WHEN $7::text <> '' AND source_state.url = $6::text
		                              THEN $7::text ELSE source_state.last_modified END,
		       updated_at      = now()`,
		r.SourceID, r.FinishedAt, failed, r.Message, etag, r.URL, lastModified)
	if err != nil {
		return err
	}
	if err := insertRun(ctx, tx, r); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PruneRuns apaga o histórico de execuções mais antigo que retention.
func (s *Store) PruneRuns(ctx context.Context, retention time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM ingest.source_run WHERE started_at < now() - make_interval(secs => $1)`,
		retention.Seconds())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Dataset é a versão atual do central.
type Dataset struct {
	ID      int64
	BuiltAt time.Time
}

// LatestDataset devolve a versão mais recente; ok=false se nunca houve build.
func (s *Store) LatestDataset(ctx context.Context) (Dataset, bool, error) {
	var d Dataset
	err := s.pool.QueryRow(ctx, `SELECT id, built_at FROM registry.dataset ORDER BY id DESC LIMIT 1`).
		Scan(&d.ID, &d.BuiltAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, false, nil
	}
	return d, err == nil, err
}

// HasIngestData diz se já existe algum dado de RIR carregado em ingest.
func (s *Store) HasIngestData(ctx context.Context) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM ingest.delegation)`).Scan(&ok)
	return ok, err
}

// PendingRemovals diz se há linhas cuja carência de remoção já venceu, o que
// pede uma reconstrução mesmo sem fonte nova.
func (s *Store) PendingRemovals(ctx context.Context, grace time.Duration) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM registry.asn    WHERE removed_at IS NULL AND missing_since <= now() - make_interval(secs => $1))
		    OR EXISTS (SELECT 1 FROM registry.prefix WHERE removed_at IS NULL AND missing_since <= now() - make_interval(secs => $1))
		    OR EXISTS (SELECT 1 FROM registry.holder WHERE removed_at IS NULL AND missing_since <= now() - make_interval(secs => $1))`,
		grace.Seconds()).Scan(&ok)
	return ok, err
}
