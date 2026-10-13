// Package store faz as consultas da api-rdns nas tabelas rdns_* e jobs.
//
// Só leitura (nenhum INSERT/UPDATE/DELETE). O schema é de
// database/postgres/rdns/; as consultas estão em
// specs/fontes/rdns/dados.md#consultas-da-api-rdns.
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
const CollectorApp = "collector-rdns"

// ruleSQL é a regra da sonda: só IPv4 de /22 a /24 é varrido.
const ruleSQL = "family(prefix) = 4 AND masklen(prefix) BETWEEN 22 AND 24"

// ErrNotFound indica que o registro pedido não existe.
var ErrNotFound = errors.New("não encontrado")

// Prefix é a linha de rdns_prefix que contém um IP ou prefixo.
type Prefix struct {
	Prefix     netip.Prefix
	ASN        int64
	Source     string
	Active     bool
	LastScanAt *time.Time
}

// InRule diz se a sonda varre o prefixo (IPv4 de /22 a /24).
func (p Prefix) InRule() bool {
	return p.Prefix.Addr().Is4() && p.Prefix.Bits() >= 22 && p.Prefix.Bits() <= 24
}

// PTR é a linha de um IP em rdns_ptr.
type PTR struct {
	IP        netip.Addr
	Status    int16 // 1=ptr | 2=servfail | 3=refused | 4=timeout
	Name      *string
	Names     []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IPRow é um IP: o prefixo que o contém e a linha dele (nil = sem linha).
type IPRow struct {
	Prefix Prefix
	PTR    *PTR
}

// PrefixRows é um prefixo pedido: o prefixo de rdns_prefix que o contém e as
// linhas dos IPs dele, em ordem.
type PrefixRows struct {
	Prefix Prefix
	PTRs   []PTR
}

// ScanStats é o bloco scan do /meta.
type ScanStats struct {
	Prefixes     int
	Active       int
	Static       int
	OutOfRule    int
	Addresses    int64
	NeverScanned int
	Overdue      int
	Rescan       int
	Scanning     int
	OldestScanAt *time.Time
	PTR          int64
	NoPTR        int64
	Failed       int64
}

// Derivation é a última derivação aplicada de uma origem (rdns_run).
type Derivation struct {
	Source          string
	Version         string
	SourceVersion   string
	SourceUpdatedAt time.Time
	CreatedAt       time.Time
	Blocks          int
	Units           int
	Inserted        int
	ASNUpdated      int
	Covered         int
	NotInSource     int
}

// Worker é uma instância do collector-rdns com sinal nas últimas 24 h.
type Worker struct {
	Host        string
	Version     string
	StartedAt   time.Time
	SeenAt      time.Time
	Alive       bool
	RateLimit   int
	Queries     int64
	LiveQueries int64
	Prefixes    int64
}

// Job é a linha do collector-rdns na tabela jobs.
type Job struct {
	LastSyncAt   *time.Time
	LastCheckAt  *time.Time
	Consolidated int
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
		cfg.ConnConfig.RuntimeParams["application_name"] = "api-rdns"
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

// HasPrefixes diz se já há algum prefixo cadastrado (a API está pronta).
func (s *Store) HasPrefixes(ctx context.Context) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM rdns_prefix)").Scan(&ok)
	return ok, err
}

// IP devolve o prefixo que contém ip e a linha dele em rdns_ptr.
func (s *Store) IP(ctx context.Context, ip netip.Addr) (*IPRow, error) {
	var r IPRow
	var status *int16
	var created, updated *time.Time
	var name *string
	var names []string
	err := s.pool.QueryRow(ctx, `
		SELECT p.prefix, p.asn, p.source, p.active, p.last_scan_at,
		       r.status, r.name, r.names, r.created_at, r.updated_at
		  FROM rdns_prefix p
		  LEFT JOIN rdns_ptr r ON r.ip = $1::inet
		 WHERE p.prefix >>= $1::inet`, ip.String()).
		Scan(&r.Prefix.Prefix, &r.Prefix.ASN, &r.Prefix.Source, &r.Prefix.Active, &r.Prefix.LastScanAt,
			&status, &name, &names, &created, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != nil {
		r.PTR = &PTR{IP: ip, Status: *status, Name: name, Names: names, CreatedAt: *created, UpdatedAt: *updated}
	}
	return &r, nil
}

// Prefix devolve o prefixo de rdns_prefix que contém p e as linhas dos IPs de
// p em rdns_ptr, em ordem de endereço.
func (s *Store) Prefix(ctx context.Context, p netip.Prefix) (*PrefixRows, error) {
	var r PrefixRows
	err := s.pool.QueryRow(ctx, `
		SELECT prefix, asn, source, active, last_scan_at FROM rdns_prefix WHERE prefix >>= $1::cidr`, p.String()).
		Scan(&r.Prefix.Prefix, &r.Prefix.ASN, &r.Prefix.Source, &r.Prefix.Active, &r.Prefix.LastScanAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT ip, status, name, names FROM rdns_ptr WHERE ip <<= $1::cidr ORDER BY ip`, p.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ip netip.Prefix
		var row PTR
		if err := rows.Scan(&ip, &row.Status, &row.Name, &row.Names); err != nil {
			return nil, err
		}
		row.IP = ip.Addr()
		r.PTRs = append(r.PTRs, row)
	}
	return &r, rows.Err()
}

// Config devolve rdns_config inteira (chave → valor).
func (s *Store) Config(ctx context.Context) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, "SELECT key, value FROM rdns_config")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// ScanStats resume rdns_prefix inteira; overdue usa interval.
func (s *Store) ScanStats(ctx context.Context, interval time.Duration) (*ScanStats, error) {
	var st ScanStats
	err := s.pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE active),
		       count(*) FILTER (WHERE source = 'static'),
		       count(*) FILTER (WHERE NOT (`+ruleSQL+`)),
		       coalesce(sum(1::bigint << (32 - masklen(prefix))) FILTER (WHERE active AND `+ruleSQL+`), 0)::bigint,
		       count(*) FILTER (WHERE active AND `+ruleSQL+` AND last_scan_at IS NULL),
		       count(*) FILTER (WHERE active AND `+ruleSQL+` AND last_scan_at < NOW() - $1::float8 * interval '1 second'),
		       count(*) FILTER (WHERE active AND `+ruleSQL+` AND rescan),
		       count(*) FILTER (WHERE scan_worker IS NOT NULL),
		       min(last_scan_at) FILTER (WHERE active AND `+ruleSQL+`),
		       coalesce(sum(last_scan_ptr), 0),
		       coalesce(sum(last_scan_no_ptr), 0),
		       coalesce(sum(last_scan_failed), 0)
		  FROM rdns_prefix`, interval.Seconds()).
		Scan(&st.Prefixes, &st.Active, &st.Static, &st.OutOfRule, &st.Addresses, &st.NeverScanned, &st.Overdue,
			&st.Rescan, &st.Scanning, &st.OldestScanAt, &st.PTR, &st.NoPTR, &st.Failed)
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// Derivations devolve a última derivação aplicada de cada origem.
func (s *Store) Derivations(ctx context.Context) ([]Derivation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (source) source, uuid::text, source_version::text, source_updated_at, created_at,
		       blocks, units, inserted, asn_updated, covered, not_in_source
		  FROM rdns_run
		 WHERE status = 1
		 ORDER BY source, created_at DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Derivation])
}

// Workers devolve as instâncias com sinal nas últimas 24 h; viva = sinal há
// menos de 90 s.
func (s *Store) Workers(ctx context.Context) ([]Worker, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT host, version, created_at, updated_at, updated_at > NOW() - interval '90 seconds',
		       rate_limit, queries, live_queries, prefixes
		  FROM rdns_worker
		 WHERE updated_at > NOW() - interval '24 hours'
		 ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Worker])
}

// Job devolve a linha do collector-rdns em jobs, ou nil se não existir.
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
