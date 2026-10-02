// Package store faz as consultas da api-anatel-pst nas tabelas anatel_pst_*
// e jobs.
//
// Só leitura (nenhum INSERT/UPDATE/DELETE). O schema é de
// database/postgres/anatel_pst/; as consultas são as de
// specs/fontes/anatel/pst/dados.md ("Consultas da api-anatel-pst") e
// specs/fontes/anatel/pst/api.md.
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
const CollectorApp = "collector-anatel-pst"

// ErrNotFound indica que o registro pedido não existe.
var ErrNotFound = errors.New("não encontrado")

// Dataset é a última execução aplicada pelo collector-anatel-pst. Os campos
// do arquivo e as contagens são sempre preenchidos nas linhas aplicadas;
// ficam nil só numa linha fora do padrão.
type Dataset struct {
	Version       string // anatel_pst_run.uuid
	AppliedAt     time.Time
	URL           string
	SHA256        string
	CSVSHA256     *string
	CSVModifiedAt *time.Time
	Providers     *int
	Services      *int
}

// Job é a linha do collector-anatel-pst na tabela jobs.
type Job struct {
	LastSyncAt   *time.Time
	LastCheckAt  *time.Time
	Consolidated int
}

// Provider é uma linha de anatel_pst_provider.
type Provider struct {
	Document     string
	Name         string
	TradeName    *string
	Street       *string
	Number       *string
	Complement   *string
	District     *string
	PostalCode   *string
	CityIBGECode *int
	City         *string
	State        *string
	Phone        *string
	Email        *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Service é uma linha de anatel_pst_service, sem a prestadora.
type Service struct {
	ServiceCode         string
	ServiceName         string
	ServiceGroup        string
	NotificationFistel  string
	NotificationProcess *string
	NotifiedOn          *time.Time
	EntityType          string
	GrantType           string
	GrantFistel         *string
	GrantProcess        *string
	GrantedOn           *time.Time
}

// ProviderDetail é uma prestadora com todos os seus serviços.
type ProviderDetail struct {
	Provider Provider
	Services []Service
}

// ServiceSummary é um código de serviço no catálogo, com as contagens.
type ServiceSummary struct {
	ServiceCode  string
	ServiceName  string
	ServiceGroup string
	Providers    int // CNPJs distintos
	Services     int // linhas de anatel_pst_service
}

// ServiceInfo identifica um código de serviço (nome e grupo).
type ServiceInfo struct {
	ServiceCode  string
	ServiceName  string
	ServiceGroup string
}

// ProviderBrief é uma prestadora numa lista (/service e /search).
type ProviderBrief struct {
	Document  string
	Name      string
	TradeName *string
	City      *string
	State     *string
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
		cfg.ConnConfig.RuntimeParams["application_name"] = "api-anatel-pst"
	}
	// O pgx prepara as consultas, e depois de 5 execuções o Postgres pode
	// trocar para um plano genérico, que não vê o termo da busca nem o
	// código do serviço: no ILIKE ele deixa de usar os índices trigram. As
	// consultas daqui são baratas de planejar, então vale planejar sempre com
	// os valores (como na api-ripe-asnames).
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

// Dataset devolve a última execução aplicada (ix_anatel_pst_run_applied), ou
// nil se ainda não houve carga. O COALESCE do sha256 só impede que uma linha
// fora do padrão trave a leitura da versão.
func (s *Store) Dataset(ctx context.Context) (*Dataset, error) {
	var d Dataset
	err := s.pool.QueryRow(ctx, `
		SELECT uuid::text, created_at, url, COALESCE(sha256, ''), csv_sha256, csv_modified_at,
		       providers, services
		  FROM anatel_pst_run
		 WHERE status = 1
		 ORDER BY created_at DESC
		 LIMIT 1`).Scan(&d.Version, &d.AppliedAt, &d.URL, &d.SHA256, &d.CSVSHA256, &d.CSVModifiedAt,
		&d.Providers, &d.Services)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Job devolve a linha do collector-anatel-pst em jobs, ou nil se não existir.
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

// readOnly abre uma transação só de leitura em REPEATABLE READ: as consultas
// de uma rota veem o mesmo arquivo mesmo se o coletor aplicar outro no meio
// (a aplicação é uma transação só).
func (s *Store) readOnly(ctx context.Context) (pgx.Tx, error) {
	return s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
}

// Provider devolve a prestadora de um CNPJ (14 dígitos) com todos os seus
// serviços, em ordem de código, data de notificação e Fistel da notificação
// (uq_anatel_pst_provider_document e uq_anatel_pst_service_key), ou
// ErrNotFound.
func (s *Store) Provider(ctx context.Context, document string) (*ProviderDetail, error) {
	tx, err := s.readOnly(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	d := &ProviderDetail{}
	p := &d.Provider
	var id string
	err = tx.QueryRow(ctx, `
		SELECT uuid::text, document, name, trade_name, street, number, complement, district,
		       postal_code, city_ibge_code, city, state, phone, email, created_at, updated_at
		  FROM anatel_pst_provider
		 WHERE document = $1`, document).
		Scan(&id, &p.Document, &p.Name, &p.TradeName, &p.Street, &p.Number, &p.Complement, &p.District,
			&p.PostalCode, &p.CityIBGECode, &p.City, &p.State, &p.Phone, &p.Email, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `
		SELECT service_code, service_name, service_group, notification_fistel, notification_process,
		       notified_on, entity_type, grant_type, grant_fistel, grant_process, granted_on
		  FROM anatel_pst_service
		 WHERE provider_uuid = $1
		 ORDER BY service_code, notified_on, notification_fistel`, id)
	if err != nil {
		return nil, err
	}
	if d.Services, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Service]); err != nil {
		return nil, err
	}
	return d, tx.Commit(ctx)
}

// Services devolve o catálogo: cada código de serviço com o nome, o grupo,
// as prestadoras (CNPJs distintos) e as linhas (a tabela inteira, ~54 mil
// linhas). Agrupa só pelo código (um item por código, como em /service) e
// lê o nome e o grupo com min(): cada código tem um nome e um grupo só na
// fonte, e assim o planejador percorre ix_anatel_pst_service_service_code em
// ordem, sem ordenar em disco (~60 ms em vez de ~240 ms no arquivo real).
func (s *Store) Services(ctx context.Context) ([]ServiceSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT service_code, min(service_name), min(service_group),
		       count(DISTINCT provider_uuid)::int, count(*)::int
		  FROM anatel_pst_service
		 GROUP BY service_code
		 ORDER BY service_code`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ServiceSummary])
}

// ServiceProviders devolve o nome e o grupo de um código de serviço (3
// dígitos, como no catálogo) e as prestadoras que o têm, em ordem de CNPJ;
// com state (UF em maiúsculas) não vazio, só as dessa UF. Código
// inexistente: ErrNotFound. As duas consultas usam
// ix_anatel_pst_service_service_code, numa transação só de leitura.
func (s *Store) ServiceProviders(ctx context.Context, code, state string) (*ServiceInfo, []ProviderBrief, error) {
	tx, err := s.readOnly(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	// Nome e grupo como no catálogo (min); sem linha do código, NULL.
	var name, group *string
	err = tx.QueryRow(ctx, `
		SELECT min(service_name), min(service_group)
		  FROM anatel_pst_service
		 WHERE service_code = $1`, code).Scan(&name, &group)
	if err != nil {
		return nil, nil, err
	}
	if name == nil || group == nil {
		return nil, nil, ErrNotFound
	}
	info := &ServiceInfo{ServiceCode: code, ServiceName: *name, ServiceGroup: *group}

	var stateArg *string
	if state != "" {
		stateArg = &state
	}
	rows, err := tx.Query(ctx, `
		SELECT p.document, p.name, p.trade_name, p.city, p.state
		  FROM anatel_pst_provider p
		 WHERE EXISTS (SELECT 1 FROM anatel_pst_service s WHERE s.provider_uuid = p.uuid AND s.service_code = $1)
		   AND ($2::text IS NULL OR p.state = $2)
		 ORDER BY p.document`, code, stateArg)
	if err != nil {
		return nil, nil, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[ProviderBrief])
	if err != nil {
		return nil, nil, err
	}
	return info, list, tx.Commit(ctx)
}

// Search devolve até limit prestadoras cuja razão social ou nome fantasia
// contém term (sem diferenciar maiúsculas; % _ e \ são texto), em ordem de
// razão social e CNPJ (ix_anatel_pst_provider_name_trgm e
// ix_anatel_pst_provider_trade_name_trgm).
func (s *Store) Search(ctx context.Context, term string, limit int) ([]ProviderBrief, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT document, name, trade_name, city, state
		  FROM anatel_pst_provider
		 WHERE name ILIKE $1 OR trade_name ILIKE $1
		 ORDER BY name, document
		 LIMIT $2`, LikePattern(term), limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ProviderBrief])
}

// LikePattern monta o padrão "%<termo>%" do ILIKE, escapando \, % e _ (o
// escape padrão do LIKE no Postgres é a barra invertida).
func LikePattern(term string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(term) + "%"
}
