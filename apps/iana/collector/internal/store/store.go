// Package store grava o dataset da IANA nas tabelas iana_* e mantém a linha do
// app na tabela central jobs.
//
// O schema é de database/postgres/iana/ e database/postgres/central/; este
// pacote só lê e escreve dados.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/parse"
)

// AppName é o nome gravado em jobs.app.
const AppName = "collector-iana"

// ErrBusy indica outra execução aplicando dados ao mesmo tempo.
var ErrBusy = errors.New("outra execução do collector-iana está aplicando dados")

// RemovalMinRows é o piso absoluto da trava de remoção: remover até esse
// número de linhas de uma tabela nunca dispara a trava. Sem ele, nas tabelas
// minúsculas (9 ASNs especiais, ~25 blocos especiais por família) uma única
// linha removida já passaria de 5%.
const RemovalMinRows = 2

// RemovalError é a trava contra remoção em massa: um arquivo truncado ou um
// formato novo não pode apagar boa parte de uma tabela.
type RemovalError struct {
	Table     string
	Removed   int
	Current   int
	Threshold float64
}

func (e *RemovalError) Error() string {
	return fmt.Sprintf("o dataset removeria %d de %d linhas de %s (%.1f%%, limite %.1f%% e mais de %d linhas); use --force se for legítimo",
		e.Removed, e.Current, e.Table, 100*float64(e.Removed)/float64(e.Current), 100*e.Threshold, RemovalMinRows)
}

// File descreve um dos 10 arquivos numa execução (um elemento de
// iana_run.files).
type File struct {
	Name         string `json:"name"`
	URL          string `json:"url"`
	HTTPStatus   int    `json:"http_status"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
	SHA256       string `json:"sha256"`
	Bytes        int64  `json:"bytes"`
	Changed      bool   `json:"changed"`
	Rows         *int   `json:"rows,omitempty"`
	Publication  string `json:"publication,omitempty"`
}

// Applied descreve o último dataset aplicado (a versão atual).
type Applied struct {
	Version   string // iana_run.uuid
	SHA256    string // hash combinado
	Files     []File
	AppliedAt time.Time
}

// File devolve o estado de um arquivo no último dataset aplicado.
func (a *Applied) File(name string) (File, bool) {
	if a == nil {
		return File{}, false
	}
	for _, f := range a.Files {
		if f.Name == name {
			return f, true
		}
	}
	return File{}, false
}

// Run são os dados de uma execução em que algum arquivo mudou.
type Run struct {
	StartedAt time.Time
	Forced    bool
	SHA256    string // hash combinado do dataset
	Files     []File
	Warnings  []string
}

// TableChanges conta as linhas alteradas numa tabela.
type TableChanges struct {
	Inserted int `json:"inserted"`
	Updated  int `json:"updated"`
	Deleted  int `json:"deleted"`
}

// Changes são as alterações por tabela (chaves de Tables).
type Changes map[string]TableChanges

// Total é a soma de todas as alterações.
func (c Changes) Total() int {
	n := 0
	for _, t := range c {
		n += t.Inserted + t.Updated + t.Deleted
	}
	return n
}

// ApplyOptions controla as travas da aplicação.
type ApplyOptions struct {
	RemovalThreshold float64 // fração máxima de remoção por tabela sem --force
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

// LastApplied devolve o último dataset aplicado, ou nil se nunca houve um.
func (s *Store) LastApplied(ctx context.Context) (*Applied, error) {
	var a Applied
	var files []byte
	err := s.pool.QueryRow(ctx, `
		SELECT uuid::text, sha256, files, created_at
		  FROM iana_run
		 WHERE status = 1
		 ORDER BY created_at DESC, uuid DESC
		 LIMIT 1`).Scan(&a.Version, &a.SHA256, &files, &a.AppliedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(files, &a.Files); err != nil {
		return nil, fmt.Errorf("iana_run.files: %w", err)
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

// Apply substitui o conteúdo das 5 tabelas iana_* pelo do dataset numa
// transação única: carrega tudo em tabelas temporárias com COPY, confere a
// trava de remoção de cada tabela e aplica as diferenças com MERGE. Grava a
// execução em iana_run e atualiza jobs (consolidated = 0 se alguma linha
// mudou). Devolve as alterações e a nova versão do dataset.
func (s *Store) Apply(ctx context.Context, ds *parse.Dataset, run Run, opt ApplyOptions) (Changes, string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback(ctx)

	var locked bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtext($1))", AppName).Scan(&locked); err != nil {
		return nil, "", err
	}
	if !locked {
		return nil, "", ErrBusy
	}

	if err := stage(ctx, tx, ds); err != nil {
		return nil, "", err
	}
	if !opt.Force {
		for _, t := range tables {
			if err := checkRemoval(ctx, tx, t, opt.RemovalThreshold); err != nil {
				return nil, "", err
			}
		}
	}

	ch := Changes{}
	for _, t := range tables {
		actions, err := mergeActions(ctx, tx, t.mergeSQL())
		if err != nil {
			return nil, "", fmt.Errorf("merge %s: %w", t.name, err)
		}
		ch[t.name] = TableChanges{Inserted: actions["INSERT"], Updated: actions["UPDATE"], Deleted: actions["DELETE"]}
	}

	version, err := s.insertRun(ctx, tx, run, 1, ch, "")
	if err != nil {
		return nil, "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO jobs (app, last_sync_at, last_check_at, consolidated) VALUES ($1, NOW(), NOW(), 0)
		ON CONFLICT (app) DO UPDATE SET
			last_sync_at  = NOW(),
			last_check_at = NOW(),
			consolidated  = CASE WHEN $2 THEN 0 ELSE jobs.consolidated END`,
		AppName, ch.Total() > 0); err != nil {
		return nil, "", fmt.Errorf("jobs: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", err
	}
	return ch, version, nil
}

// table descreve uma tabela de dados e a tabela temporária de carga dela.
type table struct {
	name  string   // iana_asn_block
	stage string   // stage_asn_block
	key   []string // chave natural
	cols  []string // demais colunas gravadas pelo collector
}

// Tables são os nomes das tabelas de dados, na ordem de aplicação (e de
// iana_run.changes).
var Tables = func() []string {
	out := make([]string, len(tables))
	for i, t := range tables {
		out[i] = t.name
	}
	return out
}()

var tables = []table{
	{"iana_asn_block", "stage_asn_block", []string{"asn_start"},
		[]string{"asn_end", "description", "registry", "whois", "rdap_urls", "reference", "registration_date", "source_file"}},
	{"iana_prefix_block", "stage_prefix_block", []string{"prefix"},
		[]string{"designation", "registry", "whois", "rdap_urls", "status", "allocation_date", "note", "source_file"}},
	{"iana_special_prefix", "stage_special_prefix", []string{"prefix"},
		[]string{"name", "rfc", "allocation_date", "termination_date",
			"source", "destination", "forwardable", "globally_reachable", "reserved_by_protocol"}},
	{"iana_special_asn", "stage_special_asn", []string{"asn_start"},
		[]string{"asn_end", "reason", "reference"}},
	{"iana_rdap_service", "stage_rdap_service", []string{"kind", "resource"},
		[]string{"asn_start", "asn_end", "prefix", "registry", "urls"}},
}

func (t table) columns() []string { return append(append([]string{}, t.key...), t.cols...) }

func (t table) on() string {
	parts := make([]string, len(t.key))
	for i, k := range t.key {
		parts[i] = fmt.Sprintf("t.%s = s.%[1]s", k)
	}
	return strings.Join(parts, " AND ")
}

// mergeSQL insere, atualiza (só se algo mudou) e apaga o que sumiu.
func (t table) mergeSQL() string {
	prefixed := func(p string, cols []string) string {
		out := make([]string, len(cols))
		for i, c := range cols {
			out[i] = p + c
		}
		return strings.Join(out, ", ")
	}
	set := make([]string, len(t.cols))
	for i, c := range t.cols {
		set[i] = fmt.Sprintf("%s = s.%[1]s", c)
	}
	return fmt.Sprintf(`
		MERGE INTO %s t
		USING %s s ON %s
		WHEN MATCHED AND (%s) IS DISTINCT FROM (%s) THEN
			UPDATE SET %s
		WHEN NOT MATCHED BY TARGET THEN
			INSERT (%s) VALUES (%s)
		WHEN NOT MATCHED BY SOURCE THEN
			DELETE
		RETURNING merge_action()`,
		t.name, t.stage, t.on(),
		prefixed("t.", t.cols), prefixed("s.", t.cols),
		strings.Join(set, ", "),
		strings.Join(t.columns(), ", "), prefixed("s.", t.columns()))
}

// stage cria as tabelas temporárias da carga e as preenche com COPY.
func stage(ctx context.Context, tx pgx.Tx, ds *parse.Dataset) error {
	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE stage_asn_block (
			asn_start          bigint  PRIMARY KEY,
			asn_end            bigint  NOT NULL,
			description        text    NOT NULL,
			registry           text,
			whois              text,
			rdap_urls          text[]  NOT NULL,
			reference          text,
			registration_date  text,
			source_file        text    NOT NULL
		) ON COMMIT DROP;
		CREATE TEMP TABLE stage_prefix_block (
			prefix             cidr    PRIMARY KEY,
			designation        text    NOT NULL,
			registry           text,
			whois              text,
			rdap_urls          text[]  NOT NULL,
			status             text    NOT NULL,
			allocation_date    text,
			note               text,
			source_file        text    NOT NULL
		) ON COMMIT DROP;
		CREATE TEMP TABLE stage_special_prefix (
			prefix                cidr     PRIMARY KEY,
			name                  text     NOT NULL,
			rfc                   text,
			allocation_date       text,
			termination_date      text,
			source                boolean,
			destination           boolean,
			forwardable           boolean,
			globally_reachable    boolean,
			reserved_by_protocol  boolean
		) ON COMMIT DROP;
		CREATE TEMP TABLE stage_special_asn (
			asn_start          bigint  PRIMARY KEY,
			asn_end            bigint  NOT NULL,
			reason             text    NOT NULL,
			reference          text
		) ON COMMIT DROP;
		CREATE TEMP TABLE stage_rdap_service (
			kind               text    NOT NULL,
			resource           text    NOT NULL,
			asn_start          bigint,
			asn_end            bigint,
			prefix             cidr,
			registry           text,
			urls               text[]  NOT NULL,
			PRIMARY KEY (kind, resource)
		) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("staging: %w", err)
	}

	rows := map[string][][]any{}
	for _, b := range ds.ASNBlocks {
		rows["iana_asn_block"] = append(rows["iana_asn_block"], []any{b.Start, b.End, b.Description,
			nullStr(b.Registry), nullStr(b.WHOIS), nonNil(b.RDAPURLs), nullStr(b.Reference), nullStr(b.RegistrationDate), b.SourceFile})
	}
	for _, b := range ds.PrefixBlocks {
		rows["iana_prefix_block"] = append(rows["iana_prefix_block"], []any{b.Prefix, b.Designation,
			nullStr(b.Registry), nullStr(b.WHOIS), nonNil(b.RDAPURLs), b.Status, nullStr(b.AllocationDate), nullStr(b.Note), b.SourceFile})
	}
	for _, p := range ds.SpecialPrefixes {
		rows["iana_special_prefix"] = append(rows["iana_special_prefix"], []any{p.Prefix, p.Name, nullStr(p.RFC),
			nullStr(p.AllocationDate), nullStr(p.TerminationDate),
			p.Source, p.Destination, p.Forwardable, p.GloballyReachable, p.ReservedByProtocol})
	}
	for _, a := range ds.SpecialASNs {
		rows["iana_special_asn"] = append(rows["iana_special_asn"], []any{a.Start, a.End, a.Reason, nullStr(a.Reference)})
	}
	for _, r := range ds.RDAPServices {
		row := []any{r.Kind, r.Resource, nil, nil, nil, nullStr(r.Registry), nonNil(r.URLs)}
		if r.Kind == parse.KindASN {
			row[2], row[3] = r.Start, r.End
		} else {
			row[4] = r.Prefix
		}
		rows["iana_rdap_service"] = append(rows["iana_rdap_service"], row)
	}

	for _, t := range tables {
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{t.stage}, t.columns(), pgx.CopyFromRows(rows[t.name])); err != nil {
			return fmt.Errorf("copy %s: %w", t.stage, err)
		}
		if _, err := tx.Exec(ctx, "ANALYZE "+t.stage); err != nil {
			return err
		}
	}
	return nil
}

// checkRemoval recusa o dataset se ele removeria mais de threshold das linhas
// da tabela e mais de RemovalMinRows linhas.
func checkRemoval(ctx context.Context, tx pgx.Tx, t table, threshold float64) error {
	var current, removed int
	err := tx.QueryRow(ctx, fmt.Sprintf(`
		SELECT count(*),
		       count(*) FILTER (WHERE NOT EXISTS (SELECT 1 FROM %s s WHERE %s))
		  FROM %s t`, t.stage, t.on(), t.name)).Scan(&current, &removed)
	if err != nil {
		return fmt.Errorf("trava de remoção em %s: %w", t.name, err)
	}
	if current > 0 && removed > RemovalMinRows && float64(removed)/float64(current) > threshold {
		return &RemovalError{Table: t.name, Removed: removed, Current: current, Threshold: threshold}
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

func (s *Store) insertRun(ctx context.Context, q querier, run Run, status int, ch Changes, errMsg string) (string, error) {
	warnings := run.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	files := run.Files
	if files == nil {
		files = []File{}
	}
	wj, err := json.Marshal(warnings)
	if err != nil {
		return "", err
	}
	fj, err := json.Marshal(files)
	if err != nil {
		return "", err
	}
	var cj any
	if ch != nil {
		full := Changes{}
		for _, t := range Tables {
			full[t] = ch[t]
		}
		b, err := json.Marshal(full)
		if err != nil {
			return "", err
		}
		cj = string(b)
	}

	var version string
	err = q.QueryRow(ctx, `
		INSERT INTO iana_run (status, forced, sha256, files, changes, warnings, error, started_at)
		VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6::jsonb, $7, $8)
		RETURNING uuid::text`,
		status, run.Forced, nullStr(run.SHA256), string(fj), cj, string(wj), nullStr(errMsg), run.StartedAt).Scan(&version)
	if err != nil {
		return "", fmt.Errorf("iana_run: %w", err)
	}
	return version, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
