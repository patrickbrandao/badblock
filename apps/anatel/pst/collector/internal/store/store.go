// Package store grava o dataset da Anatel nas tabelas anatel_pst_* e mantém a
// linha do app na tabela central jobs.
//
// O schema é de database/postgres/anatel_pst/ e database/postgres/central/;
// este pacote só lê e escreve dados.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/patrickbrandao/badblock/apps/anatel/pst/collector/internal/parse"
)

// AppName é o nome gravado em jobs.app.
const AppName = "collector-anatel-pst"

// ErrBusy indica outra execução aplicando dados ao mesmo tempo.
var ErrBusy = errors.New("outra execução do collector-anatel-pst está aplicando dados")

// RemovalError é a trava contra remoção em massa: um arquivo truncado ou um
// formato novo não pode apagar boa parte das tabelas.
type RemovalError struct {
	Entity    string // "prestadoras" ou "serviços"
	Removed   int
	Current   int
	Threshold float64
}

func (e *RemovalError) Error() string {
	return fmt.Sprintf("o arquivo removeria %d de %d %s (%.1f%%, limite %.1f%%); use --force se for legítimo",
		e.Removed, e.Current, e.Entity, 100*float64(e.Removed)/float64(e.Current), 100*e.Threshold)
}

// Applied descreve o último arquivo aplicado (a versão atual do dataset).
type Applied struct {
	Version       string // anatel_pst_run.uuid
	URL           string
	ETag          string
	LastModified  string
	SHA256        string    // do ZIP
	CSVSHA256     string    // do CSV extraído
	CSVModifiedAt time.Time // zero = sem data
	AppliedAt     time.Time
}

// Run são os dados de uma execução que baixou um arquivo novo.
type Run struct {
	StartedAt    time.Time
	Forced       bool
	URL          string
	HTTPStatus   int
	ETag         string
	LastModified string
	SHA256       string
	Bytes        int64

	// Do CSV extraído (vazios se o ZIP foi recusado).
	CSVName       string
	CSVSHA256     string
	CSVBytes      int64
	CSVModifiedAt time.Time

	// Contagens do parser (só gravadas com Parsed).
	Parsed     bool
	Rows       int
	RowsCNPJ   int
	RowsCPF    int
	Duplicates int
	Skipped    int
	Providers  int
	Services   int
	Warnings   []string
}

// Changes conta as linhas alteradas por uma aplicação.
type Changes struct {
	ProviderInserted int
	ProviderUpdated  int
	ProviderDeleted  int
	ServiceInserted  int
	ServiceUpdated   int
	ServiceDeleted   int
}

// Total é a soma de todas as alterações.
func (c Changes) Total() int {
	return c.ProviderInserted + c.ProviderUpdated + c.ProviderDeleted + c.ServiceInserted + c.ServiceUpdated + c.ServiceDeleted
}

// ApplyOptions controla as travas da aplicação.
type ApplyOptions struct {
	RemovalThreshold float64 // fração máxima de remoção sem --force
	Force            bool
}

// Store é a conexão com o Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// Open conecta e valida com um SELECT 1.
func Open(ctx context.Context, url string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("POSTGRES_URL inválida: %w", err)
	}
	cfg.MaxConns = 2
	cfg.MaxConnIdleTime = 5 * time.Minute
	if cfg.ConnConfig.RuntimeParams["application_name"] == "" {
		cfg.ConnConfig.RuntimeParams["application_name"] = AppName
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

// LastApplied devolve o último arquivo aplicado, ou nil se nunca houve um.
func (s *Store) LastApplied(ctx context.Context) (*Applied, error) {
	var a Applied
	var modified *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT uuid::text, url, coalesce(etag, ''), coalesce(last_modified, ''),
		       coalesce(sha256, ''), coalesce(csv_sha256, ''), csv_modified_at, created_at
		  FROM anatel_pst_run
		 WHERE status = 1
		 ORDER BY created_at DESC
		 LIMIT 1`).Scan(&a.Version, &a.URL, &a.ETag, &a.LastModified, &a.SHA256, &a.CSVSHA256, &modified, &a.AppliedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if modified != nil {
		a.CSVModifiedAt = *modified
	}
	return &a, nil
}

// TouchCheck registra em jobs uma verificação sem mudança.
func (s *Store) TouchCheck(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO jobs (app, last_check_at) VALUES ($1, NOW())
		ON CONFLICT (app) DO UPDATE SET last_check_at = NOW()`, AppName)
	return err
}

// RecordFailure grava uma execução recusada; as tabelas de dados não mudam.
func (s *Store) RecordFailure(ctx context.Context, run Run, cause error) error {
	_, err := s.insertRun(ctx, s.pool, run, 0, nil, cause.Error())
	return err
}

// Apply substitui o conteúdo de anatel_pst_provider e anatel_pst_service pelo
// do dataset numa transação única: carrega tudo em tabelas temporárias com
// COPY, confere a trava de remoção e aplica as diferenças com MERGE. Grava a
// execução em anatel_pst_run e atualiza jobs (consolidated = 0 se alguma
// linha mudou). Devolve as alterações e a nova versão do dataset.
func (s *Store) Apply(ctx context.Context, ds *parse.Dataset, run Run, opt ApplyOptions) (Changes, string, error) {
	var ch Changes
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ch, "", err
	}
	defer tx.Rollback(ctx)

	var locked bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtext($1))", AppName).Scan(&locked); err != nil {
		return ch, "", err
	}
	if !locked {
		return ch, "", ErrBusy
	}

	if err := stage(ctx, tx, ds); err != nil {
		return ch, "", err
	}
	if !opt.Force {
		if err := checkRemoval(ctx, tx, opt.RemovalThreshold); err != nil {
			return ch, "", err
		}
	}

	// 1) Prestadoras novas e alteradas (as que sumiram saem por último, depois
	// dos serviços, para a contagem de serviços apagados ficar certa).
	actions, err := mergeActions(ctx, tx, `
		MERGE INTO anatel_pst_provider t
		USING stage_provider s ON t.document = s.document
		WHEN MATCHED AND (t.name, t.trade_name, t.street, t.number, t.complement, t.district,
		                  t.postal_code, t.city_ibge_code, t.city, t.state, t.phone, t.email)
		      IS DISTINCT FROM
		                 (s.name, s.trade_name, s.street, s.number, s.complement, s.district,
		                  s.postal_code, s.city_ibge_code, s.city, s.state, s.phone, s.email) THEN
			UPDATE SET name = s.name, trade_name = s.trade_name, street = s.street, number = s.number,
			           complement = s.complement, district = s.district, postal_code = s.postal_code,
			           city_ibge_code = s.city_ibge_code, city = s.city, state = s.state,
			           phone = s.phone, email = s.email
		WHEN NOT MATCHED THEN
			INSERT (document, name, trade_name, street, number, complement, district,
			        postal_code, city_ibge_code, city, state, phone, email)
			VALUES (s.document, s.name, s.trade_name, s.street, s.number, s.complement, s.district,
			        s.postal_code, s.city_ibge_code, s.city, s.state, s.phone, s.email)
		RETURNING merge_action()`)
	if err != nil {
		return ch, "", fmt.Errorf("merge anatel_pst_provider: %w", err)
	}
	ch.ProviderInserted, ch.ProviderUpdated = actions["INSERT"], actions["UPDATE"]

	// 2) Serviços novos, alterados e removidos, com o provider_uuid resolvido
	// pelo CNPJ (já gravado no passo 1). Os serviços das prestadoras que
	// sumiram não estão na origem e saem aqui, contados em service_deleted.
	actions, err = mergeActions(ctx, tx, `
		MERGE INTO anatel_pst_service t
		USING (SELECT p.uuid AS provider_uuid, s.*
		         FROM stage_service s
		         JOIN anatel_pst_provider p ON p.document = s.document) s
		   ON t.provider_uuid = s.provider_uuid
		  AND t.grant_fistel IS NOT DISTINCT FROM s.grant_fistel
		  AND t.notification_fistel = s.notification_fistel
		  AND t.service_code = s.service_code
		WHEN MATCHED AND (t.entity_type, t.grant_type, t.grant_process, t.granted_on, t.service_group,
		                  t.service_name, t.notification_process, t.notified_on)
		      IS DISTINCT FROM
		                 (s.entity_type, s.grant_type, s.grant_process, s.granted_on, s.service_group,
		                  s.service_name, s.notification_process, s.notified_on) THEN
			UPDATE SET entity_type = s.entity_type, grant_type = s.grant_type, grant_process = s.grant_process,
			           granted_on = s.granted_on, service_group = s.service_group, service_name = s.service_name,
			           notification_process = s.notification_process, notified_on = s.notified_on
		WHEN NOT MATCHED BY TARGET THEN
			INSERT (provider_uuid, entity_type, grant_type, grant_fistel, grant_process, granted_on,
			        service_group, service_code, service_name, notification_fistel, notification_process, notified_on)
			VALUES (s.provider_uuid, s.entity_type, s.grant_type, s.grant_fistel, s.grant_process, s.granted_on,
			        s.service_group, s.service_code, s.service_name, s.notification_fistel, s.notification_process, s.notified_on)
		WHEN NOT MATCHED BY SOURCE THEN
			DELETE
		RETURNING merge_action()`)
	if err != nil {
		return ch, "", fmt.Errorf("merge anatel_pst_service: %w", err)
	}
	ch.ServiceInserted, ch.ServiceUpdated, ch.ServiceDeleted = actions["INSERT"], actions["UPDATE"], actions["DELETE"]

	// 3) Prestadoras que sumiram da fonte. Os serviços delas já saíram no
	// passo 2; a cascata de fk_anatel_pst_service_provider não acha nada.
	tag, err := tx.Exec(ctx, `
		DELETE FROM anatel_pst_provider p
		 WHERE NOT EXISTS (SELECT 1 FROM stage_provider s WHERE s.document = p.document)`)
	if err != nil {
		return ch, "", fmt.Errorf("delete anatel_pst_provider: %w", err)
	}
	ch.ProviderDeleted = int(tag.RowsAffected())

	version, err := s.insertRun(ctx, tx, run, 1, &ch, "")
	if err != nil {
		return ch, "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO jobs (app, last_sync_at, last_check_at, consolidated) VALUES ($1, NOW(), NOW(), 0)
		ON CONFLICT (app) DO UPDATE SET
			last_sync_at  = NOW(),
			last_check_at = NOW(),
			consolidated  = CASE WHEN $2 THEN 0 ELSE jobs.consolidated END`,
		AppName, ch.Total() > 0); err != nil {
		return ch, "", fmt.Errorf("jobs: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ch, "", err
	}
	return ch, version, nil
}

// stage cria as tabelas temporárias da carga e as preenche com COPY.
func stage(ctx context.Context, tx pgx.Tx, ds *parse.Dataset) error {
	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE stage_provider (
			document       text PRIMARY KEY,
			name           text NOT NULL,
			trade_name     text,
			street         text,
			number         text,
			complement     text,
			district       text,
			postal_code    text,
			city_ibge_code integer,
			city           text,
			state          text,
			phone          text,
			email          text
		) ON COMMIT DROP;
		CREATE TEMP TABLE stage_service (
			document             text NOT NULL,
			entity_type          text NOT NULL,
			grant_type           text NOT NULL,
			grant_fistel         text,
			grant_process        text,
			granted_on           date,
			service_group        text NOT NULL,
			service_code         text NOT NULL,
			service_name         text NOT NULL,
			notification_fistel  text NOT NULL,
			notification_process text,
			notified_on          date,
			UNIQUE NULLS NOT DISTINCT (document, grant_fistel, notification_fistel, service_code)
		) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("staging: %w", err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"stage_provider"},
		[]string{"document", "name", "trade_name", "street", "number", "complement", "district",
			"postal_code", "city_ibge_code", "city", "state", "phone", "email"},
		pgx.CopyFromSlice(len(ds.Providers), func(i int) ([]any, error) {
			p := ds.Providers[i]
			return []any{p.Document, p.Name, null(p.TradeName), null(p.Street), null(p.Number), null(p.Complement),
				null(p.District), null(p.PostalCode), nullInt(p.CityIBGECode), null(p.City), null(p.State),
				null(p.Phone), null(p.Email)}, nil
		})); err != nil {
		return fmt.Errorf("copy stage_provider: %w", err)
	}
	rows := make([][]any, 0, ds.Services())
	for _, p := range ds.Providers {
		for _, s := range p.Services {
			rows = append(rows, []any{p.Document, s.EntityType, s.GrantType, null(s.GrantFistel), null(s.GrantProcess),
				nullDate(s.GrantedOn), s.ServiceGroup, s.ServiceCode, s.ServiceName, s.NotificationFistel,
				null(s.NotificationProcess), nullDate(s.NotifiedOn)})
		}
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"stage_service"},
		[]string{"document", "entity_type", "grant_type", "grant_fistel", "grant_process", "granted_on",
			"service_group", "service_code", "service_name", "notification_fistel", "notification_process", "notified_on"},
		pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("copy stage_service: %w", err)
	}
	if _, err := tx.Exec(ctx, "ANALYZE stage_provider; ANALYZE stage_service"); err != nil {
		return fmt.Errorf("staging: %w", err)
	}
	return nil
}

func checkRemoval(ctx context.Context, tx pgx.Tx, threshold float64) error {
	var curProv, remProv, curSvc, remSvc int
	err := tx.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM anatel_pst_provider),
		       (SELECT count(*) FROM anatel_pst_provider p
		         WHERE NOT EXISTS (SELECT 1 FROM stage_provider s WHERE s.document = p.document)),
		       (SELECT count(*) FROM anatel_pst_service),
		       (SELECT count(*) FROM anatel_pst_service t
		          JOIN anatel_pst_provider p ON p.uuid = t.provider_uuid
		         WHERE NOT EXISTS (SELECT 1 FROM stage_service s
		                            WHERE s.document = p.document
		                              AND s.grant_fistel IS NOT DISTINCT FROM t.grant_fistel
		                              AND s.notification_fistel = t.notification_fistel
		                              AND s.service_code = t.service_code))`).
		Scan(&curProv, &remProv, &curSvc, &remSvc)
	if err != nil {
		return fmt.Errorf("trava de remoção: %w", err)
	}
	if curProv > 0 && float64(remProv)/float64(curProv) > threshold {
		return &RemovalError{Entity: "prestadoras", Removed: remProv, Current: curProv, Threshold: threshold}
	}
	if curSvc > 0 && float64(remSvc)/float64(curSvc) > threshold {
		return &RemovalError{Entity: "serviços", Removed: remSvc, Current: curSvc, Threshold: threshold}
	}
	return nil
}

// mergeActions roda um MERGE ... RETURNING merge_action() e conta as ações.
func mergeActions(ctx context.Context, tx pgx.Tx, sql string) (map[string]int, error) {
	rows, err := tx.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			return nil, err
		}
		out[action]++
	}
	return out, rows.Err()
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s *Store) insertRun(ctx context.Context, q querier, run Run, status int, ch *Changes, errMsg string) (string, error) {
	warnings := run.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	wj, err := json.Marshal(warnings)
	if err != nil {
		return "", err
	}
	changes := make([]any, 6)
	if ch != nil {
		changes = []any{ch.ProviderInserted, ch.ProviderUpdated, ch.ProviderDeleted,
			ch.ServiceInserted, ch.ServiceUpdated, ch.ServiceDeleted}
	}
	// Contagens do arquivo só existem se o parser chegou a rodar.
	counts := make([]any, 7)
	if run.Parsed {
		counts = []any{run.Rows, run.RowsCNPJ, run.RowsCPF, run.Duplicates, run.Skipped, run.Providers, run.Services}
	}
	args := []any{
		status, run.Forced, run.URL, nullInt(run.HTTPStatus), null(run.ETag), null(run.LastModified),
		null(run.SHA256), nullInt64(run.Bytes),
		null(run.CSVName), null(run.CSVSHA256), nullInt64(run.CSVBytes), nullTime(run.CSVModifiedAt),
	}
	args = append(args, counts...)
	args = append(args, changes...)
	args = append(args, string(wj), null(errMsg), run.StartedAt)

	var version string
	err = q.QueryRow(ctx, `
		INSERT INTO anatel_pst_run (
			status, forced, url, http_status, etag, last_modified, sha256, bytes,
			csv_name, csv_sha256, csv_bytes, csv_modified_at,
			rows, rows_cnpj, rows_cpf, duplicates, skipped, providers, services,
			provider_inserted, provider_updated, provider_deleted,
			service_inserted, service_updated, service_deleted,
			warnings, error, started_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19,
		          $20, $21, $22, $23, $24, $25, $26::jsonb, $27, $28)
		RETURNING uuid::text`, args...).Scan(&version)
	if err != nil {
		return "", fmt.Errorf("anatel_pst_run: %w", err)
	}
	return version, nil
}

func null(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullInt64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func nullDate(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
