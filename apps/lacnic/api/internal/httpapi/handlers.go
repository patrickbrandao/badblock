package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/patrickbrandao/badblock/apps/lacnic/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/lacnic/api/internal/rir"
	"github.com/patrickbrandao/badblock/apps/lacnic/api/internal/store"
	"github.com/patrickbrandao/badblock/apps/lacnic/api/openapi"
)

// inFile completa as mensagens de "não encontrado".
const inFile = " no arquivo do RIR " + rir.Title

// opaqueIDPattern aceita o opaque-id dos cinco RIRs (conferido nos arquivos
// reais): número de até 6 dígitos, hex de 8 ou 32 dígitos e UUID.
var opaqueIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func (a *API) handleIndex(w http.ResponseWriter, _ *http.Request) {
	b := a.cfg.BasePath
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, IndexResponse{
		App:      rir.App,
		Version:  a.cfg.Version,
		Registry: rir.Title,
		BasePath: b,
		Versions: []string{CurrentAPIVersion},
		Endpoints: []string{
			b + "/asn/{asn}",
			b + "/ip/{ip}",
			b + "/prefix/{ip}/{len}",
			b + "/holder/{opaque_id}",
			b + "/meta",
			b + "/status",
			b + "/openapi.yaml",
		},
		Source: rir.SourceURL,
	})
}

// GET /openapi.yaml — o manifesto OpenAPI embutido no binário.
func (a *API) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(openapi.Spec)
}

// GET /asn/{asn} — aceita "61613", "AS61613" ou "as61613".
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
			return nil, notFound("AS" + strconv.FormatUint(asn, 10) + " não consta" + inFile)
		}
		if err != nil {
			return nil, err
		}
		return ASNResponse{
			ASN:        int64(asn),
			Range:      Range{Start: rec.Start, End: rec.End, Count: rec.Count},
			Delegation: delegation(rec.Info),
			FirstSeen:  ts(rec.CreatedAt),
			UpdatedAt:  ts(rec.UpdatedAt),
			Dataset:    datasetInfo(snap),
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
		b, err := a.store.Covering(ctx, netip.PrefixFrom(ip, ip.BitLen()))
		if errors.Is(err, store.ErrNotFound) {
			return nil, notFound(ip.String() + " não pertence a nenhum bloco" + inFile)
		}
		if err != nil {
			return nil, err
		}
		return IPResponse{
			IP: ip.String(), Prefix: b.Prefix.String(), Delegation: delegation(b.Info),
			Record: record(b), Dataset: datasetInfo(snap),
		}, nil
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
		b, err := a.store.Covering(ctx, query)
		if errors.Is(err, store.ErrNotFound) {
			return nil, notFound(query.String() + " não está contido em nenhum bloco" + inFile)
		}
		if err != nil {
			return nil, err
		}
		return PrefixResponse{
			Query: query.String(), Prefix: b.Prefix.String(), Exact: b.Prefix == query,
			Delegation: delegation(b.Info), Record: record(b), Dataset: datasetInfo(snap),
		}, nil
	})
}

// GET /holder/{opaque_id} — todos os ASNs e blocos de um titular.
func (a *API) handleHolder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !opaqueIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "bad_request", "opaque_id inválido: use de 1 a 128 letras, dígitos, ponto, hífen ou sublinhado")
		return
	}
	a.serveCached(w, r, "holder:"+id, func(ctx context.Context, snap dataset.Snapshot) (any, error) {
		h, err := a.store.Holder(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			msg := "o titular " + id + " não consta" + inFile
			if rir.OpaqueIDChangesDaily {
				b := a.cfg.BasePath
				msg += "; o " + rir.Title + " gera opaque_id novos a cada arquivo diário: consulte " +
					b + "/ip/{ip} ou " + b + "/asn/{asn} para obter o atual"
			}
			return nil, notFound(msg)
		}
		if err != nil {
			return nil, err
		}
		return holderResponse(h, snap), nil
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
	resp := MetaResponse{App: rir.App, Version: a.cfg.Version}
	if d != nil {
		resp.Dataset = &MetaDataset{
			Version: d.Version, UpdatedAt: ts(d.AppliedAt), Source: d.URL,
			SHA256: d.SHA256, MD5: d.MD5, Serial: d.Serial,
			StartDate: datePtr(d.StartDate), EndDate: datePtr(d.EndDate),
			ASNRecords: d.ASNRecords, IPv4Records: d.IPv4Records, IPv6Records: d.IPv6Records,
			PrefixesV4: d.PrefixesV4, PrefixesV6: d.PrefixesV6,
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
		Success: true, Status: "ok", Timestamp: ts(time.Now()), Message: rir.App + " operacional",
		Checks: map[string]string{"postgres": "ok", "valkey": "disabled", "dataset": "ok"},
	}
	code := http.StatusOK

	if !a.data.Current().Ready() {
		resp.Checks["dataset"] = "empty"
		resp.Status, resp.Message = "starting", "aguardando a primeira sincronização do "+rir.Collector
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
