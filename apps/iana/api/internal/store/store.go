// Package store faz as consultas da api-iana nas tabelas iana_* e jobs.
//
// Só leitura (nenhum INSERT/UPDATE/DELETE). O schema é de
// database/postgres/iana/. As consultas que juntam várias tabelas rodam numa
// transação só de leitura com REPEATABLE READ: como o collector-iana aplica os
// 10 arquivos numa transação só, a resposta nunca mistura dois datasets.
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
)

// CollectorApp é a linha de jobs que descreve a fonte destes dados.
const CollectorApp = "collector-iana"

// Nomes dos arquivos RDAP em iana_run.files (collector-iana, internal/source).
const (
	FileRDAPASN  = "rdap-asn"
	FileRDAPIPv4 = "rdap-ipv4"
	FileRDAPIPv6 = "rdap-ipv6"
)

// Dataset é o último dataset aplicado pelo collector-iana.
type Dataset struct {
	Version   string // iana_run.uuid
	AppliedAt time.Time
	SHA256    string
	Files     []File
}

// File é um objeto de iana_run.files (um dos 10 arquivos da IANA). Os campos
// opcionais ficam nil quando o collector não os gravou.
type File struct {
	Name         string  `json:"name"`
	URL          string  `json:"url"`
	HTTPStatus   int     `json:"http_status"`
	ETag         *string `json:"etag"`
	LastModified *string `json:"last_modified"`
	SHA256       string  `json:"sha256"`
	Bytes        int64   `json:"bytes"`
	Changed      bool    `json:"changed"`
	Rows         *int    `json:"rows"`
	Publication  *string `json:"publication"`
}

// Publication devolve o campo publication do arquivo name, ou nil.
func (d *Dataset) Publication(name string) *string {
	if d == nil {
		return nil
	}
	for _, f := range d.Files {
		if f.Name == name {
			return f.Publication
		}
	}
	return nil
}

// Job é a linha do collector-iana na tabela jobs.
type Job struct {
	LastSyncAt   *time.Time
	LastCheckAt  *time.Time
	Consolidated int
}

// ASNBlock é uma faixa de iana_asn_block.
type ASNBlock struct {
	Start            int64
	End              int64
	Description      string
	Registry         *string
	Whois            *string
	RDAPURLs         []string
	Reference        *string
	RegistrationDate *string
}

// PrefixBlock é um bloco de iana_prefix_block.
type PrefixBlock struct {
	Prefix         netip.Prefix
	Designation    string
	Registry       *string
	Whois          *string
	RDAPURLs       []string
	Status         string
	AllocationDate *string
	Note           *string
}

// SpecialPrefix é um bloco de iana_special_prefix. Flags nil = a IANA
// publica vazio ou N/A.
type SpecialPrefix struct {
	Prefix             netip.Prefix
	Name               string
	RFC                *string
	AllocationDate     *string
	TerminationDate    *string
	Source             *bool
	Destination        *bool
	Forwardable        *bool
	GloballyReachable  *bool
	ReservedByProtocol *bool
}

// SpecialASN é uma faixa de iana_special_asn.
type SpecialASN struct {
	Start     int64
	End       int64
	Reason    string
	Reference *string
}

// RDAPService é uma entrada de iana_rdap_service.
type RDAPService struct {
	Kind     string // asn, ipv4 ou ipv6
	Resource string
	Registry *string
	URLs     []string
}

// ASNLookup reúne o que a IANA diz sobre um ASN.
type ASNLookup struct {
	Block   *ASNBlock    // nil se nenhuma faixa contém o ASN
	Special []SpecialASN // faixas especiais que contêm o ASN (vazio = nenhuma)
	RDAP    *RDAPService // nil se nenhum servidor RDAP responde pelo ASN
}

// PrefixLookup reúne o que a IANA diz sobre um IP ou prefixo.
type PrefixLookup struct {
	Block   *PrefixBlock    // bloco mais específico que contém a consulta
	Special []SpecialPrefix // blocos especiais que contêm a consulta, do mais específico ao menos
	RDAP    *RDAPService    // servidor RDAP mais específico que contém a consulta
	// Unreserved diz se algum bloco da IANA que cruza a consulta tem status
	// diferente de RESERVED (entra na regra de bogon, ver Bogon).
	Unreserved bool
}

// SpecialLists são os registros de uso especial inteiros.
type SpecialLists struct {
	Prefixes []SpecialPrefix
	ASNs     []SpecialASN
}

// RDAPBootstrap são as 3 listas do bootstrap RDAP com a data de publicação
// de cada JSON.
type RDAPBootstrap struct {
	Services    []RDAPService // ordem: asn (por faixa), ipv4, ipv6 (por bloco)
	Publication map[string]*string
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
		cfg.ConnConfig.RuntimeParams["application_name"] = "api-iana"
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

// readTx roda fn numa transação só de leitura com um snapshot único.
func (s *Store) readTx(ctx context.Context, fn func(pgx.Tx) error) error {
	return pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, fn)
}

// querier é o que pool e transação têm em comum.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Dataset devolve o último dataset aplicado, ou nil se ainda não houve carga.
func (s *Store) Dataset(ctx context.Context) (*Dataset, error) {
	return dataset(ctx, s.pool)
}

func dataset(ctx context.Context, q querier) (*Dataset, error) {
	var d Dataset
	var files []byte
	err := q.QueryRow(ctx, `
		SELECT uuid::text, created_at, sha256, files
		  FROM iana_run
		 WHERE status = 1
		 ORDER BY created_at DESC
		 LIMIT 1`).Scan(&d.Version, &d.AppliedAt, &d.SHA256, &files)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(files, &d.Files); err != nil {
		return nil, fmt.Errorf("iana_run.files ilegível: %w", err)
	}
	return &d, nil
}

// Job devolve a linha do collector-iana em jobs, ou nil se não existir.
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

const (
	asnBlockCols      = `asn_start, asn_end, description, registry, whois, rdap_urls, reference, registration_date`
	prefixBlockCols   = `prefix, designation, registry, whois, rdap_urls, status, allocation_date, note`
	specialPrefixCols = `prefix, name, rfc, allocation_date, termination_date,
		source, destination, forwardable, globally_reachable, reserved_by_protocol`
	specialASNCols = `asn_start, asn_end, reason, reference`
	rdapCols       = `kind, resource, registry, urls`
)

func scanASNBlock(row pgx.CollectableRow) (ASNBlock, error) {
	var b ASNBlock
	err := row.Scan(&b.Start, &b.End, &b.Description, &b.Registry, &b.Whois, &b.RDAPURLs, &b.Reference, &b.RegistrationDate)
	return b, err
}

func scanPrefixBlock(row pgx.CollectableRow) (PrefixBlock, error) {
	var b PrefixBlock
	err := row.Scan(&b.Prefix, &b.Designation, &b.Registry, &b.Whois, &b.RDAPURLs, &b.Status, &b.AllocationDate, &b.Note)
	return b, err
}

func scanSpecialPrefix(row pgx.CollectableRow) (SpecialPrefix, error) {
	var p SpecialPrefix
	err := row.Scan(&p.Prefix, &p.Name, &p.RFC, &p.AllocationDate, &p.TerminationDate,
		&p.Source, &p.Destination, &p.Forwardable, &p.GloballyReachable, &p.ReservedByProtocol)
	return p, err
}

func scanSpecialASN(row pgx.CollectableRow) (SpecialASN, error) {
	var a SpecialASN
	err := row.Scan(&a.Start, &a.End, &a.Reason, &a.Reference)
	return a, err
}

func scanRDAP(row pgx.CollectableRow) (RDAPService, error) {
	var r RDAPService
	err := row.Scan(&r.Kind, &r.Resource, &r.Registry, &r.URLs)
	return r, err
}

// collectOne devolve a primeira linha, ou nil se não houver nenhuma.
func collectOne[T any](rows pgx.Rows, err error, fn pgx.RowToFunc[T]) (*T, error) {
	if err != nil {
		return nil, err
	}
	v, err := pgx.CollectExactlyOneRow(rows, fn)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func collectAll[T any](rows pgx.Rows, err error, fn pgx.RowToFunc[T]) ([]T, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, fn)
}

// ASN devolve a faixa da IANA, as faixas especiais e o servidor RDAP de um ASN.
func (s *Store) ASN(ctx context.Context, asn int64) (*ASNLookup, error) {
	var out ASNLookup
	err := s.readTx(ctx, func(tx pgx.Tx) error {
		// As faixas não se sobrepõem: a de maior início <= X é a única candidata.
		rows, err := tx.Query(ctx, `
			SELECT `+asnBlockCols+`
			  FROM iana_asn_block
			 WHERE asn_start <= $1 AND asn_end >= $1
			 ORDER BY asn_start DESC
			 LIMIT 1`, asn)
		if out.Block, err = collectOne(rows, err, scanASNBlock); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `
			SELECT `+specialASNCols+`
			  FROM iana_special_asn
			 WHERE asn_start <= $1 AND asn_end >= $1
			 ORDER BY asn_start`, asn)
		if out.Special, err = collectAll(rows, err, scanSpecialASN); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `
			SELECT `+rdapCols+`
			  FROM iana_rdap_service
			 WHERE kind = 'asn' AND asn_start <= $1 AND asn_end >= $1
			 ORDER BY asn_start DESC
			 LIMIT 1`, asn)
		out.RDAP, err = collectOne(rows, err, scanRDAP)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Prefix devolve o bloco da IANA, os blocos especiais e o servidor RDAP que
// contêm p (um IP é um prefixo /32 ou /128).
func (s *Store) Prefix(ctx context.Context, p netip.Prefix) (*PrefixLookup, error) {
	kind := "ipv4"
	if p.Addr().Is6() {
		kind = "ipv6"
	}
	var out PrefixLookup
	err := s.readTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT `+prefixBlockCols+`
			  FROM iana_prefix_block
			 WHERE prefix >>= $1::cidr
			 ORDER BY masklen(prefix) DESC
			 LIMIT 1`, p)
		if out.Block, err = collectOne(rows, err, scanPrefixBlock); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `
			SELECT `+specialPrefixCols+`
			  FROM iana_special_prefix
			 WHERE prefix >>= $1::cidr
			 ORDER BY masklen(prefix) DESC`, p)
		if out.Special, err = collectAll(rows, err, scanSpecialPrefix); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `
			SELECT `+rdapCols+`
			  FROM iana_rdap_service
			 WHERE kind = $2 AND prefix >>= $1::cidr
			 ORDER BY masklen(prefix) DESC
			 LIMIT 1`, p, kind)
		if out.RDAP, err = collectOne(rows, err, scanRDAP); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM iana_prefix_block
			                WHERE prefix && $1::cidr AND status <> 'RESERVED')`, p).Scan(&out.Unreserved)
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ASNBlocks devolve todas as faixas de ASN, em ordem numérica.
func (s *Store) ASNBlocks(ctx context.Context) ([]ASNBlock, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+asnBlockCols+` FROM iana_asn_block ORDER BY asn_start`)
	return collectAll(rows, err, scanASNBlock)
}

// PrefixBlocks devolve os blocos de uma família (4 ou 6), em ordem de endereço.
func (s *Store) PrefixBlocks(ctx context.Context, family int) ([]PrefixBlock, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+prefixBlockCols+` FROM iana_prefix_block WHERE family = $1 ORDER BY prefix`, family)
	return collectAll(rows, err, scanPrefixBlock)
}

// Special devolve os blocos especiais das duas famílias (IPv4 primeiro, em
// ordem de endereço) e as faixas de ASN especiais (em ordem numérica).
func (s *Store) Special(ctx context.Context) (*SpecialLists, error) {
	var out SpecialLists
	err := s.readTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT `+specialPrefixCols+` FROM iana_special_prefix ORDER BY family, prefix`)
		if out.Prefixes, err = collectAll(rows, err, scanSpecialPrefix); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT `+specialASNCols+` FROM iana_special_asn ORDER BY asn_start`)
		out.ASNs, err = collectAll(rows, err, scanSpecialASN)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// RDAP devolve o bootstrap RDAP inteiro e a data de publicação de cada JSON,
// do mesmo dataset.
func (s *Store) RDAP(ctx context.Context) (*RDAPBootstrap, error) {
	out := RDAPBootstrap{Publication: map[string]*string{}}
	err := s.readTx(ctx, func(tx pgx.Tx) error {
		d, err := dataset(ctx, tx)
		if err != nil {
			return err
		}
		for _, name := range []string{FileRDAPASN, FileRDAPIPv4, FileRDAPIPv6} {
			out.Publication[name] = d.Publication(name)
		}
		rows, err := tx.Query(ctx, `
			SELECT `+rdapCols+`
			  FROM iana_rdap_service
			 ORDER BY CASE kind WHEN 'asn' THEN 0 WHEN 'ipv4' THEN 1 ELSE 2 END, asn_start, prefix`)
		out.Services, err = collectAll(rows, err, scanRDAP)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
