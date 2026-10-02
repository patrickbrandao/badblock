// Package store faz as consultas da API nas tabelas ripencc_* e jobs.
//
// Só leitura (nenhum INSERT/UPDATE/DELETE). O schema é de
// database/postgres/ripencc/; as consultas são as documentadas em
// specs/fontes/rir/dados.md#consultas-da-api e usam os índices de lá.
package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/patrickbrandao/badblock/apps/ripencc/api/internal/rir"
)

// CollectorApp é a linha de jobs que descreve a fonte destes dados.
const CollectorApp = rir.Collector

// ErrNotFound indica que o registro pedido não existe.
var ErrNotFound = errors.New("não encontrado")

// Dataset é o último arquivo aplicado pelo collector (última linha
// status = 1 de ripencc_run). Campos que o schema permite NULL são ponteiros.
type Dataset struct {
	Version     string // ripencc_run.uuid
	AppliedAt   time.Time
	URL         string
	SHA256      *string
	MD5         *string
	Serial      *string
	StartDate   *time.Time
	EndDate     *time.Time
	ASNRecords  *int
	IPv4Records *int
	IPv6Records *int
	PrefixesV4  *int
	PrefixesV6  *int
}

// Job é a linha do collector na tabela jobs.
type Job struct {
	LastSyncAt   *time.Time
	LastCheckAt  *time.Time
	Consolidated int
}

// Info são os dados de delegação comuns a ASNs e blocos.
type Info struct {
	CC       *string    // NULL = vazio na fonte
	RegDate  *time.Time // NULL = vazia ou 00000000 na fonte
	Status   string
	OpaqueID *string // NULL = vazio na fonte (available/reserved)
}

// ASNRange é um registro de ASN (uma faixa; quase sempre de um ASN só).
type ASNRange struct {
	Start     int64
	End       int64
	Count     int64
	Info      Info
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Block é um bloco CIDR de ripencc_prefix.
type Block struct {
	Prefix      netip.Prefix
	Info        Info
	RecordStart netip.Addr // início do registro de origem
	RecordValue int64      // IPv4: endereços; IPv6: tamanho do prefixo
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Holder são todos os recursos de um titular (opaque_id).
type Holder struct {
	OpaqueID string // como gravado na tabela
	ASNs     []ASNRange
	Blocks   []Block // IPv4 antes, cada família em ordem de endereço
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
		cfg.ConnConfig.RuntimeParams["application_name"] = rir.App
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
		SELECT uuid::text, created_at, url, sha256, md5, serial, start_date, end_date,
		       asn_records, ipv4_records, ipv6_records, prefixes_v4, prefixes_v6
		  FROM ripencc_run
		 WHERE status = 1
		 ORDER BY created_at DESC
		 LIMIT 1`).Scan(&d.Version, &d.AppliedAt, &d.URL, &d.SHA256, &d.MD5, &d.Serial,
		&d.StartDate, &d.EndDate, &d.ASNRecords, &d.IPv4Records, &d.IPv6Records, &d.PrefixesV4, &d.PrefixesV6)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Job devolve a linha do collector em jobs, ou nil se não existir.
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

// ASN devolve o registro que contém o ASN. As faixas não se sobrepõem na
// fonte: o candidato é o de maior asn_start <= asn (índice único de
// asn_start), e ele só vale se asn_end >= asn.
func (s *Store) ASN(ctx context.Context, asn int64) (*ASNRange, error) {
	var a ASNRange
	err := s.pool.QueryRow(ctx, `
		SELECT asn_start, asn_end, asn_count, cc, reg_date, status, opaque_id, created_at, updated_at
		  FROM ripencc_asn
		 WHERE asn_start <= $1
		 ORDER BY asn_start DESC
		 LIMIT 1`, asn).
		Scan(&a.Start, &a.End, &a.Count, &a.Info.CC, &a.Info.RegDate, &a.Info.Status, &a.Info.OpaqueID,
			&a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && a.End < asn) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// Covering devolve o bloco mais específico que contém p (GiST inet_ops). Bits
// de host de p são zerados (o tipo cidr os recusa).
func (s *Store) Covering(ctx context.Context, p netip.Prefix) (*Block, error) {
	p = p.Masked()
	var b Block
	var start string
	err := s.pool.QueryRow(ctx, `
		SELECT prefix, cc, reg_date, status, opaque_id, host(record_start), record_value, created_at, updated_at
		  FROM ripencc_prefix
		 WHERE prefix >>= $1::cidr
		 ORDER BY masklen(prefix) DESC
		 LIMIT 1`, p).
		Scan(&b.Prefix, &b.Info.CC, &b.Info.RegDate, &b.Info.Status, &b.Info.OpaqueID, &start, &b.RecordValue,
			&b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if b.RecordStart, err = netip.ParseAddr(start); err != nil {
		return nil, fmt.Errorf("record_start inválido %q: %w", start, err)
	}
	return &b, nil
}

// Holder devolve todos os recursos de um titular. O opaque_id é comparado
// exatamente e, se não achar, sem diferenciar maiúsculas (os RIRs usam hex
// maiúsculo ou minúsculo); vale o valor gravado na tabela. ErrNotFound se o
// titular não tem nenhum recurso.
func (s *Store) Holder(ctx context.Context, id string) (*Holder, error) {
	variants := []string{id}
	for _, v := range []string{strings.ToUpper(id), strings.ToLower(id)} {
		if !slices.Contains(variants, v) {
			variants = append(variants, v)
		}
	}
	h := Holder{}
	err := s.pool.QueryRow(ctx, `
		WITH ids AS (
		    SELECT opaque_id FROM ripencc_asn    WHERE opaque_id = ANY($1::text[])
		    UNION
		    SELECT opaque_id FROM ripencc_prefix WHERE opaque_id = ANY($1::text[])
		)
		SELECT opaque_id FROM ids ORDER BY opaque_id = $2 DESC, opaque_id LIMIT 1`, variants, id).
		Scan(&h.OpaqueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT asn_start, asn_end, asn_count, cc, reg_date, status, opaque_id, created_at, updated_at
		  FROM ripencc_asn WHERE opaque_id = $1 ORDER BY asn_start`, h.OpaqueID)
	if err != nil {
		return nil, err
	}
	h.ASNs, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ASNRange, error) {
		var a ASNRange
		err := r.Scan(&a.Start, &a.End, &a.Count, &a.Info.CC, &a.Info.RegDate, &a.Info.Status, &a.Info.OpaqueID,
			&a.CreatedAt, &a.UpdatedAt)
		return a, err
	})
	if err != nil {
		return nil, err
	}

	rows, err = s.pool.Query(ctx, `
		SELECT prefix, cc, reg_date, status, opaque_id, host(record_start), record_value, created_at, updated_at
		  FROM ripencc_prefix WHERE opaque_id = $1 ORDER BY family, prefix`, h.OpaqueID)
	if err != nil {
		return nil, err
	}
	h.Blocks, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Block, error) {
		var b Block
		var start string
		if err := r.Scan(&b.Prefix, &b.Info.CC, &b.Info.RegDate, &b.Info.Status, &b.Info.OpaqueID, &start,
			&b.RecordValue, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return b, err
		}
		var err error
		b.RecordStart, err = netip.ParseAddr(start)
		return b, err
	})
	if err != nil {
		return nil, err
	}
	return &h, nil
}
