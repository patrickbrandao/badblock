package httpapi

import (
	"cmp"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/refresh"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/store"
)

// Os dados falsos reproduzem as respostas reais de 2026-10-21 da spec
// (api-rotas.md), com o contato pessoa física trocado por CONTATO EXEMPLO.

const base = "/registrobr/rdapnum"

func tm(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &t
}

func sp(s string) *string { return &s }
func ip64(n int64) *int64 { return &n }
func i16(n int16) *int16  { return &n }
func pfx(s string) *netip.Prefix {
	p := netip.MustParsePrefix(s)
	return &p
}

const (
	r61610      = "019a0a4e-7c11-7a3e-9b51-6d1c0e7b2a10"
	r61610old   = "019a0a10-0000-7000-8000-000000000001"
	r200225     = "019a0a4e-8d22-7b4f-8c62-7e2d1f8c3b21"
	r2804       = "019a0a4e-9e33-7c50-9d73-8f3e209d4c32"
	r64501      = "019a0a20-0000-7000-8000-000000000404"
	checked     = "2026-10-21T20:36:01Z"
	notice61610 = `"notices": [{"title": "Use and Privacy Policy", "description": ["Copyright (c) Nic.br - Use of this data is governed by the Use and Privacy Policy. Distribution, commercialization, reproduction, and use for advertising or similar purposes are expressly prohibited.", "2026-10-21T17:36:01-03:00 - 192.0.2.1"]}]`
)

// body61610 é o corpo como o jsonb do Postgres devolve (chaves em ordem de
// tamanho, com espaços).
const body61610 = `{"name": "ASN61610", "type": "DIRECT ALLOCATION", "handle": "61610", "port43": "whois.nic.br", "country": "BR", "endAutnum": 61610, ` +
	notice61610 + `, "startAutnum": 61610, "objectClassName": "autnum"}`

var (
	elea = store.Entity{
		Handle: "35980592000130", Kind: sp("org"), Name: sp("ELEA DATA CENTERS"), Country: sp("BR"),
		Document: sp("35.980.592/0001-30"), DocumentType: sp("cnpj"), DocumentDigits: sp("35980592000130"),
		LegalRepresentative: sp("REPRESENTANTE EXEMPLO"),
		RegisteredAt:        tm("2023-02-06T21:58:30Z"), ChangedAt: tm("2026-04-22T15:11:18Z"),
	}
	robel = store.Entity{
		Handle: "ROBEL308", Kind: sp("individual"), Name: sp("CONTATO EXEMPLO"), Email: sp("contato@example.com"),
		Country: sp("BR"), RegisteredAt: tm("2023-02-08T17:39:14Z"), ChangedAt: tm("2026-07-07T18:30:11Z"),
	}
)

// fakeLink é uma ligação: o objeto (asn ou bloco), o papel e a via.
type fakeLink struct {
	entity *store.Entity
	asn    int64
	prefix string
	role   string
	via    *string
}

type fakeNetwork struct {
	net      store.Network
	target   string // uuid do alvo
	reverses []store.Reverse
}

// fakeStore guarda as tabelas em memória.
type fakeStore struct {
	mu        sync.Mutex
	calls     int
	pingErr   error
	queryErr  error
	targets   []store.Target
	autnums   map[int64]store.Autnum
	networks  []fakeNetwork
	entities  []*store.Entity
	links     []fakeLink
	responses []store.ResponseMeta
	respTgt   map[string]string // uuid da resposta → uuid do alvo
	bodies    map[string]*string
	cfg       map[string]string
}

func newFakeStore() *fakeStore {
	t := func(uuid, kind string, asn *int64, prefix *netip.Prefix, source string, active bool, scan *time.Time,
		result *int16, retry *time.Time, resp *string) store.Target {
		return store.Target{UUID: uuid, Kind: kind, ASN: asn, Prefix: prefix, Source: source, Active: active,
			LastScanAt: scan, LastAttemptAt: scan, LastResult: result, RetryAt: retry, ResponseUUID: resp}
	}
	f := &fakeStore{
		targets: []store.Target{
			t("t-61610", "asn", ip64(61610), nil, "cgibr", true, tm(checked), i16(1), nil, sp(r61610)),
			t("t-200225", "prefix", nil, pfx("200.225.48.0/21"), "cgibr", true, tm(checked), i16(1), nil, sp(r200225)),
			t("t-187", "prefix", nil, pfx("187.87.28.0/22"), "cgibr", true, tm(checked), i16(1), nil, nil),
			t("t-64500", "asn", ip64(64500), nil, "static", true, nil, nil, nil, nil),
			t("t-64501", "asn", ip64(64501), nil, "cgibr", false, tm("2026-10-20T10:00:00Z"), i16(3), nil, sp(r64501)),
			t("t-64502", "asn", ip64(64502), nil, "cgibr", true, nil, i16(4), tm("2026-10-21T21:00:00Z"), nil),
		},
		autnums: map[int64]store.Autnum{
			61610: {ASN: 61610, Handle: sp("61610"), Name: sp("ASN61610"), Type: sp("DIRECT ALLOCATION"), Country: sp("BR"),
				RegisteredAt: tm("2023-02-27T15:58:28Z"), ChangedAt: tm("2023-02-27T15:58:28Z")},
		},
		entities: []*store.Entity{&elea, &robel},
		respTgt:  map[string]string{},
		bodies:   map[string]*string{},
		cfg:      map[string]string{"rate_limit": "48", "scan_interval": "lixo"},
	}
	// O bloco IPv6 vem de um alvo que a última tentativa truncou: o dado é o
	// da última completa.
	v6 := t("t-2804", "prefix", nil, pfx("2804:8ae0::/32"), "cgibr", true, tm("2026-10-14T08:00:00Z"), i16(2),
		tm("2026-10-21T21:05:00Z"), sp(r2804))
	v6.LastAttemptAt = tm("2026-10-21T21:00:00Z")
	f.targets = append(f.targets, v6)

	n := func(prefix, handle, name string, parent *string, target string, rev ...store.Reverse) fakeNetwork {
		return fakeNetwork{net: store.Network{Prefix: netip.MustParsePrefix(prefix), Handle: sp(handle), Name: sp(name),
			Type: sp("ALLOCATED"), Country: sp("BR"), Status: []string{"active"}, ParentHandle: parent, ASN: ip64(61610),
			RegisteredAt: tm("1999-11-18T12:00:00Z"), ChangedAt: tm("2023-07-19T11:05:57Z")}, target: target, reverses: rev}
	}
	ns := func(name string) store.Nameserver {
		return store.Nameserver{Name: sp(name), Status: []string{"ns aa"}, CheckedAt: sp("2026-10-21T12:00:00Z"), LastOKAt: sp("2026-10-21T12:00:00Z")}
	}
	f.networks = []fakeNetwork{
		n("187.87.28.0/22", "187.87.28.0/22", "462540", nil, "t-187"),
		n("200.225.48.0/21", "200.225.48.0/21", "462539", nil, "t-200225", store.Reverse{
			Prefix: pfx("200.225.48.0/24"), Start: "200.225.48.0", End: "200.225.48.255",
			Nameservers: []store.Nameserver{ns("keenan.ns.cloudflare.com"), ns("sureena.ns.cloudflare.com")},
			Signed:      new(bool), RegisteredAt: tm("2024-09-27T15:31:56Z"), ChangedAt: tm("2024-09-27T15:31:56Z"),
		}),
		n("2804:8ae0::/32", "2804:8ae0::/32", "462541", nil, "t-2804", store.Reverse{
			Start: "2804:8ae0:4000::", End: "2804:8ae0:40ff:ffff:ffff:ffff:ffff:ffff", Nameservers: nil,
		}),
	}
	f.links = []fakeLink{
		{entity: &elea, asn: 61610, role: "registrant"},
		{entity: &robel, asn: 61610, role: "administrative"},
		{entity: &robel, asn: 61610, role: "abuse"},
		{entity: &robel, asn: 61610, role: "administrative", via: sp("35980592000130")},
		{entity: &elea, prefix: "200.225.48.0/21", role: "registrant"},
		{entity: &robel, prefix: "200.225.48.0/21", role: "technical"},
		{entity: &robel, prefix: "200.225.48.0/21", role: "abuse"},
		{entity: &elea, prefix: "187.87.28.0/22", role: "registrant"},
	}
	resp := func(uuid, target string, result, status int16, fetched, seen string, n int, processed bool, body *string) {
		f.responses = append(f.responses, store.ResponseMeta{UUID: uuid, Result: result, HTTPStatus: status,
			SHA256: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", Bytes: 2697,
			FetchedAt: *tm(fetched), LastSeenAt: *tm(seen), SeenCount: n, Processed: processed})
		f.respTgt[uuid] = target
		f.bodies[uuid] = body
	}
	b61610 := body61610
	trunc := `{"handle": "61610", "remarks": [{"type": "object truncated due to server policy", "description": ["Some of the data in this object has been removed."]}], ` +
		`"notices": [{"title": "Use and Privacy Policy", "description": ["Copyright (c) Nic.br", "2026-10-14T05:00:00-03:00 - 192.0.2.7"]}], "objectClassName": "autnum"}`
	b200 := `{"handle": "200.225.48.0/21", "notices": [{"description": ["Copyright (c) Nic.br", "2026-10-21T17:36:02-03:00 - 2001:db8::7"]}], "objectClassName": "ip network"}`
	b2804 := `{"handle": "2804:8ae0::/32", "objectClassName": "ip network"}`
	resp(r61610old, "t-61610", 2, 200, "2026-10-14T08:00:00Z", "2026-10-14T08:00:00Z", 1, false, &trunc)
	resp(r61610, "t-61610", 1, 200, "2026-10-15T09:00:00Z", checked, 3, true, &b61610)
	resp(r200225, "t-200225", 1, 200, checked, checked, 1, true, &b200)
	resp(r2804, "t-2804", 1, 200, "2026-10-14T08:00:00Z", "2026-10-14T08:00:00Z", 1, true, &b2804)
	resp(r64501, "t-64501", 3, 404, "2026-10-20T10:00:00Z", "2026-10-20T10:00:00Z", 1, true, nil)
	return f
}

func (f *fakeStore) count() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.queryErr
}

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }

func (f *fakeStore) target(uuid string) store.Target {
	for _, t := range f.targets {
		if t.UUID == uuid {
			return t
		}
	}
	panic("alvo inexistente: " + uuid)
}

func (f *fakeStore) TargetASN(_ context.Context, asn int64) (*store.Target, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	for _, t := range f.targets {
		if t.ASN != nil && *t.ASN == asn {
			return &t, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) network(q netip.Prefix) *fakeNetwork {
	var best *fakeNetwork
	for i, n := range f.networks {
		p := n.net.Prefix
		if p.Bits() <= q.Bits() && p.Contains(q.Addr()) && (best == nil || p.Bits() > best.net.Prefix.Bits()) {
			best = &f.networks[i]
		}
	}
	return best
}

func (f *fakeStore) TargetPrefix(_ context.Context, q netip.Prefix) (*store.Target, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	for _, t := range f.targets {
		if t.Prefix != nil && *t.Prefix == q {
			return &t, nil
		}
	}
	if n := f.network(q); n != nil {
		t := f.target(n.target)
		return &t, nil
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) linksOf(asn int64, prefix string) []store.Link {
	var out []store.Link
	for _, l := range f.links {
		if (asn != 0 && l.asn == asn) || (prefix != "" && l.prefix == prefix) {
			out = append(out, store.Link{Entity: *l.entity, Role: l.role, Via: l.via})
		}
	}
	return out
}

func (f *fakeStore) ASN(ctx context.Context, asn int64) (*store.ASNRow, error) {
	t, err := f.TargetASN(ctx, asn)
	if err != nil {
		return nil, err
	}
	row := &store.ASNRow{Target: *t}
	a, ok := f.autnums[asn]
	if !ok {
		return row, nil
	}
	row.Autnum = &a
	row.Registrant = &elea
	row.Links = f.linksOf(asn, "")
	for _, n := range f.networks {
		if n.net.ASN != nil && *n.net.ASN == asn {
			row.Networks = append(row.Networks, store.NetworkBrief{Prefix: n.net.Prefix, ASN: n.net.ASN, Type: n.net.Type, Status: n.net.Status})
		}
	}
	return row, nil
}

func (f *fakeStore) Network(_ context.Context, q netip.Prefix) (*store.NetworkRow, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	n := f.network(q)
	if n == nil {
		return nil, store.ErrNotFound
	}
	return &store.NetworkRow{Network: n.net, Target: f.target(n.target), Registrant: &elea,
		Links: f.linksOf(0, n.net.Prefix.String()), Reverses: n.reverses}, nil
}

func (f *fakeStore) Document(_ context.Context, digits string) (*store.DocumentRow, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	var r store.DocumentRow
	for _, e := range f.entities {
		if e.DocumentDigits != nil && *e.DocumentDigits == digits {
			r.Entities = append(r.Entities, *e)
		}
	}
	if len(r.Entities) == 0 {
		return nil, store.ErrNotFound
	}
	for _, l := range f.links {
		if l.role != "registrant" || l.entity.DocumentDigits == nil || *l.entity.DocumentDigits != digits {
			continue
		}
		if l.asn != 0 {
			a := f.autnums[l.asn]
			r.ASNs = append(r.ASNs, store.DocumentASN{ASN: l.asn, Name: a.Name, LastScanAt: tm(checked)})
		} else {
			n := f.network(netip.MustParsePrefix(l.prefix))
			r.Networks = append(r.Networks, store.NetworkBrief{Prefix: n.net.Prefix, ASN: n.net.ASN, Type: n.net.Type, Status: n.net.Status})
		}
	}
	slices.SortFunc(r.Networks, func(a, b store.NetworkBrief) int { return a.Prefix.Addr().Compare(b.Prefix.Addr()) })
	return &r, nil
}

func (f *fakeStore) Contact(_ context.Context, handle string) (*store.ContactRow, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	for _, e := range f.entities {
		if e.Handle != handle {
			continue
		}
		r := &store.ContactRow{Entity: *e}
		for _, l := range f.links {
			if l.entity.Handle != handle {
				continue
			}
			o := store.ContactObject{Kind: "asn", Role: l.role, Via: l.via}
			if l.asn != 0 {
				o.ASN = ip64(l.asn)
			} else {
				o.Kind, o.Prefix = "prefix", pfx(l.prefix)
			}
			r.Objects = append(r.Objects, o)
		}
		// A ordem do banco: tipo, objeto, via (null primeiro), papel.
		slices.SortStableFunc(r.Objects, func(a, b store.ContactObject) int {
			if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
				return c
			}
			if a.ASN != nil && b.ASN != nil {
				if c := cmp.Compare(*a.ASN, *b.ASN); c != 0 {
					return c
				}
			}
			if a.Prefix != nil && b.Prefix != nil {
				if c := a.Prefix.Addr().Compare(b.Prefix.Addr()); c != 0 {
					return c
				}
			}
			if c := cmpVia(a.Via, b.Via); c != 0 {
				return c
			}
			return cmp.Compare(a.Role, b.Role)
		})
		return r, nil
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) Response(_ context.Context, uuid string) (*store.Response, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	for _, r := range f.responses {
		if r.UUID == uuid {
			return &store.Response{UUID: r.UUID, TargetUUID: f.respTgt[uuid], Result: r.Result, HTTPStatus: r.HTTPStatus,
				Body: f.bodies[uuid], LastSeenAt: r.LastSeenAt}, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) History(_ context.Context, target string) ([]store.ResponseMeta, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	var out []store.ResponseMeta
	for _, r := range f.responses {
		if f.respTgt[r.UUID] == target {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b store.ResponseMeta) int { return b.FetchedAt.Compare(a.FetchedAt) })
	return out, nil
}

func (f *fakeStore) Config(context.Context) (map[string]string, error) { return f.cfg, nil }

func (f *fakeStore) TargetStats(_ context.Context, interval time.Duration) (*store.TargetStats, error) {
	if interval != 168*time.Hour {
		return nil, errors.New("intervalo inesperado")
	}
	return &store.TargetStats{Total: 7, ASN: 4, Prefix: 3, Active: 6, Static: 1, NeverScanned: 2, Overdue: 0, Rescan: 0,
		Scanning: 1, Waiting: 2, OldestScanAt: tm("2026-10-14T08:00:00Z"), Complete: 3, Truncated: 1, NotFound: 1, Error: 1}, nil
}

func (f *fakeStore) ResponseStats(context.Context) (*store.ResponseStats, error) {
	return &store.ResponseStats{Total: 5, Complete: 3, Truncated: 1, NotFound: 1}, nil
}

func (f *fakeStore) DataStats(context.Context) (*store.DataStats, error) {
	return &store.DataStats{Autnum: 1, Network: 3, Entity: 2, Reverse: 2}, nil
}

func (f *fakeStore) Derivations(context.Context) ([]store.Derivation, error) {
	n := func(v int) *int { return &v }
	return []store.Derivation{{Source: "cgibr", Version: "019a0a00-0000-7000-8000-000000000001",
		SourceVersion: "019a09f0-0000-7000-8000-000000000001", SourceUpdatedAt: *tm("2026-10-21T03:00:00Z"),
		CreatedAt: *tm("2026-10-21T03:05:00Z"), ASNs: n(9134), Prefixes: n(21991), Inserted: n(31125), NotInSource: n(0)}}, nil
}

func (f *fakeStore) Workers(context.Context) ([]store.Worker, error) {
	return []store.Worker{{Host: "1f2e3d4c5b6a", Version: "0.1.0", StartedAt: *tm("2026-10-21T19:00:00Z"),
		SeenAt: *tm("2026-10-21T21:00:00Z"), Alive: true, RateLimit: 32, Proxy: true, Requests: 900, Complete: 850,
		Truncated: 30, NotFound: 15, Errors: 5, Refreshes: 2, EgressIPs: 37}}, nil
}

func (f *fakeStore) Job(context.Context) (*store.Job, error) {
	return &store.Job{LastSyncAt: tm(checked), LastCheckAt: tm(checked)}, nil
}

type fixedView struct{ snap dataset.Snapshot }

func (v fixedView) Current() dataset.Snapshot { return v.snap }

// memCache é um cache em memória para os testes.
type memCache struct {
	mu   sync.Mutex
	m    map[string][]byte
	down bool
}

func (c *memCache) Get(_ context.Context, k string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	return v, ok
}
func (c *memCache) Set(_ context.Context, k string, v []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[k] = v
}
func (c *memCache) Ping(context.Context) error {
	if c.down {
		return errors.New("down")
	}
	return nil
}
func (c *memCache) Enabled() bool { return true }

func (c *memCache) get(k string) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[k]
}

// fakeRefresher registra os pedidos de atualização.
type fakeRefresher struct {
	mu      sync.Mutex
	out     refresh.Outcome
	err     error
	pingErr error
	queue   int64
	calls   []refresh.Request
	scans   []*time.Time
}

func (f *fakeRefresher) Request(_ context.Context, kind, target string, last *time.Time) (refresh.Outcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, refresh.Request{Kind: kind, Target: target})
	f.scans = append(f.scans, last)
	return f.out, f.err
}
func (f *fakeRefresher) QueueLen(context.Context) (int64, error) { return f.queue, nil }
func (f *fakeRefresher) Ping(context.Context) error              { return f.pingErr }

func newTestAPI(t *testing.T, st *fakeStore, ready bool, c cache.Cache, rf Refresher) *API {
	t.Helper()
	rip, err := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	if err != nil {
		t.Fatal(err)
	}
	return New(st, fixedView{dataset.Snapshot{HasTargets: ready}}, c, rf, rip,
		slog.New(slog.NewTextHandler(io.Discard, nil)), Config{BasePath: base + "/", Version: "test"})
}

func newAPI(t *testing.T, st *fakeStore, ready bool) (http.Handler, *memCache) {
	t.Helper()
	c := &memCache{m: map[string][]byte{}}
	return newTestAPI(t, st, ready, c, nil).Handler(), c
}

func do(h http.Handler, method, path string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
