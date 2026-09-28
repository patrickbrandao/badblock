// Package store faz as consultas da API no Postgres. Só usa as views do
// schema api, o contrato de leitura mantido por database/postgresql.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound indica ausência do registro pedido.
var ErrNotFound = errors.New("não encontrado")

// Store é o acesso de leitura ao banco.
type Store struct {
	pool *pgxpool.Pool
	url  string
}

// Open cria o pool de conexões.
func Open(ctx context.Context, url string, maxConns int32) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("POSTGRES_URL inválida: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = "badblock-registry-api"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Store{pool: pool, url: url}, nil
}

// Close encerra o pool.
func (s *Store) Close() { s.pool.Close() }

// Ping confere o banco.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// URL devolve a URL de conexão (usada pelo LISTEN, que precisa de conexão própria).
func (s *Store) URL() string { return s.url }

// Prefix é uma linha de api.prefix.
type Prefix struct {
	Level             string
	Prefix            netip.Prefix
	RIR               *string
	Country           *string
	Status            *string
	Registered        *time.Time
	HolderID          *int64
	HolderKey         *string
	HolderName        *string
	HolderNameSource  *string
	HolderDocument    *string
	NICBRASNs         []int64
	Designation       *string
	SpecialRFC        *string
	GloballyReachable *bool
	SourceStart       *string
	SourceValue       *int64
	RDAPURL           *string
	FirstSeen         time.Time
	UpdatedAt         time.Time
	RemovedAt         *time.Time
}

const prefixCols = `level, prefix, rir, country, status, registered, holder_id, holder_key, holder_name,
	holder_name_source, holder_document, nicbr_asns, designation, special_rfc, globally_reachable,
	source_start, source_value, rdap_url, first_seen, updated_at, removed_at`

func scanPrefix(row pgx.Row) (Prefix, error) {
	var p Prefix
	err := row.Scan(&p.Level, &p.Prefix, &p.RIR, &p.Country, &p.Status, &p.Registered, &p.HolderID,
		&p.HolderKey, &p.HolderName, &p.HolderNameSource, &p.HolderDocument, &p.NICBRASNs, &p.Designation,
		&p.SpecialRFC, &p.GloballyReachable, &p.SourceStart, &p.SourceValue, &p.RDAPURL, &p.FirstSeen,
		&p.UpdatedAt, &p.RemovedAt)
	return p, err
}

func (s *Store) queryPrefixes(ctx context.Context, sql string, args ...any) ([]Prefix, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Prefix
	for rows.Next() {
		p, err := scanPrefix(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// levelRank desempata prefixos de mesmo tamanho: nicbr, rir, special, iana.
const levelRank = `CASE level WHEN 'nicbr' THEN 0 WHEN 'rir' THEN 1 WHEN 'special' THEN 2 ELSE 3 END`

// Chain devolve todos os prefixos ativos que contêm o IP, do mais específico
// para o menos específico.
func (s *Store) Chain(ctx context.Context, ip netip.Addr) ([]Prefix, error) {
	return s.queryPrefixes(ctx, `SELECT `+prefixCols+` FROM api.prefix
		WHERE prefix >>= $1::inet AND removed_at IS NULL
		ORDER BY masklen(prefix) DESC, `+levelRank, ip)
}

// Covering devolve os prefixos ativos que contêm (ou são iguais a) p.
func (s *Store) Covering(ctx context.Context, p netip.Prefix) ([]Prefix, error) {
	return s.queryPrefixes(ctx, `SELECT `+prefixCols+` FROM api.prefix
		WHERE prefix >>= $1::cidr AND removed_at IS NULL
		ORDER BY masklen(prefix) DESC, `+levelRank, p)
}

// Children devolve as delegações ativas mais específicas dentro de p.
func (s *Store) Children(ctx context.Context, p netip.Prefix, limit int) ([]Prefix, error) {
	return s.queryPrefixes(ctx, `SELECT `+prefixCols+` FROM api.prefix
		WHERE prefix << $1::cidr AND removed_at IS NULL AND level IN ('rir', 'nicbr')
		ORDER BY prefix LIMIT $2`, p, limit)
}

// ASN é uma linha de api.asn.
type ASN struct {
	ASN              int64
	RIR              string
	Country          *string
	Status           string
	Registered       *time.Time
	Name             *string
	NameSource       *string
	Handle           *string
	RDAPURL          *string
	HolderID         *int64
	HolderKey        *string
	HolderName       *string
	HolderNameSource *string
	HolderDocument   *string
	FirstSeen        time.Time
	UpdatedAt        time.Time
}

// ASN devolve o ASN ativo ou ErrNotFound.
func (s *Store) ASN(ctx context.Context, asn int64) (*ASN, error) {
	var a ASN
	err := s.pool.QueryRow(ctx, `
		SELECT asn, rir, country, status, registered, name, name_source, handle, rdap_url, holder_id,
		       holder_key, holder_name, holder_name_source, holder_document, first_seen, updated_at
		  FROM api.asn WHERE asn = $1 AND removed_at IS NULL`, asn).
		Scan(&a.ASN, &a.RIR, &a.Country, &a.Status, &a.Registered, &a.Name, &a.NameSource, &a.Handle,
			&a.RDAPURL, &a.HolderID, &a.HolderKey, &a.HolderName, &a.HolderNameSource, &a.HolderDocument,
			&a.FirstSeen, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ASNBlock é um bloco de ASNs da IANA ou de uso especial.
type ASNBlock struct {
	Level       string
	First, Last int64
	RIR         *string
	Designation string
	Reference   *string
	Registered  *time.Time
}

// ASNBlocks devolve os blocos que contêm o ASN, do maior para o menor.
func (s *Store) ASNBlocks(ctx context.Context, asn int64) ([]ASNBlock, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT level, asn_first, asn_last, rir, designation, reference, registered
		  FROM api.asn_block WHERE asn_range @> $1::bigint
		 ORDER BY asn_last - asn_first DESC, level`, asn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ASNBlock
	for rows.Next() {
		var b ASNBlock
		if err := rows.Scan(&b.Level, &b.First, &b.Last, &b.RIR, &b.Designation, &b.Reference, &b.Registered); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ASNBrief é um resumo de ASN para listas.
type ASNBrief struct {
	ASN        int64
	Name       *string
	RIR        string
	Country    *string
	Status     string
	Registered *time.Time
	HolderKey  *string
}

func (s *Store) queryASNBriefs(ctx context.Context, sql string, args ...any) ([]ASNBrief, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ASNBrief
	for rows.Next() {
		var a ASNBrief
		if err := rows.Scan(&a.ASN, &a.Name, &a.RIR, &a.Country, &a.Status, &a.Registered, &a.HolderKey); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

const briefCols = `asn, name, rir, country, status, registered, holder_key`

// ASNsByNumber devolve os ASNs ativos da lista.
func (s *Store) ASNsByNumber(ctx context.Context, asns []int64) ([]ASNBrief, error) {
	if len(asns) == 0 {
		return nil, nil
	}
	return s.queryASNBriefs(ctx, `SELECT `+briefCols+` FROM api.asn
		WHERE asn = ANY($1) AND removed_at IS NULL ORDER BY asn`, asns)
}

// ASNsByHolder devolve os ASNs ativos de um titular.
func (s *Store) ASNsByHolder(ctx context.Context, holderID int64, limit int) ([]ASNBrief, error) {
	return s.queryASNBriefs(ctx, `SELECT `+briefCols+` FROM api.asn
		WHERE holder_id = $1 AND removed_at IS NULL ORDER BY asn LIMIT $2`, holderID, limit)
}

// LinkedPrefix é um prefixo ligado a um ASN.
type LinkedPrefix struct {
	Prefix  netip.Prefix
	Link    string // nicbr ou holder
	Status  *string
	Country *string
}

// PrefixesForASN devolve os prefixos ligados ao ASN: primeiro os vínculos
// explícitos do NIC.br, depois os do mesmo titular. total é a contagem sem o
// limite.
func (s *Store) PrefixesForASN(ctx context.Context, asn int64, holderID *int64, limit int) ([]LinkedPrefix, int, error) {
	rows, err := s.pool.Query(ctx, `
		WITH links AS (
		    SELECT prefix, 'nicbr' AS link, status, country
		      FROM api.prefix
		     WHERE nicbr_asns @> ARRAY[$1::bigint] AND removed_at IS NULL
		    UNION ALL
		    SELECT prefix, 'holder', status, country
		      FROM api.prefix
		     WHERE $2::bigint IS NOT NULL AND holder_id = $2 AND removed_at IS NULL
		       AND level IN ('rir', 'nicbr')
		       AND NOT coalesce(nicbr_asns @> ARRAY[$1::bigint], false)
		)
		SELECT prefix, link, status, country, count(*) OVER () AS total
		  FROM links
		 ORDER BY link = 'holder', prefix
		 LIMIT $3`, asn, holderID, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []LinkedPrefix
	total := 0
	for rows.Next() {
		var lp LinkedPrefix
		if err := rows.Scan(&lp.Prefix, &lp.Link, &lp.Status, &lp.Country, &total); err != nil {
			return nil, 0, err
		}
		out = append(out, lp)
	}
	return out, total, rows.Err()
}

// Holder é uma linha de api.holder.
type Holder struct {
	ID            int64
	RIR           string
	OpaqueID      string
	Key           string
	Name          *string
	NameSource    *string
	Document      *string
	Country       *string
	ASNCount      int
	Prefix4Count  int
	Prefix6Count  int
	IPv4Addresses int64
	FirstSeen     time.Time
	UpdatedAt     time.Time
	RemovedAt     *time.Time
}

// Holder devolve o titular (mesmo removido, para o histórico) ou ErrNotFound.
func (s *Store) Holder(ctx context.Context, rir, opaqueID string) (*Holder, error) {
	var h Holder
	err := s.pool.QueryRow(ctx, `
		SELECT id, rir, opaque_id, holder_key, name, name_source, document, country, asn_count,
		       prefix4_count, prefix6_count, ipv4_addresses, first_seen, updated_at, removed_at
		  FROM api.holder WHERE rir = $1 AND opaque_id = $2`, rir, opaqueID).
		Scan(&h.ID, &h.RIR, &h.OpaqueID, &h.Key, &h.Name, &h.NameSource, &h.Document, &h.Country,
			&h.ASNCount, &h.Prefix4Count, &h.Prefix6Count, &h.IPv4Addresses, &h.FirstSeen, &h.UpdatedAt,
			&h.RemovedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

// HolderPrefixes devolve uma página das delegações ativas do titular, depois
// do cursor after.
func (s *Store) HolderPrefixes(ctx context.Context, holderID int64, after *netip.Prefix, limit int) ([]Prefix, error) {
	if after == nil {
		return s.queryPrefixes(ctx, `SELECT `+prefixCols+` FROM api.prefix
			WHERE holder_id = $1 AND removed_at IS NULL ORDER BY prefix LIMIT $2`, holderID, limit)
	}
	return s.queryPrefixes(ctx, `SELECT `+prefixCols+` FROM api.prefix
		WHERE holder_id = $1 AND removed_at IS NULL AND prefix > $3::cidr ORDER BY prefix LIMIT $2`,
		holderID, limit, *after)
}

// ListFilter filtra as listas por país ou RIR.
type ListFilter struct {
	Country  string   // "BR"; vazio = qualquer
	RIR      string   // "lacnic"; vazio = qualquer
	Statuses []string // vazio = todos
	Family   int      // 4, 6 ou 0 (ambas)
}

func (f ListFilter) where(args *[]any) string {
	conds := []string{"removed_at IS NULL"}
	add := func(cond string, v any) {
		*args = append(*args, v)
		conds = append(conds, fmt.Sprintf(cond, len(*args)))
	}
	if f.Country != "" {
		add("country = $%d", f.Country)
	}
	if f.RIR != "" {
		add("rir = $%d", f.RIR)
	}
	if len(f.Statuses) > 0 {
		add("status = ANY($%d)", f.Statuses)
	}
	return strings.Join(conds, " AND ")
}

// ListPrefix é um item das listas de prefixos.
type ListPrefix struct {
	Prefix     netip.Prefix
	RIR        *string
	Country    *string
	Status     *string
	Registered *time.Time
	HolderKey  *string
}

// ListPrefixes devolve uma página de delegações. Com limit <= 0 devolve tudo
// por fn (saída em texto, sem paginação).
func (s *Store) ListPrefixes(ctx context.Context, f ListFilter, after *netip.Prefix, limit int, fn func(ListPrefix) error) error {
	args := []any{}
	where := f.where(&args) + " AND level IN ('rir', 'nicbr')"
	if f.Family != 0 {
		args = append(args, f.Family)
		where += fmt.Sprintf(" AND family = $%d", len(args))
	}
	if after != nil {
		args = append(args, *after)
		where += fmt.Sprintf(" AND prefix > $%d::cidr", len(args))
	}
	sql := `SELECT prefix, rir, country, status, registered, holder_key FROM api.prefix WHERE ` + where + ` ORDER BY prefix`
	if limit > 0 {
		args = append(args, limit)
		sql += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var p ListPrefix
		if err := rows.Scan(&p.Prefix, &p.RIR, &p.Country, &p.Status, &p.Registered, &p.HolderKey); err != nil {
			return err
		}
		if err := fn(p); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ListASNs devolve uma página de ASNs (limit <= 0: todos).
func (s *Store) ListASNs(ctx context.Context, f ListFilter, after int64, limit int, fn func(ASNBrief) error) error {
	args := []any{}
	where := f.where(&args)
	if after >= 0 {
		args = append(args, after)
		where += fmt.Sprintf(" AND asn > $%d", len(args))
	}
	sql := `SELECT ` + briefCols + ` FROM api.asn WHERE ` + where + ` ORDER BY asn`
	if limit > 0 {
		args = append(args, limit)
		sql += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a ASNBrief
		if err := rows.Scan(&a.ASN, &a.Name, &a.RIR, &a.Country, &a.Status, &a.Registered, &a.HolderKey); err != nil {
			return err
		}
		if err := fn(a); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Change é um evento do histórico.
type Change struct {
	ID         int64
	DatasetID  int64
	ChangedAt  time.Time
	Level      *string
	Action     string
	Before     json.RawMessage
	After      json.RawMessage
	EntityKey  string
	EntityType string
}

// History devolve os eventos mais recentes de uma entidade.
func (s *Store) History(ctx context.Context, entity, key string, limit int) ([]Change, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, dataset_id, changed_at, level, action, before, after, key, entity
		  FROM api.change_log
		 WHERE entity = $1 AND key = $2
		 ORDER BY changed_at DESC, id DESC
		 LIMIT $3`, entity, key, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Change
	for rows.Next() {
		var c Change
		if err := rows.Scan(&c.ID, &c.DatasetID, &c.ChangedAt, &c.Level, &c.Action, &c.Before, &c.After,
			&c.EntityKey, &c.EntityType); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SourceStatus é o estado de uma fonte.
type SourceStatus struct {
	SourceID      string
	URL           *string
	FileDate      *time.Time
	Records       *int64
	LastCheckedAt *time.Time
	LastChangedAt *time.Time
	LastSuccessAt *time.Time
	LastError     *string
	LastErrorAt   *time.Time
}

// Sources devolve o estado de todas as fontes.
func (s *Store) Sources(ctx context.Context) ([]SourceStatus, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT source_id, url, file_date, records, last_checked_at, last_changed_at, last_success_at,
		       last_error, last_error_at
		  FROM api.source_status ORDER BY source_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceStatus
	for rows.Next() {
		var st SourceStatus
		if err := rows.Scan(&st.SourceID, &st.URL, &st.FileDate, &st.Records, &st.LastCheckedAt,
			&st.LastChangedAt, &st.LastSuccessAt, &st.LastError, &st.LastErrorAt); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// Dataset é uma versão do central.
type Dataset struct {
	Version     int64
	BuiltAt     time.Time
	ASNCount    *int64
	PrefixCount *int64
	HolderCount *int64
}

// CurrentDataset devolve a versão mais recente ou ErrNotFound.
func (s *Store) CurrentDataset(ctx context.Context) (*Dataset, error) {
	var d Dataset
	err := s.pool.QueryRow(ctx, `
		SELECT version, built_at, asn_count, prefix_count, holder_count
		  FROM api.dataset ORDER BY version DESC LIMIT 1`).
		Scan(&d.Version, &d.BuiltAt, &d.ASNCount, &d.PrefixCount, &d.HolderCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// CacheExceptions devolve os blocos /24 e /48 que precisam de cache por IP.
func (s *Store) CacheExceptions(ctx context.Context) ([]netip.Prefix, error) {
	rows, err := s.pool.Query(ctx, `SELECT bucket FROM api.cache_bucket_exception`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []netip.Prefix
	for rows.Next() {
		var p netip.Prefix
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
