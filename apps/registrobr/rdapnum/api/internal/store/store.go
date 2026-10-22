// Package store faz as consultas da api-registrobr-rdapnum nas tabelas
// registrobr_rdapnum_* e jobs.
//
// Só leitura (nenhum INSERT/UPDATE/DELETE). O schema é de
// database/postgres/registrobr_rdapnum/; as consultas estão em
// specs/fontes/registrobr/rdapnum/dados-processados.md#consultas-da-api-registrobr-rdapnum.
// Nenhuma consulta lê egress_ip nem server_time: o IP de saída do coletor
// nunca sai na API.
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
const CollectorApp = "collector-registrobr-rdapnum"

// ErrNotFound indica que o registro pedido não existe.
var ErrNotFound = errors.New("não encontrado")

// Resultados de last_result e de result.
const (
	ResultComplete  = 1
	ResultTruncated = 2
	ResultNotFound  = 3
	ResultError     = 4
)

// ResultName traduz last_result/result; "" para NULL ou desconhecido.
func ResultName(r *int16) string {
	if r == nil {
		return ""
	}
	switch *r {
	case ResultComplete:
		return "complete"
	case ResultTruncated:
		return "truncated"
	case ResultNotFound:
		return "not_found"
	case ResultError:
		return "error"
	}
	return ""
}

// Target é uma linha de registrobr_rdapnum_target (só o que a API mostra).
type Target struct {
	UUID          string
	Kind          string // asn ou prefix
	ASN           *int64
	Prefix        *netip.Prefix
	Source        string
	Active        bool
	LastScanAt    *time.Time
	LastAttemptAt *time.Time
	LastResult    *int16
	RetryAt       *time.Time
	ResponseUUID  *string
}

// Name é o alvo como texto: "61610" ou o bloco.
func (t Target) Name() string {
	if t.ASN != nil {
		return fmt.Sprint(*t.ASN)
	}
	if t.Prefix != nil {
		return t.Prefix.String()
	}
	return ""
}

const targetCols = `t.uuid::text, t.kind, t.asn, t.prefix, t.source, t.active, t.last_scan_at, t.last_attempt_at,
	t.last_result, t.retry_at, t.response_uuid::text`

func targetDest(t *Target) []any {
	return []any{&t.UUID, &t.Kind, &t.ASN, &t.Prefix, &t.Source, &t.Active, &t.LastScanAt, &t.LastAttemptAt,
		&t.LastResult, &t.RetryAt, &t.ResponseUUID}
}

// Entity é uma linha de registrobr_rdapnum_entity.
type Entity struct {
	Handle              string
	Kind                *string
	Name                *string
	Email               *string
	Country             *string
	Document            *string
	DocumentType        *string
	DocumentDigits      *string
	LegalRepresentative *string
	RegisteredAt        *time.Time
	ChangedAt           *time.Time
}

const entityCols = `e.handle, e.kind, e.name, e.email, e.country, e.document, e.document_type, e.document_digits,
	e.legal_representative, e.registered_at, e.changed_at`

func entityDest(e *Entity) []any {
	return []any{&e.Handle, &e.Kind, &e.Name, &e.Email, &e.Country, &e.Document, &e.DocumentType, &e.DocumentDigits,
		&e.LegalRepresentative, &e.RegisteredAt, &e.ChangedAt}
}

// Link é um papel de uma entidade num ASN ou bloco; Via é o handle da
// entidade dentro da qual ela veio (nil = contato do próprio objeto).
type Link struct {
	Entity Entity
	Role   string
	Via    *string
}

// Autnum é uma linha de registrobr_rdapnum_autnum.
type Autnum struct {
	ASN          int64
	Handle       *string
	Name         *string
	Type         *string
	Country      *string
	RegisteredAt *time.Time
	ChangedAt    *time.Time
}

// NetworkBrief é um bloco na lista de um ASN ou de um documento.
type NetworkBrief struct {
	Prefix netip.Prefix
	ASN    *int64
	Type   *string
	Status []string
}

// ASNRow é o que GET /asn/{asn} lê: o alvo e, se houver dado processado, o
// ASN, o titular, as ligações e os blocos.
type ASNRow struct {
	Target     Target
	Autnum     *Autnum
	Registrant *Entity
	Links      []Link
	Networks   []NetworkBrief
}

// Network é uma linha de registrobr_rdapnum_network.
type Network struct {
	Prefix       netip.Prefix
	Handle       *string
	Name         *string
	Type         *string
	Country      *string
	Status       []string
	ParentHandle *string
	ASN          *int64
	RegisteredAt *time.Time
	ChangedAt    *time.Time
}

// Nameserver é um item de registrobr_rdapnum_reverse.nameservers.
type Nameserver struct {
	Name      *string  `json:"name"`
	Status    []string `json:"status"`
	CheckedAt *string  `json:"checked_at"`
	LastOKAt  *string  `json:"last_ok_at"`
}

// Reverse é uma delegação reversa de um bloco.
type Reverse struct {
	Prefix       *netip.Prefix
	Start, End   string
	Nameservers  []Nameserver
	Signed       *bool
	RegisteredAt *time.Time
	ChangedAt    *time.Time
}

// NetworkRow é o que GET /ip e /prefix leem: o bloco registrado mais
// específico que contém o pedido, com o titular, as ligações, os reversos e o
// alvo que o gerou.
type NetworkRow struct {
	Network    Network
	Target     Target
	Registrant *Entity
	Links      []Link
	Reverses   []Reverse
}

// DocumentASN é um ASN de que uma entidade do documento é titular.
type DocumentASN struct {
	ASN        int64
	Name       *string
	LastScanAt *time.Time
}

// DocumentRow é o que GET /document lê.
type DocumentRow struct {
	Entities []Entity
	ASNs     []DocumentASN
	Networks []NetworkBrief
}

// ContactObject é uma ligação de uma entidade: o ASN ou o bloco, o papel e o
// handle da entidade pela qual veio.
type ContactObject struct {
	Kind   string // asn ou prefix
	ASN    *int64
	Prefix *netip.Prefix
	Role   string
	Via    *string
}

// ContactRow é o que GET /contact lê.
type ContactRow struct {
	Entity  Entity
	Objects []ContactObject
}

// Response é uma versão da resposta crua (sem egress_ip nem server_time).
type Response struct {
	UUID       string
	TargetUUID string
	Result     int16
	HTTPStatus int16
	Body       *string // NULL no 404 sem JSON
	LastSeenAt time.Time
}

// ResponseMeta é uma linha do histórico.
type ResponseMeta struct {
	UUID         string
	Result       int16
	HTTPStatus   int16
	SHA256       string
	Bytes        int
	FetchedAt    time.Time
	LastSeenAt   time.Time
	SeenCount    int
	Processed    bool
	ProcessError *string
}

// TargetStats é o bloco targets do /meta.
type TargetStats struct {
	Total, ASN, Prefix, Active, Static, NeverScanned, Overdue, Rescan, Scanning, Waiting int
	OldestScanAt                                                                         *time.Time
	Complete, Truncated, NotFound, Error                                                 int
}

// ResponseStats é o bloco responses do /meta.
type ResponseStats struct {
	Total, Complete, Truncated, NotFound, Pending, ProcessErrors int64
}

// DataStats é o bloco data do /meta.
type DataStats struct {
	Autnum, Network, Entity, Reverse int64
}

// Derivation é a última derivação aplicada (registrobr_rdapnum_run).
type Derivation struct {
	Source          string
	Version         string
	SourceVersion   string
	SourceUpdatedAt time.Time
	CreatedAt       time.Time
	ASNs            *int
	Prefixes        *int
	Inserted        *int
	NotInSource     *int
}

// Worker é uma instância do coletor com sinal nas últimas 24 h (sem o IP de
// saída).
type Worker struct {
	Host        string
	Version     string
	StartedAt   time.Time
	SeenAt      time.Time
	Alive       bool
	RateLimit   int
	Proxy       bool
	Requests    int64
	Complete    int64
	Truncated   int64
	NotFound    int64
	Errors      int64
	Refreshes   int64
	EgressIPs   int
	PausedUntil *time.Time
}

// Job é a linha do coletor na tabela jobs.
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
		cfg.ConnConfig.RuntimeParams["application_name"] = "api-registrobr-rdapnum"
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

// HasTargets diz se já há algum alvo cadastrado (a API está pronta).
func (s *Store) HasTargets(ctx context.Context) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM registrobr_rdapnum_target)").Scan(&ok)
	return ok, err
}

func noRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// TargetASN devolve o alvo de um ASN.
func (s *Store) TargetASN(ctx context.Context, asn int64) (*Target, error) {
	var t Target
	err := s.pool.QueryRow(ctx, `SELECT `+targetCols+` FROM registrobr_rdapnum_target t WHERE t.asn = $1`, asn).
		Scan(targetDest(&t)...)
	if err != nil {
		return nil, noRows(err)
	}
	return &t, nil
}

// TargetPrefix devolve o alvo com esse bloco exato ou, sem ele, o alvo que
// gerou o bloco registrado mais específico que contém o pedido.
func (s *Store) TargetPrefix(ctx context.Context, p netip.Prefix) (*Target, error) {
	var t Target
	err := s.pool.QueryRow(ctx, `SELECT `+targetCols+` FROM registrobr_rdapnum_target t WHERE t.prefix = $1::cidr`, p.String()).
		Scan(targetDest(&t)...)
	if err == nil {
		return &t, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	err = s.pool.QueryRow(ctx, `
		SELECT `+targetCols+`
		  FROM registrobr_rdapnum_network n
		  JOIN registrobr_rdapnum_target t ON t.uuid = n.target_uuid
		 WHERE n.prefix >>= $1::cidr
		 ORDER BY masklen(n.prefix) DESC
		 LIMIT 1`, p.String()).Scan(targetDest(&t)...)
	if err != nil {
		return nil, noRows(err)
	}
	return &t, nil
}

func (s *Store) entity(ctx context.Context, uuid *string) (*Entity, error) {
	if uuid == nil {
		return nil, nil
	}
	var e Entity
	err := s.pool.QueryRow(ctx, `SELECT `+entityCols+` FROM registrobr_rdapnum_entity e WHERE e.uuid = $1::uuid`, *uuid).
		Scan(entityDest(&e)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// links lê as ligações de um ASN (col = autnum_uuid) ou bloco (network_uuid).
func (s *Store) links(ctx context.Context, col, uuid string) ([]Link, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT l.role, v.handle, `+entityCols+`
		  FROM registrobr_rdapnum_link l
		  JOIN registrobr_rdapnum_entity e ON e.uuid = l.entity_uuid
		  LEFT JOIN registrobr_rdapnum_entity v ON v.uuid = l.via_uuid
		 WHERE l.`+col+` = $1::uuid`, uuid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Link
	for rows.Next() {
		var l Link
		if err := rows.Scan(append([]any{&l.Role, &l.Via}, entityDest(&l.Entity)...)...); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ASN devolve o alvo do ASN e, se houver, o dado processado dele.
func (s *Store) ASN(ctx context.Context, asn int64) (*ASNRow, error) {
	var r ASNRow
	var autnumUUID, registrant *string
	var a Autnum
	err := s.pool.QueryRow(ctx, `
		SELECT `+targetCols+`,
		       a.uuid::text, a.handle, a.name, a.type, a.country, a.registered_at, a.changed_at, a.registrant_uuid::text
		  FROM registrobr_rdapnum_target t
		  LEFT JOIN registrobr_rdapnum_autnum a ON a.asn = t.asn
		 WHERE t.asn = $1`, asn).
		Scan(append(targetDest(&r.Target), &autnumUUID, &a.Handle, &a.Name, &a.Type, &a.Country,
			&a.RegisteredAt, &a.ChangedAt, &registrant)...)
	if err != nil {
		return nil, noRows(err)
	}
	if autnumUUID == nil {
		return &r, nil
	}
	a.ASN = asn
	r.Autnum = &a
	if r.Registrant, err = s.entity(ctx, registrant); err != nil {
		return nil, err
	}
	if r.Links, err = s.links(ctx, "autnum_uuid", *autnumUUID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT prefix, asn, type, status FROM registrobr_rdapnum_network WHERE asn = $1 ORDER BY family, prefix`, asn)
	if err != nil {
		return nil, err
	}
	r.Networks, err = pgx.CollectRows(rows, pgx.RowToStructByPos[NetworkBrief])
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Network devolve o bloco registrado mais específico que contém p.
func (s *Store) Network(ctx context.Context, p netip.Prefix) (*NetworkRow, error) {
	var r NetworkRow
	var uuid string
	var registrant *string
	n := &r.Network
	err := s.pool.QueryRow(ctx, `
		SELECT n.uuid::text, n.prefix, n.handle, n.name, n.type, n.country, n.status, n.parent_handle, n.asn,
		       n.registered_at, n.changed_at, n.registrant_uuid::text, `+targetCols+`
		  FROM registrobr_rdapnum_network n
		  JOIN registrobr_rdapnum_target t ON t.uuid = n.target_uuid
		 WHERE n.prefix >>= $1::cidr
		 ORDER BY masklen(n.prefix) DESC
		 LIMIT 1`, p.String()).
		Scan(append([]any{&uuid, &n.Prefix, &n.Handle, &n.Name, &n.Type, &n.Country, &n.Status, &n.ParentHandle,
			&n.ASN, &n.RegisteredAt, &n.ChangedAt, &registrant}, targetDest(&r.Target)...)...)
	if err != nil {
		return nil, noRows(err)
	}
	if r.Registrant, err = s.entity(ctx, registrant); err != nil {
		return nil, err
	}
	if r.Links, err = s.links(ctx, "network_uuid", uuid); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT prefix, host(start_address), host(end_address), nameservers::text, signed, registered_at, changed_at
		  FROM registrobr_rdapnum_reverse
		 WHERE network_uuid = $1::uuid
		 ORDER BY start_address, end_address`, uuid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rv Reverse
		var ns string
		if err := rows.Scan(&rv.Prefix, &rv.Start, &rv.End, &ns, &rv.Signed, &rv.RegisteredAt, &rv.ChangedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(ns), &rv.Nameservers); err != nil {
			return nil, fmt.Errorf("nameservers ilegíveis: %w", err)
		}
		r.Reverses = append(r.Reverses, rv)
	}
	return &r, rows.Err()
}

// Document devolve as entidades com esses dígitos e os ASNs e blocos de que
// alguma delas é titular.
func (s *Store) Document(ctx context.Context, digits string) (*DocumentRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.uuid::text, `+entityCols+`
		  FROM registrobr_rdapnum_entity e
		 WHERE e.document_digits = $1
		 ORDER BY e.handle`, digits)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var r DocumentRow
	var uuids []string
	for rows.Next() {
		var e Entity
		var u string
		if err := rows.Scan(append([]any{&u}, entityDest(&e)...)...); err != nil {
			return nil, err
		}
		uuids = append(uuids, u)
		r.Entities = append(r.Entities, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(uuids) == 0 {
		return nil, ErrNotFound
	}
	rows, err = s.pool.Query(ctx, `
		SELECT DISTINCT a.asn, a.name, t.last_scan_at
		  FROM registrobr_rdapnum_link l
		  JOIN registrobr_rdapnum_autnum a ON a.uuid = l.autnum_uuid
		  JOIN registrobr_rdapnum_target t ON t.uuid = a.target_uuid
		 WHERE l.entity_uuid = ANY($1::uuid[]) AND l.role = 'registrant'
		 ORDER BY a.asn`, uuids)
	if err != nil {
		return nil, err
	}
	if r.ASNs, err = pgx.CollectRows(rows, pgx.RowToStructByPos[DocumentASN]); err != nil {
		return nil, err
	}
	rows, err = s.pool.Query(ctx, `
		SELECT n.prefix, n.asn, n.type, n.status
		  FROM registrobr_rdapnum_network n
		 WHERE n.uuid IN (SELECT l.network_uuid FROM registrobr_rdapnum_link l
		                   WHERE l.entity_uuid = ANY($1::uuid[]) AND l.role = 'registrant')
		 ORDER BY n.family, n.prefix`, uuids)
	if err != nil {
		return nil, err
	}
	if r.Networks, err = pgx.CollectRows(rows, pgx.RowToStructByPos[NetworkBrief]); err != nil {
		return nil, err
	}
	return &r, nil
}

// Contact devolve a entidade de um handle e todas as ligações dela, em ordem
// de tipo e objeto.
func (s *Store) Contact(ctx context.Context, handle string) (*ContactRow, error) {
	var r ContactRow
	var uuid string
	err := s.pool.QueryRow(ctx, `SELECT e.uuid::text, `+entityCols+` FROM registrobr_rdapnum_entity e WHERE e.handle = $1`, handle).
		Scan(append([]any{&uuid}, entityDest(&r.Entity)...)...)
	if err != nil {
		return nil, noRows(err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT CASE WHEN l.autnum_uuid IS NOT NULL THEN 'asn' ELSE 'prefix' END, a.asn, n.prefix, l.role, v.handle
		  FROM registrobr_rdapnum_link l
		  LEFT JOIN registrobr_rdapnum_autnum a ON a.uuid = l.autnum_uuid
		  LEFT JOIN registrobr_rdapnum_network n ON n.uuid = l.network_uuid
		  LEFT JOIN registrobr_rdapnum_entity v ON v.uuid = l.via_uuid
		 WHERE l.entity_uuid = $1::uuid
		 ORDER BY 1, a.asn, n.family, n.prefix, v.handle NULLS FIRST, l.role`, uuid)
	if err != nil {
		return nil, err
	}
	if r.Objects, err = pgx.CollectRows(rows, pgx.RowToStructByPos[ContactObject]); err != nil {
		return nil, err
	}
	return &r, nil
}

// Response devolve uma versão da resposta crua pelo uuid.
func (s *Store) Response(ctx context.Context, uuid string) (*Response, error) {
	var r Response
	err := s.pool.QueryRow(ctx, `
		SELECT uuid::text, target_uuid::text, result, http_status, body::text, last_seen_at
		  FROM registrobr_rdapnum_response WHERE uuid = $1::uuid`, uuid).
		Scan(&r.UUID, &r.TargetUUID, &r.Result, &r.HTTPStatus, &r.Body, &r.LastSeenAt)
	if err != nil {
		return nil, noRows(err)
	}
	return &r, nil
}

// History devolve todas as versões das respostas de um alvo, a mais nova
// primeiro.
func (s *Store) History(ctx context.Context, targetUUID string) ([]ResponseMeta, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT uuid::text, result, http_status, sha256, bytes, fetched_at, last_seen_at, seen_count,
		       processed_at IS NOT NULL, process_error
		  FROM registrobr_rdapnum_response
		 WHERE target_uuid = $1::uuid
		 ORDER BY fetched_at DESC, uuid DESC`, targetUUID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ResponseMeta])
}

// Config devolve registrobr_rdapnum_config inteira (chave → valor).
func (s *Store) Config(ctx context.Context) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, "SELECT key, value FROM registrobr_rdapnum_config")
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

// TargetStats resume registrobr_rdapnum_target; overdue usa interval.
// never_scanned, overdue, rescan e oldest_scan_at contam só os alvos ativos
// (os que a fila pega); o resto, a tabela inteira.
func (s *Store) TargetStats(ctx context.Context, interval time.Duration) (*TargetStats, error) {
	var st TargetStats
	err := s.pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE kind = 'asn'),
		       count(*) FILTER (WHERE kind = 'prefix'),
		       count(*) FILTER (WHERE active),
		       count(*) FILTER (WHERE source = 'static'),
		       count(*) FILTER (WHERE active AND last_scan_at IS NULL),
		       count(*) FILTER (WHERE active AND last_scan_at < NOW() - $1::float8 * interval '1 second'),
		       count(*) FILTER (WHERE active AND rescan),
		       count(*) FILTER (WHERE scan_worker IS NOT NULL),
		       count(*) FILTER (WHERE retry_at > NOW()),
		       min(last_scan_at) FILTER (WHERE active),
		       count(*) FILTER (WHERE last_result = 1),
		       count(*) FILTER (WHERE last_result = 2),
		       count(*) FILTER (WHERE last_result = 3),
		       count(*) FILTER (WHERE last_result = 4)
		  FROM registrobr_rdapnum_target`, interval.Seconds()).
		Scan(&st.Total, &st.ASN, &st.Prefix, &st.Active, &st.Static, &st.NeverScanned, &st.Overdue, &st.Rescan,
			&st.Scanning, &st.Waiting, &st.OldestScanAt, &st.Complete, &st.Truncated, &st.NotFound, &st.Error)
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// ResponseStats resume registrobr_rdapnum_response; pending são as respostas
// conclusivas atuais (target.response_uuid) ainda sem processar.
func (s *Store) ResponseStats(ctx context.Context) (*ResponseStats, error) {
	var st ResponseStats
	err := s.pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE result = 1),
		       count(*) FILTER (WHERE result = 2),
		       count(*) FILTER (WHERE result = 3),
		       count(*) FILTER (WHERE process_error IS NOT NULL)
		  FROM registrobr_rdapnum_response`).
		Scan(&st.Total, &st.Complete, &st.Truncated, &st.NotFound, &st.ProcessErrors)
	if err != nil {
		return nil, err
	}
	err = s.pool.QueryRow(ctx, `
		SELECT count(*)
		  FROM registrobr_rdapnum_response r
		  JOIN registrobr_rdapnum_target t ON t.response_uuid = r.uuid
		 WHERE r.result IN (1, 3) AND r.processed_at IS NULL AND r.process_error IS NULL`).Scan(&st.Pending)
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// DataStats conta as linhas das tabelas processadas.
func (s *Store) DataStats(ctx context.Context) (*DataStats, error) {
	var st DataStats
	err := s.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM registrobr_rdapnum_autnum),
		       (SELECT count(*) FROM registrobr_rdapnum_network),
		       (SELECT count(*) FROM registrobr_rdapnum_entity),
		       (SELECT count(*) FROM registrobr_rdapnum_reverse)`).
		Scan(&st.Autnum, &st.Network, &st.Entity, &st.Reverse)
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// Derivations devolve a última derivação aplicada (zero ou uma).
func (s *Store) Derivations(ctx context.Context) ([]Derivation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT source, uuid::text, source_version::text, source_updated_at, created_at,
		       asns, prefixes, inserted, not_in_source
		  FROM registrobr_rdapnum_run
		 WHERE status = 1
		 ORDER BY created_at DESC
		 LIMIT 1`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Derivation])
}

// Workers devolve as instâncias com sinal nas últimas 24 h; viva = sinal há
// menos de 90 s. egress_ip não é lido.
func (s *Store) Workers(ctx context.Context) ([]Worker, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT host, version, created_at, updated_at, updated_at > NOW() - interval '90 seconds',
		       rate_limit, proxy, requests, complete, truncated, not_found, errors, refreshes, egress_ips, paused_until
		  FROM registrobr_rdapnum_worker
		 WHERE updated_at > NOW() - interval '24 hours'
		 ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Worker])
}

// Job devolve a linha do coletor em jobs, ou nil se não existir.
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
