package parse

import (
	"os"
	"strings"
	"testing"
)

func open(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open("../../testdata/sources/" + name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestParseIANAASN(t *testing.T) {
	blocks, st, err := ParseIANAASN(open(t, "iana-asn-16"))
	if err != nil {
		t.Fatalf("iana-asn-16: %v", err)
	}
	if st.Skipped != 0 {
		t.Errorf("linhas descartadas: %v", st.Samples)
	}
	var found bool
	for _, b := range blocks {
		if b.First == 1 && b.Last == 1876 {
			found = true
			if b.RDAP != "https://rdap.arin.net/registry/" {
				t.Errorf("RDAP colado deveria virar a URL https com barra, veio %q", b.RDAP)
			}
			if b.WHOIS != "whois.arin.net" {
				t.Errorf("WHOIS = %q", b.WHOIS)
			}
		}
	}
	if !found {
		t.Error("bloco 1-1876 não encontrado")
	}

	blocks32, _, err := ParseIANAASN(open(t, "iana-asn-32"))
	if err != nil {
		t.Fatalf("iana-asn-32: %v", err)
	}
	for _, b := range blocks32 {
		if b.First == 0 && b.Last == 65535 {
			t.Error("a linha 'See Sub-registry' deveria ser ignorada")
		}
	}
	if len(blocks32) == 0 || blocks32[len(blocks32)-1].Last != 4294967295 {
		t.Errorf("o último bloco de 32 bits deveria terminar em 4294967295")
	}
}

func TestParseIANAIPv4(t *testing.T) {
	blocks, _, err := ParseIANAIPv4(open(t, "iana-ipv4"))
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 256 {
		t.Fatalf("esperados 256 blocos /8, vieram %d", len(blocks))
	}
	b := blocks[200]
	if b.Prefix.String() != "200.0.0.0/8" || b.Designation != "LACNIC" || b.Status != "allocated" {
		t.Errorf("200/8 = %+v", b)
	}
	if b.RDAP != "https://rdap.lacnic.net/rdap/" {
		t.Errorf("RDAP = %q", b.RDAP)
	}
	if b.Date == nil || b.Date.Format("2006-01") != "2002-11" {
		t.Errorf("data = %v", b.Date)
	}
	// 45/8 é legado administrado pela ARIN, embora a LACNIC delegue blocos
	// dentro dele (45.171.60.0/22, por exemplo).
	if legacy := blocks[45]; legacy.Status != "legacy" || legacy.WHOIS != "whois.arin.net" {
		t.Errorf("45/8 = %+v", legacy)
	}
}

func TestParseIANAIPv6(t *testing.T) {
	blocks, _, err := ParseIANAIPv6(open(t, "iana-ipv6"))
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range blocks {
		if b.Prefix.String() == "2800::/12" {
			if b.Designation != "LACNIC" {
				t.Errorf("2800::/12 = %+v", b)
			}
			return
		}
	}
	t.Error("2800::/12 não encontrado")
}

func TestParseIANASpecialIP(t *testing.T) {
	blocks, _, err := ParseIANASpecialIP(open(t, "iana-special-ipv4"))
	if err != nil {
		t.Fatal(err)
	}
	byPrefix := map[string]SpecialIP{}
	for _, b := range blocks {
		byPrefix[b.Prefix.String()] = b
	}
	if b, ok := byPrefix["192.0.0.0/24"]; !ok || b.Name != "IETF Protocol Assignments" {
		t.Errorf("192.0.0.0/24 [2] deveria perder a nota de rodapé: %+v", b)
	}
	for _, p := range []string{"192.0.0.170/32", "192.0.0.171/32"} {
		if _, ok := byPrefix[p]; !ok {
			t.Errorf("célula com dois blocos deveria gerar %s", p)
		}
	}
	if b := byPrefix["0.0.0.0/8"]; b.Name != "This network" {
		t.Errorf("aspas do nome deveriam sair: %q", b.Name)
	}
	if b := byPrefix["10.0.0.0/8"]; b.GloballyReachable == nil || *b.GloballyReachable {
		t.Errorf("10/8 deveria ser globally_reachable=false")
	}
	if b := byPrefix["127.0.0.0/8"]; b.GloballyReachable == nil || *b.GloballyReachable {
		t.Errorf("'False [1]' deveria virar false")
	}
	if b := byPrefix["192.88.99.0/24"]; b.GloballyReachable != nil {
		t.Errorf("célula vazia deveria virar nil")
	}

	v6, _, err := ParseIANASpecialIP(open(t, "iana-special-ipv6"))
	if err != nil {
		t.Fatal(err)
	}
	var teredo bool
	for _, b := range v6 {
		if b.Prefix.String() == "2001::/32" {
			teredo = true
			if b.GloballyReachable != nil {
				t.Errorf("'N/A [2]' deveria virar nil")
			}
		}
	}
	if !teredo {
		t.Error("TEREDO 2001::/32 não encontrado")
	}
}

func TestParseIANASpecialASN(t *testing.T) {
	blocks, _, err := ParseIANASpecialASN(open(t, "iana-special-asn"))
	if err != nil {
		t.Fatal(err)
	}
	var private bool
	for _, b := range blocks {
		if b.First == 64512 && b.Last == 65534 {
			private = true
		}
	}
	if !private {
		t.Error("faixa privada 64512-65534 não encontrada")
	}
}

func TestReadCSVHeaderMismatch(t *testing.T) {
	_, _, err := ParseIANAIPv4(strings.NewReader("Prefixo,Designation,Date,WHOIS,RDAP,Status,Note\n"))
	if err == nil {
		t.Error("cabeçalho diferente deveria gerar erro")
	}
}

func TestSplitURLs(t *testing.T) {
	got := splitURLs("https://rdap.arin.net/registryhttp://rdap.arin.net/registry")
	if len(got) != 2 || got[0] != "https://rdap.arin.net/registry" || got[1] != "http://rdap.arin.net/registry" {
		t.Errorf("splitURLs = %v", got)
	}
	if got := PreferHTTPS([]string{"http://a.example/", "https://b.example"}); got != "https://b.example/" {
		t.Errorf("PreferHTTPS = %q", got)
	}
	if got := PreferHTTPS(nil); got != "" {
		t.Errorf("PreferHTTPS(nil) = %q", got)
	}
}
