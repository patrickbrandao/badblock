package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/patrickbrandao/badblock/apps/registry-api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/store"
)

// maxASNPrefixes limita os prefixos listados em /v1/asn/{asn}.
const maxASNPrefixes = 500

// parseASN aceita "61613" e "AS61613".
func parseASN(v string) (int64, bool) {
	v = strings.TrimSpace(v)
	if len(v) > 2 && strings.EqualFold(v[:2], "AS") {
		v = v[2:]
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return 0, false
	}
	return int64(n), true
}

// handleASN responde GET /v1/asn/{asn}.
func (a *API) handleASN(w http.ResponseWriter, r *http.Request) {
	asn, ok := parseASN(r.PathValue("asn"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_asn", "ASN inválido: "+r.PathValue("asn"))
		return
	}
	snap, ok := a.ready(w)
	if !ok {
		return
	}
	key := cache.Prefix(snap.Version) + "asn:" + strconv.FormatInt(asn, 10)
	a.serveCached(w, r, snap, key, func(ctx context.Context) (payload, error) {
		ctx, cancel := a.dbCtx(ctx)
		defer cancel()
		return a.buildASN(ctx, snap, asn)
	})
}

func (a *API) buildASN(ctx context.Context, snap dataset.Snapshot, asn int64) (payload, error) {
	rec, err := a.store.ASN(ctx, asn)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return payload{}, err
	}
	blocks, err := a.store.ASNBlocks(ctx, asn)
	if err != nil {
		return payload{}, err
	}
	if rec == nil && len(blocks) == 0 {
		return payload{}, notFound("ASN sem registro")
	}

	res := ASNResponse{ASN: asn, Prefixes: []ASNPrefix{}, Chain: []ASNChainItem{}, Dataset: datasetInfo(snap)}
	var special *string
	for _, b := range blocks {
		res.Chain = append(res.Chain, ASNChainItem{
			First: b.First, Last: b.Last, Level: b.Level, RIR: rirName(b.RIR),
			Designation: b.Designation, Reference: b.Reference,
		})
		if b.Level == "special" && special == nil {
			special = &b.Designation
		}
	}
	res.Flags = Flags{Special: special, Bogon: special != nil}

	if rec == nil {
		// Número sem delegação de RIR: só a cadeia da IANA.
		res.Flags.Bogon = true
		return jsonPayload(res)
	}
	status := rec.Status
	res.Name, res.NameSource, res.Handle = rec.Name, rec.NameSource, rec.Handle
	res.RIR, res.Country, res.Status = rirName(&rec.RIR), rec.Country, &status
	res.Registered, res.RDAP = dateStr(rec.Registered), rec.RDAPURL
	res.FirstSeen, res.UpdatedAt = strPtr(tsStr(rec.FirstSeen)), strPtr(tsStr(rec.UpdatedAt))
	if rec.HolderKey != nil {
		res.Holder = &HolderInfo{ID: *rec.HolderKey, Name: rec.HolderName, NameSource: rec.HolderNameSource, Document: rec.HolderDocument}
	}
	if !isDelegated(&status) {
		res.Flags.Bogon = true
	}

	links, total, err := a.store.PrefixesForASN(ctx, asn, rec.HolderID, maxASNPrefixes)
	if err != nil {
		return payload{}, err
	}
	for _, l := range links {
		res.Prefixes = append(res.Prefixes, ASNPrefix{CIDR: l.Prefix.String(), Link: l.Link, Status: l.Status, Country: l.Country})
	}
	res.PrefixesTotal = total
	res.PrefixesTruncated = total > len(links)
	return jsonPayload(res)
}

// handleLegacyASN responde GET /asn/{asn} no formato exato do POC antigo
// (webhook do n8n): campos planos, status em maiúsculas, score
// "0" e 404 com corpo {}.
func (a *API) handleLegacyASN(w http.ResponseWriter, r *http.Request) {
	empty := func(status int) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write([]byte("{}\n"))
	}
	asn, ok := parseASN(r.PathValue("asn"))
	if !ok {
		empty(http.StatusNotFound)
		return
	}
	snap := a.data.Current()
	if !snap.Ready() {
		empty(http.StatusServiceUnavailable)
		return
	}
	key := cache.Prefix(snap.Version) + "legacy-asn:" + strconv.FormatInt(asn, 10)
	a.serveCached(w, r, snap, key, func(ctx context.Context) (payload, error) {
		ctx, cancel := a.dbCtx(ctx)
		defer cancel()
		rec, err := a.store.ASN(ctx, asn)
		if errors.Is(err, store.ErrNotFound) {
			return payload{}, &httpError{status: http.StatusNotFound, raw: []byte("{}\n")}
		}
		if err != nil {
			return payload{}, err
		}
		out := LegacyASN{ASN: rec.ASN, RIR: *rirName(&rec.RIR), Status: strings.ToUpper(rec.Status), Score: "0"}
		if rec.Name != nil {
			out.Name = *rec.Name
		}
		if rec.HolderDocument != nil {
			out.CID = *rec.HolderDocument
		}
		if rec.Country != nil {
			out.Country = *rec.Country
		}
		b, err := json.Marshal(out)
		if err != nil {
			return payload{}, err
		}
		return payload{contentType: "application/json; charset=utf-8", body: append(b, '\n')}, nil
	})
}
