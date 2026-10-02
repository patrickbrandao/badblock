// Package store faz as consultas da api-asnames nas tabelas asnames_* e jobs.
//
// Só leitura (nenhum INSERT/UPDATE/DELETE). O schema é de
// database/postgres/asnames/.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CollectorApp é a linha de jobs que descreve a fonte destes dados.
const CollectorApp = "collector-asnames"

// ErrNotFound indica que o registro pedido não existe.
var ErrNotFound = errors.New("não encontrado")

// Dataset é o último arquivo aplicado pelo collector-asnames.
type Dataset struct {
	Version   string // asnames_run.uuid
	AppliedAt time.Time
	URL       string
	SHA256    string
	ASNs      int
}

// Job é a linha do collector-asnames na tabela jobs.
type Job struct {
	LastSyncAt   *time.Time
	LastCheckAt  *time.Time
	Consolidated int
}

// ASN é uma linha completa de asnames_asn. Handle, Name e Country são nil
// quando a linha da fonte não os traz.
type ASN struct {
	ASN         int64
	Description string
	Handle      *string
	Name        *string
	Country     *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Brief é um ASN só com número, handle e nome (listas por país).
type Brief struct {
	ASN    int64
	Handle *string
	Name   *string
}

// Entry é um ASN sem as datas (listas por handle e busca).
type Entry struct {
	ASN         int64
	Handle      *string
	Name        *string
	Country     *string
	Description string
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
		cfg.ConnConfig.RuntimeParams["application_name"] = "api-asnames"
	}
	// O pgx prepara as consultas, e depois de 5 execuções o Postgres pode
	// trocar para um plano genérico, que não vê o termo da busca: no ILIKE ele
	// passa a percorrer a tabela inteira pelo índice de asn mesmo com termo
	// raro (~65 ms em vez de ~0,1 ms no arquivo real). As consultas daqui são
	// baratas de planejar, então vale planejar sempre com os valores.
	if cfg.ConnConfig.RuntimeParams["plan_cache_mode"] == "" {
		cfg.ConnConfig.RuntimeParams["plan_cache_mode"] = "force_custom_plan"
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
// sha256 e asns são sempre preenchidos nas linhas aplicadas; o COALESCE só
// impede que uma linha fora do padrão trave a leitura da versão.
func (s *Store) Dataset(ctx context.Context) (*Dataset, error) {
	var d Dataset
	err := s.pool.QueryRow(ctx, `
		SELECT uuid::text, created_at, url, COALESCE(sha256, ''), COALESCE(asns, 0)
		  FROM asnames_run
		 WHERE status = 1
		 ORDER BY created_at DESC
		 LIMIT 1`).Scan(&d.Version, &d.AppliedAt, &d.URL, &d.SHA256, &d.ASNs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Job devolve a linha do collector-asnames em jobs, ou nil se não existir.
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

// ASN devolve um ASN pelo número (índice único uq_asnames_asn_asn).
func (s *Store) ASN(ctx context.Context, asn int64) (*ASN, error) {
	var a ASN
	err := s.pool.QueryRow(ctx, `
		SELECT asn, description, handle, name, country, created_at, updated_at
		  FROM asnames_asn WHERE asn = $1`, asn).
		Scan(&a.ASN, &a.Description, &a.Handle, &a.Name, &a.Country, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ByCountry devolve os ASNs de um país (duas letras maiúsculas), em ordem
// numérica (índice ix_asnames_asn_country).
func (s *Store) ByCountry(ctx context.Context, country string) ([]Brief, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT asn, handle, name FROM asnames_asn WHERE country = $1 ORDER BY asn`, country)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Brief])
}

// ByHandle devolve os ASNs cujo handle é igual ao pedido, sem diferenciar
// maiúsculas (índice ix_asnames_asn_handle, sobre lower(handle)), em ordem
// numérica.
func (s *Store) ByHandle(ctx context.Context, handle string) ([]Entry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT asn, handle, name, country, description
		  FROM asnames_asn WHERE lower(handle) = lower($1) ORDER BY asn`, handle)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Entry])
}

// Search devolve até limit ASNs cuja description contém term (sem
// diferenciar maiúsculas), em ordem numérica. Os curingas do LIKE no termo
// (%, _ e \) são escapados: o termo é sempre um trecho literal.
//
// O planejador escolhe sozinho entre o índice trigram
// (ix_asnames_asn_description_trgm, termo raro: bitmap + ordenação) e o
// índice de asn em ordem com filtro (termo comum: para nos primeiros limit).
// Medido no arquivo real (122 mil linhas): até ~15 ms nos dois casos; termo
// sem letras nem dígitos (ex.: "---") não gera trigramas e varre a tabela em
// ~60 ms. Forçar o trigram (CTE MATERIALIZED) deixava os termos comuns
// (~28 mil linhas) 10 vezes mais lentos.
func (s *Store) Search(ctx context.Context, term string, limit int) ([]Entry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT asn, handle, name, country, description
		  FROM asnames_asn
		 WHERE description ILIKE $1
		 ORDER BY asn
		 LIMIT $2`, LikePattern(term), limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Entry])
}

// LikePattern monta o padrão "%<termo>%" do ILIKE, escapando \, % e _ (o
// escape padrão do LIKE no Postgres é a barra invertida).
func LikePattern(term string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(term) + "%"
}
