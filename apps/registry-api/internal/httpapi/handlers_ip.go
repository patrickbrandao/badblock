package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/patrickbrandao/badblock/apps/registry-api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/store"
)

// maxHolderASNs limita os ASNs "do mesmo titular" nas respostas de IP e prefixo;
// a lista completa fica em /v1/holder.
const maxHolderASNs = 20

// parseIP aceita IPv4 e IPv6; IPv4 mapeado em IPv6 vira IPv4.
func parseIP(v string) (netip.Addr, bool) {
	ip, err := netip.ParseAddr(v)
	if err != nil || ip.Zone() != "" {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}

// handleIP responde GET /v1/ip/{ip}.
func (a *API) handleIP(w http.ResponseWriter, r *http.Request) {
	ip, ok := parseIP(r.PathValue("ip"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_ip", "IP inválido: "+r.PathValue("ip"))
		return
	}
	a.serveIP(w, r, ip, true)
}

// handleMyIP responde GET /v1/ip com o IP de quem chama.
func (a *API) handleMyIP(w http.ResponseWriter, r *http.Request) {
	c := clientFrom(r.Context())
	if !c.ip.IsValid() {
		writeError(w, http.StatusBadRequest, "invalid_ip", "não foi possível identificar o IP de origem")
		return
	}
	w.Header().Set("X-Client-IP-Source", string(c.source))
	a.serveIP(w, r, c.ip, false)
}

func (a *API) serveIP(w http.ResponseWriter, r *http.Request, ip netip.Addr, cacheable bool) {
	snap, ok := a.ready(w)
	if !ok {
		return
	}
	key := cache.IPKey(snap.Version, ip, snap.Exceptions)
	etag := etagFor(snap.Version, key+"|"+ip.String())
	if cacheable && match(r.Header.Get("If-None-Match"), etag) {
		setDataHeaders(w, snap, etag, "HIT")
		w.WriteHeader(http.StatusNotModified)
		return
	}

	state := "HIT"
	var lk ipLookup
	raw, hit := a.cache.Get(r.Context(), key)
	if !hit || json.Unmarshal(raw, &lk) != nil {
		state = a.cacheLabel()
		v, err, _ := a.sf.Do(key, func() (any, error) {
			ctx, cancel := a.dbCtx(context.WithoutCancel(r.Context()))
			defer cancel()
			res, err := a.lookupIP(ctx, ip)
			if err != nil {
				return nil, err
			}
			if b, err := json.Marshal(res); err == nil {
				a.cache.Set(context.WithoutCancel(r.Context()), key, b)
			}
			return res, nil
		})
		if err != nil {
			a.writeStoreError(w, r, err)
			return
		}
		lk = *v.(*ipLookup)
	}

	body, err := json.Marshal(IPResponse{IP: ip.String(), ipLookup: lk, Dataset: datasetInfo(snap)})
	if err != nil {
		a.writeStoreError(w, r, err)
		return
	}
	setDataHeaders(w, snap, etag, state)
	if !cacheable {
		// A resposta de /v1/ip depende de quem chama: nada de cache compartilhado.
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Del("ETag")
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(append(body, '\n'))
}

// lookupIP monta a resposta de um IP a partir da cadeia de prefixos. O
// resultado vale para o bloco inteiro (/24 ou /48) quando não é exceção.
func (a *API) lookupIP(ctx context.Context, ip netip.Addr) (*ipLookup, error) {
	rows, err := a.store.Chain(ctx, ip)
	if err != nil {
		return nil, err
	}
	res := &ipLookup{ASNs: []ASNLink{}, Chain: []ChainItem{}}
	if len(rows) == 0 {
		// Fora de qualquer bloco conhecido (IPv6 fora do espaço alocado, por exemplo).
		res.Flags.Bogon = true
		return res, nil
	}
	res.Prefix = prefixInfo(rows[0])
	if err := a.fillLinks(ctx, rows, &res.Holder, &res.ASNs, &res.ASNsTruncated); err != nil {
		return nil, err
	}
	res.Flags = flagsFor(rows)
	for i := len(rows) - 1; i >= 0; i-- {
		res.Chain = append(res.Chain, chainItem(rows[i]))
	}
	return res, nil
}

// fillLinks preenche o titular (da linha mais específica que tem um) e os
// ASNs ligados: primeiro os do NIC.br, depois os do mesmo titular.
func (a *API) fillLinks(ctx context.Context, rows []store.Prefix, holder **HolderInfo, asns *[]ASNLink, truncated *bool) error {
	var holderID *int64
	for _, r := range rows {
		if r.HolderKey != nil {
			*holder = holderFromPrefix(r)
			holderID = r.HolderID
			break
		}
	}
	var explicit []int64
	for _, r := range rows {
		if r.Level == "rir" || r.Level == "nicbr" {
			explicit = r.NICBRASNs
			break
		}
	}
	seen := map[int64]bool{}
	if len(explicit) > 0 {
		briefs, err := a.store.ASNsByNumber(ctx, explicit)
		if err != nil {
			return err
		}
		names := map[int64]*string{}
		for _, b := range briefs {
			names[b.ASN] = b.Name
		}
		for _, n := range explicit {
			seen[n] = true
			*asns = append(*asns, ASNLink{ASN: n, Name: names[n], Link: "nicbr"})
		}
	}
	if holderID != nil {
		briefs, err := a.store.ASNsByHolder(ctx, *holderID, maxHolderASNs+1)
		if err != nil {
			return err
		}
		if len(briefs) > maxHolderASNs {
			briefs = briefs[:maxHolderASNs]
			*truncated = true
		}
		for _, b := range briefs {
			if !seen[b.ASN] {
				*asns = append(*asns, ASNLink{ASN: b.ASN, Name: b.Name, Link: "holder"})
			}
		}
	}
	return nil
}

// flagsFor calcula bogon e special a partir da cadeia. É bogon o que cai em
// bloco special-purpose não roteável ou o que nenhum RIR delegou
// (allocated/assigned): reservado, disponível ou fora de qualquer delegação.
func flagsFor(rows []store.Prefix) Flags {
	var f Flags
	delegated, nonGlobal := false, false
	for _, r := range rows {
		switch r.Level {
		case "rir", "nicbr":
			if isDelegated(r.Status) {
				delegated = true
			}
		case "special":
			if f.Special == nil {
				f.Special = r.Designation
			}
			if r.GloballyReachable != nil && !*r.GloballyReachable {
				nonGlobal = true
			}
		}
	}
	f.Bogon = nonGlobal || !delegated
	return f
}

// parsePrefixPath lê {ip}/{len} e devolve o prefixo normalizado.
func parsePrefixPath(ipStr, lenStr string) (netip.Prefix, error) {
	ip, ok := parseIP(ipStr)
	if !ok {
		return netip.Prefix{}, errors.New("IP inválido: " + ipStr)
	}
	bits, err := strconv.Atoi(lenStr)
	if err != nil || bits < 0 || bits > ip.BitLen() {
		return netip.Prefix{}, errors.New("tamanho de prefixo inválido: /" + lenStr)
	}
	return netip.PrefixFrom(ip, bits).Masked(), nil
}

// maxChildren limita as delegações listadas dentro de um prefixo.
const maxChildren = 1000

// handlePrefix responde GET /v1/prefix/{ip}/{len}.
func (a *API) handlePrefix(w http.ResponseWriter, r *http.Request) {
	p, err := parsePrefixPath(r.PathValue("ip"), r.PathValue("len"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_prefix", err.Error())
		return
	}
	snap, ok := a.ready(w)
	if !ok {
		return
	}
	key := cache.Prefix(snap.Version) + "prefix:" + p.String()
	a.serveCached(w, r, snap, key, func(ctx context.Context) (payload, error) {
		ctx, cancel := a.dbCtx(ctx)
		defer cancel()
		return a.buildPrefix(ctx, snap, p)
	})
}

func (a *API) buildPrefix(ctx context.Context, snap dataset.Snapshot, p netip.Prefix) (payload, error) {
	rows, err := a.store.Covering(ctx, p)
	if err != nil {
		return payload{}, err
	}
	res := PrefixResponse{
		Query: p.String(), Match: "none", ASNs: []ASNLink{}, Chain: []ChainItem{},
		Children: []ChildPrefix{}, Dataset: datasetInfo(snap),
	}
	if len(rows) > 0 {
		// Linha exata, se houver (a mais relevante pelo nível); senão, a que cobre.
		primary := rows[0]
		res.Match = "covering"
		if i := slices.IndexFunc(rows, func(x store.Prefix) bool { return x.Prefix == p }); i >= 0 {
			primary = rows[i]
			res.Match = "exact"
		}
		res.Prefix = prefixInfo(primary)
		if err := a.fillLinks(ctx, rows, &res.Holder, &res.ASNs, new(bool)); err != nil {
			return payload{}, err
		}
		res.Flags = flagsFor(rows)
		for i := len(rows) - 1; i >= 0; i-- {
			res.Chain = append(res.Chain, chainItem(rows[i]))
		}
	} else {
		res.Flags.Bogon = true
	}

	children, err := a.store.Children(ctx, p, maxChildren+1)
	if err != nil {
		return payload{}, err
	}
	if len(children) > maxChildren {
		children = children[:maxChildren]
		res.ChildrenTruncated = true
	}
	for _, c := range children {
		res.Children = append(res.Children, ChildPrefix{
			CIDR: c.Prefix.String(), Level: c.Level, Status: c.Status, Country: c.Country, Holder: c.HolderKey,
		})
	}
	return jsonPayload(res)
}

// normalizeRIR aceita o id em qualquer caixa e o apelido "ripe".
func normalizeRIR(v string) (string, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "ripe" || v == "ripe-ncc" || v == "ripe_ncc" {
		v = "ripencc"
	}
	_, ok := rirNames[v]
	return v, ok
}
