// Package store é o acesso do collector-rdns ao Postgres: a derivação dos
// prefixos, a fila de varredura com lease, a gravação dos resultados em
// rdns_ptr, a linha da instância em rdns_worker, a releitura de rdns_config e
// a linha do app em jobs.
//
// O schema é de database/postgres/rdns/ e database/postgres/central/; este
// pacote só lê e escreve dados. As tabelas do cgibr são só lidas, na
// derivação.
package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/resolver"
)

// AppName é o nome gravado em jobs.app e a chave da trava da derivação.
const AppName = "collector-rdns"

// RuleSQL é a regra da sonda: só IPv4 de /22 a /24 é varrido. O mesmo
// predicado está no índice parcial ix_rdns_prefix_queue.
const RuleSQL = "family(prefix) = 4 AND masklen(prefix) BETWEEN 22 AND 24"

// ErrLeaseLost indica que o lease venceu e outra instância pegou o prefixo:
// o resultado da varredura foi descartado.
var ErrLeaseLost = errors.New("lease perdido")

// Store é a conexão com o Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// Open conecta e valida com um SELECT 1. O pool tem até 4 conexões: correm
// juntos a fila, o tempo real, a batida e a derivação.
func Open(ctx context.Context, url string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("POSTGRES_URL inválida: %w", err)
	}
	cfg.MaxConns = 4
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

// seconds converte uma duração para o parâmetro $n::float8 * interval '1 second'.
func seconds(d time.Duration) float64 { return d.Seconds() }

// --- rdns_worker e rdns_config ---------------------------------------------

// Worker é a linha de uma instância em rdns_worker.
type Worker struct {
	UUID        string
	Host        string
	Version     string
	RateLimit   int
	Queries     int64
	LiveQueries int64
	Prefixes    int64
}

// Heartbeat grava a linha da instância (upsert: recria a linha que a
// manutenção apagou durante uma queda longa do banco).
func (s *Store) Heartbeat(ctx context.Context, w Worker) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO rdns_worker (uuid, host, version, rate_limit, queries, live_queries, prefixes)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (uuid) DO UPDATE SET
			rate_limit   = EXCLUDED.rate_limit,
			queries      = EXCLUDED.queries,
			live_queries = EXCLUDED.live_queries,
			prefixes     = EXCLUDED.prefixes`,
		w.UUID, w.Host, w.Version, w.RateLimit, w.Queries, w.LiveQueries, w.Prefixes)
	return err
}

// DeleteWorker apaga a linha da instância; a FK devolve à fila os prefixos
// que ela varria (scan_worker vira NULL).
func (s *Store) DeleteWorker(ctx context.Context, uuid string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM rdns_worker WHERE uuid = $1::uuid", uuid)
	return err
}

// Config lê rdns_config inteira (chave → valor).
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

// --- fila de varredura -----------------------------------------------------

// Claim é um prefixo pego da fila.
type Claim struct {
	UUID      string
	Prefix    netip.Prefix
	Rescan    bool      // o rescan lido ao pegar (um pedido feito durante a varredura não se perde)
	StartedAt time.Time // scan_started_at gravado (a conferência do lease)
}

// Claim pega o próximo prefixo vencido da fila para a instância worker, numa
// instrução só (FOR UPDATE SKIP LOCKED): rescan → nunca varrido → mais
// antigo. Devolve nil sem prefixo vencido.
func (s *Store) Claim(ctx context.Context, worker string, lease, interval time.Duration) (*Claim, error) {
	var c Claim
	err := s.pool.QueryRow(ctx, `
		UPDATE rdns_prefix SET scan_worker = $1::uuid, scan_started_at = NOW()
		 WHERE uuid = (SELECT uuid FROM rdns_prefix
		                WHERE active AND `+RuleSQL+`
		                  AND (scan_worker IS NULL OR scan_started_at < NOW() - $2::float8 * interval '1 second')
		                  AND (rescan OR last_scan_at IS NULL OR last_scan_at < NOW() - $3::float8 * interval '1 second')
		                ORDER BY rescan DESC, last_scan_at ASC NULLS FIRST
		                LIMIT 1
		                FOR UPDATE SKIP LOCKED)
		RETURNING uuid::text, prefix, rescan, scan_started_at`,
		worker, seconds(lease), seconds(interval)).Scan(&c.UUID, &c.Prefix, &c.Rescan, &c.StartedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Release devolve um prefixo à fila sem gravar nada (varredura abandonada).
func (s *Store) Release(ctx context.Context, worker string, c *Claim) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE rdns_prefix SET scan_worker = NULL, scan_started_at = NULL
		 WHERE uuid = $1::uuid AND scan_worker = $2::uuid AND scan_started_at = $3`,
		c.UUID, worker, c.StartedAt)
	return err
}

// --- gravação dos resultados ----------------------------------------------

// Row é a linha atual de um IP em rdns_ptr.
type Row struct {
	Status resolver.Status
	Names  []string
}

// Op é o que fazer com a linha de um IP.
type Op int

const (
	None Op = iota
	Insert
	Update
	Delete
)

// Decide aplica as regras de escrita de rdns_ptr (dados.md): sem PTR apaga;
// uma falha nunca apaga um PTR; a linha só é regravada quando muda. cur é a
// linha atual (nil = sem linha).
func Decide(cur *Row, res resolver.Result) Op {
	switch {
	case res.Status == resolver.NoPTR:
		if cur == nil {
			return None
		}
		return Delete
	case cur == nil:
		return Insert
	case res.Status.Failed() && cur.Status == resolver.PTR:
		return None // a falha nunca apaga um PTR
	case cur.Status == res.Status && equalNames(cur.Names, res.Names):
		return None
	}
	return Update
}

func equalNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Counts são os contadores de uma varredura completa.
type Counts struct {
	PTR     int
	NoPTR   int
	Failed  int
	Changed int
}

// Tally conta os estados de uma varredura.
func Tally(results []resolver.Result) Counts {
	var c Counts
	for _, r := range results {
		switch r.Status {
		case resolver.PTR:
			c.PTR++
		case resolver.NoPTR:
			c.NoPTR++
		default:
			c.Failed++
		}
	}
	return c
}

// queueChange põe no batch a instrução de uma linha de rdns_ptr.
func queueChange(b *pgx.Batch, op Op, res resolver.Result) {
	ip := res.IP.String()
	switch op {
	case Insert:
		b.Queue(`INSERT INTO rdns_ptr (ip, status, name, names) VALUES ($1::inet, $2, $3, $4)`,
			ip, int16(res.Status), nullName(res), nullNames(res))
	case Update:
		b.Queue(`UPDATE rdns_ptr SET status = $2, name = $3, names = $4 WHERE ip = $1::inet`,
			ip, int16(res.Status), nullName(res), nullNames(res))
	case Delete:
		b.Queue(`DELETE FROM rdns_ptr WHERE ip = $1::inet`, ip)
	}
}

func nullName(r resolver.Result) any {
	if r.Status != resolver.PTR {
		return nil
	}
	return r.Name()
}

func nullNames(r resolver.Result) any {
	if r.Status != resolver.PTR {
		return nil
	}
	return r.Names
}

// current lê, com FOR UPDATE, as linhas de rdns_ptr dentro de p.
func current(ctx context.Context, tx pgx.Tx, p netip.Prefix) (map[netip.Addr]*Row, error) {
	rows, err := tx.Query(ctx, `
		SELECT ip, status, name, names FROM rdns_ptr WHERE ip <<= $1::cidr FOR UPDATE`, p.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[netip.Addr]*Row{}
	for rows.Next() {
		var ip netip.Prefix
		var status int16
		var name *string
		var names []string
		if err := rows.Scan(&ip, &status, &name, &names); err != nil {
			return nil, err
		}
		out[ip.Addr()] = &Row{Status: resolver.Status(status), Names: names}
	}
	return out, rows.Err()
}

func applyChanges(ctx context.Context, tx pgx.Tx, cur map[netip.Addr]*Row, results []resolver.Result) (int, error) {
	b := &pgx.Batch{}
	for _, r := range results {
		if op := Decide(cur[r.IP], r); op != None {
			queueChange(b, op, r)
		}
	}
	if b.Len() == 0 {
		return 0, nil
	}
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return 0, fmt.Errorf("rdns_ptr: %w", err)
	}
	return b.Len(), nil
}

// touchSync marca em jobs que as tabelas mudaram (última instrução da
// transação: segura a trava da linha de jobs o menor tempo possível).
func touchSync(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO jobs (app, last_sync_at, consolidated) VALUES ($1, NOW(), 0)
		ON CONFLICT (app) DO UPDATE SET last_sync_at = NOW(), consolidated = 0`, AppName)
	if err != nil {
		return fmt.Errorf("jobs: %w", err)
	}
	return nil
}

// SaveScan grava o resultado da varredura de um prefixo numa transação: as
// diferenças em rdns_ptr, o fim da varredura em rdns_prefix (com a
// conferência do lease) e jobs, se algo mudou. Lease vencido: nada é gravado
// e o erro é ErrLeaseLost.
func (s *Store) SaveScan(ctx context.Context, worker string, c *Claim, results []resolver.Result, elapsed time.Duration) (Counts, error) {
	counts := Tally(results)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return counts, err
	}
	defer tx.Rollback(ctx)

	cur, err := current(ctx, tx, c.Prefix)
	if err != nil {
		return counts, err
	}
	if counts.Changed, err = applyChanges(ctx, tx, cur, results); err != nil {
		return counts, err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE rdns_prefix SET last_scan_at = NOW(), scan_worker = NULL, scan_started_at = NULL,
		       rescan = CASE WHEN $3 THEN false ELSE rescan END,
		       last_scan_ms = $5, last_scan_ptr = $6, last_scan_no_ptr = $7,
		       last_scan_failed = $8, last_scan_changed = $9
		 WHERE uuid = $1::uuid AND scan_worker = $2::uuid AND scan_started_at = $4`,
		c.UUID, worker, c.Rescan, c.StartedAt, elapsed.Milliseconds(),
		counts.PTR, counts.NoPTR, counts.Failed, counts.Changed)
	if err != nil {
		return counts, fmt.Errorf("rdns_prefix: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return counts, ErrLeaseLost
	}
	if counts.Changed > 0 {
		if err := touchSync(ctx, tx); err != nil {
			return counts, err
		}
	}
	return counts, tx.Commit(ctx)
}

// LiveAllowed diz se ip está num prefixo ativo que a regra da sonda aceita.
func (s *Store) LiveAllowed(ctx context.Context, ip netip.Addr) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM rdns_prefix
		                WHERE prefix >>= $1::inet AND active AND `+RuleSQL+`)`, ip.String()).Scan(&ok)
	return ok, err
}

// SaveLive grava o resultado de um pedido de tempo real numa transação de um
// IP, com as mesmas regras da varredura (e jobs, se mudou). O last_scan_at
// do prefixo não muda. Devolve se a linha mudou.
func (s *Store) SaveLive(ctx context.Context, res resolver.Result) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	cur, err := current(ctx, tx, netip.PrefixFrom(res.IP, 32))
	if err != nil {
		return false, err
	}
	n, err := applyChanges(ctx, tx, cur, []resolver.Result{res})
	if err != nil {
		return false, err
	}
	if n > 0 {
		if err := touchSync(ctx, tx); err != nil {
			return false, err
		}
	}
	return n > 0, tx.Commit(ctx)
}
