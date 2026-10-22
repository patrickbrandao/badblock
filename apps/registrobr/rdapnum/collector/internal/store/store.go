// Package store é o acesso do collector-registrobr-rdapnum ao Postgres: a
// derivação dos alvos, a fila com lease, a gravação das respostas cruas
// (com o histórico), o processamento nas tabelas processadas, a linha da
// instância em registrobr_rdapnum_worker, a releitura de
// registrobr_rdapnum_config e a linha do app em jobs.
//
// O schema é de database/postgres/registrobr_rdapnum/ e
// database/postgres/central/; este pacote só lê e escreve dados. As tabelas
// do cgibr são só lidas, na derivação.
package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/rdap"
)

// AppName é o nome gravado em jobs.app e a chave da trava da derivação.
const AppName = "collector-registrobr-rdapnum"

// ErrLeaseLost indica que o lease venceu e outra instância pegou o alvo: a
// resposta foi descartada.
var ErrLeaseLost = errors.New("lease perdido")

// Store é a conexão com o Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// Open conecta e valida com um SELECT 1. O pool tem até 4 conexões: correm
// juntos a fila, o processamento, a batida e a derivação.
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

// --- _worker e _config -----------------------------------------------------

// Worker é a linha de uma instância em registrobr_rdapnum_worker.
type Worker struct {
	UUID        string
	Host        string
	Version     string
	RateLimit   int
	Proxy       bool
	Requests    int64
	Complete    int64
	Truncated   int64
	NotFound    int64
	Errors      int64
	Refreshes   int64
	EgressIP    netip.Addr // inválido = nenhum visto
	EgressIPs   int
	PausedUntil time.Time // zero = sem pausa
}

// Heartbeat grava a linha da instância (upsert: recria a linha que a
// manutenção apagou durante uma queda longa do banco).
func (s *Store) Heartbeat(ctx context.Context, w Worker) error {
	var egress, paused any
	if w.EgressIP.IsValid() {
		egress = w.EgressIP.String()
	}
	if !w.PausedUntil.IsZero() {
		paused = w.PausedUntil
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO registrobr_rdapnum_worker (uuid, host, version, rate_limit, proxy, requests, complete,
		       truncated, not_found, errors, refreshes, egress_ip, egress_ips, paused_until)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::inet, $13, $14)
		ON CONFLICT (uuid) DO UPDATE SET
			rate_limit   = EXCLUDED.rate_limit,
			proxy        = EXCLUDED.proxy,
			requests     = EXCLUDED.requests,
			complete     = EXCLUDED.complete,
			truncated    = EXCLUDED.truncated,
			not_found    = EXCLUDED.not_found,
			errors       = EXCLUDED.errors,
			refreshes    = EXCLUDED.refreshes,
			egress_ip    = EXCLUDED.egress_ip,
			egress_ips   = EXCLUDED.egress_ips,
			paused_until = EXCLUDED.paused_until`,
		w.UUID, w.Host, w.Version, w.RateLimit, w.Proxy, w.Requests, w.Complete, w.Truncated, w.NotFound,
		w.Errors, w.Refreshes, egress, w.EgressIPs, paused)
	return err
}

// DeleteWorker apaga a linha da instância; a FK devolve à fila os alvos que
// ela consultava (scan_worker vira NULL).
func (s *Store) DeleteWorker(ctx context.Context, uuid string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM registrobr_rdapnum_worker WHERE uuid = $1::uuid", uuid)
	return err
}

// Config lê registrobr_rdapnum_config inteira (chave → valor).
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

// --- fila ------------------------------------------------------------------

// Claim é um alvo pego da fila.
type Claim struct {
	UUID      string
	Kind      string // asn ou prefix
	ASN       int64
	Prefix    netip.Prefix
	Rescan    bool      // o rescan lido ao pegar (um pedido feito durante a coleta não se perde)
	StartedAt time.Time // scan_started_at gravado (a conferência do lease)
	Failures  int       // truncadas e erros seguidos antes desta tentativa
}

// Target é o alvo como vai na URL: o ASN ou o bloco.
func (c *Claim) Target() string {
	if c.Kind == rdap.KindASN {
		return strconv.FormatInt(c.ASN, 10)
	}
	return c.Prefix.String()
}

// Claim pega o próximo alvo vencido da fila para a instância worker, numa
// instrução só (FOR UPDATE SKIP LOCKED): rescan → nunca concluído → mais
// antigo, fora os que estão em espera (retry_at). Devolve nil sem alvo
// vencido.
func (s *Store) Claim(ctx context.Context, worker string, lease, interval time.Duration) (*Claim, error) {
	var c Claim
	var asn *int64
	var prefix *netip.Prefix
	err := s.pool.QueryRow(ctx, `
		UPDATE registrobr_rdapnum_target SET scan_worker = $1::uuid, scan_started_at = NOW()
		 WHERE uuid = (SELECT uuid FROM registrobr_rdapnum_target
		                WHERE active
		                  AND (scan_worker IS NULL OR scan_started_at < NOW() - $2::float8 * interval '1 second')
		                  AND (retry_at IS NULL OR retry_at <= NOW())
		                  AND (rescan OR last_scan_at IS NULL OR last_scan_at < NOW() - $3::float8 * interval '1 second')
		                ORDER BY rescan DESC, last_scan_at ASC NULLS FIRST
		                LIMIT 1
		                FOR UPDATE SKIP LOCKED)
		RETURNING uuid::text, kind, asn, prefix, rescan, scan_started_at, failures`,
		worker, lease.Seconds(), interval.Seconds()).
		Scan(&c.UUID, &c.Kind, &asn, &prefix, &c.Rescan, &c.StartedAt, &c.Failures)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if asn != nil {
		c.ASN = *asn
	}
	if prefix != nil {
		c.Prefix = *prefix
	}
	return &c, nil
}

// Release devolve um alvo à fila sem gravar nada (coleta abandonada).
func (s *Store) Release(ctx context.Context, worker string, c *Claim) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE registrobr_rdapnum_target SET scan_worker = NULL, scan_started_at = NULL
		 WHERE uuid = $1::uuid AND scan_worker = $2::uuid AND scan_started_at = $3`,
		c.UUID, worker, c.StartedAt)
	return err
}

// --- tentativa e resposta crua ---------------------------------------------

// Attempt é o resultado de uma consulta a gravar.
type Attempt struct {
	Resp     rdap.Response
	RetryIn  time.Duration // espera do alvo, quando não é conclusiva
	ErrorMsg string        // last_error, quando Resp.Result = Error
}

// Saved é o que a gravação de uma tentativa fez.
type Saved struct {
	ResponseUUID string // a linha da resposta (nova ou a igual anterior); vazio em erro
	Changed      bool   // linha nova em _response
}

// SaveAttempt grava uma tentativa numa transação (collector.md#coleta-de-um-alvo):
// a resposta crua, se houver (só linha nova quando mudou), e o estado do
// alvo, com a conferência do lease. Conclusiva: a resposta vira a atual.
// Truncada ou erro: falhas + 1 e espera. Lease vencido: nada é gravado e o
// erro é ErrLeaseLost.
func (s *Store) SaveAttempt(ctx context.Context, worker string, c *Claim, a Attempt) (Saved, error) {
	var out Saved
	r := a.Resp
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)

	if r.Result != rdap.Error {
		if out.ResponseUUID, out.Changed, err = saveResponse(ctx, tx, worker, c, r); err != nil {
			return out, err
		}
	}
	var status any
	if r.HTTPStatus > 0 {
		status = r.HTTPStatus
	}
	var tag pgconn.CommandTag
	if r.Result.Conclusive() {
		tag, err = tx.Exec(ctx, `
			UPDATE registrobr_rdapnum_target SET
			       last_scan_at = NOW(), response_uuid = $4::uuid, failures = 0, retry_at = NULL, last_error = NULL,
			       rescan = CASE WHEN $5 THEN false ELSE rescan END,
			       scan_worker = NULL, scan_started_at = NULL,
			       last_attempt_at = NOW(), last_result = $6, last_http_status = $7
			 WHERE uuid = $1::uuid AND scan_worker = $2::uuid AND scan_started_at = $3`,
			c.UUID, worker, c.StartedAt, out.ResponseUUID, c.Rescan, int16(r.Result), status)
	} else {
		var lastErr any
		if r.Result == rdap.Error {
			lastErr = a.ErrorMsg
		}
		tag, err = tx.Exec(ctx, `
			UPDATE registrobr_rdapnum_target SET
			       failures = LEAST(failures + 1, 32767), retry_at = NOW() + $4::float8 * interval '1 second',
			       last_error = CASE WHEN $5::text IS NULL THEN last_error ELSE $5::text END,
			       scan_worker = NULL, scan_started_at = NULL,
			       last_attempt_at = NOW(), last_result = $6, last_http_status = $7
			 WHERE uuid = $1::uuid AND scan_worker = $2::uuid AND scan_started_at = $3`,
			c.UUID, worker, c.StartedAt, a.RetryIn.Seconds(), lastErr, int16(r.Result), status)
	}
	if err != nil {
		return out, fmt.Errorf("registrobr_rdapnum_target: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Saved{}, ErrLeaseLost
	}
	return out, tx.Commit(ctx)
}

// saveResponse grava a resposta crua: igual à última do mesmo result do
// alvo, só anda last_seen_at e seen_count; diferente, ou nenhuma, linha nova
// (dados.md#resposta-igual).
func saveResponse(ctx context.Context, tx pgx.Tx, worker string, c *Claim, r rdap.Response) (string, bool, error) {
	var sum string
	var err error
	if r.Result == rdap.NotFound {
		sum, err = Hash(nil)
	} else {
		sum, err = Hash(r.Body)
	}
	if err != nil {
		return "", false, err
	}
	var id, last string
	err = tx.QueryRow(ctx, `
		SELECT uuid::text, sha256 FROM registrobr_rdapnum_response
		 WHERE target_uuid = $1::uuid AND result = $2
		 ORDER BY fetched_at DESC, uuid DESC LIMIT 1
		 FOR UPDATE`, c.UUID, int16(r.Result)).Scan(&id, &last)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("registrobr_rdapnum_response: %w", err)
	}
	if err == nil && last == sum {
		if _, err := tx.Exec(ctx, `
			UPDATE registrobr_rdapnum_response SET last_seen_at = NOW(), seen_count = seen_count + 1
			 WHERE uuid = $1::uuid`, id); err != nil {
			return "", false, fmt.Errorf("registrobr_rdapnum_response: %w", err)
		}
		return id, false, nil
	}
	var body, egress, srvTime any
	if r.Body != nil {
		body = string(r.Body)
	}
	if r.EgressIP.IsValid() {
		egress = r.EgressIP.String()
	}
	if !r.ServerTime.IsZero() {
		srvTime = r.ServerTime
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO registrobr_rdapnum_response (target_uuid, result, url, http_status, body, sha256, bytes,
		       egress_ip, server_time, worker)
		VALUES ($1::uuid, $2, $3, $4, $5::jsonb, $6, $7, $8::inet, $9, $10::uuid)
		RETURNING uuid::text`,
		c.UUID, int16(r.Result), r.URL, r.HTTPStatus, body, sum, r.Bytes, egress, srvTime, worker).Scan(&id)
	if err != nil {
		return "", false, fmt.Errorf("registrobr_rdapnum_response: %w", err)
	}
	return id, true, nil
}

// --- atualização pedida pela API -------------------------------------------

// MarkRescan marca rescan = true no alvo ativo (asn ou prefix). Devolve se
// achou o alvo.
func (s *Store) MarkRescan(ctx context.Context, kind, target string) (bool, error) {
	col := "asn = $1::bigint"
	if kind == rdap.KindPrefix {
		col = "prefix = $1::cidr"
	}
	tag, err := s.pool.Exec(ctx, "UPDATE registrobr_rdapnum_target SET rescan = true WHERE "+col+" AND active", target)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// --- reprocessamento -------------------------------------------------------

// PendingProcess lista até limit respostas atuais (target.response_uuid) que
// precisam de processamento: parser de outra versão (ou nunca processadas)
// ou recusadas há mais de 1 h (collector.md#reprocessamento).
func (s *Store) PendingProcess(ctx context.Context, version, limit int) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.uuid::text FROM registrobr_rdapnum_target t
		  JOIN registrobr_rdapnum_response r ON r.uuid = t.response_uuid
		 WHERE r.result IN (1, 3)
		   AND (r.parser_version IS DISTINCT FROM $1::smallint
		        OR (r.process_error IS NOT NULL AND r.processed_at < NOW() - interval '1 hour'))
		 ORDER BY r.fetched_at
		 LIMIT $2`, version, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
