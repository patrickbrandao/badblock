package httpapi

import (
	"context"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/patrickbrandao/badblock/apps/iana/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/iana/api/internal/store"
)

// SourceURL é a página da IANA que reúne os registros de numeração, citada no
// índice (os 10 arquivos e suas URLs estão no /meta).
const SourceURL = "https://www.iana.org/numbers"

func (a *API) handleIndex(w http.ResponseWriter, _ *http.Request) {
	b := a.cfg.BasePath
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, IndexResponse{
		App:      "api-iana",
		Version:  a.cfg.Version,
		BasePath: b,
		Versions: []string{CurrentAPIVersion},
		Endpoints: []string{
			b + "/asn/{asn}",
			b + "/ip/{ip}",
			b + "/prefix/{ip}/{len}",
			b + "/asns",
			b + "/ipv4",
			b + "/ipv6",
			b + "/special",
			b + "/rdap",
			b + "/meta",
			b + "/status",
			b + "/openapi.yaml",
		},
		Source: SourceURL,
	})
}

// GET /asn/{asn} — aceita "61610", "AS61610" ou "as61610".
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
		l, err := a.store.ASN(ctx, int64(asn))
		if err != nil {
			return nil, err
		}
		return ASNResponse{
			ASN:     int64(asn),
			Block:   asnBlockPtr(l.Block),
			Special: specialASNs(l.Special),
			RDAP:    rdapPtr(l.RDAP),
			Dataset: datasetInfo(snap),
		}, nil
	})
}

// parseIP aceita IPv4 e IPv6 sem zona; IPv4 mapeado em IPv6 vira IPv4.
func parseIP(w http.ResponseWriter, raw string) (netip.Addr, bool) {
	ip, err := netip.ParseAddr(raw)
	if err != nil || ip.Zone() != "" {
		writeError(w, http.StatusBadRequest, "bad_request", "endereço IP inválido")
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}

// GET /ip/{ip}
func (a *API) handleIP(w http.ResponseWriter, r *http.Request) {
	ip, ok := parseIP(w, r.PathValue("ip"))
	if !ok {
		return
	}
	a.serveCached(w, r, "ip:"+ip.String(), func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		l, err := a.store.Prefix(ctx, netip.PrefixFrom(ip, ip.BitLen()))
		if err != nil {
			return nil, err
		}
		return IPResponse{
			IP:      ip.String(),
			Block:   prefixBlockPtr(l.Block),
			Special: specialPrefixes(l.Special),
			Bogon:   l.Bogon(),
			RDAP:    rdapPtr(l.RDAP),
			Dataset: datasetInfo(snap),
		}, nil
	})
}

// GET /prefix/{ip}/{len} — bits de host são zerados (10.0.0.1/8 vira 10.0.0.0/8).
func (a *API) handlePrefix(w http.ResponseWriter, r *http.Request) {
	ip, ok := parseIP(w, r.PathValue("ip"))
	if !ok {
		return
	}
	bits, err := strconv.Atoi(r.PathValue("len"))
	if err != nil || bits < 0 || bits > ip.BitLen() {
		writeError(w, http.StatusBadRequest, "bad_request", "tamanho de prefixo inválido")
		return
	}
	query := netip.PrefixFrom(ip, bits).Masked()
	a.serveCached(w, r, "prefix:"+query.String(), func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		l, err := a.store.Prefix(ctx, query)
		if err != nil {
			return nil, err
		}
		return PrefixResponse{
			Query:   query.String(),
			Block:   prefixBlockPtr(l.Block),
			Special: specialPrefixes(l.Special),
			Bogon:   l.Bogon(),
			RDAP:    rdapPtr(l.RDAP),
			Dataset: datasetInfo(snap),
		}, nil
	})
}

// GET /asns — todas as faixas de ASN da IANA.
func (a *API) handleASNs(w http.ResponseWriter, r *http.Request) {
	a.serveCached(w, r, "asns", func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		list, err := a.store.ASNBlocks(ctx)
		if err != nil {
			return nil, err
		}
		return ASNBlocksResponse{Count: len(list), Blocks: asnBlocks(list), Dataset: datasetInfo(snap)}, nil
	})
}

// GET /ipv4 e GET /ipv6 — todos os blocos de uma família.
func (a *API) handlePrefixBlocks(family int) http.HandlerFunc {
	key := "ipv" + strconv.Itoa(family)
	return func(w http.ResponseWriter, r *http.Request) {
		a.serveCached(w, r, key, func(ctx context.Context, snap dataset.Snapshot) (any, error) {
			list, err := a.store.PrefixBlocks(ctx, family)
			if err != nil {
				return nil, err
			}
			return PrefixBlocksResponse{Count: len(list), Blocks: prefixBlocks(list), Dataset: datasetInfo(snap)}, nil
		})
	}
}

// GET /special — registros de uso especial (IPv4, IPv6 e ASN).
func (a *API) handleSpecial(w http.ResponseWriter, r *http.Request) {
	a.serveCached(w, r, "special", func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		l, err := a.store.Special(ctx)
		if err != nil {
			return nil, err
		}
		resp := SpecialResponse{IPv4: []SpecialPrefix{}, IPv6: []SpecialPrefix{}, ASN: specialASNs(l.ASNs), Dataset: datasetInfo(snap)}
		for _, p := range l.Prefixes {
			if p.Prefix.Addr().Is4() {
				resp.IPv4 = append(resp.IPv4, specialPrefix(p))
			} else {
				resp.IPv6 = append(resp.IPv6, specialPrefix(p))
			}
		}
		return resp, nil
	})
}

// GET /rdap — bootstrap RDAP (RFC 9224) com a publicação de cada JSON.
func (a *API) handleRDAP(w http.ResponseWriter, r *http.Request) {
	a.serveCached(w, r, "rdap", func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		b, err := a.store.RDAP(ctx)
		if err != nil {
			return nil, err
		}
		resp := RDAPResponse{
			Publication: RDAPPublication{
				ASN:  b.Publication[store.FileRDAPASN],
				IPv4: b.Publication[store.FileRDAPIPv4],
				IPv6: b.Publication[store.FileRDAPIPv6],
			},
			ASN: []RDAPService{}, IPv4: []RDAPService{}, IPv6: []RDAPService{},
			Dataset: datasetInfo(snap),
		}
		for _, s := range b.Services {
			switch s.Kind {
			case "asn":
				resp.ASN = append(resp.ASN, rdapService(s))
			case "ipv4":
				resp.IPv4 = append(resp.IPv4, rdapService(s))
			case "ipv6":
				resp.IPv6 = append(resp.IPv6, rdapService(s))
			}
		}
		return resp, nil
	})
}

// GET /meta — dataset aplicado e estado do collector (sem cache).
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
	resp := MetaResponse{App: "api-iana", Version: a.cfg.Version}
	if d != nil {
		md := &MetaDataset{Version: d.Version, UpdatedAt: ts(d.AppliedAt), SHA256: d.SHA256, Files: make([]MetaFile, 0, len(d.Files))}
		for _, f := range d.Files {
			md.Files = append(md.Files, MetaFile{
				Name: f.Name, URL: f.URL, SHA256: f.SHA256, Bytes: f.Bytes, Rows: f.Rows,
				LastModified: f.LastModified, Publication: f.Publication,
			})
		}
		resp.Dataset = md
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
		Success: true, Status: "ok", Timestamp: ts(time.Now()), Message: "api-iana operacional",
		Checks: map[string]string{"postgres": "ok", "valkey": "disabled", "dataset": "ok"},
	}
	code := http.StatusOK

	if !a.data.Current().Ready() {
		resp.Checks["dataset"] = "empty"
		resp.Status, resp.Message = "starting", "aguardando a primeira sincronização do collector-iana"
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
