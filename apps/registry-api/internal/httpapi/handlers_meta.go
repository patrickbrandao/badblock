package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/patrickbrandao/badblock/apps/registry-api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/registry-api/openapi"
)

const title = "BadBlock Registry API"

// --- histórico ---------------------------------------------------------------

func historyLimit(q url.Values) (int, error) {
	limit := defaultLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxLimit {
			return 0, badRequest("invalid_param", "limit deve ficar entre 1 e 1000")
		}
		limit = n
	}
	return limit, nil
}

func (a *API) serveHistory(w http.ResponseWriter, r *http.Request, entity, key string) {
	limit, err := historyLimit(r.URL.Query())
	if err != nil {
		writeHTTPError(w, err)
		return
	}
	snap, ok := a.ready(w)
	if !ok {
		return
	}
	ckey := cache.Prefix(snap.Version) + "history:" + entity + ":" + key + ":" + strconv.Itoa(limit)
	a.serveCached(w, r, snap, ckey, func(ctx context.Context) (payload, error) {
		ctx, cancel := a.dbCtx(ctx)
		defer cancel()
		changes, err := a.store.History(ctx, entity, key, limit)
		if err != nil {
			return payload{}, err
		}
		res := HistoryResponse{Entity: entity, Key: key, Events: []HistoryEvent{}, Dataset: datasetInfo(snap)}
		for _, c := range changes {
			res.Events = append(res.Events, HistoryEvent{
				At: tsStr(c.ChangedAt), DatasetVersion: c.DatasetID, Action: c.Action, Level: c.Level,
				Before: nullJSON(c.Before), After: nullJSON(c.After),
			})
		}
		return jsonPayload(res)
	})
}

func nullJSON(b json.RawMessage) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return b
}

func (a *API) handleASNHistory(w http.ResponseWriter, r *http.Request) {
	asn, ok := parseASN(r.PathValue("asn"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_asn", "ASN inválido: "+r.PathValue("asn"))
		return
	}
	a.serveHistory(w, r, "asn", strconv.FormatInt(asn, 10))
}

func (a *API) handlePrefixHistory(w http.ResponseWriter, r *http.Request) {
	p, err := parsePrefixPath(r.PathValue("ip"), r.PathValue("len"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_prefix", err.Error())
		return
	}
	a.serveHistory(w, r, "prefix", p.String())
}

func (a *API) handleHolderHistory(w http.ResponseWriter, r *http.Request) {
	rir, ok := normalizeRIR(r.PathValue("rir"))
	id := r.PathValue("id")
	if !ok || !opaqueIDRe.MatchString(id) {
		writeError(w, http.StatusBadRequest, "invalid_holder", "titular inválido")
		return
	}
	a.serveHistory(w, r, "holder", rir+":"+id)
}

// --- fontes ------------------------------------------------------------------

func (a *API) handleSources(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := a.dbCtx(r.Context())
	defer cancel()
	list, err := a.store.Sources(ctx)
	if err != nil {
		a.writeStoreError(w, r, err)
		return
	}
	res := SourcesResponse{Sources: []SourceInfo{}, Dataset: datasetInfo(a.data.Current())}
	for _, s := range list {
		res.Sources = append(res.Sources, SourceInfo{
			ID: s.SourceID, URL: s.URL, FileDate: dateStr(s.FileDate), Records: s.Records,
			LastCheckedAt: tsPtr(s.LastCheckedAt), LastChangedAt: tsPtr(s.LastChangedAt),
			LastSuccessAt: tsPtr(s.LastSuccessAt), LastError: s.LastError, LastErrorAt: tsPtr(s.LastErrorAt),
		})
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, http.StatusOK, res)
}

// --- saúde -------------------------------------------------------------------

// handleStatus segue a convenção dos serviços do BadBlock: success, status,
// timestamp e message. online com tudo no ar; degraded quando só o cache caiu
// (a API segue pelo Postgres); offline, com HTTP 503, sem o Postgres.
func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	checks := map[string]string{"postgres": "online", "cache": "disabled"}
	status, success, code := "online", 1, http.StatusOK
	if err := a.store.Ping(ctx); err != nil {
		checks["postgres"] = "offline"
		status, success, code = "offline", 0, http.StatusServiceUnavailable
	}
	if a.cache.Enabled() {
		checks["cache"] = "online"
		if err := a.cache.Ping(ctx); err != nil {
			checks["cache"] = "offline"
			if status == "online" {
				status = "degraded"
			}
		}
	}
	snap := a.data.Current()
	body := map[string]any{
		"success":         success,
		"status":          status,
		"timestamp":       time.Now().Unix(),
		"message":         title,
		"version":         a.cfg.Version,
		"dataset_version": snap.Version,
		"checks":          checks,
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, code, body)
}

// --- índice e documentação -----------------------------------------------------

func (a *API) handleIndex(w http.ResponseWriter, r *http.Request) {
	snap := a.data.Current()
	writeJSON(w, http.StatusOK, map[string]any{
		"name":    title,
		"version": a.cfg.Version,
		"docs":    "/docs",
		"openapi": "/openapi.yaml",
		"dataset": datasetInfo(snap),
		"endpoints": []string{
			"GET /v1/ip", "GET /v1/ip/{ip}", "GET /v1/asn/{asn}", "GET /v1/asn/{asn}/history",
			"GET /v1/prefix/{ip}/{len}", "GET /v1/prefix/{ip}/{len}/history",
			"GET /v1/holder/{rir}/{id}", "GET /v1/holder/{rir}/{id}/history",
			"GET /v1/country/{cc}/prefixes", "GET /v1/country/{cc}/asns",
			"GET /v1/rir/{rir}/prefixes", "GET /v1/rir/{rir}/asns",
			"GET /v1/meta/sources", "GET /asn/{asn} (legado)", "GET /status", "GET /ping",
		},
	})
}

func (a *API) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(openapi.Spec)
}

// docsPage usa o Scalar (servido pelo jsDelivr) para renderizar /openapi.yaml.
const docsPage = `<!doctype html>
<html lang="pt-BR">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>BadBlock Registry API</title>
</head>
<body>
<script id="api-reference" data-url="/openapi.yaml"></script>
<script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
</body>
</html>
`

func (a *API) handleDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; script-src 'self' https://cdn.jsdelivr.net 'unsafe-inline'; "+
			"style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://fonts.googleapis.com; "+
			"font-src 'self' data: https://cdn.jsdelivr.net https://fonts.gstatic.com; "+
			"img-src 'self' data: https:; connect-src 'self'")
	_, _ = w.Write([]byte(docsPage))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
