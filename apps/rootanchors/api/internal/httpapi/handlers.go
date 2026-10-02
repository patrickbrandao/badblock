package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/patrickbrandao/badblock/apps/rootanchors/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/rootanchors/api/internal/store"
	"github.com/patrickbrandao/badblock/apps/rootanchors/api/openapi"
)

// SourceURL é a fonte dos dados, citada no índice.
const SourceURL = "https://data.iana.org/root-anchors/root-anchors.xml"

func (a *API) handleIndex(w http.ResponseWriter, _ *http.Request) {
	b := a.cfg.BasePath
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, IndexResponse{
		App:      "api-rootanchors",
		Version:  a.cfg.Version,
		BasePath: b,
		Versions: []string{CurrentAPIVersion},
		Endpoints: []string{
			b + "/keys",
			b + "/key/{key_tag}",
			b + "/meta",
			b + "/status",
			b + "/openapi.yaml",
		},
		Source: SourceURL,
	})
}

// GET /openapi.yaml — o manifesto OpenAPI embutido no binário (fora do
// versionamento, sem Valkey).
func (a *API) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(openapi.Spec)
}

// trustAnchorFor lê o TrustAnchor do último arquivo aplicado. Sem execução
// aplicada (só numa corrida com o watcher), a resposta é a mesma de antes da
// primeira carga.
func (a *API) trustAnchorFor(ctx context.Context) (TrustAnchor, error) {
	d, err := a.store.Dataset(ctx)
	if err != nil {
		return TrustAnchor{}, err
	}
	if d == nil {
		return TrustAnchor{}, errNotReady
	}
	return trustAnchor(d), nil
}

// GET /keys — todas as chaves do arquivo aplicado, inclusive as aposentadas,
// da mais antiga para a mais nova.
func (a *API) handleKeys(w http.ResponseWriter, r *http.Request) {
	a.serveCached(w, r, "keys", func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		ta, err := a.trustAnchorFor(ctx)
		if err != nil {
			return nil, err
		}
		list, err := a.store.Keys(ctx)
		if err != nil {
			return nil, err
		}
		return KeysResponse{TrustAnchor: ta, Count: len(list), Keys: keysOf(ta.Zone, list), Dataset: datasetInfo(snap)}, nil
	})
}

// parseKeyTag aceita só dígitos decimais, de 0 a 65535 (zeros à esquerda são
// tirados: 020326 = 20326).
func parseKeyTag(raw string) (int, bool) {
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(raw, 10, 16)
	if err != nil {
		return 0, false
	}
	return int(n), true
}

// GET /key/{key_tag} — as chaves de um key tag. O key tag não é único (duas
// chaves podem ter o mesmo), então a resposta é sempre uma lista; nenhuma
// chave é 404.
func (a *API) handleKeyTag(w http.ResponseWriter, r *http.Request) {
	tag, ok := parseKeyTag(r.PathValue("key_tag"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "key tag inválido: use um número de 0 a 65535 (ex.: 20326)")
		return
	}
	a.serveCached(w, r, "key:"+strconv.Itoa(tag), func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		list, err := a.store.KeysByTag(ctx, tag)
		if err != nil {
			return nil, err
		}
		if len(list) == 0 {
			return nil, notFound("nenhuma chave com o key tag " + strconv.Itoa(tag) + " no root-anchors.xml da IANA")
		}
		ta, err := a.trustAnchorFor(ctx)
		if err != nil {
			return nil, err
		}
		return KeyTagResponse{KeyTag: tag, TrustAnchor: ta, Count: len(list), Keys: keysOf(ta.Zone, list), Dataset: datasetInfo(snap)}, nil
	})
}

// GET /meta — arquivo aplicado e estado do collector (sem cache).
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
	resp := MetaResponse{App: "api-rootanchors", Version: a.cfg.Version}
	if d != nil {
		resp.Dataset = &MetaDataset{
			Version: d.Version, UpdatedAt: ts(d.AppliedAt), Source: d.URL, SHA256: d.SHA256, Keys: d.Keys,
			TrustAnchor: trustAnchor(d),
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
		Success: true, Status: "ok", Timestamp: ts(time.Now()), Message: "api-rootanchors operacional",
		Checks: map[string]string{"postgres": "ok", "valkey": "disabled", "dataset": "ok"},
	}
	code := http.StatusOK

	if !a.data.Current().Ready() {
		resp.Checks["dataset"] = "empty"
		resp.Status, resp.Message = "starting", "aguardando a primeira sincronização do collector-rootanchors"
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
