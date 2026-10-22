package parse

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func load(t *testing.T, name string) *Parsed {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(b)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return p
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func findEntity(t *testing.T, p *Parsed, handle string) Entity {
	t.Helper()
	for _, e := range p.Entities {
		if e.Handle == handle {
			return e
		}
	}
	t.Fatalf("entidade %s ausente em %+v", handle, p.Entities)
	return Entity{}
}

func links(p *Parsed) []string {
	var out []string
	for _, l := range p.Links {
		out = append(out, l.Handle+"|"+l.Role+"|"+l.Via)
	}
	return out
}

func TestAutnum61610(t *testing.T) {
	p := load(t, "autnum-61610.json")
	if p.Class != "autnum" || p.Autnum == nil || p.Network != nil {
		t.Fatalf("classe = %+v", p)
	}
	a := p.Autnum
	if a.ASN != 61610 || deref(a.Handle) != "61610" || deref(a.Name) != "ASN61610" ||
		deref(a.Type) != "DIRECT ALLOCATION" || deref(a.Country) != "BR" {
		t.Errorf("autnum = %+v", a)
	}
	if !deref(a.RegisteredAt).Equal(time.Date(2023, 2, 27, 15, 58, 28, 0, time.UTC)) || a.ChangedAt == nil {
		t.Errorf("datas = %v %v", a.RegisteredAt, a.ChangedAt)
	}
	if p.Registrant != "35980592000130" || len(p.Entities) != 2 {
		t.Fatalf("titular = %q, entidades = %d", p.Registrant, len(p.Entities))
	}
	reg := findEntity(t, p, "35980592000130")
	if deref(reg.Kind) != "org" || deref(reg.Name) != "ELEA DATA CENTERS" || deref(reg.Country) != "BR" ||
		deref(reg.Document) != "35.980.592/0001-30" || deref(reg.DocumentType) != "cnpj" ||
		deref(reg.LegalRepresentative) != "REPRESENTANTE EXEMPLO" || reg.Email != nil {
		t.Errorf("titular = %+v", reg)
	}
	c := findEntity(t, p, "ROBEL308")
	if deref(c.Kind) != "individual" || deref(c.Name) != "CONTATO EXEMPLO" || deref(c.Email) != "contato@example.com" ||
		c.Document != nil || c.RegisteredAt == nil {
		t.Errorf("contato = %+v", c)
	}
	// Contato aninhado no titular (via) e contato do objeto com dois papéis.
	want := []string{
		"35980592000130|registrant|",
		"ROBEL308|abuse|",
		"ROBEL308|administrative|",
		"ROBEL308|administrative|35980592000130",
	}
	if got := links(p); !slices.Equal(got, want) {
		t.Errorf("ligações = %v", got)
	}
}

func TestAutnumOthers(t *testing.T) {
	p := load(t, "autnum-22548.json")
	if p.Autnum.ASN != 22548 || p.Registrant != "05506560000136" {
		t.Errorf("22548 = %+v %q", p.Autnum, p.Registrant)
	}
	if e := findEntity(t, p, "05506560000136"); deref(e.Document) != "05.506.560/0001-36" {
		t.Errorf("NIC.br = %+v", e)
	}
	p = load(t, "autnum-174.json")
	if p.Autnum.ASN != 174 || p.Registrant != "29484413000170" {
		t.Errorf("174 = %+v %q", p.Autnum, p.Registrant)
	}
	// A truncada também se lê (o parser não decide truncagem): sem e-mail e
	// sem CNPJ.
	p = load(t, "autnum-61610-truncated.json")
	if e := findEntity(t, p, "ROBEL308"); e.Email != nil || e.Country != nil {
		t.Errorf("truncada = %+v", e)
	}
	if e := findEntity(t, p, "35980592000130"); e.Document != nil {
		t.Errorf("truncada com documento = %+v", e)
	}
}

func TestNetworkIPv4(t *testing.T) {
	p := load(t, "ip-200.225.48.0-21.json")
	n := p.Network
	if p.Class != "ip network" || n == nil || p.Autnum != nil {
		t.Fatalf("classe = %+v", p)
	}
	if n.Prefix != netip.MustParsePrefix("200.225.48.0/21") || deref(n.Handle) != "200.225.48.0/21" ||
		deref(n.Name) != "462539" || deref(n.Type) != "ALLOCATED" || deref(n.ASN) != 61610 ||
		!slices.Equal(n.Status, []string{"active"}) || n.ParentHandle != nil {
		t.Errorf("bloco = %+v", n)
	}
	if len(p.Reverses) != 1 {
		t.Fatalf("reversos = %+v", p.Reverses)
	}
	r := p.Reverses[0]
	if r.Prefix == nil || len(r.Nameservers) == 0 || r.Signed == nil {
		t.Errorf("reverso = %+v", r)
	}
	for i := 1; i < len(r.Nameservers); i++ {
		if r.Nameservers[i-1].Name > r.Nameservers[i].Name {
			t.Errorf("nameservers fora de ordem: %+v", r.Nameservers)
		}
	}
	if ns := r.Nameservers[0]; ns.CheckedAt == nil || ns.LastOKAt == nil || len(ns.Status) == 0 {
		t.Errorf("nameserver = %+v", ns)
	}
	want := []string{
		"35980592000130|registrant|",
		"ROBEL308|abuse|",
		"ROBEL308|administrative|35980592000130",
		"ROBEL308|technical|",
	}
	if got := links(p); !slices.Equal(got, want) {
		t.Errorf("ligações = %v", got)
	}
}

func TestNetworkIPv6(t *testing.T) {
	p := load(t, "ip-2804-8ae0--32.json")
	if p.Network.Prefix != netip.MustParsePrefix("2804:8ae0::/32") {
		t.Errorf("prefixo = %v", p.Network.Prefix)
	}
	if len(p.Reverses) != 1 {
		t.Fatalf("reversos = %+v", p.Reverses)
	}
	r := p.Reverses[0]
	if r.Start != netip.MustParseAddr("2804:8ae0:4000::") || deref(r.Prefix) != netip.MustParsePrefix("2804:8ae0:4000::/40") {
		t.Errorf("reverso = %+v", r)
	}
	if len(r.Nameservers) != 2 || r.Nameservers[0].Name != "liberty.ns.cloudflare.com" || deref(r.Signed) {
		t.Errorf("nameservers = %+v", r.Nameservers)
	}
}

func TestNetworkWithoutReverse(t *testing.T) {
	p := load(t, "ip-187.87.28.0-22.json")
	if p.Network.Prefix != netip.MustParsePrefix("187.87.28.0/22") || len(p.Reverses) != 0 {
		t.Errorf("bloco = %+v, reversos = %d", p.Network, len(p.Reverses))
	}
}

func TestSynthetic(t *testing.T) {
	// Faixa reversa que não é CIDR: prefix nil, sem recusar.
	p := load(t, "synthetic-ip-reverse-not-cidr.json")
	if len(p.Reverses) != 2 || p.Reverses[0].Prefix != nil ||
		deref(p.Reverses[1].Prefix) != netip.MustParsePrefix("198.51.100.192/26") || !deref(p.Reverses[1].Signed) {
		t.Errorf("reversos = %+v", p.Reverses)
	}
	ns := p.Reverses[0].Nameservers
	if len(ns) != 2 || ns[0].Name != "a.ns.example.com" || !slices.Equal(ns[0].Status, []string{"ns timeout"}) ||
		ns[1].CheckedAt != nil || ns[1].Status == nil {
		t.Errorf("nameservers = %+v", ns)
	}
	if e := findEntity(t, p, "11111111000111"); deref(e.Name) != "EMPRESA EXEMPLO LTDA" || deref(e.DocumentType) != "cnpj" {
		t.Errorf("titular = %+v", e)
	}

	// Entidade sem vCard, entidade sem handle, events ausente, texto vazio.
	p = load(t, "synthetic-autnum-no-vcard.json")
	if p.Autnum.ASN != 64500 || p.Autnum.RegisteredAt != nil || p.Autnum.ChangedAt != nil {
		t.Errorf("autnum = %+v", p.Autnum)
	}
	if len(p.Entities) != 2 {
		t.Fatalf("entidades = %+v", p.Entities)
	}
	if e := findEntity(t, p, "11111111000111"); e.Kind != nil || e.Name != nil || e.RegisteredAt != nil {
		t.Errorf("sem vCard = %+v", e)
	}
	if e := findEntity(t, p, "EXEMPLO1"); e.Name != nil || deref(e.Email) != "contato@example.com" {
		t.Errorf("texto = %+v", e)
	}

	// Bloco cuja faixa não é CIDR: recusado.
	b, _ := os.ReadFile(filepath.Join("..", "..", "testdata", "synthetic-ip-block-not-cidr.json"))
	_, err := Parse(b)
	if !errors.Is(err, ErrRefused) || Reason(err) != "faixa não é CIDR: 203.0.113.0-203.0.113.191" {
		t.Errorf("bloco não CIDR = %v", err)
	}

	for _, name := range []string{"synthetic-invalid.json", "synthetic-wrong-class.json"} {
		b, _ := os.ReadFile(filepath.Join("..", "..", "testdata", name))
		if _, err := Parse(b); !errors.Is(err, ErrRefused) {
			t.Errorf("%s = %v", name, err)
		}
	}
}

func TestRangePrefix(t *testing.T) {
	cases := []struct {
		a, b string
		want string
	}{
		{"10.0.0.0", "10.0.0.255", "10.0.0.0/24"},
		{"10.0.0.0", "10.0.0.0", "10.0.0.0/32"},
		{"0.0.0.0", "255.255.255.255", "0.0.0.0/0"},
		{"10.0.0.0", "10.0.0.191", ""},
		{"10.0.0.1", "10.0.0.2", ""},
		{"10.0.0.255", "10.0.0.0", ""},
		{"2804:8ae0:4000::", "2804:8ae0:40ff:ffff:ffff:ffff:ffff:ffff", "2804:8ae0:4000::/40"},
		{"10.0.0.0", "::ffff", ""},
	}
	for _, c := range cases {
		p, ok := RangePrefix(netip.MustParseAddr(c.a), netip.MustParseAddr(c.b))
		got := ""
		if ok {
			got = p.String()
		}
		if got != c.want {
			t.Errorf("%s-%s = %q, quero %q", c.a, c.b, got, c.want)
		}
	}
}

func TestFixturesHaveNoPersonalData(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "testdata", "*.json"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		p, err := Parse(b)
		if err != nil {
			continue
		}
		for _, e := range p.Entities {
			if deref(e.Kind) == "individual" && e.Name != nil && *e.Name != "CONTATO EXEMPLO" {
				t.Errorf("%s: nome de pessoa física %q", f, *e.Name)
			}
			if e.Email != nil && !strings.HasSuffix(*e.Email, "@example.com") {
				t.Errorf("%s: e-mail %q", f, *e.Email)
			}
		}
	}
}
