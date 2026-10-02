package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/patrickbrandao/badblock/apps/anatel/pst/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/api/internal/store"
)

// Limites de /search.
const (
	// SearchMinLen e SearchMaxLen limitam o termo, em caracteres, depois da
	// normalização. Menos de 3 não aproveita os índices trigram.
	SearchMinLen = 3
	SearchMaxLen = 100
	// SearchLimit é o limite padrão e o máximo de resultados (?limit=).
	SearchLimit = 100
)

// GET /provider/{cnpj} — a prestadora de um CNPJ e todos os seus serviços.
// Aceita os 14 dígitos com ou sem pontuação (a barra do CNPJ formatado
// precisa vir como %2F): todo caractere que não é dígito ASCII é descartado.
func (a *API) handleProvider(w http.ResponseWriter, r *http.Request) {
	cnpj := onlyDigits(r.PathValue("cnpj"))
	if len(cnpj) != 14 {
		writeError(w, http.StatusBadRequest, "bad_request",
			"CNPJ inválido: use os 14 dígitos, com ou sem pontuação (ex.: 02558157000162 ou 02.558.157%2F0001-62)")
		return
	}
	a.serveCached(w, r, "provider:"+cnpj, func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		d, err := a.store.Provider(ctx, cnpj)
		if errors.Is(err, store.ErrNotFound) {
			return nil, notFound("CNPJ " + cnpj + " não consta no cadastro de prestadoras da Anatel")
		}
		if err != nil {
			return nil, err
		}
		return providerResponse(d, snap), nil
	})
}

// providerResponse monta a resposta de /provider: os serviços na ordem do
// store e os códigos distintos, em ordem.
func providerResponse(d *store.ProviderDetail, snap dataset.Snapshot) ProviderResponse {
	p := d.Provider
	resp := ProviderResponse{
		Document: p.Document, Name: p.Name, TradeName: p.TradeName,
		Address: Address{
			Street: p.Street, Number: p.Number, Complement: p.Complement, District: p.District,
			PostalCode: p.PostalCode, CityIBGECode: p.CityIBGECode, City: p.City, State: p.State,
		},
		Phone: p.Phone, Email: p.Email,
		FirstSeen: ts(p.CreatedAt), UpdatedAt: ts(p.UpdatedAt),
		ServiceCodes: []string{}, Services: make([]Service, 0, len(d.Services)),
		Dataset: datasetInfo(snap),
	}
	for _, s := range d.Services {
		resp.Services = append(resp.Services, Service{
			ServiceCode: s.ServiceCode, ServiceName: s.ServiceName, ServiceGroup: s.ServiceGroup,
			NotificationFistel: s.NotificationFistel, NotificationProcess: s.NotificationProcess,
			NotifiedOn: datePtr(s.NotifiedOn), EntityType: s.EntityType, GrantType: s.GrantType,
			GrantFistel: s.GrantFistel, GrantProcess: s.GrantProcess, GrantedOn: datePtr(s.GrantedOn),
		})
		if !slices.Contains(resp.ServiceCodes, s.ServiceCode) {
			resp.ServiceCodes = append(resp.ServiceCodes, s.ServiceCode)
		}
	}
	slices.Sort(resp.ServiceCodes)
	return resp
}

// GET /search?q={texto}[&limit=N] — prestadoras cuja razão social ou nome
// fantasia contém o trecho, em ordem de razão social e CNPJ.
func (a *API) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	q, ok := normalizeQuery(query.Get("q"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request",
			"use o parâmetro q com um trecho de "+strconv.Itoa(SearchMinLen)+" a "+strconv.Itoa(SearchMaxLen)+
				" caracteres, sem caracteres de controle (ex.: ?q=telefonica)")
		return
	}
	limit, ok := parseLimit(query.Get("limit"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request",
			"limit inválido: use um número de 1 a "+strconv.Itoa(SearchLimit)+" (padrão "+strconv.Itoa(SearchLimit)+")")
		return
	}
	a.serveCached(w, r, "search:"+strconv.Itoa(limit)+":"+q, func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		list, err := a.store.Search(ctx, q, limit+1)
		if err != nil {
			return nil, err
		}
		resp := SearchResponse{Query: q, Limit: limit, Dataset: datasetInfo(snap)}
		if len(list) > limit {
			list, resp.Truncated = list[:limit], true
		}
		resp.Count = len(list)
		resp.Providers = providerBriefs(list)
		return resp, nil
	})
}

// normalizeQuery tira os espaços das pontas, colapsa os internos e passa para
// minúsculas; recusa UTF-8 inválido, caracteres de controle e termos fora de
// SearchMinLen..SearchMaxLen caracteres (a regra da api-ripe-asnames).
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

// parseLimit lê ?limit=: ausente ou vazio vale SearchLimit; senão, só dígitos
// ASCII (sem sinal nem espaço), de 1 a SearchLimit. Zeros à esquerda valem
// (007 = 7).
func parseLimit(raw string) (int, bool) {
	if raw == "" {
		return SearchLimit, true
	}
	if len(raw) > 6 || strings.ContainsFunc(raw, func(c rune) bool { return c < '0' || c > '9' }) {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > SearchLimit {
		return 0, false
	}
	return n, true
}

// cleanText recusa UTF-8 inválido e caracteres de controle (o Postgres não
// aceita NUL nem UTF-8 inválido num parâmetro text: seria erro 503 em vez de
// 400).
func cleanText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl)
}

// onlyDigits guarda só os dígitos ASCII (como /cgibr/document/{doc}).
func onlyDigits(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return b.String()
}
