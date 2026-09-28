package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/patrickbrandao/badblock/apps/registry-api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/store"
)

const (
	defaultLimit = 100
	maxLimit     = 1000
)

var countryRe = regexp.MustCompile(`^[A-Za-z]{2}$`)

// listParams são os parâmetros comuns das listas.
type listParams struct {
	filter    store.ListFilter
	limit     int
	cursor    string
	text      bool
	aggregate bool
}

var validStatus = map[string]bool{"allocated": true, "assigned": true, "available": true, "reserved": true}

// parseListParams lê status, family, limit, cursor, format e aggregate.
func parseListParams(q url.Values) (listParams, error) {
	p := listParams{limit: defaultLimit}

	switch st := strings.ToLower(strings.TrimSpace(q.Get("status"))); st {
	case "":
		p.filter.Statuses = []string{"allocated", "assigned"}
	case "all":
	default:
		for _, s := range strings.Split(st, ",") {
			s = strings.TrimSpace(s)
			if !validStatus[s] {
				return p, badRequest("invalid_param", "status inválido: "+s+" (use allocated, assigned, available, reserved ou all)")
			}
			p.filter.Statuses = append(p.filter.Statuses, s)
		}
	}
	switch q.Get("family") {
	case "":
	case "4", "ipv4":
		p.filter.Family = 4
	case "6", "ipv6":
		p.filter.Family = 6
	default:
		return p, badRequest("invalid_param", "family deve ser 4 ou 6")
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxLimit {
			return p, badRequest("invalid_param", "limit deve ficar entre 1 e 1000")
		}
		p.limit = n
	}
	p.cursor = q.Get("cursor")
	switch strings.ToLower(q.Get("format")) {
	case "", "json":
	case "txt", "text":
		p.text = true
	default:
		return p, badRequest("invalid_param", "format deve ser json ou txt")
	}
	switch strings.ToLower(q.Get("aggregate")) {
	case "", "0", "false", "no":
	case "1", "true", "yes":
		p.aggregate = true
	default:
		return p, badRequest("invalid_param", "aggregate deve ser true ou false")
	}
	return p, nil
}

func encodeCursor(v string) *string {
	s := base64.RawURLEncoding.EncodeToString([]byte(v))
	return &s
}

func decodeCursor(v string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil {
		return "", badRequest("invalid_param", "cursor inválido")
	}
	return string(b), nil
}

func (a *API) handleCountryPrefixes(w http.ResponseWriter, r *http.Request) {
	cc := r.PathValue("cc")
	if !countryRe.MatchString(cc) {
		writeError(w, http.StatusBadRequest, "invalid_country", "código de país inválido (ISO 3166, 2 letras): "+cc)
		return
	}
	a.servePrefixList(w, r, store.ListFilter{Country: strings.ToUpper(cc)}, map[string]any{"country": strings.ToUpper(cc)})
}

func (a *API) handleRIRPrefixes(w http.ResponseWriter, r *http.Request) {
	rir, ok := normalizeRIR(r.PathValue("rir"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_rir", "RIR inválido (afrinic, apnic, arin, lacnic, ripencc)")
		return
	}
	f := store.ListFilter{RIR: rir}
	filter := map[string]any{"rir": *rirName(&rir)}
	if cc := r.URL.Query().Get("country"); cc != "" {
		if !countryRe.MatchString(cc) {
			writeError(w, http.StatusBadRequest, "invalid_country", "código de país inválido: "+cc)
			return
		}
		f.Country = strings.ToUpper(cc)
		filter["country"] = f.Country
	}
	a.servePrefixList(w, r, f, filter)
}

func (a *API) servePrefixList(w http.ResponseWriter, r *http.Request, base store.ListFilter, filter map[string]any) {
	p, err := parseListParams(r.URL.Query())
	if err != nil {
		writeHTTPError(w, err)
		return
	}
	p.filter.Country, p.filter.RIR = base.Country, base.RIR
	snap, ok := a.ready(w)
	if !ok {
		return
	}
	key := cache.RequestKey(snap.Version, r.URL.Path, r.URL.Query())
	a.serveCached(w, r, snap, key, func(ctx context.Context) (payload, error) {
		ctx, cancel := context.WithTimeout(ctx, a.cfg.ListTimeout)
		defer cancel()
		if p.text {
			return a.prefixText(ctx, p)
		}
		return a.prefixPage(ctx, snap, p, filter)
	})
}

func (a *API) prefixText(ctx context.Context, p listParams) (payload, error) {
	var list []netip.Prefix
	err := a.store.ListPrefixes(ctx, p.filter, nil, 0, func(lp store.ListPrefix) error {
		list = append(list, lp.Prefix)
		return nil
	})
	if err != nil {
		return payload{}, err
	}
	if p.aggregate {
		list = Aggregate(list)
	}
	var buf bytes.Buffer
	for _, pfx := range list {
		buf.WriteString(pfx.String())
		buf.WriteByte('\n')
	}
	return payload{contentType: "text/plain; charset=utf-8", body: buf.Bytes()}, nil
}

func (a *API) prefixPage(ctx context.Context, snap dataset.Snapshot, p listParams, filter map[string]any) (payload, error) {
	var after *netip.Prefix
	if p.cursor != "" {
		raw, err := decodeCursor(p.cursor)
		if err != nil {
			return payload{}, err
		}
		pfx, err := netip.ParsePrefix(raw)
		if err != nil {
			return payload{}, badRequest("invalid_param", "cursor inválido")
		}
		after = &pfx
	}
	res := PrefixListResponse{Filter: filter, Items: []ListPrefixItem{}, Dataset: datasetInfo(snap)}
	res.Filter["status"] = statusFilter(p.filter.Statuses)
	if p.filter.Family != 0 {
		res.Filter["family"] = p.filter.Family
	}
	err := a.store.ListPrefixes(ctx, p.filter, after, p.limit+1, func(lp store.ListPrefix) error {
		res.Items = append(res.Items, ListPrefixItem{
			CIDR: lp.Prefix.String(), RIR: rirName(lp.RIR), Country: lp.Country, Status: lp.Status,
			Registered: dateStr(lp.Registered), Holder: lp.HolderKey,
		})
		return nil
	})
	if err != nil {
		return payload{}, err
	}
	if len(res.Items) > p.limit {
		res.Items = res.Items[:p.limit]
		res.NextCursor = encodeCursor(res.Items[p.limit-1].CIDR)
	}
	return jsonPayload(res)
}

func statusFilter(s []string) any {
	if len(s) == 0 {
		return "all"
	}
	return s
}

func (a *API) handleCountryASNs(w http.ResponseWriter, r *http.Request) {
	cc := r.PathValue("cc")
	if !countryRe.MatchString(cc) {
		writeError(w, http.StatusBadRequest, "invalid_country", "código de país inválido (ISO 3166, 2 letras): "+cc)
		return
	}
	a.serveASNList(w, r, store.ListFilter{Country: strings.ToUpper(cc)}, map[string]any{"country": strings.ToUpper(cc)})
}

func (a *API) handleRIRASNs(w http.ResponseWriter, r *http.Request) {
	rir, ok := normalizeRIR(r.PathValue("rir"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_rir", "RIR inválido (afrinic, apnic, arin, lacnic, ripencc)")
		return
	}
	f := store.ListFilter{RIR: rir}
	filter := map[string]any{"rir": *rirName(&rir)}
	if cc := r.URL.Query().Get("country"); cc != "" {
		if !countryRe.MatchString(cc) {
			writeError(w, http.StatusBadRequest, "invalid_country", "código de país inválido: "+cc)
			return
		}
		f.Country = strings.ToUpper(cc)
		filter["country"] = f.Country
	}
	a.serveASNList(w, r, f, filter)
}

func (a *API) serveASNList(w http.ResponseWriter, r *http.Request, base store.ListFilter, filter map[string]any) {
	p, err := parseListParams(r.URL.Query())
	if err != nil {
		writeHTTPError(w, err)
		return
	}
	if p.filter.Family != 0 || p.aggregate {
		writeError(w, http.StatusBadRequest, "invalid_param", "family e aggregate só valem para listas de prefixos")
		return
	}
	p.filter.Country, p.filter.RIR = base.Country, base.RIR
	snap, ok := a.ready(w)
	if !ok {
		return
	}
	key := cache.RequestKey(snap.Version, r.URL.Path, r.URL.Query())
	a.serveCached(w, r, snap, key, func(ctx context.Context) (payload, error) {
		ctx, cancel := context.WithTimeout(ctx, a.cfg.ListTimeout)
		defer cancel()
		if p.text {
			var buf bytes.Buffer
			err := a.store.ListASNs(ctx, p.filter, -1, 0, func(b store.ASNBrief) error {
				buf.WriteString(strconv.FormatInt(b.ASN, 10))
				buf.WriteByte('\n')
				return nil
			})
			if err != nil {
				return payload{}, err
			}
			return payload{contentType: "text/plain; charset=utf-8", body: buf.Bytes()}, nil
		}
		after := int64(-1)
		if p.cursor != "" {
			raw, err := decodeCursor(p.cursor)
			if err != nil {
				return payload{}, err
			}
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return payload{}, badRequest("invalid_param", "cursor inválido")
			}
			after = n
		}
		res := ASNListResponse{Filter: filter, Items: []ListASNItem{}, Dataset: datasetInfo(snap)}
		res.Filter["status"] = statusFilter(p.filter.Statuses)
		err := a.store.ListASNs(ctx, p.filter, after, p.limit+1, func(b store.ASNBrief) error {
			res.Items = append(res.Items, listASNItem(b))
			return nil
		})
		if err != nil {
			return payload{}, err
		}
		if len(res.Items) > p.limit {
			res.Items = res.Items[:p.limit]
			res.NextCursor = encodeCursor(strconv.FormatInt(res.Items[p.limit-1].ASN, 10))
		}
		return jsonPayload(res)
	})
}

// writeHTTPError escreve um httpError de validação.
func writeHTTPError(w http.ResponseWriter, err error) {
	var he *httpError
	if errors.As(err, &he) {
		writeError(w, he.status, he.code, he.msg)
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_param", err.Error())
}
