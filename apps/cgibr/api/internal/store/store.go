// Package store faz as consultas da api-cgibr nas tabelas cgibr_* e jobs.
//
// Só leitura (nenhum INSERT/UPDATE/DELETE). O schema é de database/postgres/cgibr/.
package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CollectorApp é a linha de jobs que descreve a fonte destes dados.
const CollectorApp = "collector-cgibr"

// ErrNotFound indica que o registro pedido não existe.
var ErrNotFound = errors.New("não encontrado")

// Dataset é o último arquivo aplicado pelo collector-cgibr.
type Dataset struct {
	Version    string // cgibr_run.uuid
	AppliedAt  time.Time
	URL        string
	SHA256     string
	ASNs       int
	PrefixesV4 int
	PrefixesV6 int
}

// Job é a linha do collector-cgibr na tabela jobs.
type Job struct {
	LastSyncAt   *time.Time
	LastCheckAt  *time.Time
	Consolidated int
}

// ASNBrief é um ASN sem os blocos.
type ASNBrief struct {
	ASN            int64
	Name           string
	Document       string
	DocumentDigits string
}

// ASN é um ASN com os blocos.
type ASN struct {
	ASNBrief
	Prefixes  []netip.Prefix
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Match é o bloco registrado que contém um IP ou prefixo.
type Match struct {
	Prefix netip.Prefix
	ASN    ASNBrief
}

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
		cfg.ConnConfig.RuntimeParams["application_name"] = "api-cgibr"
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

// Dataset devolve o último arquivo aplicado, ou nil se ainda não houve carga.
func (s *Store) Dataset(ctx context.Context) (*Dataset, error) {
	var d Dataset
	err := s.pool.QueryRow(ctx, `
		SELECT uuid::text, created_at, url, sha256, asns, prefixes_v4, prefixes_v6
		  FROM cgibr_run
		 WHERE status = 1
		 ORDER BY created_at DESC
		 LIMIT 1`).Scan(&d.Version, &d.AppliedAt, &d.URL, &d.SHA256, &d.ASNs, &d.PrefixesV4, &d.PrefixesV6)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Job devolve a linha do collector-cgibr em jobs, ou nil se não existir.
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

// ASN devolve um ASN com seus blocos (IPv4 primeiro, em ordem de endereço).
func (s *Store) ASN(ctx context.Context, asn int64) (*ASN, error) {
	var a ASN
	var id string
	err := s.pool.QueryRow(ctx, `
		SELECT uuid::text, asn, name, document, document_digits, created_at, updated_at
		  FROM cgibr_asn WHERE asn = $1`, asn).
		Scan(&id, &a.ASN, &a.Name, &a.Document, &a.DocumentDigits, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT prefix FROM cgibr_prefix WHERE asn_uuid = $1::uuid ORDER BY family, prefix`, id)
	if err != nil {
		return nil, err
	}
	a.Prefixes, err = pgx.CollectRows(rows, pgx.RowTo[netip.Prefix])
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// Covering devolve o bloco registrado mais específico que contém p.
func (s *Store) Covering(ctx context.Context, p netip.Prefix) (*Match, error) {
	var m Match
	err := s.pool.QueryRow(ctx, `
		SELECT p.prefix, a.asn, a.name, a.document, a.document_digits
		  FROM cgibr_prefix p
		  JOIN cgibr_asn a ON a.uuid = p.asn_uuid
		 WHERE p.prefix >>= $1::cidr
		 ORDER BY masklen(p.prefix) DESC
		 LIMIT 1`, p).
		Scan(&m.Prefix, &m.ASN.ASN, &m.ASN.Name, &m.ASN.Document, &m.ASN.DocumentDigits)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ByDocument devolve os ASNs de um titular, pelos dígitos do documento.
func (s *Store) ByDocument(ctx context.Context, digits string) ([]ASNBrief, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT asn, name, document, document_digits
		  FROM cgibr_asn WHERE document_digits = $1 ORDER BY asn`, digits)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ASNBrief])
}

// ListASNs devolve todos os ASNs, em ordem numérica.
func (s *Store) ListASNs(ctx context.Context) ([]ASNBrief, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT asn, name, document, document_digits FROM cgibr_asn ORDER BY asn`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ASNBrief])
}
