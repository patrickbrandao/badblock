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

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/refresh"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/store"
)

// SourceURL são as specs da fonte, citadas no índice: o rdapnum não tem
// arquivo publicado (é uma sonda ativa).
const SourceURL = "https://github.com/patrickbrandao/badblock/tree/main/specs/fontes/registrobr/rdapnum"

// UpstreamURL é o serviço consultado pelo coletor.
const UpstreamURL = "https://rdap.registro.br/"

// DerivedFrom são as fontes de onde vêm os alvos derivados.
var DerivedFrom = []string{"cgibr"}

func (a *API) handleIndex(w http.ResponseWriter, _ *http.Request) {
	b := a.cfg.BasePath
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, IndexResponse{
		App:      "api-registrobr-rdapnum",
		Version:  a.cfg.Version,
		BasePath: b,
		Versions: []string{CurrentAPIVersion},
		Endpoints: []string{
			b + "/asn/{asn}",
			b + "/asn/{asn}/raw",
			b + "/asn/{asn}/history",
			b + "/ip/{ip}",
			b + "/prefix/{ip}/{len}",
			b + "/prefix/{ip}/{len}/raw",
			b + "/prefix/{ip}/{len}/history",
			b + "/document/{document}",
			b + "/contact/{handle}",
			b + "/response/{uuid}",
			b + "/meta",
			b + "/status",
			b + "/openapi.yaml",
		},
		Source:      SourceURL,
		Upstream:    UpstreamURL,
		DerivedFrom: DerivedFrom,
	})
}

// --- validação ----------------------------------------------------------

func badRequest(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusBadRequest, "bad_request", msg)
}

// parseASN aceita 0 a 4294967295, com ou sem AS/as na frente.
func parseASN(w http.ResponseWriter, raw string) (int64, bool) {
	s := raw
	if len(s) > 2 && (s[:2] == "AS" || s[:2] == "as") {
		s = s[2:]
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil || s == "" || s[0] < '0' || s[0] > '9' {
		badRequest(w, "ASN inválido")
		return 0, false
	}
	return int64(n), true
}

// parseAddr valida um IPv4 ou IPv6; o IPv4 mapeado em IPv6 vira IPv4.
func parseAddr(w http.ResponseWriter, raw string) (netip.Addr, bool) {
	ip, err := netip.ParseAddr(raw)
	if err != nil || ip.Zone() != "" {
		badRequest(w, "endereço IP inválido")
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}

// parsePrefix valida {ip}/{len}: 0 a 32 (IPv4) ou 0 a 128 (IPv6), bits de
// host zerados.
func parsePrefix(w http.ResponseWriter, r *http.Request) (netip.Prefix, bool) {
	ip, ok := parseAddr(w, r.PathValue("ip"))
	if !ok {
		return netip.Prefix{}, false
	}
	raw := r.PathValue("len")
	bits, err := strconv.Atoi(raw)
	if err != nil || bits < 0 || bits > ip.BitLen() || raw != strconv.Itoa(bits) {
		badRequest(w, "tamanho de prefixo inválido")
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(ip, bits).Masked(), true
}

var (
	handleRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	uuidRE   = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	docRE    = regexp.MustCompile(`^[0-9./ -]+$`)
)

// parseDocument aceita CNPJ ou CPF com ou sem pontuação (. / - e espaço);
// devolve só os dígitos (14 ou 11).
func parseDocument(w http.ResponseWriter, raw string) (string, bool) {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, raw)
	if !docRE.MatchString(raw) || (len(digits) != 11 && len(digits) != 14) {
		badRequest(w, "documento inválido: use CNPJ (14 dígitos) ou CPF (11)")
		return "", false
	}
	return digits, true
}

// --- dados processados ----------------------------------------------------

func asnLabel(asn int64) string { return "AS" + strconv.FormatInt(asn, 10) }

// GET /asn/{asn}
func (a *API) handleASN(w http.ResponseWriter, r *http.Request) {
	asn, ok := parseASN(w, r.PathValue("asn"))
	if !ok {
		return
	}
	a.serveCached(w, r, "asn:"+strconv.FormatInt(asn, 10), cachePublic, func(ctx context.Context) (any, error) {
		row, err := a.store.ASN(ctx, asn)
		if errors.Is(err, store.ErrNotFound) {
			return nil, notFound(asnLabel(asn) + " não é alvo do rdapnum")
		}
		if err != nil {
			return nil, err
		}
		return asnResponse(asn, row), nil
	})
}

// serveNetwork responde /ip e /prefix: o bloco registrado mais específico que
// contém o pedido.
func (a *API) serveNetwork(w http.ResponseWriter, r *http.Request, key, query string, q netip.Prefix) {
	a.serveCached(w, r, key, cachePublic, func(ctx context.Context) (any, error) {
		row, err := a.store.Network(ctx, q)
		if errors.Is(err, store.ErrNotFound) {
			return nil, notFound(query + " não está em nenhum bloco do rdapnum")
		}
		if err != nil {
			return nil, err
		}
		return networkResponse(query, row), nil
	})
}

// GET /ip/{ip}
func (a *API) handleIP(w http.ResponseWriter, r *http.Request) {
	ip, ok := parseAddr(w, r.PathValue("ip"))
	if !ok {
		return
	}
	a.serveNetwork(w, r, "ip:"+ip.String(), ip.String(), netip.PrefixFrom(ip, ip.BitLen()))
}

// GET /prefix/{ip}/{len}
func (a *API) handlePrefix(w http.ResponseWriter, r *http.Request) {
	q, ok := parsePrefix(w, r)
	if !ok {
		return
	}
	a.serveNetwork(w, r, "prefix:"+q.String(), q.String(), q)
}

// GET /document/{document}
func (a *API) handleDocument(w http.ResponseWriter, r *http.Request) {
	digits, ok := parseDocument(w, r.PathValue("document"))
	if !ok {
		return
	}
	a.serveCached(w, r, "document:"+digits, cachePublic, func(ctx context.Context) (any, error) {
		row, err := a.store.Document(ctx, digits)
		if errors.Is(err, store.ErrNotFound) {
			return nil, notFound("documento " + digits + " não encontrado no rdapnum")
		}
		if err != nil {
			return nil, err
		}
		return documentResponse(digits, row), nil
	})
}

// GET /contact/{handle}
func (a *API) handleContact(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")
	if !handleRE.MatchString(handle) {
		badRequest(w, "handle inválido")
		return
	}
	a.serveCached(w, r, "contact:"+handle, cachePublic, func(ctx context.Context) (any, error) {
		row, err := a.store.Contact(ctx, handle)
		if errors.Is(err, store.ErrNotFound) {
			return nil, notFound("contato " + handle + " não encontrado no rdapnum")
		}
		if err != nil {
			return nil, err
		}
		return contactResponse(row), nil
	})
}

// --- resposta crua --------------------------------------------------------

// targetLookup acha o alvo de uma rota de ASN ou de bloco; label é o pedido
// como aparece nas mensagens.
type targetLookup struct {
	key   string // parte da chave de cache: "asn:61610" ou "prefix:200.225.48.0/24"
	label string // "AS61610" ou "200.225.48.0/24"
	find  func(ctx context.Context) (*store.Target, error)
}

func (a *API) asnLookup(w http.ResponseWriter, r *http.Request) (targetLookup, bool) {
	asn, ok := parseASN(w, r.PathValue("asn"))
	if !ok {
		return targetLookup{}, false
	}
	return targetLookup{
		key: "asn", label: asnLabel(asn),
		find: func(ctx context.Context) (*store.Target, error) { return a.store.TargetASN(ctx, asn) },
	}, true
}

func (a *API) prefixLookup(w http.ResponseWriter, r *http.Request) (targetLookup, bool) {
	q, ok := parsePrefix(w, r)
	if !ok {
		return targetLookup{}, false
	}
	return targetLookup{
		key: "prefix", label: q.String(),
		find: func(ctx context.Context) (*store.Target, error) { return a.store.TargetPrefix(ctx, q) },
	}, true
}

// targetOf roda a busca e traduz "sem alvo" no 404 da rota.
func (t targetLookup) targetOf(ctx context.Context) (*store.Target, error) {
	tg, err := t.find(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return nil, notFound(t.label + " não é alvo do rdapnum")
	}
	return tg, err
}

// targetLabel é o alvo como aparece nas mensagens: "AS61610" ou o bloco.
func targetLabel(t *store.Target) string {
	if t.ASN != nil {
		return asnLabel(*t.ASN)
	}
	return t.Name()
}

// rawEntryOf monta a resposta crua de uma versão: o corpo RDAP sem as linhas
// do IP de saída, ou o JSON curto de uma not_found.
func rawEntryOf(resp *store.Response) (*entry, error) {
	e := &entry{responseUUID: resp.UUID, lastModified: resp.LastSeenAt.UTC().Format(http.TimeFormat)}
	if resp.Result == store.ResultNotFound || resp.Body == nil {
		b, err := render(NotFoundResponse{Result: "not_found", HTTPStatus: resp.HTTPStatus})
		if err != nil {
			return nil, err
		}
		e.body = b.body
		return e, nil
	}
	body, err := publicRDAP(*resp.Body)
	if err != nil {
		return nil, err
	}
	e.body = body
	e.contentType = "application/rdap+json"
	return e, nil
}

func (a *API) serveRaw(w http.ResponseWriter, r *http.Request, t targetLookup) {
	a.serveCached(w, r, t.key+"-raw:"+strings.TrimPrefix(t.label, "AS"), cachePublic, func(ctx context.Context) (any, error) {
		tg, err := t.targetOf(ctx)
		if err != nil {
			return nil, err
		}
		missing := notFound(targetLabel(tg) + " sem resposta coletada")
		if tg.ResponseUUID == nil {
			return nil, missing
		}
		resp, err := a.store.Response(ctx, *tg.ResponseUUID)
		if errors.Is(err, store.ErrNotFound) {
			return nil, missing
		}
		if err != nil {
			return nil, err
		}
		// A resposta atual é sempre conclusiva; uma 404 (ou, por defesa,
		// qualquer coisa que não seja completa) não tem corpo para mostrar.
		if resp.Result != store.ResultComplete || resp.Body == nil {
			return nil, missing
		}
		return rawEntryOf(resp)
	})
}

func (a *API) serveHistory(w http.ResponseWriter, r *http.Request, t targetLookup) {
	a.serveCached(w, r, t.key+"-history:"+strings.TrimPrefix(t.label, "AS"), cachePublic, func(ctx context.Context) (any, error) {
		tg, err := t.targetOf(ctx)
		if err != nil {
			return nil, err
		}
		rows, err := a.store.History(ctx, tg.UUID)
		if err != nil {
			return nil, err
		}
		return historyResponse(tg, rows), nil
	})
}

// GET /asn/{asn}/raw
func (a *API) handleASNRaw(w http.ResponseWriter, r *http.Request) {
	if t, ok := a.asnLookup(w, r); ok {
		a.serveRaw(w, r, t)
	}
}

// GET /asn/{asn}/history
func (a *API) handleASNHistory(w http.ResponseWriter, r *http.Request) {
	if t, ok := a.asnLookup(w, r); ok {
		a.serveHistory(w, r, t)
	}
}

// GET /prefix/{ip}/{len}/raw
func (a *API) handlePrefixRaw(w http.ResponseWriter, r *http.Request) {
	if t, ok := a.prefixLookup(w, r); ok {
		a.serveRaw(w, r, t)
	}
}

// GET /prefix/{ip}/{len}/history
func (a *API) handlePrefixHistory(w http.ResponseWriter, r *http.Request) {
	if t, ok := a.prefixLookup(w, r); ok {
		a.serveHistory(w, r, t)
	}
}

// GET /response/{uuid} — uma versão do histórico; o uuid é imutável.
func (a *API) handleResponse(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("uuid")
	if !uuidRE.MatchString(raw) {
		badRequest(w, "uuid inválido")
		return
	}
	id := strings.ToLower(raw)
	a.serveCached(w, r, "response:"+id, cacheImmutable, func(ctx context.Context) (any, error) {
		resp, err := a.store.Response(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			return nil, notFound("resposta " + id + " não encontrada")
		}
		if err != nil {
			return nil, err
		}
		return rawEntryOf(resp)
	})
}

// --- atualização ----------------------------------------------------------

// POST /asn/{asn}/refresh
func (a *API) handleASNRefresh(w http.ResponseWriter, r *http.Request) {
	if t, ok := a.asnLookup(w, r); ok {
		a.serveRefresh(w, r, t)
	}
}

// POST /prefix/{ip}/{len}/refresh
func (a *API) handlePrefixRefresh(w http.ResponseWriter, r *http.Request) {
	if t, ok := a.prefixLookup(w, r); ok {
		a.serveRefresh(w, r, t)
	}
}

// serveRefresh põe o alvo na fila de atualização (202), sem esperar a coleta.
func (a *API) serveRefresh(w http.ResponseWriter, r *http.Request, t targetLookup) {
	w.Header().Set("X-Cache", "BYPASS")
	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.DBTimeout)
	defer cancel()
	tg, err := t.targetOf(ctx)
	if err != nil {
		a.writeFailure(w, r, err)
		return
	}
	label := targetLabel(tg)
	if !tg.Active {
		writeError(w, http.StatusConflict, "target_inactive", label+" está desativado no rdapnum")
		return
	}
	if a.refresh == nil {
		writeError(w, http.StatusServiceUnavailable, "refresh_unavailable", "atualização indisponível: Valkey fora do ar")
		return
	}
	out, err := a.refresh.Request(ctx, tg.Kind, tg.Name(), tg.LastScanAt)
	switch {
	case errors.Is(err, refresh.ErrQueueFull):
		writeError(w, http.StatusServiceUnavailable, "refresh_unavailable", "fila de atualização cheia; tente mais tarde")
		return
	case err != nil:
		a.log.Warn("Valkey indisponível na atualização", "err", err)
		writeError(w, http.StatusServiceUnavailable, "refresh_unavailable", "atualização indisponível: Valkey fora do ar")
		return
	}
	a.log.Info("atualização pedida", "kind", tg.Kind, "target", tg.Name(), "queued", out.Queued, "reason", out.Reason)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusAccepted, RefreshResponse{
		Kind: tg.Kind, Target: tg.Name(), Queued: out.Queued, Reason: strPtr(out.Reason), CheckedAt: tsPtr(tg.LastScanAt),
	})
}

// --- meta e saúde ---------------------------------------------------------

// GET /meta — estado da coleta, das respostas, das derivações e das
// instâncias (sem cache, lido direto do banco).
func (a *API) handleMeta(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.DBTimeout)
	defer cancel()
	resp := MetaResponse{App: "api-registrobr-rdapnum", Version: a.cfg.Version,
		Derivations: []MetaDerivation{}, Workers: []MetaWorker{}}

	cfg, err := a.store.Config(ctx)
	if err != nil {
		a.writeFailure(w, r, err)
		return
	}
	interval := 168 * time.Hour
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

	ts0, err := a.store.TargetStats(ctx, interval)
	if err != nil {
		a.writeFailure(w, r, err)
		return
	}
	resp.Targets = MetaTargets{
		Total: ts0.Total, ASN: ts0.ASN, Prefix: ts0.Prefix, Active: ts0.Active, Static: ts0.Static,
		NeverScanned: ts0.NeverScanned, Overdue: ts0.Overdue, Rescan: ts0.Rescan, Scanning: ts0.Scanning,
		Waiting: ts0.Waiting, OldestScanAt: tsPtr(ts0.OldestScanAt), Complete: ts0.Complete,
		Truncated: ts0.Truncated, NotFound: ts0.NotFound, Error: ts0.Error,
	}
	rs, err := a.store.ResponseStats(ctx)
	if err != nil {
		a.writeFailure(w, r, err)
		return
	}
	resp.Responses = MetaResponses{Total: rs.Total, Complete: rs.Complete, Truncated: rs.Truncated,
		NotFound: rs.NotFound, Pending: rs.Pending, ProcessErrors: rs.ProcessErrors}
	ds, err := a.store.DataStats(ctx)
	if err != nil {
		a.writeFailure(w, r, err)
		return
	}
	resp.Data = MetaData{Autnum: ds.Autnum, Network: ds.Network, Entity: ds.Entity, Reverse: ds.Reverse}

	ders, err := a.store.Derivations(ctx)
	if err != nil {
		a.writeFailure(w, r, err)
		return
	}
	for _, d := range ders {
		resp.Derivations = append(resp.Derivations, MetaDerivation{
			Source: d.Source, Version: d.Version, SourceVersion: d.SourceVersion,
			SourceUpdatedAt: ts(d.SourceUpdatedAt), CreatedAt: ts(d.CreatedAt), ASNs: d.ASNs,
			Prefixes: d.Prefixes, Inserted: d.Inserted, NotInSource: d.NotInSource,
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
			RateLimit: x.RateLimit, Proxy: x.Proxy, Requests: x.Requests, Complete: x.Complete,
			Truncated: x.Truncated, NotFound: x.NotFound, Errors: x.Errors, Refreshes: x.Refreshes,
			EgressIPs: x.EgressIPs, PausedUntil: tsPtr(x.PausedUntil),
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

	if a.refresh != nil {
		if n, err := a.refresh.QueueLen(ctx); err == nil {
			resp.Refresh.Queue = &n
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

// /health e /status: Postgres fora = error (503); só o Valkey fora =
// degraded (200, a API segue sem cache e sem atualização); sem alvos ainda =
// starting (200).
func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	resp := StatusResponse{
		Success: true, Status: "ok", Timestamp: ts(time.Now()), Message: "api-registrobr-rdapnum operacional",
		Checks: map[string]string{"postgres": "ok", "valkey": "disabled", "dataset": "ok"},
	}
	code := http.StatusOK

	if !a.data.Current().Ready() {
		resp.Checks["dataset"] = "empty"
		resp.Status, resp.Message = "starting", "aguardando a primeira derivação do collector-registrobr-rdapnum"
	}
	var ping func(context.Context) error
	switch {
	case a.cache.Enabled():
		ping = a.cache.Ping
	case a.refresh != nil:
		ping = a.refresh.Ping
	}
	if ping != nil {
		if err := ping(ctx); err != nil {
			resp.Checks["valkey"] = "error"
			resp.Status, resp.Message = "degraded", "Valkey indisponível; respondendo sem cache e sem atualização"
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
