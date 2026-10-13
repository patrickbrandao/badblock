package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// maxWarnings avisos vão para rdns_run.warnings; os outros viram um item
// "... e mais N avisos".
const maxWarnings = 50

// DeriveRun é a derivação de uma fonte de origem numa passada.
type DeriveRun struct {
	Source          string
	Version         string // versão lida da origem (vazio = não chegou a ler)
	SourceUpdatedAt time.Time
	Forced          bool
	StartedAt       time.Time

	Applied bool   // false = sem mudança (Reason)
	Reason  string // por que não derivou
	RunID   string // rdns_run.uuid da linha gravada

	Blocks, Units, Inserted, ASNUpdated, Covered, NotInSource int
	Warnings                                                  []string
}

// DeriveResult é uma passada da derivação.
type DeriveResult struct {
	Locked         bool // false = outra instância está derivando; nada foi feito
	Runs           []DeriveRun
	OutOfRule      int   // prefixos que a regra da sonda não aceita
	WorkersRemoved int64 // linhas de rdns_worker sem sinal há mais de 24 h, apagadas
}

// Changed diz se a passada inseriu ou alterou algum prefixo.
func (r DeriveResult) Changed() bool {
	for _, run := range r.Runs {
		if run.Inserted+run.ASNUpdated > 0 {
			return true
		}
	}
	return false
}

// DeriveError é uma falha depois de ler a versão da origem: Run vai para
// rdns_run com status = 0 (RecordDeriveFailure).
type DeriveError struct {
	Run DeriveRun
	Err error
}

func (e *DeriveError) Error() string { return e.Err.Error() }
func (e *DeriveError) Unwrap() error { return e.Err }

// Derive faz uma passada da derivação: com a trava, numa transação
// REPEATABLE READ (a versão lida e os blocos lidos são do mesmo instante), a
// derivação de cada origem em sources, a manutenção (instâncias mortas,
// prefixos fora da regra) e jobs.
//
// A trava é de sessão (pg_try_advisory_lock), pega numa conexão dedicada
// ANTES do BEGIN: em REPEATABLE READ o instantâneo da transação sai no início
// da primeira instrução, e uma trava de transação pega nessa instrução podia
// vir com um instantâneo anterior ao commit da instância que acabou de
// derivar — e esta derivaria de novo sobre dados velhos.
func (s *Store) Derive(ctx context.Context, sources []string, force bool) (DeriveResult, error) {
	var res DeriveResult
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return res, err
	}
	defer conn.Release()
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtext($1))", AppName).Scan(&res.Locked); err != nil {
		return res, err
	}
	if !res.Locked {
		return res, nil
	}
	defer func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(c, "SELECT pg_advisory_unlock(hashtext($1))", AppName); err != nil {
			// Sem soltar a trava, a conexão sai do pool: fechada, o servidor
			// solta a trava junto.
			_ = conn.Hijack().Close(c)
		}
	}()

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return res, err
	}
	defer tx.Rollback(ctx)
	for _, src := range sources {
		run := DeriveRun{Source: src, Forced: force, StartedAt: time.Now()}
		switch src {
		case "cgibr":
			err = deriveCgibr(ctx, tx, &run, force)
		default:
			err = fmt.Errorf("fonte de origem desconhecida: %s", src)
		}
		if err != nil {
			if run.Version != "" {
				return res, &DeriveError{Run: run, Err: err}
			}
			return res, err
		}
		res.Runs = append(res.Runs, run)
	}

	// Manutenção, ainda com a trava.
	tag, err := tx.Exec(ctx, "DELETE FROM rdns_worker WHERE updated_at < NOW() - interval '24 hours'")
	if err != nil {
		return res, fmt.Errorf("rdns_worker: %w", err)
	}
	res.WorkersRemoved = tag.RowsAffected()
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM rdns_prefix WHERE NOT ("+RuleSQL+")").Scan(&res.OutOfRule); err != nil {
		return res, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO jobs (app, last_check_at, last_sync_at, consolidated)
		VALUES ($1, NOW(), CASE WHEN $2 THEN NOW() END, 0)
		ON CONFLICT (app) DO UPDATE SET
			last_check_at = NOW(),
			last_sync_at  = CASE WHEN $2 THEN NOW() ELSE jobs.last_sync_at END,
			consolidated  = CASE WHEN $2 THEN 0 ELSE jobs.consolidated END`,
		AppName, res.Changed()); err != nil {
		return res, fmt.Errorf("jobs: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		if len(res.Runs) > 0 && res.Runs[len(res.Runs)-1].Version != "" {
			run := res.Runs[len(res.Runs)-1]
			run.Applied = false
			return res, &DeriveError{Run: run, Err: err}
		}
		return res, err
	}
	return res, nil
}

// deriveCgibr deriva os /24 dos blocos IPv4 de cgibr_prefix (dados.md e
// collector.md#derivação).
func deriveCgibr(ctx context.Context, tx pgx.Tx, run *DeriveRun, force bool) error {
	err := tx.QueryRow(ctx, `
		SELECT uuid::text, created_at FROM cgibr_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1`).
		Scan(&run.Version, &run.SourceUpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		run.Reason = "cgibr sem dataset aplicado"
		return nil
	}
	if err != nil {
		return fmt.Errorf("cgibr_run: %w", err)
	}
	var last string
	err = tx.QueryRow(ctx, `
		SELECT source_version::text FROM rdns_run
		 WHERE status = 1 AND source = 'cgibr' ORDER BY created_at DESC LIMIT 1`).Scan(&last)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("rdns_run: %w", err)
	}
	if last == run.Version && !force {
		run.Reason = "versão do cgibr já derivada"
		return nil
	}

	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE stage_unit (
			prefix  cidr    PRIMARY KEY,
			asn     bigint  NOT NULL
		) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("stage_unit: %w", err)
	}
	// /24 repetido (blocos sobrepostos): vale o bloco mais específico.
	tag, err := tx.Exec(ctx, `
		INSERT INTO stage_unit (prefix, asn)
		SELECT DISTINCT ON (u.prefix) u.prefix, u.asn
		  FROM (SELECT set_masklen(network(p.prefix) + i * 256, 24)::cidr AS prefix,
		               a.asn, masklen(p.prefix) AS len
		          FROM cgibr_prefix p
		          JOIN cgibr_asn a ON a.uuid = p.asn_uuid
		         CROSS JOIN LATERAL generate_series(0::bigint, (1::bigint << (24 - masklen(p.prefix))) - 1) AS i
		         WHERE family(p.prefix) = 4 AND masklen(p.prefix) <= 24) u
		 ORDER BY u.prefix, u.len DESC`)
	if err != nil {
		return fmt.Errorf("stage_unit: %w", err)
	}
	run.Units = int(tag.RowsAffected())
	if _, err := tx.Exec(ctx, "ANALYZE stage_unit"); err != nil {
		return err
	}

	if err := tx.QueryRow(ctx, "SELECT count(*) FROM cgibr_prefix WHERE family(prefix) = 4").Scan(&run.Blocks); err != nil {
		return fmt.Errorf("cgibr_prefix: %w", err)
	}
	long, err := tx.Query(ctx, `
		SELECT prefix::text FROM cgibr_prefix WHERE family(prefix) = 4 AND masklen(prefix) > 24 ORDER BY prefix`)
	if err != nil {
		return fmt.Errorf("cgibr_prefix: %w", err)
	}
	blocks, err := pgx.CollectRows(long, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("cgibr_prefix: %w", err)
	}
	for _, b := range blocks {
		run.Warnings = append(run.Warnings, "bloco "+b+" mais longo que /24 ignorado")
	}

	// Novos: o que já está coberto (pela própria linha ou por uma estática)
	// fica de fora. Sem leitura sequencial: na primeira derivação rdns_prefix
	// está vazia quando o plano é feito, e o anti-join leria a tabela inteira
	// — que cresce durante o INSERT — para cada /24; o GiST da
	// ex_rdns_prefix_overlap responde o && de cada um.
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		return err
	}
	tag, err = tx.Exec(ctx, `
		INSERT INTO rdns_prefix (prefix, asn, source)
		SELECT s.prefix, s.asn, 'cgibr' FROM stage_unit s
		 WHERE NOT EXISTS (SELECT 1 FROM rdns_prefix p WHERE p.prefix && s.prefix)`)
	if err != nil {
		return fmt.Errorf("rdns_prefix: %w", err)
	}
	run.Inserted = int(tag.RowsAffected())
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = on"); err != nil {
		return err
	}

	// ASN corrigido, sem rescan (o PTR não depende do ASN). Linhas static
	// nunca são tocadas.
	tag, err = tx.Exec(ctx, `
		UPDATE rdns_prefix p SET asn = s.asn FROM stage_unit s
		 WHERE p.prefix = s.prefix AND p.source = 'cgibr' AND p.asn <> s.asn`)
	if err != nil {
		return fmt.Errorf("rdns_prefix: %w", err)
	}
	run.ASNUpdated = int(tag.RowsAffected())
	run.Covered = run.Units - run.Inserted

	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM rdns_prefix p
		 WHERE p.source = 'cgibr' AND NOT EXISTS (SELECT 1 FROM stage_unit s WHERE s.prefix = p.prefix)`).
		Scan(&run.NotInSource); err != nil {
		return fmt.Errorf("rdns_prefix: %w", err)
	}

	run.Applied = true
	id, err := insertRun(ctx, tx, *run, 1, "")
	if err != nil {
		return err
	}
	run.RunID = id
	return nil
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// RecordDeriveFailure grava, fora da transação da passada, a linha status =
// 0 de uma derivação que falhou depois de ler a versão da origem.
func (s *Store) RecordDeriveFailure(ctx context.Context, run DeriveRun, cause error) error {
	_, err := insertRun(ctx, s.pool, run, 0, cause.Error())
	return err
}

func insertRun(ctx context.Context, q querier, run DeriveRun, status int, errMsg string) (string, error) {
	warnings := capWarnings(run.Warnings)
	wj, err := json.Marshal(warnings)
	if err != nil {
		return "", err
	}
	counts := []any{nil, nil, nil, nil, nil, nil}
	if status == 1 {
		counts = []any{run.Blocks, run.Units, run.Inserted, run.ASNUpdated, run.Covered, run.NotInSource}
	}
	var errArg any
	if errMsg != "" {
		errArg = errMsg
	}
	args := []any{status, run.Forced, run.Source, run.Version, run.SourceUpdatedAt}
	args = append(args, counts...)
	args = append(args, string(wj), errArg, run.StartedAt)
	var id string
	err = q.QueryRow(ctx, `
		INSERT INTO rdns_run (
			status, forced, source, source_version, source_updated_at,
			blocks, units, inserted, asn_updated, covered, not_in_source,
			warnings, error, started_at
		) VALUES ($1, $2, $3, $4::uuid, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, $14)
		RETURNING uuid::text`, args...).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("rdns_run: %w", err)
	}
	return id, nil
}

func capWarnings(w []string) []string {
	if len(w) == 0 {
		return []string{}
	}
	if len(w) <= maxWarnings {
		return w
	}
	out := append([]string(nil), w[:maxWarnings]...)
	return append(out, fmt.Sprintf("... e mais %d avisos", len(w)-maxWarnings))
}
