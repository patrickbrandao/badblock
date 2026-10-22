package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// maxWarnings avisos vão para _run.warnings; os outros viram um item
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
	RunID   string // registrobr_rdapnum_run.uuid da linha gravada

	ASNs, Prefixes, Inserted, NotInSource int
	Warnings                              []string
}

// DeriveResult é uma passada da derivação.
type DeriveResult struct {
	Locked          bool // false = outra instância está derivando; nada foi feito
	Runs            []DeriveRun
	WorkersRemoved  int64 // instâncias sem sinal há mais de 24 h, apagadas
	EntitiesRemoved int64 // entidades sem nenhuma ligação, apagadas
}

// Changed diz se a passada inseriu algum alvo ou apagou entidade órfã.
func (r DeriveResult) Changed() bool {
	if r.EntitiesRemoved > 0 {
		return true
	}
	for _, run := range r.Runs {
		if run.Inserted > 0 {
			return true
		}
	}
	return false
}

// DeriveError é uma falha depois de ler a versão da origem: Run vai para
// _run com status = 0 (RecordDeriveFailure).
type DeriveError struct {
	Run DeriveRun
	Err error
}

func (e *DeriveError) Error() string { return e.Err.Error() }
func (e *DeriveError) Unwrap() error { return e.Err }

// Derive faz uma passada da derivação: com a trava, numa transação
// REPEATABLE READ (a versão lida e os ASNs e blocos lidos são do mesmo
// instante), a derivação de cada origem em sources, a manutenção (instâncias
// mortas, entidades órfãs) e jobs.
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
	tag, err := tx.Exec(ctx, "DELETE FROM registrobr_rdapnum_worker WHERE updated_at < NOW() - interval '24 hours'")
	if err != nil {
		return res, fmt.Errorf("registrobr_rdapnum_worker: %w", err)
	}
	res.WorkersRemoved = tag.RowsAffected()
	tag, err = tx.Exec(ctx, `
		DELETE FROM registrobr_rdapnum_entity e
		 WHERE NOT EXISTS (SELECT 1 FROM registrobr_rdapnum_link l
		                    WHERE l.entity_uuid = e.uuid OR l.via_uuid = e.uuid)`)
	if err != nil {
		return res, fmt.Errorf("registrobr_rdapnum_entity: %w", err)
	}
	res.EntitiesRemoved = tag.RowsAffected()

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

// deriveCgibr deriva os alvos dos ASNs de cgibr_asn e dos blocos de
// cgibr_prefix (collector.md#derivação).
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
		SELECT source_version::text FROM registrobr_rdapnum_run
		 WHERE status = 1 AND source = 'cgibr' ORDER BY created_at DESC LIMIT 1`).Scan(&last)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("registrobr_rdapnum_run: %w", err)
	}
	if last == run.Version && !force {
		run.Reason = "versão do cgibr já derivada"
		return nil
	}

	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE stage_target (
			kind    text    NOT NULL,
			asn     bigint,
			prefix  cidr
		) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("stage_target: %w", err)
	}
	tag, err := tx.Exec(ctx, `INSERT INTO stage_target (kind, asn) SELECT DISTINCT 'asn', asn FROM cgibr_asn`)
	if err != nil {
		return fmt.Errorf("stage_target: %w", err)
	}
	run.ASNs = int(tag.RowsAffected())
	tag, err = tx.Exec(ctx, `INSERT INTO stage_target (kind, prefix) SELECT DISTINCT 'prefix', prefix FROM cgibr_prefix`)
	if err != nil {
		return fmt.Errorf("stage_target: %w", err)
	}
	run.Prefixes = int(tag.RowsAffected())
	if _, err := tx.Exec(ctx, "ANALYZE stage_target"); err != nil {
		return err
	}

	// Novos; o que já existe (derivado ou estático) fica como está.
	tag, err = tx.Exec(ctx, `
		INSERT INTO registrobr_rdapnum_target (kind, asn, prefix, source)
		SELECT kind, asn, prefix, 'cgibr' FROM stage_target
		ON CONFLICT DO NOTHING`)
	if err != nil {
		return fmt.Errorf("registrobr_rdapnum_target: %w", err)
	}
	run.Inserted = int(tag.RowsAffected())

	if err := tx.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM registrobr_rdapnum_target t
		         WHERE t.source = 'cgibr' AND t.kind = 'asn'
		           AND NOT EXISTS (SELECT 1 FROM stage_target s WHERE s.asn = t.asn))
		     + (SELECT count(*) FROM registrobr_rdapnum_target t
		         WHERE t.source = 'cgibr' AND t.kind = 'prefix'
		           AND NOT EXISTS (SELECT 1 FROM stage_target s WHERE s.prefix = t.prefix))`).
		Scan(&run.NotInSource); err != nil {
		return fmt.Errorf("registrobr_rdapnum_target: %w", err)
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
	wj, err := json.Marshal(capWarnings(run.Warnings))
	if err != nil {
		return "", err
	}
	counts := []any{nil, nil, nil, nil}
	if status == 1 {
		counts = []any{run.ASNs, run.Prefixes, run.Inserted, run.NotInSource}
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
		INSERT INTO registrobr_rdapnum_run (
			status, forced, source, source_version, source_updated_at,
			asns, prefixes, inserted, not_in_source,
			warnings, error, started_at
		) VALUES ($1, $2, $3, $4::uuid, $5, $6, $7, $8, $9, $10::jsonb, $11, $12)
		RETURNING uuid::text`, args...).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("registrobr_rdapnum_run: %w", err)
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
