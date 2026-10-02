package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/patrickbrandao/badblock/apps/roothints/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/roothints/api/internal/store"
	"github.com/patrickbrandao/badblock/apps/roothints/api/openapi"
)

// SourceURL é a fonte dos dados, citada no índice.
const SourceURL = "https://www.internic.net/domain/named.root"

// serverDomain é o domínio de todo servidor raiz (a.root-servers.net).
const serverDomain = ".root-servers.net"

// dataRoute é uma rota de dados: registrada com e sem /v1, listada no índice
// e servida por serveCached. Rota nova = um item aqui, o handler, a spec
// (specs/fontes/roothints/api.md) e o manifesto (openapi/openapi.yaml).
type dataRoute struct {
	path    string // relativo ao caminho de base, no formato do http.ServeMux
	handler http.HandlerFunc
}

func (a *API) dataRoutes() []dataRoute {
	return []dataRoute{
		{"/servers", a.handleServers},
		{"/server/{server}", a.handleServer},
	}
}

func (a *API) handleIndex(w http.ResponseWriter, _ *http.Request) {
	b := a.cfg.BasePath
	var endpoints []string
	for _, rt := range a.dataRoutes() {
		endpoints = append(endpoints, b+rt.path)
	}
	endpoints = append(endpoints, b+"/meta", b+"/status", b+"/openapi.yaml")
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, IndexResponse{
		App:       "api-roothints",
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

// GET /servers — todos os servidores raiz, em ordem de letra.
func (a *API) handleServers(w http.ResponseWriter, r *http.Request) {
	a.serveCached(w, r, "servers", func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		list, err := a.store.Servers(ctx)
		if err != nil {
			return nil, err
		}
		resp := ServersResponse{Source: sourceInfo(snap), Count: len(list), Servers: make([]Server, 0, len(list)),
			Dataset: datasetInfo(snap)}
		for _, s := range list {
			resp.Servers = append(resp.Servers, serverOf(s))
		}
		return resp, nil
	})
}

// GET /server/{server} — um servidor pela letra ("a", "A") ou pelo nome
// ("a.root-servers.net", "A.ROOT-SERVERS.NET."). Todas as formas caem na
// mesma chave, server:<letra>.
func (a *API) handleServer(w http.ResponseWriter, r *http.Request) {
	letter, ok := normalizeServer(r.PathValue("server"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request",
			"servidor inválido: use a letra (ex.: a) ou o nome (ex.: a.root-servers.net), sem diferenciar maiúsculas")
		return
	}
	a.serveCached(w, r, "server:"+letter, func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		s, err := a.store.ServerByLetter(ctx, letter)
		if errors.Is(err, store.ErrNotFound) {
			return nil, notFound("o servidor raiz " + letter + serverDomain + " não consta no named.root da InterNIC")
		}
		if err != nil {
			return nil, err
		}
		return ServerResponse{
			Server:    serverOf(*s),
			FirstSeen: ts(s.CreatedAt),
			UpdatedAt: ts(s.UpdatedAt),
			Source:    sourceInfo(snap),
			Dataset:   datasetInfo(snap),
		}, nil
	})
}

// normalizeServer aceita uma letra de a a z ou o nome <letra>.root-servers.net,
// com ou sem o ponto final, sem diferenciar maiúsculas, e devolve a letra em
// minúsculas. Só ASCII: strings.ToLower (e o (?i) do regexp) trocariam o
// sinal de kelvin (U+212A) por "k".
func normalizeServer(raw string) (string, bool) {
	for i := range len(raw) {
		if raw[i] >= 0x80 {
			return "", false
		}
	}
	s := strings.TrimSuffix(strings.ToLower(raw), ".")
	if len(s) == 1+len(serverDomain) && strings.HasSuffix(s, serverDomain) {
		s = s[:1]
	} else if len(s) != len(raw) {
		return "", false // o ponto final só vale no nome, não depois da letra
	}
	if len(s) != 1 || s[0] < 'a' || s[0] > 'z' {
		return "", false
	}
	return s, true
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
	resp := MetaResponse{App: "api-roothints", Version: a.cfg.Version}
	if d != nil {
		resp.Dataset = &MetaDataset{
			Version: d.Version, UpdatedAt: ts(d.AppliedAt), Source: d.URL, MD5: d.MD5, SHA256: d.SHA256,
			LastUpdate: datePtr(d.LastUpdate), ZoneSerial: d.ZoneSerial, Servers: d.Servers,
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
		Success: true, Status: "ok", Timestamp: ts(time.Now()), Message: "api-roothints operacional",
		Checks: map[string]string{"postgres": "ok", "valkey": "disabled", "dataset": "ok"},
	}
	code := http.StatusOK

	if !a.data.Current().Ready() {
		resp.Checks["dataset"] = "empty"
		resp.Status, resp.Message = "starting", "aguardando a primeira sincronização do collector-roothints"
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
