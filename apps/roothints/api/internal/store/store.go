// Package store faz as consultas da api-roothints nas tabelas roothints_* e
// jobs.
//
// Só leitura (nenhum INSERT/UPDATE/DELETE). O schema é de
// database/postgres/roothints/; as consultas são as de
// specs/fontes/roothints/dados.md (consultas da API).
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
const CollectorApp = "collector-roothints"

// ErrNotFound indica que o registro pedido não existe.
var ErrNotFound = errors.New("não encontrado")

// Dataset é o último arquivo aplicado pelo collector-roothints.
type Dataset struct {
	Version    string // roothints_run.uuid
	AppliedAt  time.Time
	URL        string
	MD5        string
	SHA256     string
	LastUpdate *time.Time // data de "last update:" do cabeçalho
	ZoneSerial *int64     // serial de "related version of root zone:"
	Servers    int
}

// Job é a linha do collector-roothints na tabela jobs.
type Job struct {
	LastSyncAt   *time.Time
	LastCheckAt  *time.Time
	Consolidated int
}

// Server é uma linha de roothints_server. IPv4 e IPv6 são nil quando o
// arquivo não traz aquela família (os TTLs vêm juntos).
type Server struct {
	Name      string
	Letter    string
	IPv4      *netip.Addr
	IPv6      *netip.Addr
	NSTTL     int
	IPv4TTL   *int
	IPv6TTL   *int
	Note      *string
	CreatedAt time.Time
	UpdatedAt time.Time
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
		cfg.ConnConfig.RuntimeParams["application_name"] = "api-roothints"
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

// Dataset devolve o último arquivo aplicado (índice ix_roothints_run_applied),
// ou nil se ainda não houve carga. md5, sha256 e servers são sempre
// preenchidos nas linhas aplicadas; o COALESCE só impede que uma linha fora do
// padrão trave a leitura da versão.
func (s *Store) Dataset(ctx context.Context) (*Dataset, error) {
	var d Dataset
	err := s.pool.QueryRow(ctx, `
		SELECT uuid::text, created_at, url, COALESCE(md5, ''), COALESCE(sha256, ''),
		       last_update, zone_serial, COALESCE(servers, 0)
		  FROM roothints_run
		 WHERE status = 1
		 ORDER BY created_at DESC
		 LIMIT 1`).Scan(&d.Version, &d.AppliedAt, &d.URL, &d.MD5, &d.SHA256, &d.LastUpdate, &d.ZoneSerial, &d.Servers)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Job devolve a linha do collector-roothints em jobs, ou nil se não existir.
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

// serverColumns são as colunas de roothints_server lidas pela API (todas
// menos uuid), na ordem de scanServer.
const serverColumns = `name, letter, ipv4, ipv6, ns_ttl, ipv4_ttl, ipv6_ttl, note, created_at, updated_at`

// Servers devolve todos os servidores em ordem de letra (13 linhas: uma
// página, sem índice).
func (s *Store) Servers(ctx context.Context) ([]Server, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+serverColumns+` FROM roothints_server ORDER BY letter`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Server, error) { return scanServer(r) })
}

// ServerByLetter devolve o servidor de uma letra (em minúsculas), pelo índice
// único uq_roothints_server_letter. Como name = letter + ".root-servers.net"
// (constraints chk_roothints_server_name e chk_roothints_server_letter), a
// consulta pelo nome também chega aqui.
func (s *Store) ServerByLetter(ctx context.Context, letter string) (*Server, error) {
	srv, err := scanServer(s.pool.QueryRow(ctx, `SELECT `+serverColumns+` FROM roothints_server WHERE letter = $1`, letter))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &srv, nil
}

// scanServer lê uma linha de serverColumns. Os endereços são inet /32 e /128
// (host sem máscara): vêm como netip.Prefix e saem só com o endereço.
func scanServer(row pgx.Row) (Server, error) {
	var (
		srv        Server
		ipv4, ipv6 *netip.Prefix
	)
	err := row.Scan(&srv.Name, &srv.Letter, &ipv4, &ipv6, &srv.NSTTL, &srv.IPv4TTL, &srv.IPv6TTL, &srv.Note,
		&srv.CreatedAt, &srv.UpdatedAt)
	if err != nil {
		return Server{}, err
	}
	srv.IPv4, srv.IPv6 = addr(ipv4), addr(ipv6)
	return srv, nil
}

func addr(p *netip.Prefix) *netip.Addr {
	if p == nil {
		return nil
	}
	a := p.Addr()
	return &a
}
