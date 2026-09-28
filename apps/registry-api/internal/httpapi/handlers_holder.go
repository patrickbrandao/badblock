package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"

	"github.com/patrickbrandao/badblock/apps/registry-api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/store"
)

// opaqueIDRe cobre os formatos dos RIRs: números (LACNIC), hex (ARIN, APNIC,
// AFRINIC) e UUID (RIPE).
var opaqueIDRe = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// maxHolderList limita os ASNs listados em /v1/holder.
const maxHolderList = 1000

// handleHolder responde GET /v1/holder/{rir}/{id}.
func (a *API) handleHolder(w http.ResponseWriter, r *http.Request) {
	rir, ok := normalizeRIR(r.PathValue("rir"))
	id := r.PathValue("id")
	if !ok || !opaqueIDRe.MatchString(id) {
		writeError(w, http.StatusBadRequest, "invalid_holder", "titular inválido; use /v1/holder/{rir}/{id}, ex.: /v1/holder/lacnic/258500")
		return
	}
	q := r.URL.Query()
	limit := defaultLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxLimit {
			writeError(w, http.StatusBadRequest, "invalid_param", "limit deve ficar entre 1 e 1000")
			return
		}
		limit = n
	}
	var after *netip.Prefix
	if c := q.Get("cursor"); c != "" {
		raw, err := decodeCursor(c)
		if err == nil {
			if p, perr := netip.ParsePrefix(raw); perr == nil {
				after = &p
			} else {
				err = perr
			}
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_param", "cursor inválido")
			return
		}
	}
	snap, ok := a.ready(w)
	if !ok {
		return
	}
	key := cache.RequestKey(snap.Version, r.URL.Path, url.Values{"limit": {strconv.Itoa(limit)}, "cursor": {q.Get("cursor")}})
	a.serveCached(w, r, snap, key, func(ctx context.Context) (payload, error) {
		ctx, cancel := a.dbCtx(ctx)
		defer cancel()
		h, err := a.store.Holder(ctx, rir, id)
		if errors.Is(err, store.ErrNotFound) {
			return payload{}, notFound("titular não encontrado")
		}
		if err != nil {
			return payload{}, err
		}
		res := HolderResponse{
			ID: h.Key, RIR: rirName(&h.RIR), Name: h.Name, NameSource: h.NameSource, Document: h.Document,
			Country: h.Country,
			Counts: HolderCounts{
				ASNs: h.ASNCount, IPv4Prefixes: h.Prefix4Count, IPv6Prefixes: h.Prefix6Count,
				IPv4Addresses: h.IPv4Addresses,
			},
			ASNs: []ListASNItem{}, Prefixes: []ListPrefixItem{},
			FirstSeen: tsStr(h.FirstSeen), UpdatedAt: tsStr(h.UpdatedAt), RemovedAt: tsPtr(h.RemovedAt),
			Dataset: datasetInfo(snap),
		}
		asns, err := a.store.ASNsByHolder(ctx, h.ID, maxHolderList)
		if err != nil {
			return payload{}, err
		}
		for _, b := range asns {
			item := listASNItem(b)
			item.Holder = nil
			res.ASNs = append(res.ASNs, item)
		}
		rows, err := a.store.HolderPrefixes(ctx, h.ID, after, limit+1)
		if err != nil {
			return payload{}, err
		}
		if len(rows) > limit {
			rows = rows[:limit]
			res.NextCursor = encodeCursor(rows[limit-1].Prefix.String())
		}
		for _, p := range rows {
			res.Prefixes = append(res.Prefixes, ListPrefixItem{
				CIDR: p.Prefix.String(), RIR: rirName(p.RIR), Country: p.Country, Status: p.Status,
				Registered: dateStr(p.Registered),
			})
		}
		return jsonPayload(res)
	})
}
