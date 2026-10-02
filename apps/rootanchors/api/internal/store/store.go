// Package store faz as consultas da api-rootanchors nas tabelas rootanchors_*
// e jobs.
//
// Só leitura (nenhum INSERT/UPDATE/DELETE). O schema é de
// database/postgres/rootanchors/ e as consultas são as de
// specs/fontes/rootanchors/dados.md.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CollectorApp é a linha de jobs que descreve a fonte destes dados.
const CollectorApp = "collector-rootanchors"

// ErrNotFound indica que o registro pedido não existe.
var ErrNotFound = errors.New("não encontrado")

// Dataset é o último arquivo aplicado pelo collector-rootanchors, com o
// TrustAnchor dele (há um por arquivo).
type Dataset struct {
	Version      string // rootanchors_run.uuid
	AppliedAt    time.Time
	URL          string
	SHA256       string
	AnchorID     string
	AnchorSource *string // NULL quando o atributo source veio vazio
	Zone         string
	Keys         int
}

// Job é a linha do collector-rootanchors na tabela jobs.
type Job struct {
	LastSyncAt   *time.Time
	LastCheckAt  *time.Time
	Consolidated int
}

// Key é uma linha de rootanchors_key (um KeyDigest do arquivo). PublicKey e
// Flags são nil juntos, nas chaves que só trazem o DS; ValidUntil é nil
// quando a chave não tem data de fim.
type Key struct {
	KeyID      string
	KeyTag     int
	Algorithm  int
	DigestType int
	Digest     string
	PublicKey  *string
	Flags      *int
	ValidFrom  time.Time
	ValidUntil *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// keyColumns é a lista de colunas de Key, na ordem dos campos. Toda consulta
// de chaves usa esta lista e a ordem valid_from, key_id (a do arquivo, na
// prática: da chave mais antiga para a mais nova).
const keyColumns = `key_id, key_tag, algorithm, digest_type, digest, public_key, flags,
	valid_from, valid_until, created_at, updated_at`

// Store é o pool de conexões.
type Store struct {
	pool *pgxpool.Pool
}

// Open conecta e valida com um SELECT 1.
func Open(ctx context.Context, url string, poolMax int) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("POSTGRES_URL inválida: %w", err)
	}
	cfg.MaxConns = int32(poolMax)
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second
	if cfg.ConnConfig.RuntimeParams["application_name"] == "" {
		cfg.ConnConfig.RuntimeParams["application_name"] = "api-rootanchors"
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

// Close fecha o pool.
func (s *Store) Close() { s.pool.Close() }

// Ping confere a conexão.
func (s *Store) Ping(ctx context.Context) error {
	var one int
	return s.pool.QueryRow(ctx, "SELECT 1").Scan(&one)
}

// Dataset devolve o último arquivo aplicado (índice ix_rootanchors_run_applied),
// ou nil se ainda não houve carga. Nas linhas aplicadas sha256, anchor_id,
// zone e keys nunca são NULL; o COALESCE só impede que uma linha fora do
// padrão trave a leitura da versão.
func (s *Store) Dataset(ctx context.Context) (*Dataset, error) {
	var d Dataset
	err := s.pool.QueryRow(ctx, `
		SELECT uuid::text, created_at, url, COALESCE(sha256, ''), COALESCE(anchor_id, ''),
		       anchor_source, COALESCE(zone, '.'), COALESCE(keys, 0)
		  FROM rootanchors_run
		 WHERE status = 1
		 ORDER BY created_at DESC
		 LIMIT 1`).Scan(&d.Version, &d.AppliedAt, &d.URL, &d.SHA256, &d.AnchorID, &d.AnchorSource, &d.Zone, &d.Keys)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Job devolve a linha do collector-rootanchors em jobs, ou nil se não existir.
func (s *Store) Job(ctx context.Context) (*Job, error) {
	var j Job
	err := s.pool.QueryRow(ctx, `
		SELECT last_sync_at, last_check_at, consolidated FROM jobs WHERE app = $1`, CollectorApp).
		Scan(&j.LastSyncAt, &j.LastCheckAt, &j.Consolidated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// Keys devolve todas as chaves (a tabela inteira; poucas linhas).
func (s *Store) Keys(ctx context.Context) ([]Key, error) {
	return s.keys(ctx, `SELECT `+keyColumns+` FROM rootanchors_key ORDER BY valid_from, key_id`)
}

// KeysByTag devolve as chaves de um key tag (índice
// ix_rootanchors_key_key_tag): 0, 1 ou mais, porque o key tag não é único.
func (s *Store) KeysByTag(ctx context.Context, tag int) ([]Key, error) {
	return s.keys(ctx, `SELECT `+keyColumns+` FROM rootanchors_key WHERE key_tag = $1 ORDER BY valid_from, key_id`, tag)
}

func (s *Store) keys(ctx context.Context, sql string, args ...any) ([]Key, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Key])
}
