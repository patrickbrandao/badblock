package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/iana/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/iana/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/iana/api/internal/store"
)

// --- dados: recorte das linhas reais da IANA (2026-09-28) -------------------

var (
	arin    = new("arin")
	lacnic  = new("lacnic")
	apnic   = new("apnic")
	arinURL = []string{"https://rdap.arin.net/registry", "http://rdap.arin.net/registry"}
)

func sprefix(p, name string, gr *bool, termination *string) store.SpecialPrefix {
	return store.SpecialPrefix{Prefix: netip.MustParsePrefix(p), Name: name, GloballyReachable: gr, TerminationDate: termination}
}

var specials = []store.SpecialPrefix{
	sprefix("0.0.0.0/8", `"This network"`, new(false), nil),
	sprefix("0.0.0.0/32", `"This host on this network"`, new(false), nil),
	sprefix("10.0.0.0/8", "Private-Use", new(false), nil),
	sprefix("100.64.0.0/10", "Shared Address Space", new(false), nil),
	sprefix("127.0.0.0/8", "Loopback", new(false), nil),
	sprefix("192.0.0.0/24", "IETF Protocol Assignments", new(false), nil),
	sprefix("192.0.0.9/32", "Port Control Protocol Anycast", new(true), nil),
	sprefix("192.0.2.0/24", "Documentation (TEST-NET-1)", new(false), nil),
	sprefix("192.88.99.0/24", "Deprecated (6to4 Relay Anycast)", nil, new("2015-03")),
	sprefix("192.88.99.2/32", "6a44-relay anycast address", new(false), nil),
	sprefix("192.168.0.0/16", "Private-Use", new(false), nil),
	sprefix("240.0.0.0/4", "Reserved", new(false), nil),
	sprefix("::1/128", "Loopback Address", new(false), nil),
	sprefix("64:ff9b::/96", "IPv4-IPv6 Translat.", new(true), nil),
	sprefix("2001::/23", "IETF Protocol Assignments", new(false), nil),
	sprefix("2001::/32", "TEREDO", nil, nil),
	sprefix("2001:10::/28", "Deprecated (previously ORCHID)", nil, new("2014-03")),
	sprefix("2001:db8::/32", "Documentation", new(false), nil),
	sprefix("2002::/16", "6to4", nil, nil),
	sprefix("3fff::/20", "Documentation", new(false), nil),
	sprefix("fc00::/7", "Unique-Local", new(false), nil),
	sprefix("fe80::/10", "Link-Local Unicast", new(false), nil),
}

func pblock(p, designation, status string, registry *string) store.PrefixBlock {
	return store.PrefixBlock{Prefix: netip.MustParsePrefix(p), Designation: designation, Status: status, Registry: registry}
}

var blocks = func() []store.PrefixBlock {
	out := []store.PrefixBlock{
		pblock("0.0.0.0/8", "IANA - Local Identification", "RESERVED", nil),
		pblock("8.0.0.0/8", "Administered by ARIN", "LEGACY", arin),
		pblock("10.0.0.0/8", "IANA - Private Use", "RESERVED", nil),
		pblock("100.0.0.0/8", "ARIN", "ALLOCATED", arin),
		pblock("127.0.0.0/8", "IANA - Loopback", "RESERVED", nil),
		pblock("187.0.0.0/8", "LACNIC", "ALLOCATED", lacnic),
		pblock("192.0.0.0/8", "Administered by ARIN", "LEGACY", arin),
	}
	for i := 224; i <= 255; i++ {
		d := "Multicast"
		if i >= 240 {
			d = "Future use"
		}
		out = append(out, pblock(fmt.Sprintf("%d.0.0.0/8", i), d, "RESERVED", nil))
	}
	b := pblock("2001::/23", "IANA", "ALLOCATED", nil)
	b.Whois = new("whois.iana.org")
	return append(out, b,
		pblock("2001:c00::/23", "APNIC", "ALLOCATED", apnic),
		pblock("2002::/16", "6to4", "ALLOCATED", nil),
		pblock("2800::/12", "LACNIC", "ALLOCATED", lacnic),
		pblock("3ffe::/16", "IANA", "RESERVED", nil),
		pblock("3fff::/20", "Documentation", "RESERVED", nil),
	)
}()

var rdapPrefixes = []struct {
	prefix netip.Prefix
	svc    store.RDAPService
}{
	{netip.MustParsePrefix("8.0.0.0/8"), store.RDAPService{Kind: "ipv4", Resource: "8.0.0.0/8", Registry: arin, URLs: arinURL}},
	{netip.MustParsePrefix("187.0.0.0/8"), store.RDAPService{Kind: "ipv4", Resource: "187.0.0.0/8", Registry: lacnic, URLs: []string{"https://rdap.lacnic.net/rdap/"}}},
	{netip.MustParsePrefix("2800::/12"), store.RDAPService{Kind: "ipv6", Resource: "2800::/12", Registry: lacnic, URLs: []string{"https://rdap.lacnic.net/rdap/"}}},
}

var asnBlockList = []store.ASNBlock{
	{Start: 0, End: 0, Description: "Reserved", Reference: new("[RFC7607]")},
	{Start: 1, End: 1876, Description: "Assigned by ARIN", Registry: arin, Whois: new("whois.arin.net"), RDAPURLs: arinURL},
	{Start: 23456, End: 23456, Description: "AS_TRANS", Reference: new("[RFC6793]")},
	{Start: 61440, End: 61951, Description: "Assigned by LACNIC", Registry: lacnic, Whois: new("whois.lacnic.net"),
		RDAPURLs: []string{"https://rdap.lacnic.net/rdap/"}, RegistrationDate: new("2013-06-11")},
	{Start: 64512, End: 65534, Description: "Reserved for Private Use", Reference: new("[RFC6996]")},
	{Start: 4294967295, End: 4294967295, Description: "Reserved", Reference: new("[RFC7300]")},
}

var specialASNList = []store.SpecialASN{
	{Start: 0, End: 0, Reason: "Reserved by [RFC7607]", Reference: new("[RFC7607]")},
	{Start: 23456, End: 23456, Reason: "AS_TRANS; reserved by [RFC6793]", Reference: new("[RFC6793]")},
	{Start: 64512, End: 65534, Reason: "For private use; reserved by [RFC6996]", Reference: new("[RFC6996]")},
	{Start: 4294967295, End: 4294967295, Reason: "Reserved by [RFC7300]", Reference: new("[RFC7300]")},
}

var rdapASN = store.RDAPService{Kind: "asn", Resource: "61440-61951", Registry: lacnic, URLs: []string{"https://rdap.lacnic.net/rdap/"}}

// --- store falso: mesmas regras das consultas SQL, em Go ------------------

type fakeStore struct {
	mu      sync.Mutex
	calls   int
	pingErr error
	err     error // devolvido por todas as consultas de dados
	panic   bool
	noData  bool // Dataset e Job devolvem nil
}

func (f *fakeStore) begin() error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.panic {
		panic("teste")
	}
	return f.err
}

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }

func (f *fakeStore) Dataset(context.Context) (*store.Dataset, error) {
	if f.err != nil || f.noData {
		return nil, f.err
	}
	rows := 159
	return &store.Dataset{Version: "v1", AppliedAt: time.Unix(0, 0), SHA256: "ab", Files: []store.File{
		{Name: "as-numbers-1", URL: "https://www.iana.org/assignments/as-numbers/as-numbers-1.csv", SHA256: "cd", Bytes: 7936,
			LastModified: new("Sat, 19 Sep 2026 00:44:44 GMT")},
		{Name: store.FileRDAPASN, URL: "https://data.iana.org/rdap/asn.json", SHA256: "ef", Bytes: 4408, Rows: &rows,
			Publication: new("2026-06-01T20:00:01Z")},
	}}, nil
}

func (f *fakeStore) Job(context.Context) (*store.Job, error) {
	if f.err != nil || f.noData {
		return nil, f.err
	}
	t := time.Unix(10, 0)
	return &store.Job{LastSyncAt: &t, LastCheckAt: &t, Consolidated: 0}, nil
}

func (f *fakeStore) ASN(_ context.Context, asn int64) (*store.ASNLookup, error) {
	if err := f.begin(); err != nil {
		return nil, err
	}
	l := &store.ASNLookup{}
	for _, b := range asnBlockList {
		if b.Start <= asn && asn <= b.End {
			l.Block = &b
		}
	}
	for _, s := range specialASNList {
		if s.Start <= asn && asn <= s.End {
			l.Special = append(l.Special, s)
		}
	}
	if asn >= 61440 && asn <= 61951 {
		l.RDAP = &rdapASN
	}
	return l, nil
}

func (f *fakeStore) Prefix(_ context.Context, p netip.Prefix) (*store.PrefixLookup, error) {
	if err := f.begin(); err != nil {
		return nil, err
	}
	contains := func(outer netip.Prefix) bool { return outer.Bits() <= p.Bits() && outer.Contains(p.Addr()) }
	l := &store.PrefixLookup{}
	for _, b := range blocks {
		if contains(b.Prefix) && (l.Block == nil || b.Prefix.Bits() > l.Block.Prefix.Bits()) {
			l.Block = &b
		}
		if b.Prefix.Overlaps(p) && b.Status != "RESERVED" {
			l.Unreserved = true
		}
	}
	for _, s := range specials {
		if contains(s.Prefix) {
			l.Special = append(l.Special, s)
		}
	}
	slices.SortFunc(l.Special, func(a, b store.SpecialPrefix) int { return b.Prefix.Bits() - a.Prefix.Bits() })
	for _, r := range rdapPrefixes {
		if contains(r.prefix) {
			l.RDAP = &r.svc
		}
	}
	return l, nil
}

func (f *fakeStore) ASNBlocks(context.Context) ([]store.ASNBlock, error) {
	if err := f.begin(); err != nil {
		return nil, err
	}
	return asnBlockList, nil
}

func (f *fakeStore) PrefixBlocks(_ context.Context, family int) ([]store.PrefixBlock, error) {
	if err := f.begin(); err != nil {
		return nil, err
	}
	var out []store.PrefixBlock
	for _, b := range blocks {
		if b.Prefix.Addr().Is4() == (family == 4) {
			out = append(out, b)
		}
	}
	return out, nil
}

func (f *fakeStore) Special(context.Context) (*store.SpecialLists, error) {
	if err := f.begin(); err != nil {
		return nil, err
	}
	return &store.SpecialLists{Prefixes: specials, ASNs: specialASNList}, nil
}

func (f *fakeStore) RDAP(context.Context) (*store.RDAPBootstrap, error) {
	if err := f.begin(); err != nil {
		return nil, err
	}
	b := &store.RDAPBootstrap{Services: []store.RDAPService{rdapASN},
		Publication: map[string]*string{store.FileRDAPASN: new("2026-06-01T20:00:01Z"), store.FileRDAPIPv4: new("2019-06-07T19:00:02Z")}}
	for _, r := range rdapPrefixes {
		b.Services = append(b.Services, r.svc)
	}
	return b, nil
}

type fixedView struct{ snap dataset.Snapshot }

func (v fixedView) Current() dataset.Snapshot { return v.snap }

// memCache é um cache em memória para os testes.
type memCache struct {
	mu      sync.Mutex
	m       map[string][]byte
	pingErr error
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
func (c *memCache) Ping(context.Context) error { return c.pingErr }
func (c *memCache) Enabled() bool              { return true }

const version = "01a0eaa2-fd5c-7c24-bc6c-4e3c35d61c53"

func newAPI(t *testing.T, st *fakeStore, ready bool) (http.Handler, *memCache) {
	t.Helper()
	rip, err := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	if err != nil {
		t.Fatal(err)
	}
	snap := dataset.Snapshot{}
	if ready {
		snap = dataset.Snapshot{Version: version, AppliedAt: time.Unix(0, 0)}
	}
	c := &memCache{m: map[string][]byte{}}
	a := New(st, fixedView{snap}, c, rip, slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config{BasePath: "/iana/", Version: "test"})
	return a.Handler(), c
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

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("JSON inválido (%d): %v\n%s", rec.Code, err, rec.Body)
	}
	return v
}

// --- rotas de dados -------------------------------------------------------

func TestASN(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	for _, p := range []string{"/iana/asn/61610", "/iana/v1/asn/61610", "/iana/asn/AS61610", "/iana/asn/as61610"} {
		rec := do(h, "GET", p)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", p, rec.Code, rec.Body)
		}
		got := decode[ASNResponse](t, rec)
		if got.ASN != 61610 || got.Block == nil || got.Block.Start != 61440 || *got.Block.Registry != "lacnic" ||
			got.RDAP == nil || got.RDAP.Resource != "61440-61951" || got.Dataset.Version != version {
			t.Errorf("%s: %+v", p, got)
		}
		// Sem faixa especial: lista vazia, não null.
		if !strings.Contains(rec.Body.String(), `"special":[]`) {
			t.Errorf("%s: special deveria ser []: %s", p, rec.Body)
		}
	}

	got := decode[ASNResponse](t, do(h, "GET", "/iana/asn/23456"))
	if got.Block.Description != "AS_TRANS" || len(got.Special) != 1 || got.Special[0].Reason != "AS_TRANS; reserved by [RFC6793]" || got.RDAP != nil {
		t.Errorf("AS23456 = %+v", got)
	}
	rec := do(h, "GET", "/iana/asn/4294967295")
	got = decode[ASNResponse](t, rec)
	if rec.Code != 200 || got.Block.Start != 4294967295 || len(got.Special) != 1 || !strings.Contains(rec.Body.String(), `"rdap":null`) {
		t.Errorf("AS4294967295 = %d %s", rec.Code, rec.Body)
	}
	// ASN fora de toda faixa (só possível com dados incompletos): 200 com null.
	rec = do(h, "GET", "/iana/asn/100000")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"block":null`) {
		t.Errorf("sem faixa = %d %s", rec.Code, rec.Body)
	}
}

// Os casos do pedido e as exceções da regra de bogon (specs/fontes/iana/api.md,
// "Regra de bogon").
func TestIPBogon(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	cases := []struct {
		ip, block, special string // "" = null / nenhum
		bogon              bool
	}{
		{"10.0.0.1", "10.0.0.0/8", "10.0.0.0/8", true},
		{"192.168.1.1", "192.0.0.0/8", "192.168.0.0/16", true},
		{"100.64.0.1", "100.0.0.0/8", "100.64.0.0/10", true},
		{"192.0.2.1", "192.0.0.0/8", "192.0.2.0/24", true},
		{"8.8.8.8", "8.0.0.0/8", "", false},
		{"187.87.29.10", "187.0.0.0/8", "", false},
		{"2001:db8::1", "2001:c00::/23", "2001:db8::/32", true},
		{"fe80::1", "", "fe80::/10", true},
		{"2804:8ae0::1", "2800::/12", "", false},
		{"::1", "", "::1/128", true},
		{"240.0.0.1", "240.0.0.0/8", "240.0.0.0/4", true},
		{"0.0.0.1", "0.0.0.0/8", "0.0.0.0/8", true},
		// Exceções e casos de borda.
		{"0.0.0.0", "0.0.0.0/8", "0.0.0.0/32", true},
		{"192.0.0.9", "192.0.0.0/8", "192.0.0.9/32", false},
		{"224.0.0.1", "224.0.0.0/8", "", true},
		{"2001::1", "2001::/23", "2001::/32", false},
		{"2001:10::1", "2001::/23", "2001:10::/28", true},
		{"ff02::1", "", "", true},
		{"64:ff9b::1", "", "64:ff9b::/96", false},
		{"192.88.99.1", "192.0.0.0/8", "192.88.99.0/24", false},
		{"192.88.99.2", "192.0.0.0/8", "192.88.99.2/32", true},
		{"3fff::1", "3fff::/20", "3fff::/20", true},
	}
	for _, c := range cases {
		rec := do(h, "GET", "/iana/ip/"+c.ip)
		if rec.Code != 200 {
			t.Errorf("%s: %d %s", c.ip, rec.Code, rec.Body)
			continue
		}
		got := decode[IPResponse](t, rec)
		var block, special string
		if got.Block != nil {
			block = got.Block.Prefix
		}
		if len(got.Special) > 0 {
			special = got.Special[0].Prefix
		}
		if got.IP != c.ip || got.Bogon != c.bogon || block != c.block || special != c.special {
			t.Errorf("%s: ip=%s bogon=%v block=%q special[0]=%q; quero bogon=%v block=%q special[0]=%q",
				c.ip, got.IP, got.Bogon, block, special, c.bogon, c.block, c.special)
		}
	}
}

func TestIPDetails(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)

	// Entradas aninhadas: da mais específica para a menos.
	got := decode[IPResponse](t, do(h, "GET", "/iana/ip/192.0.0.9"))
	if len(got.Special) != 2 || got.Special[0].Prefix != "192.0.0.9/32" || got.Special[1].Prefix != "192.0.0.0/24" ||
		!*got.Special[0].GloballyReachable || *got.Special[1].GloballyReachable {
		t.Errorf("aninhados = %+v", got.Special)
	}
	// IPv4 mapeado em IPv6 vira IPv4 (e usa a mesma chave de cache).
	got = decode[IPResponse](t, do(h, "GET", "/iana/ip/::ffff:10.0.0.1"))
	if got.IP != "10.0.0.1" || !got.Bogon {
		t.Errorf("mapeado = %+v", got)
	}
	// Bloco LEGACY administrado pela ARIN, RDAP com duas URLs.
	rec := do(h, "GET", "/iana/ip/8.8.8.8")
	got = decode[IPResponse](t, rec)
	if got.RDAP == nil || got.RDAP.Resource != "8.0.0.0/8" || len(got.RDAP.URLs) != 2 || got.Block.Status != "LEGACY" ||
		*got.Block.Registry != "arin" || !strings.Contains(rec.Body.String(), `"special":[]`) ||
		!strings.Contains(rec.Body.String(), `"rdap_urls":[]`) {
		t.Errorf("8.8.8.8 = %s", rec.Body)
	}
	// Bloco ALLOCATED à LACNIC (o ASN de exemplo), RDAP com uma URL.
	rec = do(h, "GET", "/iana/ip/187.87.29.10")
	got = decode[IPResponse](t, rec)
	if got.RDAP == nil || got.RDAP.Resource != "187.0.0.0/8" || *got.RDAP.Registry != "lacnic" ||
		strings.Join(got.RDAP.URLs, " ") != "https://rdap.lacnic.net/rdap/" || got.Block.Status != "ALLOCATED" ||
		*got.Block.Registry != "lacnic" || !strings.Contains(rec.Body.String(), `"special":[]`) {
		t.Errorf("187.87.29.10 = %s", rec.Body)
	}
	// TEREDO: globally_reachable N/A sai como null.
	rec = do(h, "GET", "/iana/ip/2001::1")
	if !strings.Contains(rec.Body.String(), `"name":"TEREDO"`) || !strings.Contains(rec.Body.String(), `"globally_reachable":null`) {
		t.Errorf("TEREDO = %s", rec.Body)
	}
	// Sem bloco nem RDAP: null.
	rec = do(h, "GET", "/iana/ip/fe80::1")
	if !strings.Contains(rec.Body.String(), `"block":null`) || !strings.Contains(rec.Body.String(), `"rdap":null`) {
		t.Errorf("fe80::1 = %s", rec.Body)
	}
}

func TestPrefix(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	cases := []struct {
		path, query, block, special string
		bogon                       bool
	}{
		{"/iana/prefix/10.1.2.3/16", "10.1.0.0/16", "10.0.0.0/8", "10.0.0.0/8", true},
		{"/iana/v1/prefix/192.0.2.0/24", "192.0.2.0/24", "192.0.0.0/8", "192.0.2.0/24", true},
		{"/iana/prefix/187.87.28.0/22", "187.87.28.0/22", "187.0.0.0/8", "", false},
		{"/iana/prefix/8.0.0.0/7", "8.0.0.0/7", "", "", false},                  // maior que um /8: há bloco não reservado dentro
		{"/iana/prefix/224.0.0.0/4", "224.0.0.0/4", "", "", true},               // só blocos RESERVED
		{"/iana/prefix/192.0.0.0/16", "192.0.0.0/16", "192.0.0.0/8", "", false}, // especiais dentro não contam
		{"/iana/prefix/2804:8ae0::/32", "2804:8ae0::/32", "2800::/12", "", false},
		{"/iana/prefix/2001:db8::/48", "2001:db8::/48", "2001:c00::/23", "2001:db8::/32", true},
		{"/iana/prefix/ff00::/8", "ff00::/8", "", "", true},
		{"/iana/prefix/::/0", "::/0", "", "", false},
	}
	for _, c := range cases {
		rec := do(h, "GET", c.path)
		if rec.Code != 200 {
			t.Errorf("%s: %d %s", c.path, rec.Code, rec.Body)
			continue
		}
		got := decode[PrefixResponse](t, rec)
		var block, special string
		if got.Block != nil {
			block = got.Block.Prefix
		}
		if len(got.Special) > 0 {
			special = got.Special[0].Prefix
		}
		if got.Query != c.query || got.Bogon != c.bogon || block != c.block || special != c.special {
			t.Errorf("%s: %+v", c.path, got)
		}
	}
}

func TestLists(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)

	asns := decode[ASNBlocksResponse](t, do(h, "GET", "/iana/asns"))
	if asns.Count != len(asnBlockList) || len(asns.Blocks) != asns.Count || asns.Blocks[1].RDAPURLs[1] != "http://rdap.arin.net/registry" {
		t.Errorf("asns = %+v", asns)
	}
	v4 := decode[PrefixBlocksResponse](t, do(h, "GET", "/iana/ipv4"))
	if v4.Count != 39 || v4.Blocks[0].Prefix != "0.0.0.0/8" || v4.Blocks[0].Status != "RESERVED" || v4.Dataset.Version != version {
		t.Errorf("ipv4 = %d %+v", v4.Count, v4.Blocks[0])
	}
	v6 := decode[PrefixBlocksResponse](t, do(h, "GET", "/iana/v1/ipv6"))
	if v6.Count != 6 || v6.Blocks[0].Prefix != "2001::/23" || *v6.Blocks[0].Whois != "whois.iana.org" || v6.Blocks[0].Registry != nil {
		t.Errorf("ipv6 = %+v", v6)
	}

	rec := do(h, "GET", "/iana/special")
	spec := decode[SpecialResponse](t, rec)
	if len(spec.IPv4) != 12 || len(spec.IPv6) != 10 || len(spec.ASN) != 4 || spec.IPv4[0].Prefix != "0.0.0.0/8" || spec.IPv6[0].Prefix != "::1/128" {
		t.Errorf("special = %d/%d/%d", len(spec.IPv4), len(spec.IPv6), len(spec.ASN))
	}
	if !strings.Contains(rec.Body.String(), `"termination_date":"2015-03"`) {
		t.Errorf("special sem termination_date: %s", rec.Body)
	}

	rec = do(h, "GET", "/iana/rdap")
	rd := decode[RDAPResponse](t, rec)
	if len(rd.ASN) != 1 || len(rd.IPv4) != 2 || len(rd.IPv6) != 1 || *rd.Publication.ASN != "2026-06-01T20:00:01Z" ||
		rd.Publication.IPv6 != nil || rd.IPv6[0].Resource != "2800::/12" {
		t.Errorf("rdap = %s", rec.Body)
	}
}

func TestErrors(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	cases := map[string]int{
		"/iana/asn/abc":               400,
		"/iana/asn/-1":                400,
		"/iana/asn/4294967296":        400,
		"/iana/asn/AS":                400,
		"/iana/ip/999.1.1.1":          400,
		"/iana/ip/fe80::1%25eth0":     400,
		"/iana/ip/10.0.0.0%2F8":       400,
		"/iana/prefix/10.0.0.0/33":    400,
		"/iana/prefix/10.0.0.0/-1":    400,
		"/iana/prefix/2001:db8::/129": 400,
		"/iana/prefix/x/8":            400,
		"/iana/v2/asn/61610":          404,
		"/iana/nada":                  404,
		"/iana/asn":                   404,
		"/iana/prefix/10.0.0.0":       404,
		"/outra-coisa":                404,
		"/asn/61610":                  404,
	}
	for p, want := range cases {
		rec := do(h, "GET", p)
		if rec.Code != want {
			t.Errorf("%s: %d, quero %d (%s)", p, rec.Code, want, rec.Body)
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") || !strings.Contains(rec.Body.String(), `"error"`) {
			t.Errorf("%s: erro deveria ser JSON: %s", p, rec.Body)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: erro não pode ir para cache", p)
		}
	}
	// Métodos que não existem nas rotas de dados.
	if rec := do(h, "POST", "/iana/asn/61610"); rec.Code != http.StatusMethodNotAllowed && rec.Code != 404 {
		t.Errorf("POST em rota de dados = %d", rec.Code)
	}
}

func TestHead(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	for _, p := range []string{"/iana/asn/61610", "/iana/v1/ip/10.0.0.1", "/iana/ipv4", "/iana/rdap"} {
		rec := do(h, "HEAD", p)
		if rec.Code != 200 || rec.Header().Get("ETag") == "" || rec.Header().Get("X-Dataset-Version") != version {
			t.Errorf("HEAD %s = %d %v", p, rec.Code, rec.Header())
		}
	}
}

func TestCacheAndETag(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)

	first := do(h, "GET", "/iana/ip/10.0.0.1")
	if first.Header().Get("X-Cache") != "MISS" {
		t.Errorf("primeira = %s", first.Header().Get("X-Cache"))
	}
	// Versionada, sem versão e IPv4 mapeado compartilham o cache (mesmo conteúdo).
	for _, p := range []string{"/iana/v1/ip/10.0.0.1", "/iana/ip/::ffff:10.0.0.1"} {
		rec := do(h, "GET", p)
		if rec.Header().Get("X-Cache") != "HIT" || rec.Body.String() != first.Body.String() {
			t.Errorf("%s: %s", p, rec.Header().Get("X-Cache"))
		}
	}
	if st.calls != 1 || len(c.m) != 1 {
		t.Errorf("calls = %d, chaves = %d", st.calls, len(c.m))
	}
	for k := range c.m {
		if k != "badblock:api-iana:"+version+":ip:10.0.0.1" {
			t.Errorf("chave = %s", k)
		}
	}
	etag := first.Header().Get("ETag")
	if !strings.HasPrefix(etag, `W/"`) || first.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Fatalf("cabeçalhos = %v", first.Header())
	}
	for _, inm := range []string{etag, strings.TrimPrefix(etag, "W/"), `"outro", ` + etag, "*"} {
		rec := do(h, "GET", "/iana/v1/ip/10.0.0.1", "If-None-Match", inm)
		if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 || rec.Header().Get("ETag") != etag {
			t.Errorf("If-None-Match %q = %d", inm, rec.Code)
		}
	}
	if rec := do(h, "GET", "/iana/ip/10.0.0.1", "If-None-Match", `W/"outro"`); rec.Code != 200 {
		t.Errorf("ETag diferente = %d", rec.Code)
	}
	// Outra consulta, outro ETag; 304 não consulta o banco.
	if do(h, "GET", "/iana/ip/10.0.0.2").Header().Get("ETag") == etag {
		t.Error("consultas diferentes com o mesmo ETag")
	}
	if st.calls != 2 {
		t.Errorf("calls = %d", st.calls)
	}
}

func TestNotReady(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, false)
	for _, p := range []string{"/iana/asn/61610", "/iana/ip/10.0.0.1", "/iana/prefix/10.0.0.0/8", "/iana/asns",
		"/iana/ipv4", "/iana/ipv6", "/iana/special", "/iana/v1/rdap"} {
		rec := do(h, "GET", p)
		if rec.Code != 503 || !strings.Contains(rec.Body.String(), "dataset_not_ready") {
			t.Errorf("%s sem dataset = %d %s", p, rec.Code, rec.Body)
		}
	}
	s := decode[StatusResponse](t, do(h, "GET", "/iana/status"))
	if !s.Success || s.Status != "starting" || s.Checks["dataset"] != "empty" {
		t.Errorf("status sem dataset = %+v", s)
	}
}

func TestDatabaseErrors(t *testing.T) {
	st := &fakeStore{err: errors.New("conexão recusada")}
	h, c := newAPI(t, st, true)
	for _, p := range []string{"/iana/asn/61610", "/iana/ip/10.0.0.1", "/iana/prefix/10.0.0.0/8", "/iana/asns",
		"/iana/ipv4", "/iana/special", "/iana/rdap", "/iana/meta"} {
		rec := do(h, "GET", p)
		if rec.Code != 503 || !strings.Contains(rec.Body.String(), "database_unavailable") {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	if len(c.m) != 0 {
		t.Errorf("erro não pode ir para o cache: %v", c.m)
	}
	st.err = fmt.Errorf("consulta: %w", context.DeadlineExceeded)
	if rec := do(h, "GET", "/iana/ip/10.0.0.1"); rec.Code != 504 || !strings.Contains(rec.Body.String(), `"timeout"`) {
		t.Errorf("timeout = %d %s", rec.Code, rec.Body)
	}
	st.err, st.panic = nil, true
	if rec := do(h, "GET", "/iana/asn/1"); rec.Code != 500 || !strings.Contains(rec.Body.String(), "internal_error") {
		t.Errorf("panic = %d %s", rec.Code, rec.Body)
	}
}

func TestStatus(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)
	for _, path := range []string{"/iana/health", "/iana/status"} {
		for _, m := range []string{"GET", "POST"} {
			rec := do(h, m, path)
			s := decode[StatusResponse](t, rec)
			if rec.Code != 200 || !s.Success || s.Status != "ok" || s.Timestamp == "" || s.Message == "" ||
				s.Checks["postgres"] != "ok" || s.Checks["valkey"] != "ok" || s.Checks["dataset"] != "ok" {
				t.Errorf("%s %s = %d %s", m, path, rec.Code, rec.Body)
			}
		}
	}
	if rec := do(h, "GET", "/iana/ping"); rec.Code != 200 || rec.Body.String() != "pong" {
		t.Errorf("ping = %d %q", rec.Code, rec.Body)
	}

	c.pingErr = errors.New("valkey fora")
	rec := do(h, "GET", "/iana/status")
	s := decode[StatusResponse](t, rec)
	if rec.Code != 200 || !s.Success || s.Status != "degraded" || s.Checks["valkey"] != "error" {
		t.Errorf("valkey fora = %d %s", rec.Code, rec.Body)
	}

	st.pingErr = errors.New("postgres fora")
	rec = do(h, "POST", "/iana/health")
	s = decode[StatusResponse](t, rec)
	if rec.Code != 503 || s.Success || s.Status != "error" || s.Checks["postgres"] != "error" {
		t.Errorf("postgres fora = %d %s", rec.Code, rec.Body)
	}
}

func TestCORSAndHeaders(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	rec := do(h, "OPTIONS", "/iana/ip/10.0.0.1", "Origin", "https://exemplo.com", "Access-Control-Request-Method", "GET")
	if rec.Code != http.StatusNoContent || !strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), "GET") ||
		rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("preflight = %d %v", rec.Code, rec.Header())
	}
	rec = do(h, "GET", "/iana/asn/61610")
	hd := rec.Header()
	if hd.Get("Access-Control-Allow-Origin") != "*" || hd.Get("X-Content-Type-Options") != "nosniff" ||
		hd.Get("Server") != "badblock-api-iana/test" || !strings.Contains(hd.Get("Access-Control-Expose-Headers"), "ETag") {
		t.Errorf("cabeçalhos = %v", hd)
	}
}

func TestIndexMetaAndRedirect(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	for _, p := range []string{"/iana/", "/iana/v1/"} {
		rec := do(h, "GET", p)
		idx := decode[IndexResponse](t, rec)
		if rec.Code != 200 || idx.App != "api-iana" || !slices.Contains(idx.Endpoints, "/iana/ip/{ip}") || !slices.Contains(idx.Endpoints, "/iana/rdap") {
			t.Errorf("índice %s = %d %s", p, rec.Code, rec.Body)
		}
	}
	if rec := do(h, "GET", "/iana"); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/iana/" {
		t.Errorf("redirect = %d %s", rec.Code, rec.Header().Get("Location"))
	}

	rec := do(h, "GET", "/iana/meta")
	m := decode[MetaResponse](t, rec)
	if rec.Code != 200 || m.Dataset == nil || m.Collector == nil || m.Collector.Consolidated || m.Collector.App != "collector-iana" ||
		len(m.Dataset.Files) != 2 || *m.Dataset.Files[1].Publication != "2026-06-01T20:00:01Z" || *m.Dataset.Files[1].Rows != 159 ||
		rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("meta = %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"rows":null`) || !strings.Contains(rec.Body.String(), `"publication":null`) {
		t.Errorf("meta sem campos opcionais nulos: %s", rec.Body)
	}

	// Antes da primeira carga: dataset e collector null.
	h, _ = newAPI(t, &fakeStore{noData: true}, false)
	rec = do(h, "GET", "/iana/v1/meta")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"dataset":null`) || !strings.Contains(rec.Body.String(), `"collector":null`) {
		t.Errorf("meta vazio = %d %s", rec.Code, rec.Body)
	}
}
