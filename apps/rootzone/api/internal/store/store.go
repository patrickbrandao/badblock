// Package store faz as consultas da api-rootzone nas tabelas rootzone_* e
// jobs.
//
// Só leitura (nenhum INSERT/UPDATE/DELETE). O schema é de
// database/postgres/rootzone/; as consultas são as de
// specs/fontes/rootzone/dados.md ("Consultas da API").
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
const CollectorApp = "collector-rootzone"

// ErrNotFound indica que o registro pedido não existe.
var ErrNotFound = errors.New("não encontrado")

// Dataset é a última zona aplicada pelo collector-rootzone, com o SOA dela.
// Os campos do parser (serial, SOA e contagens) são sempre preenchidos nas
// linhas aplicadas; ficam nil só numa linha fora do padrão.
type Dataset struct {
	Version    string // rootzone_run.uuid
	AppliedAt  time.Time
	URL        string
	SHA256     string
	Serial     *int64
	SOAMName   *string
	SOARName   *string
	SOARefresh *int64
	SOARetry   *int64
	SOAExpire  *int64
	SOAMinimum *int64
	TLDs       *int
	Records    *int
	RRSIGs     *int
}

// Job é a linha do collector-rootzone na tabela jobs.
type Job struct {
	LastSyncAt   *time.Time
	LastCheckAt  *time.Time
	Consolidated int
}

// TLD é uma linha de rootzone_tld (as datas só são lidas em TLD e
// Delegation; na listagem ficam zeradas).
type TLD struct {
	TLD           string
	Unicode       string
	Nameservers   int
	NameserversV4 int
	NameserversV6 int
	DSRecords     int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Record é uma RR de rootzone_record sem o dono.
type Record struct {
	Type  string
	RData string
	TTL   int64
}

// Glue é um endereço (A ou AAAA) de um servidor de nome, com o dono.
type Glue struct {
	Owner string
	Type  string
	RData string
	TTL   int64
}

// Delegation é tudo o que a zona raiz diz de um TLD: a linha de
// rootzone_tld, os NS e DS (em ordem de tipo e rdata) e o glue A/AAAA dos
// servidores de nome (em ordem de dono, tipo e rdata).
type Delegation struct {
	TLD     TLD
	Records []Record
	Glue    []Glue
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
		cfg.ConnConfig.RuntimeParams["application_name"] = "api-rootzone"
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

// Dataset devolve a última zona aplicada (ix_rootzone_run_applied), ou nil
// se ainda não houve carga. O COALESCE do sha256 só impede que uma linha
// fora do padrão trave a leitura da versão.
func (s *Store) Dataset(ctx context.Context) (*Dataset, error) {
	var d Dataset
	err := s.pool.QueryRow(ctx, `
		SELECT uuid::text, created_at, url, COALESCE(sha256, ''), serial,
		       soa_mname, soa_rname, soa_refresh, soa_retry, soa_expire, soa_minimum,
		       tlds, records, rrsigs
		  FROM rootzone_run
		 WHERE status = 1
		 ORDER BY created_at DESC
		 LIMIT 1`).Scan(&d.Version, &d.AppliedAt, &d.URL, &d.SHA256, &d.Serial,
		&d.SOAMName, &d.SOARName, &d.SOARefresh, &d.SOARetry, &d.SOAExpire, &d.SOAMinimum,
		&d.TLDs, &d.Records, &d.RRSIGs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Job devolve a linha do collector-rootzone em jobs, ou nil se não existir.
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

// TLDs devolve todos os TLDs delegados, em ordem de nome (a tabela inteira,
// ~1,4 mil linhas).
func (s *Store) TLDs(ctx context.Context) ([]TLD, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT tld, tld_unicode, nameservers, nameservers_ipv4, nameservers_ipv6, ds_records
		  FROM rootzone_tld
		 ORDER BY tld`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (TLD, error) {
		var t TLD
		err := r.Scan(&t.TLD, &t.Unicode, &t.Nameservers, &t.NameserversV4, &t.NameserversV6, &t.DSRecords)
		return t, err
	})
}

// Delegation devolve a delegação de um TLD já normalizado (ASCII,
// minúsculas, sem o ponto final), ou ErrNotFound. As três consultas rodam
// numa transação só de leitura em REPEATABLE READ: veem a mesma versão da
// zona mesmo se o coletor aplicar outra no meio.
func (s *Store) Delegation(ctx context.Context, tld string) (*Delegation, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	d := &Delegation{}
	t := &d.TLD
	err = tx.QueryRow(ctx, `
		SELECT tld, tld_unicode, nameservers, nameservers_ipv4, nameservers_ipv6, ds_records,
		       created_at, updated_at
		  FROM rootzone_tld
		 WHERE tld = $1`, tld).
		Scan(&t.TLD, &t.Unicode, &t.Nameservers, &t.NameserversV4, &t.NameserversV6, &t.DSRecords,
			&t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `
		SELECT type, rdata, ttl
		  FROM rootzone_record
		 WHERE owner = $1 AND type IN ('NS', 'DS')
		 ORDER BY type, rdata`, tld)
	if err != nil {
		return nil, err
	}
	if d.Records, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Record]); err != nil {
		return nil, err
	}

	rows, err = tx.Query(ctx, `
		SELECT g.owner, g.type, g.rdata, g.ttl
		  FROM rootzone_record ns
		  JOIN rootzone_record g ON g.owner = ns.rdata AND g.type IN ('A', 'AAAA')
		 WHERE ns.owner = $1 AND ns.type = 'NS'
		 ORDER BY g.owner, g.type, g.rdata`, tld)
	if err != nil {
		return nil, err
	}
	if d.Glue, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Glue]); err != nil {
		return nil, err
	}
	return d, tx.Commit(ctx)
}
