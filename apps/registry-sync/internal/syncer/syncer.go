// Package syncer orquestra os ciclos de sincronização: decide quais fontes
// verificar, baixa, valida, aplica e reconstrói o central quando algo mudou.
package syncer

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/source"
	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/store"
)

// Config são os parâmetros de um Syncer.
type Config struct {
	Sources          []source.Source
	DefaultInterval  time.Duration
	Intervals        map[string]time.Duration // intervalo por fonte (sobrescreve o padrão)
	RemovalThreshold float64
	RemovalGrace     time.Duration
	RawDir           string // onde guardar os arquivos aplicados (vazio desliga)
	RawKeep          int
	RunRetention     time.Duration
	Force            bool
	SkipMinRecords   bool          // modo fixture: as fixtures são recortes pequenos
	Tick             time.Duration // de quanto em quanto o laço confere fontes vencidas
}

// Syncer executa os ciclos.
type Syncer struct {
	cfg     Config
	store   *store.Store
	fetcher *fetch.Fetcher
	log     *slog.Logger

	mu     sync.Mutex
	status Status
}

// Status é o resumo exposto em /status.
type Status struct {
	Running        bool       `json:"running"`
	LastCycleStart *time.Time `json:"last_cycle_start,omitempty"`
	LastCycleEnd   *time.Time `json:"last_cycle_end,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	LastApplied    []string   `json:"last_applied,omitempty"`
	LastBuild      *BuildInfo `json:"last_build,omitempty"`
}

// BuildInfo resume a última reconstrução feita por este processo.
type BuildInfo struct {
	DatasetID  int64     `json:"dataset_id"`
	Changes    int64     `json:"changes"`
	DurationMS int64     `json:"duration_ms"`
	At         time.Time `json:"at"`
}

// New cria um Syncer.
func New(cfg Config, st *store.Store, f *fetch.Fetcher, log *slog.Logger) *Syncer {
	if cfg.Tick <= 0 {
		cfg.Tick = time.Minute
	}
	return &Syncer{cfg: cfg, store: st, fetcher: f, log: log}
}

// Status devolve uma cópia do estado atual.
func (s *Syncer) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// CycleResult resume um ciclo.
type CycleResult struct {
	Skipped  bool // outro processo estava sincronizando
	Checked  []string
	Applied  []string
	Failed   []string
	Build    *store.BuildStats
	Duration time.Duration
}

// Run executa ciclos até o contexto acabar. O primeiro ciclo roda na hora.
func (s *Syncer) Run(ctx context.Context) error {
	for {
		if _, err := s.Cycle(ctx, false); err != nil && ctx.Err() == nil {
			s.log.Error("ciclo falhou", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.cfg.Tick):
		}
	}
}

// Cycle verifica as fontes vencidas (ou todas, com all=true), aplica o que
// mudou e reconstrói o central se preciso.
//
// Os resultados são nomeados para que o defer que publica o status enxergue o
// erro devolvido por qualquer return.
func (s *Syncer) Cycle(ctx context.Context, all bool) (res CycleResult, err error) {
	start := time.Now()

	unlock, ok, lockErr := s.store.TryLock(ctx)
	if lockErr != nil {
		return res, fmt.Errorf("lock: %w", lockErr)
	}
	if !ok {
		s.log.Info("outro processo está sincronizando; ciclo ignorado")
		res.Skipped = true
		return res, nil
	}
	defer unlock()

	s.setRunning(start)
	defer func() { s.setDone(res, err) }()

	states, err := s.store.States(ctx)
	if err != nil {
		return res, err
	}

	for _, src := range s.cfg.Sources {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		prev := states[src.ID]
		if !all && !s.due(src, prev, start) {
			continue
		}
		res.Checked = append(res.Checked, src.ID)
		outcome, err := s.processSource(ctx, src, prev)
		switch {
		case err != nil:
			res.Failed = append(res.Failed, src.ID)
			s.log.Error("fonte falhou", "source", src.ID, "outcome", outcome, "err", err)
		case outcome == "applied":
			res.Applied = append(res.Applied, src.ID)
		}
	}

	needBuild := len(res.Applied) > 0
	if !needBuild {
		needBuild, err = s.buildPending(ctx)
		if err != nil {
			return res, err
		}
	}
	if needBuild {
		bs, err := s.store.Build(ctx, s.cfg.RemovalGrace)
		if err != nil {
			return res, fmt.Errorf("build: %w", err)
		}
		res.Build = &bs
		s.log.Info("central reconstruído", "dataset", bs.DatasetID, "baseline", bs.Baseline,
			"changes", bs.Changes, "asns", bs.ASNs, "prefixes", bs.Prefixes, "holders", bs.Holders,
			"duration", bs.Duration.Round(time.Millisecond).String())
	}

	if s.cfg.RunRetention > 0 {
		if n, err := s.store.PruneRuns(ctx, s.cfg.RunRetention); err != nil {
			s.log.Warn("limpeza do histórico de execuções falhou", "err", err)
		} else if n > 0 {
			s.log.Info("histórico de execuções limpo", "rows", n)
		}
	}
	res.Duration = time.Since(start)
	if len(res.Failed) > 0 {
		return res, fmt.Errorf("fontes com falha: %s", strings.Join(res.Failed, ", "))
	}
	return res, nil
}

// Rebuild só reconstrói o central, sem baixar nada.
func (s *Syncer) Rebuild(ctx context.Context) (store.BuildStats, error) {
	unlock, ok, err := s.store.TryLock(ctx)
	if err != nil {
		return store.BuildStats{}, err
	}
	if !ok {
		return store.BuildStats{}, errors.New("outro processo está sincronizando; tente de novo em instantes")
	}
	defer unlock()
	return s.store.Build(ctx, s.cfg.RemovalGrace)
}

// buildPending diz se o central precisa ser reconstruído sem fonte nova: nunca
// houve build (e já há dados) ou alguma carência de remoção venceu.
func (s *Syncer) buildPending(ctx context.Context) (bool, error) {
	_, exists, err := s.store.LatestDataset(ctx)
	if err != nil {
		return false, err
	}
	if !exists {
		return s.store.HasIngestData(ctx)
	}
	return s.store.PendingRemovals(ctx, s.cfg.RemovalGrace)
}

func (s *Syncer) interval(src source.Source) time.Duration {
	if d, ok := s.cfg.Intervals[src.ID]; ok && d > 0 {
		return d
	}
	return s.cfg.DefaultInterval
}

func (s *Syncer) due(src source.Source, prev store.State, now time.Time) bool {
	if prev.LastCheckedAt == nil {
		return true
	}
	return now.Sub(*prev.LastCheckedAt) >= s.interval(src)
}

// processSource baixa, valida e aplica uma fonte. Devolve o resultado gravado
// em ingest.source_run: unchanged, applied, aborted, failed ou stale.
func (s *Syncer) processSource(ctx context.Context, src source.Source, prev store.State) (string, error) {
	started := store.Now()
	log := s.log.With("source", src.ID)
	run := store.Run{SourceID: src.ID, StartedAt: started}

	record := func(outcome string, cause error, res *fetch.Result) (string, error) {
		run.Outcome = outcome
		run.FinishedAt = store.Now()
		etag, lastModified := "", ""
		if res != nil {
			run.URL, run.HTTPStatus, run.Bytes, run.SHA256 = res.URL, res.Status, int64(len(res.Body)), res.SHA256
			etag, lastModified = res.ETag, res.LastModified
		}
		if cause != nil {
			run.Message = cause.Error()
		}
		if err := s.store.RecordCheck(ctx, run, etag, lastModified); err != nil {
			return outcome, errors.Join(cause, fmt.Errorf("gravar verificação: %w", err))
		}
		return outcome, cause
	}

	res, err := s.fetcher.Fetch(ctx, src, fetch.Previous{
		URL: prev.URL, ETag: prev.ETag, LastModified: prev.LastModified, SHA256: prev.ContentSHA256,
	})
	if err != nil {
		return record("failed", err, nil)
	}
	for _, f := range res.Failures {
		log.Warn("URL falhou, usando a próxima", "detail", f)
	}
	if res.NotModified {
		log.Debug("sem mudança", "url", res.URL, "status", res.Status)
		return record("unchanged", nil, res)
	}

	payload, err := Convert(src, res.Body)
	if err != nil {
		return record("failed", fmt.Errorf("conteúdo inválido: %w", err), res)
	}
	if payload.Stats.Warnings > 0 {
		log.Warn("avisos na leitura", "warnings", payload.Stats.Warnings, "samples", payload.Stats.Samples)
	}
	if !s.cfg.SkipMinRecords && payload.Records < src.MinRecords {
		return record("failed", fmt.Errorf("arquivo com %d registros, mínimo esperado %d (truncado?)",
			payload.Records, src.MinRecords), res)
	}
	run.Records = int64(payload.Records)
	run.FileDate = payload.FileDate
	// Um espelho atrasado não pode desfazer um arquivo mais novo já aplicado.
	if !s.cfg.Force && payload.FileDate != nil && prev.FileDate != nil && payload.FileDate.Before(*prev.FileDate) {
		return record("stale", fmt.Errorf("arquivo de %s é mais antigo que o aplicado (%s)",
			payload.FileDate.Format("2006-01-02"), prev.FileDate.Format("2006-01-02")), res)
	}

	run.URL, run.HTTPStatus, run.Bytes, run.SHA256 = res.URL, res.Status, int64(len(res.Body)), res.SHA256
	run.Outcome = "applied"
	run.FinishedAt = store.Now()
	stats, err := s.store.Apply(ctx, store.ApplyInput{
		SourceID:         src.ID,
		Tables:           payload.Tables,
		RemovalThreshold: s.cfg.RemovalThreshold,
		Force:            s.cfg.Force,
		Run:              run,
		ETag:             res.ETag,
		LastModified:     res.LastModified,
	})
	if errors.Is(err, store.ErrRemovalThreshold) {
		return record("aborted", err, res)
	}
	if err != nil {
		return record("failed", err, res)
	}
	log.Info("fonte aplicada", "url", res.URL, "fallback", res.Fallback, "records", payload.Records,
		"inserted", stats.Inserted, "updated", stats.Updated, "deleted", stats.Deleted)
	if err := s.saveRaw(src.ID, res); err != nil {
		log.Warn("não foi possível guardar o arquivo bruto", "err", err)
	}
	return "applied", nil
}

// saveRaw guarda o arquivo aplicado (gzip) e mantém só os RawKeep mais novos.
func (s *Syncer) saveRaw(id string, res *fetch.Result) error {
	if s.cfg.RawDir == "" || s.cfg.RawKeep <= 0 {
		return nil
	}
	dir := filepath.Join(s.cfg.RawDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%s.gz", time.Now().UTC().Format("20060102T150405Z"), res.SHA256[:12])
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(f)
	if _, err := zw.Write(res.Body); err != nil {
		f.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".gz") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	for len(names) > s.cfg.RawKeep {
		_ = os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
	return nil
}

func (s *Syncer) setRunning(start time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Running = true
	s.status.LastCycleStart = &start
}

func (s *Syncer) setDone(res CycleResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.status.Running = false
	s.status.LastCycleEnd = &now
	s.status.LastApplied = res.Applied
	s.status.LastError = ""
	if err != nil {
		s.status.LastError = err.Error()
	}
	if res.Build != nil {
		s.status.LastBuild = &BuildInfo{
			DatasetID: res.Build.DatasetID, Changes: res.Build.Changes,
			DurationMS: res.Build.Duration.Milliseconds(), At: now,
		}
	}
}
