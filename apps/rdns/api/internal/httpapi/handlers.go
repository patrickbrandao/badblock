package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/patrickbrandao/badblock/apps/rdns/api/internal/live"
	"github.com/patrickbrandao/badblock/apps/rdns/api/internal/store"
)

// SourceURL são as specs da fonte, citadas no índice: o rdns não tem arquivo
// publicado (é uma sonda ativa).
const SourceURL = "https://github.com/patrickbrandao/badblock/tree/main/specs/fontes/rdns"

// DerivedFrom são as fontes de onde vêm os prefixos derivados.
var DerivedFrom = []string{"cgibr"}

func (a *API) handleIndex(w http.ResponseWriter, _ *http.Request) {
	b := a.cfg.BasePath
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, IndexResponse{
		App:      "api-rdns",
		Version:  a.cfg.Version,
		BasePath: b,
		Versions: []string{CurrentAPIVersion},
		Endpoints: []string{
			b + "/ip/{ip}",
			b + "/prefix/{ip}/{len}",
			b + "/meta",
			b + "/status",
			b + "/openapi.yaml",
		},
		Source:      SourceURL,
		DerivedFrom: DerivedFrom,
	})
}

// parseIPv4 valida o IP de /ip e /prefix: o IPv4 mapeado em IPv6 vira IPv4;
// IPv6 e o resto são 400.
func parseIPv4(w http.ResponseWriter, raw string) (netip.Addr, bool) {
	ip, err := netip.ParseAddr(raw)
	if err != nil || ip.Zone() != "" {
		writeError(w, http.StatusBadRequest, "bad_request", "endereço IP inválido")
		return netip.Addr{}, false
	}
	ip = ip.Unmap()
	if !ip.Is4() {
		writeError(w, http.StatusBadRequest, "bad_request", "o rdns varre só IPv4")
		return netip.Addr{}, false
	}
	return ip, true
}

// buildIP é o corpo frio de /ip, lido do banco.
func (a *API) buildIP(ctx context.Context, ip netip.Addr) (any, error) {
	row, err := a.store.IP(ctx, ip)
	if errors.Is(err, store.ErrNotFound) {
		return nil, notFound(ip.String() + " não está em nenhum prefixo do rdns")
	}
	if err != nil {
		return nil, err
	}
	return ipResponse(ip, row), nil
}

// GET /ip/{ip}?live=true|false
func (a *API) handleIP(w http.ResponseWriter, r *http.Request) {
	ip, ok := parseIPv4(w, r.PathValue("ip"))
	if !ok {
		return
	}
	// Só "true" e "false", uma vez: outra grafia escaparia do router do
	// Traefik que limita o live=true.
	liveOn := false
	if vals, present := r.URL.Query()["live"]; present {
		if len(vals) != 1 || (vals[0] != "true" && vals[0] != "false") {
			writeError(w, http.StatusBadRequest, "bad_request", "live inválido: use true ou false")
			return
		}
		liveOn = vals[0] == "true"
	}
	build := func(ctx context.Context) (any, error) { return a.buildIP(ctx, ip) }
	if !liveOn {
		a.serveCached(w, r, "ip:"+ip.String(), build)
		return
	}
	a.serveLive(w, r, ip, build)
}

// serveLive responde /ip com live=true: o dado frio mais o bloco live, sem
// ETag e sem cache no cliente.
func (a *API) serveLive(w http.ResponseWriter, r *http.Request, ip netip.Addr, build func(ctx context.Context) (any, error)) {
	start := time.Now()
	if !a.data.Current().Ready() {
		writeNotReady(w)
		return
	}
	key := "ip:" + ip.String()
	cold, _, err := a.fetch(r, key, build)
	if err != nil {
		a.writeFailure(w, r, err)
		return
	}
	var resp IPResponse
	if err := json.Unmarshal(cold, &resp); err != nil {
		a.writeFailure(w, r, err)
		return
	}

	out := live.Outcome{Status: live.Unavailable}
	switch {
	case !resp.Scan.Active || !inRule(resp.Scan.Prefix):
		out.Status = live.NotAllowed
	case a.live == nil:
	default:
		out, err = a.live.Resolve(r.Context(), ip.String(), func(ctx context.Context) ([]byte, error) {
			// Relê o IP no banco depois da gravação e regrava a chave fria.
			ctx, cancel := context.WithTimeout(ctx, a.cfg.DBTimeout)
			defer cancel()
			obj, err := build(ctx)
			if err != nil {
				return nil, err
			}
			body, err := render(obj)
			if err != nil {
				return nil, err
			}
			a.storeBody(ctx, key, body)
			return body, nil
		})
		if errors.Is(err, live.ErrQueueFull) {
			a.log.Warn("fila de tempo real cheia", "ip", ip.String())
			writeError(w, http.StatusServiceUnavailable, "live_queue_full",
				"fila de tempo real cheia; tente sem live=true ou mais tarde")
			return
		}
		if err != nil {
			a.writeFailure(w, r, err)
			return
		}
		if out.Status == live.Resolved {
			resp = IPResponse{}
			if err := json.Unmarshal(out.Body, &resp); err != nil {
				a.writeFailure(w, r, err)
				return
			}
		}
	}
	if out.Err != nil {
		a.log.Warn("Valkey indisponível no tempo real", "ip", ip.String(), "err", out.Err)
	}
	resp.Live = &LiveInfo{Status: out.Status, Result: strPtr(out.Result), CheckedAt: strPtr(out.CheckedAt)}
	a.log.Debug("tempo real", "ip", ip.String(), "live_status", out.Status, "result", out.Result,
		"ms", time.Since(start).Milliseconds())
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Cache", "LIVE")
	writeJSON(w, http.StatusOK, resp)
}

// inRule diz se a sonda varre o prefixo (IPv4 de /22 a /24).
func inRule(prefix string) bool {
	p, err := netip.ParsePrefix(prefix)
	return err == nil && p.Addr().Is4() && p.Bits() >= 22 && p.Bits() <= 24
}

// GET /prefix/{ip}/{len} — de /24 a /31; bits de host zerados.
func (a *API) handlePrefix(w http.ResponseWriter, r *http.Request) {
	ip, ok := parseIPv4(w, r.PathValue("ip"))
	if !ok {
		return
	}
	bits, err := strconv.Atoi(r.PathValue("len"))
	if err != nil || bits < 24 || bits > 31 {
		writeError(w, http.StatusBadRequest, "bad_request", "tamanho de prefixo inválido: use de 24 a 31")
		return
	}
	if _, present := r.URL.Query()["live"]; present {
		writeError(w, http.StatusBadRequest, "bad_request", "live só vale em "+a.cfg.BasePath+"/ip")
		return
	}
	q := netip.PrefixFrom(ip, bits).Masked()
	a.serveCached(w, r, "prefix:"+q.String(), func(ctx context.Context) (any, error) {
		rows, err := a.store.Prefix(ctx, q)
		if errors.Is(err, store.ErrNotFound) {
			return nil, notFound(q.String() + " não está contido em nenhum prefixo do rdns")
		}
		if err != nil {
			return nil, err
		}
		return prefixResponse(q, rows), nil
	})
}

// GET /meta — estado da varredura, das derivações e das instâncias (sem
// cache, lido direto do banco).
func (a *API) handleMeta(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.DBTimeout)
	defer cancel()
	resp := MetaResponse{App: "api-rdns", Version: a.cfg.Version, Derivations: []MetaDerivation{}, Workers: []MetaWorker{}}

	cfg, err := a.store.Config(ctx)
	if err != nil {
		a.writeFailure(w, r, err)
		return
	}
	interval := 24 * time.Hour
	if v, ok := cfg["rate_limit"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			resp.Config.RateLimit = &n
		}
	}
	if v, ok := cfg["scan_interval"]; ok {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			resp.Config.ScanInterval = &v
			interval = d
		}
	}

	st, err := a.store.ScanStats(ctx, interval)
	if err != nil {
		a.writeFailure(w, r, err)
		return
	}
	resp.Scan = MetaScan{
		Prefixes: st.Prefixes, Active: st.Active, Static: st.Static, OutOfRule: st.OutOfRule,
		Addresses: st.Addresses, NeverScanned: st.NeverScanned, Overdue: st.Overdue, Rescan: st.Rescan,
		Scanning: st.Scanning, OldestScanAt: tsPtr(st.OldestScanAt), PTR: st.PTR, NoPTR: st.NoPTR, Failed: st.Failed,
	}

	ders, err := a.store.Derivations(ctx)
	if err != nil {
		a.writeFailure(w, r, err)
		return
	}
	for _, d := range ders {
		resp.Derivations = append(resp.Derivations, MetaDerivation{
			Source: d.Source, Version: d.Version, SourceVersion: d.SourceVersion,
			SourceUpdatedAt: ts(d.SourceUpdatedAt), CreatedAt: ts(d.CreatedAt), Blocks: d.Blocks, Units: d.Units,
			Inserted: d.Inserted, ASNUpdated: d.ASNUpdated, Covered: d.Covered, NotInSource: d.NotInSource,
		})
	}

	workers, err := a.store.Workers(ctx)
	if err != nil {
		a.writeFailure(w, r, err)
		return
	}
	for _, x := range workers {
		resp.Workers = append(resp.Workers, MetaWorker{
			Host: x.Host, Version: x.Version, StartedAt: ts(x.StartedAt), SeenAt: ts(x.SeenAt), Alive: x.Alive,
			RateLimit: x.RateLimit, Queries: x.Queries, LiveQueries: x.LiveQueries, Prefixes: x.Prefixes,
		})
	}

	j, err := a.store.Job(ctx)
	if err != nil {
		a.writeFailure(w, r, err)
		return
	}
	if j != nil {
		resp.Collector = &MetaCollector{
			App: store.CollectorApp, LastSyncAt: tsPtr(j.LastSyncAt), LastCheckAt: tsPtr(j.LastCheckAt),
			Consolidated: j.Consolidated == 1,
		}
	}

	if a.live != nil {
		if n, err := a.live.QueueLen(ctx); err == nil {
			resp.Live.Queue = &n
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

// /health e /status: Postgres fora = error (503); só o Valkey fora =
// degraded (200, a API segue sem cache e sem tempo real); sem prefixos ainda
// = starting (200).
func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	resp := StatusResponse{
		Success: true, Status: "ok", Timestamp: ts(time.Now()), Message: "api-rdns operacional",
		Checks: map[string]string{"postgres": "ok", "valkey": "disabled", "dataset": "ok"},
	}
	code := http.StatusOK

	if !a.data.Current().Ready() {
		resp.Checks["dataset"] = "empty"
		resp.Status, resp.Message = "starting", "aguardando a primeira derivação do collector-rdns"
	}
	var ping func(context.Context) error
	switch {
	case a.cache.Enabled():
		ping = a.cache.Ping
	case a.live != nil:
		ping = a.live.Ping
	}
	if ping != nil {
		if err := ping(ctx); err != nil {
			resp.Checks["valkey"] = "error"
			resp.Status, resp.Message = "degraded", "Valkey indisponível; respondendo sem cache e sem tempo real"
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
