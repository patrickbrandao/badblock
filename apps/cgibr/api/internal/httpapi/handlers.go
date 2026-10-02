package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/patrickbrandao/badblock/apps/cgibr/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/cgibr/api/internal/store"
)

// SourceURL é a fonte dos dados, citada no índice.
const SourceURL = "https://ftp.registro.br/pub/numeracao/origin/nicbr-asn-blk-latest.txt"

func (a *API) handleIndex(w http.ResponseWriter, _ *http.Request) {
	b := a.cfg.BasePath
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, IndexResponse{
		App:      "api-cgibr",
		Version:  a.cfg.Version,
		BasePath: b,
		Versions: []string{CurrentAPIVersion},
		Endpoints: []string{
			b + "/asn/{asn}",
			b + "/ip/{ip}",
			b + "/prefix/{ip}/{len}",
			b + "/document/{cnpj}",
			b + "/asns",
			b + "/meta",
			b + "/status",
			b + "/openapi.yaml",
		},
		Source: SourceURL,
	})
}

// GET /asn/{asn} — aceita "61613" ou "AS61613".
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
			return nil, notFound("AS" + strconv.FormatUint(asn, 10) + " não consta no arquivo do NIC.br")
		}
		if err != nil {
			return nil, err
		}
		return ASNResponse{
			ASNRef:    asnRef(rec.ASNBrief),
			Prefixes:  splitPrefixes(rec.Prefixes),
			FirstSeen: ts(rec.CreatedAt),
			UpdatedAt: ts(rec.UpdatedAt),
			Dataset:   datasetInfo(snap),
		}, nil
	})
}

// GET /ip/{ip}
func (a *API) handleIP(w http.ResponseWriter, r *http.Request) {
	ip, err := netip.ParseAddr(r.PathValue("ip"))
	if err != nil || ip.Zone() != "" {
		writeError(w, http.StatusBadRequest, "bad_request", "endereço IP inválido")
		return
	}
	ip = ip.Unmap()
	a.serveCached(w, r, "ip:"+ip.String(), func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		m, err := a.store.Covering(ctx, netip.PrefixFrom(ip, ip.BitLen()))
		if errors.Is(err, store.ErrNotFound) {
			return nil, notFound(ip.String() + " não pertence a nenhum bloco do arquivo do NIC.br")
		}
		if err != nil {
			return nil, err
		}
		return IPResponse{IP: ip.String(), Prefix: m.Prefix.String(), ASN: asnRef(m.ASN), Dataset: datasetInfo(snap)}, nil
	})
}

// GET /prefix/{ip}/{len} — bits de host são zerados (10.0.0.1/8 vira 10.0.0.0/8).
func (a *API) handlePrefix(w http.ResponseWriter, r *http.Request) {
	ip, err := netip.ParseAddr(r.PathValue("ip"))
	if err != nil || ip.Zone() != "" {
		writeError(w, http.StatusBadRequest, "bad_request", "endereço IP inválido")
		return
	}
	ip = ip.Unmap()
	bits, err := strconv.Atoi(r.PathValue("len"))
	if err != nil || bits < 0 || bits > ip.BitLen() {
		writeError(w, http.StatusBadRequest, "bad_request", "tamanho de prefixo inválido")
		return
	}
	query := netip.PrefixFrom(ip, bits).Masked()
	a.serveCached(w, r, "prefix:"+query.String(), func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		m, err := a.store.Covering(ctx, query)
		if errors.Is(err, store.ErrNotFound) {
			return nil, notFound(query.String() + " não está contido em nenhum bloco do arquivo do NIC.br")
		}
		if err != nil {
			return nil, err
		}
		return PrefixResponse{
			Query: query.String(), Prefix: m.Prefix.String(), Exact: m.Prefix == query,
			ASN: asnRef(m.ASN), Dataset: datasetInfo(snap),
		}, nil
	})
}

// GET /document/{doc} — CNPJ (14 dígitos) ou identificador estrangeiro (8),
// com ou sem pontuação (a barra do CNPJ precisa vir como %2F).
func (a *API) handleDocument(w http.ResponseWriter, r *http.Request) {
	digits := onlyDigits(r.PathValue("doc"))
	if len(digits) != 14 && len(digits) != 8 {
		writeError(w, http.StatusBadRequest, "bad_request", "documento inválido: use o CNPJ (14 dígitos) ou o identificador estrangeiro (8 dígitos)")
		return
	}
	a.serveCached(w, r, "doc:"+digits, func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		list, err := a.store.ByDocument(ctx, digits)
		if err != nil {
			return nil, err
		}
		if len(list) == 0 {
			return nil, notFound("nenhum ASN com esse documento no arquivo do NIC.br")
		}
		resp := DocumentResponse{
			Document: list[0].Document, DocumentDigits: digits, DocumentType: documentType(digits),
			Name: list[0].Name, Count: len(list), ASNs: make([]ASNName, 0, len(list)), Dataset: datasetInfo(snap),
		}
		for _, x := range list {
			resp.ASNs = append(resp.ASNs, ASNName{ASN: x.ASN, Name: x.Name})
		}
		return resp, nil
	})
}

// GET /asns — todos os ASNs, sem os blocos.
func (a *API) handleASNs(w http.ResponseWriter, r *http.Request) {
	a.serveCached(w, r, "asns", func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		list, err := a.store.ListASNs(ctx)
		if err != nil {
			return nil, err
		}
		resp := ASNListResponse{Count: len(list), ASNs: make([]ASNRef, 0, len(list)), Dataset: datasetInfo(snap)}
		for _, x := range list {
			resp.ASNs = append(resp.ASNs, asnRef(x))
		}
		return resp, nil
	})
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
	resp := MetaResponse{App: "api-cgibr", Version: a.cfg.Version}
	if d != nil {
		resp.Dataset = &MetaDataset{
			Version: d.Version, UpdatedAt: ts(d.AppliedAt), Source: d.URL, SHA256: d.SHA256,
			ASNs: d.ASNs, PrefixesV4: d.PrefixesV4, PrefixesV6: d.PrefixesV6,
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
		Success: true, Status: "ok", Timestamp: ts(time.Now()), Message: "api-cgibr operacional",
		Checks: map[string]string{"postgres": "ok", "valkey": "disabled", "dataset": "ok"},
	}
	code := http.StatusOK

	if !a.data.Current().Ready() {
		resp.Checks["dataset"] = "empty"
		resp.Status, resp.Message = "starting", "aguardando a primeira sincronização do collector-cgibr"
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

func onlyDigits(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return b.String()
}
