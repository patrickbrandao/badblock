package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/store"
	"github.com/patrickbrandao/badblock/apps/rootzone/api/openapi"
)

// SourceURL é a fonte dos dados, citada no índice.
const SourceURL = "https://www.internic.net/domain/root.zone"

// dataRoute é uma rota de dados: registrada com e sem /v1 e listada no
// índice.
type dataRoute struct {
	pattern string // padrão do ServeMux, relativo ao caminho de base
	index   string // como aparece em endpoints do índice
	handler http.HandlerFunc
}

// dataRoutes são as rotas de dados, na ordem do índice.
func (a *API) dataRoutes() []dataRoute {
	return []dataRoute{
		{"/tlds", "/tlds", a.handleTLDs},
		{"/tld/{tld}", "/tld/{tld}", a.handleTLD},
	}
}

func (a *API) handleIndex(w http.ResponseWriter, _ *http.Request) {
	b := a.cfg.BasePath
	var endpoints []string
	for _, rt := range a.dataRoutes() {
		endpoints = append(endpoints, b+rt.index)
	}
	endpoints = append(endpoints, b+"/meta", b+"/status", b+"/openapi.yaml")
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, IndexResponse{
		App:       "api-rootzone",
		Version:   a.cfg.Version,
		BasePath:  b,
		Versions:  []string{CurrentAPIVersion},
		Endpoints: endpoints,
		Source:    SourceURL,
	})
}

// GET /openapi.yaml — o manifesto OpenAPI embutido no binário (fora do
// versionamento, sem Valkey).
func (a *API) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(openapi.Spec)
}

// GET /meta — zona aplicada (com o SOA) e estado do collector (sem cache).
func (a *API) handleMeta(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.DBTimeout)
	defer cancel()
	d, err := a.store.Dataset(ctx)
	if err != nil {
		a.writeFailure(w, r, err)
		return
	}
	j, err := a.store.Job(ctx)
	if err != nil {
		a.writeFailure(w, r, err)
		return
	}
	resp := MetaResponse{App: "api-rootzone", Version: a.cfg.Version}
	if d != nil {
		resp.Dataset = &MetaDataset{
			Version: d.Version, UpdatedAt: ts(d.AppliedAt), Source: d.URL, SHA256: d.SHA256,
			Serial: d.Serial, TLDs: d.TLDs, Records: d.Records, RRSIGs: d.RRSIGs,
		}
		if d.SOAMName != nil {
			resp.Dataset.SOA = &MetaSOA{
				MName: *d.SOAMName, RName: d.SOARName,
				Refresh: d.SOARefresh, Retry: d.SOARetry, Expire: d.SOAExpire, Minimum: d.SOAMinimum,
			}
		}
	}
	if j != nil {
		resp.Collector = &MetaCollector{
			App: store.CollectorApp, LastSyncAt: tsPtr(j.LastSyncAt), LastCheckAt: tsPtr(j.LastCheckAt),
			Consolidated: j.Consolidated == 1,
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

// /health e /status: Postgres fora = error (503); só o Valkey fora =
// degraded (200, a API segue sem cache); sem dados ainda = starting (200).
func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	resp := StatusResponse{
		Success: true, Status: "ok", Timestamp: ts(time.Now()), Message: "api-rootzone operacional",
		Checks: map[string]string{"postgres": "ok", "valkey": "disabled", "dataset": "ok"},
	}
	code := http.StatusOK

	if !a.data.Current().Ready() {
		resp.Checks["dataset"] = "empty"
		resp.Status, resp.Message = "starting", "aguardando a primeira sincronização do collector-rootzone"
	}
	if a.cache.Enabled() {
		if err := a.cache.Ping(ctx); err != nil {
			resp.Checks["valkey"] = "error"
			resp.Status, resp.Message = "degraded", "Valkey indisponível; respondendo sem cache"
		} else {
			resp.Checks["valkey"] = "ok"
		}
	}
	if err := a.store.Ping(ctx); err != nil {
		resp.Checks["postgres"] = "error"
		resp.Success, resp.Status, resp.Message = false, "error", "PostgreSQL indisponível"
		code = http.StatusServiceUnavailable
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, code, resp)
}
