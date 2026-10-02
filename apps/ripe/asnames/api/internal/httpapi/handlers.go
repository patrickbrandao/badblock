package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/patrickbrandao/badblock/apps/ripe/asnames/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/ripe/asnames/api/internal/store"
	"github.com/patrickbrandao/badblock/apps/ripe/asnames/api/openapi"
)

// SourceURL é a fonte dos dados, citada no índice.
const SourceURL = "https://ftp.ripe.net/ripe/asnames/asn.txt"

// Limites das rotas de texto livre.
const (
	// SearchMinLen e SearchMaxLen limitam o termo de /search, em caracteres,
	// depois da normalização. Menos de 3 não aproveita o índice trigram.
	SearchMinLen = 3
	SearchMaxLen = 100
	// SearchLimit é o máximo de resultados de /search.
	SearchLimit = 100
	// HandleMaxLen limita /handle/{handle}; a maior description do arquivo
	// real tem ~220 caracteres, então nenhum handle passa disso.
	HandleMaxLen = 255
)

func (a *API) handleIndex(w http.ResponseWriter, _ *http.Request) {
	b := a.cfg.BasePath
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, IndexResponse{
		App:      "api-ripe-asnames",
		Version:  a.cfg.Version,
		BasePath: b,
		Versions: []string{CurrentAPIVersion},
		Endpoints: []string{
			b + "/asn/{asn}",
			b + "/country/{cc}",
			b + "/handle/{handle}",
			b + "/search?q={texto}",
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

// GET /asn/{asn} — aceita "15169", "AS15169" ou "as15169".
func (a *API) handleASN(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("asn")
	num, ok := strings.CutPrefix(strings.ToUpper(raw), "AS")
	if !ok {
		num = raw
	}
	asn, err := strconv.ParseUint(num, 10, 32)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "ASN inválido: use um número de 0 a 4294967295, com ou sem o prefixo AS")
		return
	}
	a.serveCached(w, r, "asn:"+strconv.FormatUint(asn, 10), func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		rec, err := a.store.ASN(ctx, int64(asn))
		if errors.Is(err, store.ErrNotFound) {
			return nil, notFound("AS" + strconv.FormatUint(asn, 10) + " não consta no asn.txt do RIPE NCC")
		}
		if err != nil {
			return nil, err
		}
		return ASNResponse{
			ASN:         rec.ASN,
			Handle:      rec.Handle,
			Name:        rec.Name,
			Country:     rec.Country,
			Description: rec.Description,
			FirstSeen:   ts(rec.CreatedAt),
			UpdatedAt:   ts(rec.UpdatedAt),
			Dataset:     datasetInfo(snap),
		}, nil
	})
}

// GET /country/{cc} — duas letras, sem diferenciar maiúsculas.
func (a *API) handleCountry(w http.ResponseWriter, r *http.Request) {
	cc := strings.ToUpper(r.PathValue("cc"))
	if len(cc) != 2 || cc[0] < 'A' || cc[0] > 'Z' || cc[1] < 'A' || cc[1] > 'Z' {
		writeError(w, http.StatusBadRequest, "bad_request", "país inválido: use o código de duas letras (ex.: BR, US, EU)")
		return
	}
	a.serveCached(w, r, "country:"+cc, func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		list, err := a.store.ByCountry(ctx, cc)
		if err != nil {
			return nil, err
		}
		if len(list) == 0 {
			return nil, notFound("nenhum ASN do país " + cc + " no asn.txt do RIPE NCC")
		}
		resp := CountryResponse{Country: cc, Count: len(list), ASNs: make([]ASNBrief, 0, len(list)), Dataset: datasetInfo(snap)}
		for _, x := range list {
			resp.ASNs = append(resp.ASNs, ASNBrief{ASN: x.ASN, Handle: x.Handle, Name: x.Name})
		}
		return resp, nil
	})
}

// GET /handle/{handle} — igualdade sem diferenciar maiúsculas. O handle não é
// único na fonte, então a resposta é uma lista.
func (a *API) handleHandle(w http.ResponseWriter, r *http.Request) {
	handle := strings.TrimSpace(r.PathValue("handle"))
	if handle == "" || !cleanText(handle) || utf8.RuneCountInString(handle) > HandleMaxLen {
		writeError(w, http.StatusBadRequest, "bad_request",
			"handle inválido: use de 1 a "+strconv.Itoa(HandleMaxLen)+" caracteres, sem caracteres de controle")
		return
	}
	key := strings.ToLower(handle)
	a.serveCached(w, r, "handle:"+key, func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		list, err := a.store.ByHandle(ctx, key)
		if err != nil {
			return nil, err
		}
		if len(list) == 0 {
			return nil, notFound("nenhum ASN com o handle " + handle + " no asn.txt do RIPE NCC")
		}
		resp := HandleResponse{Handle: key, Count: len(list), ASNs: make([]ASNEntry, 0, len(list)), Dataset: datasetInfo(snap)}
		for _, x := range list {
			resp.ASNs = append(resp.ASNs, asnEntry(x))
		}
		return resp, nil
	})
}

// GET /search?q=texto — trecho da description, sem diferenciar maiúsculas.
// O termo é normalizado (minúsculas, espaços colapsados) antes de virar chave
// de cache e consulta: "Google  LLC" e "google llc" são a mesma busca.
func (a *API) handleSearch(w http.ResponseWriter, r *http.Request) {
	q, ok := normalizeQuery(r.URL.Query().Get("q"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request",
			"use o parâmetro q com um trecho de "+strconv.Itoa(SearchMinLen)+" a "+strconv.Itoa(SearchMaxLen)+
				" caracteres, sem caracteres de controle (ex.: ?q=google)")
		return
	}
	a.serveCached(w, r, "search:"+q, func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		list, err := a.store.Search(ctx, q, SearchLimit+1)
		if err != nil {
			return nil, err
		}
		resp := SearchResponse{Query: q, Dataset: datasetInfo(snap)}
		if len(list) > SearchLimit {
			list, resp.Truncated = list[:SearchLimit], true
		}
		resp.Count = len(list)
		resp.ASNs = make([]ASNEntry, 0, len(list))
		for _, x := range list {
			resp.ASNs = append(resp.ASNs, asnEntry(x))
		}
		return resp, nil
	})
}

// normalizeQuery tira os espaços das pontas, colapsa os internos e passa para
// minúsculas. Recusa termo curto, longo, UTF-8 inválido ou com controles.
// A validação vem antes do ToLower, que trocaria bytes inválidos por U+FFFD.
func normalizeQuery(raw string) (string, bool) {
	q := strings.Join(strings.Fields(raw), " ")
	if !cleanText(q) {
		return "", false
	}
	q = strings.ToLower(q)
	if n := utf8.RuneCountInString(q); n < SearchMinLen || n > SearchMaxLen {
		return "", false
	}
	return q, true
}

// cleanText recusa UTF-8 inválido e caracteres de controle (o Postgres não
// aceita NUL nem UTF-8 inválido num parâmetro text: seria erro 503 em vez de
// 400).
func cleanText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl)
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
	resp := MetaResponse{App: "api-ripe-asnames", Version: a.cfg.Version}
	if d != nil {
		resp.Dataset = &MetaDataset{
			Version: d.Version, UpdatedAt: ts(d.AppliedAt), Source: d.URL, SHA256: d.SHA256, ASNs: d.ASNs,
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
		Success: true, Status: "ok", Timestamp: ts(time.Now()), Message: "api-ripe-asnames operacional",
		Checks: map[string]string{"postgres": "ok", "valkey": "disabled", "dataset": "ok"},
	}
	code := http.StatusOK

	if !a.data.Current().Ready() {
		resp.Checks["dataset"] = "empty"
		resp.Status, resp.Message = "starting", "aguardando a primeira sincronização do collector-ripe-asnames"
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
