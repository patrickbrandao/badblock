package parse

import (
	"net/netip"
	"os"
	"strings"
	"testing"
)

func TestParseSample(t *testing.T) {
	f, err := os.Open("../../testdata/nicbr-asn-blk-sample.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	d, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(d.ASNs); got != 11 {
		t.Errorf("ASNs = %d, quero 11", got)
	}
	if d.PrefixesV4 != 52 || d.PrefixesV6 != 7 {
		t.Errorf("blocos = %d IPv4 / %d IPv6, quero 52 / 7", d.PrefixesV4, d.PrefixesV6)
	}
	if d.WarningCount() != 0 || d.Skipped != 0 {
		t.Errorf("avisos inesperados: %v", d.Warnings)
	}

	byNum := map[int64]ASN{}
	for _, a := range d.ASNs {
		byNum[a.Number] = a
	}
	tm := byNum[61610]
	if tm.Name != "ELEA DATA CENTERS" || tm.Document != "35.980.592/0001-30" {
		t.Errorf("AS61610 = %+v", tm)
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("187.87.28.0/22"),
		netip.MustParsePrefix("2804:8ae0::/32"),
		netip.MustParsePrefix("200.225.48.0/21"),
	}
	if len(tm.Prefixes) != len(want) {
		t.Fatalf("AS61610 blocos = %v", tm.Prefixes)
	}
	for i := range want {
		if tm.Prefixes[i] != want[i] {
			t.Errorf("AS61610 bloco %d = %s, quero %s", i, tm.Prefixes[i], want[i])
		}
	}
	if a := byNum[6125]; len(a.Prefixes) != 0 {
		t.Errorf("AS6125 não tem blocos na fonte, veio %v", a.Prefixes)
	}
	if a := byNum[275689]; a.Document != "10996639" {
		t.Errorf("AS275689 documento estrangeiro = %q", a.Document)
	}
	if a := byNum[174]; !strings.Contains(a.Name, "COGENT") {
		t.Errorf("AS174 nome UTF-8 = %q", a.Name)
	}
}

func TestParseEdgeCases(t *testing.T) {
	in := "\xef\xbb\xbf# comentário\n" +
		"AS1|Um|11.111.111/0001-11|10.0.0.1/8|10.0.0.0/8|lixo||2001:db8::/32\r\n" +
		"\n" +
		"AS2|Dois|22.222.222/0002-22|10.0.0.0/8|192.0.2.0/24\n" +
		"as1|Um de novo|x|198.51.100.0/24\n" +
		strings.Repeat("AS3|Tres|33|203.0.113.0/24\n", 100)

	d, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.ASNs) != 3 {
		t.Fatalf("ASNs = %d, quero 3", len(d.ASNs))
	}
	a1 := d.ASNs[0]
	if a1.Name != "Um" {
		t.Errorf("nome da primeira ocorrência deveria ficar: %q", a1.Name)
	}
	// 10.0.0.1/8 vira 10.0.0.0/8; a repetição na mesma linha e em AS2 some;
	// o bloco do "as1" repetido é somado ao AS1.
	want := []string{"10.0.0.0/8", "2001:db8::/32", "198.51.100.0/24"}
	if len(a1.Prefixes) != len(want) {
		t.Fatalf("AS1 blocos = %v", a1.Prefixes)
	}
	for i, w := range want {
		if a1.Prefixes[i].String() != w {
			t.Errorf("AS1 bloco %d = %s, quero %s", i, a1.Prefixes[i], w)
		}
	}
	if got := d.ASNs[1].Prefixes; len(got) != 1 || got[0].String() != "192.0.2.0/24" {
		t.Errorf("AS2 blocos = %v", got)
	}
	if d.PrefixesV4 != 4 || d.PrefixesV6 != 1 {
		t.Errorf("contagem = %d/%d", d.PrefixesV4, d.PrefixesV6)
	}
	if d.WarningCount() < 4 {
		t.Errorf("esperava avisos (host bits, lixo, duplicados, ASN repetido): %v", d.Warnings)
	}
}

func TestParseRejectsBrokenFile(t *testing.T) {
	cases := map[string]string{
		"vazio":          "\n# só comentário\n",
		"formato mudou":  "ASN;nome;doc\nAS1;x;y\n",
		"ASN sem prefix": strings.Repeat("123|x|y\n", 10),
	}
	for name, in := range cases {
		if _, err := Parse(strings.NewReader(in)); err == nil {
			t.Errorf("%s: esperava erro", name)
		}
	}
}

func TestParseToleratesFewBadLines(t *testing.T) {
	in := strings.Repeat("AS1|x|y|192.0.2.0/24\n", 200) + "ASX|ruim|z\n"
	d, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("0,5%% de linhas ruins deveria passar: %v", err)
	}
	if d.Skipped != 1 {
		t.Errorf("Skipped = %d", d.Skipped)
	}
}
