package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/patrickbrandao/badblock/apps/anatel/pst/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/api/internal/store"
)

// GET /services — o catálogo: cada código de serviço com o nome, o grupo e
// as contagens de prestadoras e de serviços notificados.
func (a *API) handleServices(w http.ResponseWriter, r *http.Request) {
	a.serveCached(w, r, "services", func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		list, err := a.store.Services(ctx)
		if err != nil {
			return nil, err
		}
		resp := ServicesResponse{Count: len(list), Services: make([]ServiceSummary, 0, len(list)), Dataset: datasetInfo(snap)}
		for _, s := range list {
			resp.Services = append(resp.Services, ServiceSummary(s))
		}
		return resp, nil
	})
}

// GET /service/{code}[?state=UF] — as prestadoras que têm o serviço, em ordem
// de CNPJ. O código tem de 1 a 3 dígitos e ganha zeros à esquerda (45 → 045).
func (a *API) handleService(w http.ResponseWriter, r *http.Request) {
	code, ok := normalizeServiceCode(r.PathValue("code"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request",
			"código de serviço inválido: use de 1 a 3 dígitos (ex.: 045 ou 45 para o SCM)")
		return
	}
	state, ok := normalizeState(r.URL.Query().Get("state"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "UF inválida: use a sigla de duas letras (ex.: ?state=SP)")
		return
	}
	key := "service:" + code
	if state != "" {
		key += ":" + state
	}
	a.serveCached(w, r, key, func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		info, list, err := a.store.ServiceProviders(ctx, code, state)
		if errors.Is(err, store.ErrNotFound) {
			return nil, notFound("serviço " + code + " não consta no cadastro de prestadoras da Anatel")
		}
		if err != nil {
			return nil, err
		}
		resp := ServiceResponse{
			ServiceCode: info.ServiceCode, ServiceName: info.ServiceName, ServiceGroup: info.ServiceGroup,
			Count: len(list), Providers: providerBriefs(list), Dataset: datasetInfo(snap),
		}
		if state != "" {
			resp.State = &state
		}
		return resp, nil
	})
}

// normalizeServiceCode aceita de 1 a 3 dígitos ASCII e completa com zeros à
// esquerda.
func normalizeServiceCode(raw string) (string, bool) {
	if raw == "" || len(raw) > 3 || strings.ContainsFunc(raw, func(c rune) bool { return c < '0' || c > '9' }) {
		return "", false
	}
	return strings.Repeat("0", 3-len(raw)) + raw, true
}

// normalizeState lê ?state=: ausente ou vazio = sem filtro; senão, duas
// letras ASCII em qualquer caixa, devolvidas em maiúsculas.
func normalizeState(raw string) (string, bool) {
	if raw == "" {
		return "", true
	}
	if len(raw) != 2 || strings.ContainsFunc(raw, func(c rune) bool {
		return (c < 'a' || c > 'z') && (c < 'A' || c > 'Z')
	}) {
		return "", false
	}
	return strings.ToUpper(raw), true
}
