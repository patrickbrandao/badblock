package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/store"
	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/tldname"
)

// GET /tlds — todos os TLDs delegados, em ordem de nome, com as contagens da
// delegação (rootzone_tld).
func (a *API) handleTLDs(w http.ResponseWriter, r *http.Request) {
	a.serveCached(w, r, "tlds", func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		list, err := a.store.TLDs(ctx)
		if err != nil {
			return nil, err
		}
		resp := TLDsResponse{Zone: zoneInfo(snap), Count: len(list), TLDs: make([]TLDSummary, 0, len(list)), Dataset: datasetInfo(snap)}
		for _, t := range list {
			resp.TLDs = append(resp.TLDs, TLDSummary{
				TLD: t.TLD, TLDUnicode: t.Unicode, Nameservers: t.Nameservers,
				NameserversIPv4: t.NameserversV4, NameserversIPv6: t.NameserversV6, DSRecords: t.DSRecords,
			})
		}
		return resp, nil
	})
}

// GET /tld/{tld} — a delegação de um TLD: NS com TTL, o glue A/AAAA de cada
// servidor e os DS. Aceita qualquer caixa, com ou sem o ponto final, em
// ASCII ("xn--p1ai") ou em Unicode ("рф"); a chave é o nome normalizado.
func (a *API) handleTLD(w http.ResponseWriter, r *http.Request) {
	tld, err := tldname.Normalize(r.PathValue("tld"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request",
			"TLD inválido: use um rótulo só, em ASCII (letras, dígitos, - e _, até 63 caracteres; ex.: br, xn--p1ai) "+
				"ou em Unicode (ex.: рф), com ou sem o ponto final")
		return
	}
	a.serveCached(w, r, "tld:"+tld, func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		d, err := a.store.Delegation(ctx, tld)
		if errors.Is(err, store.ErrNotFound) {
			return nil, notFound("o TLD " + tld + " não está delegado na zona raiz")
		}
		if err != nil {
			return nil, err
		}
		resp, err := delegationResponse(d, snap)
		if err != nil {
			a.log.Error("registro fora do formato", "path", r.URL.Path, "err", err)
			return nil, &httpError{status: http.StatusInternalServerError, code: "internal_error", msg: "erro interno"}
		}
		return resp, nil
	})
}

// delegationResponse monta a resposta de /tld: os NS (em ordem de nome) com
// o glue de cada um e os DS decompostos. Um rdata fora do formato gravado
// pelo coletor é erro (quebra de contrato com rootzone_record).
func delegationResponse(d *store.Delegation, snap dataset.Snapshot) (TLDResponse, error) {
	resp := TLDResponse{
		TLD: d.TLD.TLD, TLDUnicode: d.TLD.Unicode,
		Nameservers: []Nameserver{}, DS: []DS{},
		FirstSeen: ts(d.TLD.CreatedAt), UpdatedAt: ts(d.TLD.UpdatedAt),
		Zone: zoneInfo(snap), Dataset: datasetInfo(snap),
	}
	glue := map[string]*Nameserver{}
	for _, rec := range d.Records {
		switch rec.Type {
		case "NS":
			resp.Nameservers = append(resp.Nameservers, Nameserver{Name: rec.RData, TTL: rec.TTL, IPv4: []Address{}, IPv6: []Address{}})
		case "DS":
			ds, err := parseDS(rec.RData)
			if err != nil {
				return TLDResponse{}, fmt.Errorf("DS de %s: %w", d.TLD.TLD, err)
			}
			ds.TTL = rec.TTL
			resp.DS = append(resp.DS, ds)
		default:
			return TLDResponse{}, fmt.Errorf("tipo %q inesperado na delegação de %s", rec.Type, d.TLD.TLD)
		}
	}
	for i := range resp.Nameservers {
		glue[resp.Nameservers[i].Name] = &resp.Nameservers[i]
	}
	for _, g := range d.Glue {
		ns := glue[g.Owner]
		if ns == nil {
			return TLDResponse{}, fmt.Errorf("glue de %s, que não é NS de %s", g.Owner, d.TLD.TLD)
		}
		addr := Address{Address: g.RData, TTL: g.TTL}
		switch g.Type {
		case "A":
			ns.IPv4 = append(ns.IPv4, addr)
		case "AAAA":
			ns.IPv6 = append(ns.IPv6, addr)
		default:
			return TLDResponse{}, fmt.Errorf("glue %s de tipo %q", g.Owner, g.Type)
		}
	}
	return resp, nil
}

// parseDS decompõe o rdata normalizado de um DS: "keytag alg tipo DIGEST".
func parseDS(rdata string) (DS, error) {
	f := strings.Fields(rdata)
	if len(f) != 4 {
		return DS{}, fmt.Errorf("rdata %q: esperado 4 campos", rdata)
	}
	keyTag, err1 := strconv.ParseUint(f[0], 10, 16)
	alg, err2 := strconv.ParseUint(f[1], 10, 8)
	digestType, err3 := strconv.ParseUint(f[2], 10, 8)
	if err := errors.Join(err1, err2, err3); err != nil {
		return DS{}, fmt.Errorf("rdata %q: %w", rdata, err)
	}
	return DS{KeyTag: int(keyTag), Algorithm: int(alg), DigestType: int(digestType), Digest: f[3]}, nil
}
