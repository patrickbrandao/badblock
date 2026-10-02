package parse

import (
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/source"
)

// loadDir lê os 10 arquivos de uma pasta pelo nome no servidor (asn.json,
// as-numbers-1.csv...).
func loadDir(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	for _, f := range source.Files {
		b, err := os.ReadFile(filepath.Join(dir, f.Basename()))
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name] = b
	}
	return files
}

func fixtures(t *testing.T) map[string][]byte {
	return loadDir(t, "../../testdata")
}

func mustParse(t *testing.T, files map[string][]byte) *Dataset {
	t.Helper()
	d, err := Parse(files)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func asnBlock(d *Dataset, start int64) (ASNBlock, bool) {
	for _, b := range d.ASNBlocks {
		if b.Start == start {
			return b, true
		}
	}
	return ASNBlock{}, false
}

func prefixBlock(d *Dataset, p string) (PrefixBlock, bool) {
	want := netip.MustParsePrefix(p)
	for _, b := range d.PrefixBlocks {
		if b.Prefix == want {
			return b, true
		}
	}
	return PrefixBlock{}, false
}

func special(d *Dataset, p string) (SpecialPrefix, bool) {
	want := netip.MustParsePrefix(p)
	for _, s := range d.SpecialPrefixes {
		if s.Prefix == want {
			return s, true
		}
	}
	return SpecialPrefix{}, false
}

func TestParseFixtures(t *testing.T) {
	d := mustParse(t, fixtures(t))
	if d.WarningCount() != 0 {
		t.Errorf("avisos inesperados: %v", d.Warnings)
	}
	want := map[string][2]int{ // arquivo → {linhas, registros}
		source.ASNumbers1:  {11, 11},
		source.ASNumbers2:  {10, 10}, // "See Sub-registry" ignorada
		source.IPv4Space:   {16, 16},
		source.IPv6Unicast: {14, 14},
		source.SpecialIPv4: {8, 9}, // "192.0.0.170/32, 192.0.0.171/32" vira duas
		source.SpecialIPv6: {8, 8},
		source.SpecialASN:  {5, 5},
		source.RDAPASN:     {15, 15},
		source.RDAPIPv4:    {15, 15},
		source.RDAPIPv6:    {13, 13},
	}
	for name, w := range want {
		st := d.Files[name]
		if st.Records != w[0] || st.Rows != w[1] || st.Skipped != 0 {
			t.Errorf("%s = %+v, quero %d linhas / %d registros", name, st, w[0], w[1])
		}
	}
	if len(d.ASNBlocks) != 21 || len(d.PrefixBlocks) != 30 || len(d.SpecialPrefixes) != 17 ||
		len(d.SpecialASNs) != 5 || len(d.RDAPServices) != 43 {
		t.Errorf("totais = %d/%d/%d/%d/%d", len(d.ASNBlocks), len(d.PrefixBlocks), len(d.SpecialPrefixes),
			len(d.SpecialASNs), len(d.RDAPServices))
	}
}

func TestParseASNBlocks(t *testing.T) {
	d := mustParse(t, fixtures(t))

	arin, _ := asnBlock(d, 1)
	if arin.End != 1876 || arin.Registry != "arin" || arin.WHOIS != "whois.arin.net" || arin.SourceFile != source.ASNumbers1 {
		t.Errorf("1-1876 = %+v", arin)
	}
	// Duas URLs coladas na coluna RDAP.
	if !slices.Equal(arin.RDAPURLs, []string{"https://rdap.arin.net/registry", "http://rdap.arin.net/registry"}) {
		t.Errorf("RDAP 1-1876 = %q", arin.RDAPURLs)
	}
	if b, _ := asnBlock(d, 0); b.Registry != "" || b.Reference != "[RFC7607]" || len(b.RDAPURLs) != 0 || b.Description != "Reserved" {
		t.Errorf("AS0 = %+v", b)
	}
	if b, _ := asnBlock(d, 23456); b.Description != "AS_TRANS" || b.Registry != "" {
		t.Errorf("AS23456 = %+v", b)
	}
	if b, _ := asnBlock(d, 1877); b.Registry != "ripencc" || b.RegistrationDate != "" {
		t.Errorf("1877-1901 = %+v", b)
	}
	if b, _ := asnBlock(d, 64297); b.Registry != "apnic" || b.RegistrationDate != "2016-05-25" {
		t.Errorf("64297-64395 = %+v", b)
	}
	if b, _ := asnBlock(d, 36864); b.Registry != "afrinic" || len(b.RDAPURLs) != 2 {
		t.Errorf("36864-37887 = %+v", b)
	}
	// as-numbers-2: a linha 0-65535 "See Sub-registry" não entra (e não é
	// confundida com o AS0 do as-numbers-1).
	if b, _ := asnBlock(d, 0); b.SourceFile != source.ASNumbers1 {
		t.Errorf("AS0 veio de %s", b.SourceFile)
	}
	if b, ok := asnBlock(d, 404381); !ok || b.End != 4199999999 || b.Description != "Unallocated" || b.SourceFile != source.ASNumbers2 {
		t.Errorf("Unallocated = %+v", b)
	}
	if b, _ := asnBlock(d, 4294967295); b.End != 4294967295 || b.Reference != "[RFC7300]" {
		t.Errorf("AS4294967295 = %+v", b)
	}
}

func TestParsePrefixBlocks(t *testing.T) {
	d := mustParse(t, fixtures(t))

	// "000/8" → 0.0.0.0/8, nota só com as marcas de rodapé.
	b, ok := prefixBlock(d, "0.0.0.0/8")
	if !ok || b.Designation != "IANA - Local Identification" || b.Status != "RESERVED" || b.Note != "[2][3]" ||
		b.AllocationDate != "1981-09" || b.Registry != "" || b.WHOIS != "" || len(b.RDAPURLs) != 0 {
		t.Errorf("000/8 = %+v (ok=%v)", b, ok)
	}
	if b, _ := prefixBlock(d, "45.0.0.0/8"); b.Designation != "Administered by ARIN" || b.Registry != "arin" || b.Status != "LEGACY" {
		t.Errorf("045/8 = %+v", b)
	}
	// Legado com titular: o RIR sai do WHOIS.
	if b, _ := prefixBlock(d, "53.0.0.0/8"); b.Designation != "Daimler AG" || b.Registry != "ripencc" {
		t.Errorf("053/8 = %+v", b)
	}
	if b, _ := prefixBlock(d, "224.0.0.0/8"); b.Designation != "Multicast" || b.Registry != "" {
		t.Errorf("224/8 = %+v", b)
	}
	// IPv6: whois.iana.org não é RIR.
	if b, _ := prefixBlock(d, "2001::/23"); b.Registry != "" || b.WHOIS != "whois.iana.org" || b.SourceFile != source.IPv6Unicast {
		t.Errorf("2001::/23 = %+v", b)
	}
	// Nota com quebras de linha e espaço duplo vira uma linha só.
	b, _ = prefixBlock(d, "2400::/12")
	if strings.ContainsAny(b.Note, "\r\n") || strings.Contains(b.Note, "  ") ||
		!strings.HasPrefix(b.Note, "2400::/19 was allocated on 2005-05-20. 2400:2000::/19") || b.AllocationDate != "2006-10-03" {
		t.Errorf("2400::/12 nota = %q", b.Note)
	}
	if b, _ := prefixBlock(d, "3ffe::/16"); b.AllocationDate != "2008-04" || b.Status != "RESERVED" {
		t.Errorf("3ffe::/16 = %+v", b)
	}
	if b, _ := prefixBlock(d, "2c00::/12"); b.Registry != "afrinic" || len(b.RDAPURLs) != 2 {
		t.Errorf("2c00::/12 = %+v", b)
	}
}

func TestParseSpecialPrefixes(t *testing.T) {
	d := mustParse(t, fixtures(t))
	tr, fa := true, false

	s, _ := special(d, "0.0.0.0/8")
	if s.Name != `"This network"` || s.RFC != "[RFC791], Section 3.2" || s.AllocationDate != "1981-09" || s.TerminationDate != "" {
		t.Errorf("0.0.0.0/8 = %+v", s)
	}
	// "False [1]" → false.
	s, _ = special(d, "127.0.0.0/8")
	if !eq(s.Source, &fa) || !eq(s.GloballyReachable, &fa) || !eq(s.ReservedByProtocol, &tr) {
		t.Errorf("127.0.0.0/8 = %+v", s)
	}
	// "192.0.0.0/24 [2]": a nota de rodapé sai do bloco.
	if _, ok := special(d, "192.0.0.0/24"); !ok {
		t.Error("192.0.0.0/24 ausente")
	}
	// Duas faixas numa célula = duas linhas iguais.
	a, okA := special(d, "192.0.0.170/32")
	b, okB := special(d, "192.0.0.171/32")
	if !okA || !okB || a.Name != "NAT64/DNS64 Discovery" || b.Name != a.Name || b.RFC != "[RFC8880][RFC7050], Section 2.2" {
		t.Errorf("NAT64 = %+v / %+v", a, b)
	}
	// Registro encerrado: flags vazias viram NULL.
	s, _ = special(d, "192.88.99.0/24")
	if s.TerminationDate != "2015-03" || s.Source != nil || s.Destination != nil || s.Forwardable != nil ||
		s.GloballyReachable != nil || s.ReservedByProtocol != nil {
		t.Errorf("192.88.99.0/24 = %+v", s)
	}
	// Quebra de linha dentro da célula RFC.
	if s, _ := special(d, "255.255.255.255/32"); s.RFC != "[RFC8190] [RFC919], Section 7" || !eq(s.Destination, &tr) {
		t.Errorf("255.255.255.255/32 = %+v", s)
	}
	// "N/A [2]" → NULL; "False [4]" → false; "2002::/16 [3]" → 2002::/16.
	if s, _ := special(d, "2001::/32"); s.Name != "TEREDO" || s.GloballyReachable != nil || !eq(s.Source, &tr) {
		t.Errorf("2001::/32 = %+v", s)
	}
	if s, _ := special(d, "fc00::/7"); !eq(s.GloballyReachable, &fa) || s.RFC != "[RFC4193] [RFC8190]" {
		t.Errorf("fc00::/7 = %+v", s)
	}
	if s, ok := special(d, "2002::/16"); !ok || s.GloballyReachable != nil {
		t.Errorf("2002::/16 = %+v (ok=%v)", s, ok)
	}
	if s, _ := special(d, "2001:10::/28"); s.TerminationDate != "2014-03" || s.Source != nil {
		t.Errorf("2001:10::/28 = %+v", s)
	}
}

func TestParseSpecialASNsAndRDAP(t *testing.T) {
	d := mustParse(t, fixtures(t))
	if s := d.SpecialASNs[3]; s.Start != 4200000000 || s.End != 4294967294 || s.Reason != "For private use; reserved by [RFC6996]" || s.Reference != "[RFC6996]" {
		t.Errorf("special ASN = %+v", s)
	}

	byKey := map[string]RDAPService{}
	for _, s := range d.RDAPServices {
		byKey[s.Kind+" "+s.Resource] = s
	}
	if s := byKey["asn 1-1876"]; s.Start != 1 || s.End != 1876 || s.Registry != "arin" ||
		!slices.Equal(s.URLs, []string{"https://rdap.arin.net/registry/", "http://rdap.arin.net/registry/"}) {
		t.Errorf("rdap asn 1-1876 = %+v", s)
	}
	if s, ok := byKey["asn 2043"]; !ok || s.Start != 2043 || s.End != 2043 || s.Registry != "ripencc" {
		t.Errorf("rdap asn 2043 = %+v", s)
	}
	if s := byKey["ipv4 41.0.0.0/8"]; s.Registry != "afrinic" || s.Prefix.String() != "41.0.0.0/8" {
		t.Errorf("rdap ipv4 = %+v", s)
	}
	if s := byKey["ipv6 2800::/12"]; s.Registry != "lacnic" || s.Kind != KindIPv6 {
		t.Errorf("rdap ipv6 = %+v", s)
	}
	if p := d.Files[source.RDAPASN].Publication; p != "2026-06-01T20:00:01Z" {
		t.Errorf("publication = %q", p)
	}
	if p := d.Files[source.RDAPIPv4].Publication; p != "2019-06-07T19:00:02Z" {
		t.Errorf("publication ipv4 = %q", p)
	}
}

func eq(a, b *bool) bool { return a != nil && b != nil && *a == *b }

func TestSplitURLs(t *testing.T) {
	cases := map[string][]string{
		"":                          nil,
		"https://rdap.db.ripe.net/": {"https://rdap.db.ripe.net/"},
		"https://rdap.arin.net/registryhttp://rdap.arin.net/registry": {"https://rdap.arin.net/registry", "http://rdap.arin.net/registry"},
		"https://rdap.afrinic.net/rdap/http://rdap.afrinic.net/rdap/": {"https://rdap.afrinic.net/rdap/", "http://rdap.afrinic.net/rdap/"},
		"https://a.example/, https://b.example/":                      {"https://a.example/", "https://b.example/"},
		"https://a.example/ https://a.example/":                       {"https://a.example/"},
		" HTTPS://rdap.lacnic.net/rdap/ ":                             {"HTTPS://rdap.lacnic.net/rdap/"},
	}
	for in, want := range cases {
		got, ok := splitURLs(in)
		if !ok || !slices.Equal(got, want) {
			t.Errorf("splitURLs(%q) = %q, %v; quero %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"rdap.arin.net", "x https://a.example/", "https://"} {
		if _, ok := splitURLs(bad); ok {
			t.Errorf("splitURLs(%q) deveria falhar", bad)
		}
	}
}

func TestFieldParsers(t *testing.T) {
	for in, want := range map[string]string{
		"000/8": "0.0.0.0/8", "045/8": "45.0.0.0/8", "255/8": "255.0.0.0/8",
		"192.0.0.0/24 [2]": "192.0.0.0/24", "2002::/16 [3]": "2002::/16", "2001:db8::/32": "2001:db8::/32",
	} {
		p, host, err := parsePrefix(in)
		if err != nil || host || p.String() != want {
			t.Errorf("parsePrefix(%q) = %s, %v, %v", in, p, host, err)
		}
	}
	if p, host, err := parsePrefix("10.0.0.1/8"); err != nil || !host || p.String() != "10.0.0.0/8" {
		t.Errorf("bits de host: %s %v %v", p, host, err)
	}
	for _, bad := range []string{"256/8", "abc", "10.0.0.0", "1.2.3.4/33", ""} {
		if _, _, err := parsePrefix(bad); err == nil {
			t.Errorf("parsePrefix(%q) deveria falhar", bad)
		}
	}

	for in, want := range map[string][2]int64{"0": {0, 0}, "1-1876": {1, 1876}, "AS23456": {23456, 23456},
		"4200000000-4294967294": {4200000000, 4294967294}, " 64512 - 65534 [1]": {64512, 65534}} {
		a, b, err := parseASNRange(in)
		if err != nil || a != want[0] || b != want[1] {
			t.Errorf("parseASNRange(%q) = %d-%d, %v", in, a, b, err)
		}
	}
	for _, bad := range []string{"", "x", "5-1", "4294967296", "1-2-3"} {
		if _, _, err := parseASNRange(bad); err == nil {
			t.Errorf("parseASNRange(%q) deveria falhar", bad)
		}
	}

	for in, want := range map[string]string{"1981-09": "1981-09", "2008-12-03": "2008-12-03", "": "", "N/A": "", "2015-03 [1]": "2015-03"} {
		if got, ok := parseDate(in); !ok || got != want {
			t.Errorf("parseDate(%q) = %q, %v", in, got, ok)
		}
	}
	for _, bad := range []string{"2008-13", "2008-02-30", "1981", "set/1981"} {
		if _, ok := parseDate(bad); ok {
			t.Errorf("parseDate(%q) deveria falhar", bad)
		}
	}

	for in, want := range map[string]string{"True": "true", "False [1]": "false", "N/A [2]": "nil", "": "nil"} {
		v, ok := parseBool(in)
		got := "nil"
		if v != nil {
			got = map[bool]string{true: "true", false: "false"}[*v]
		}
		if !ok || got != want {
			t.Errorf("parseBool(%q) = %s, %v", in, got, ok)
		}
	}
	if _, ok := parseBool("talvez"); ok {
		t.Error("parseBool(talvez) deveria falhar")
	}

	for in, want := range map[string]string{"whois.arin.net": "arin", "WHOIS.RIPE.NET": "ripencc", "rdap.db.ripe.net": "ripencc",
		"whois.iana.org": "", "": "", "notarin.net": ""} {
		if got := registryFromHost(in); got != want {
			t.Errorf("registryFromHost(%q) = %q", in, got)
		}
	}
	for in, want := range map[string]string{"Assigned by RIPE NCC": "ripencc", "Administered by AFRINIC": "afrinic",
		"LACNIC": "lacnic", "Apple Computer Inc.": "", "Reserved": ""} {
		if got := registryFromName(in); got != want {
			t.Errorf("registryFromName(%q) = %q", in, got)
		}
	}
}

// replace troca um arquivo das fixtures.
func replace(t *testing.T, name, body string) map[string][]byte {
	files := fixtures(t)
	files[name] = []byte(body)
	return files
}

func TestParseRejects(t *testing.T) {
	missing := fixtures(t)
	delete(missing, source.RDAPIPv6)

	cases := map[string]map[string][]byte{
		"arquivo ausente":    missing,
		"coluna obrigatória": replace(t, source.ASNumbers1, "ASN,Descrição\r\n1,x\r\n"),
		"sem linhas":         replace(t, source.SpecialASN, "AS Number,Reason for Reservation,Reference\r\n"),
		"JSON inválido":      replace(t, source.RDAPIPv4, "<html>erro</html>"),
		"JSON sem services":  replace(t, source.RDAPIPv4, `{"description": "x"}`),
		"linha ruim em arquivo pequeno": replace(t, source.SpecialIPv4, string(fixtures(t)[source.SpecialIPv4])+
			"lixo,Nome,[RFC1],2020-01,N/A,True,True,True,True,False\r\n"),
		"status vazio": replace(t, source.IPv4Space, "Prefix,Designation,Date,WHOIS,RDAP,Status [1],Note\r\n"+
			"001/8,APNIC,2010-01,whois.apnic.net,https://rdap.apnic.net/,,\r\n"),
	}
	for name, files := range cases {
		if _, err := Parse(files); err == nil {
			t.Errorf("%s: esperava erro", name)
		}
	}
}

func TestParseToleratesFewBadLines(t *testing.T) {
	var b strings.Builder
	b.WriteString("Number,Description,WHOIS,RDAP,Reference,Registration Date\r\n")
	for i := range 200 {
		b.WriteString(formatASNRange(int64(i*2), int64(i*2+1)) + ",Assigned by ARIN,whois.arin.net,https://rdap.arin.net/registry,,\r\n")
	}
	b.WriteString("x-y,Lixo,,,,\r\n")
	d, err := Parse(replace(t, source.ASNumbers1, b.String()))
	if err != nil {
		t.Fatalf("0,5%% de linhas ruins deveria passar: %v", err)
	}
	if st := d.Files[source.ASNumbers1]; st.Skipped != 1 || st.Rows != 200 {
		t.Errorf("stats = %+v", st)
	}
}

func TestParseTolerantFields(t *testing.T) {
	// Colunas trocadas de lugar, coluna opcional ausente, data e flag
	// irreconhecíveis: o arquivo passa, com avisos.
	body := "Name,Address Block,RFC,Allocation Date,Source,Destination,Forwardable,Globally Reachable,Reserved-by-Protocol\r\n" +
		"Teste,198.51.100.0/24,[RFC5737],ontem,talvez,True,True,False,False\r\n"
	d, err := Parse(replace(t, source.SpecialIPv4, body))
	if err != nil {
		t.Fatal(err)
	}
	s, ok := special(d, "198.51.100.0/24")
	if !ok || s.Name != "Teste" || s.AllocationDate != "" || s.Source != nil || s.TerminationDate != "" {
		t.Errorf("special = %+v", s)
	}
	if d.WarningCount() != 3 { // coluna ausente, data, flag
		t.Errorf("avisos = %v", d.Warnings)
	}
}

func TestParseDuplicates(t *testing.T) {
	// AS65535 já vem do as-numbers-1: no as-numbers-2 é descartado, e sendo a
	// única linha (100% descartado) o dataset é recusado.
	as2 := "Number,Description,WHOIS,RDAP,Reference,Registration Date\r\n65535,Reserved,,,[RFC7300],\r\n"
	if _, err := Parse(replace(t, source.ASNumbers2, as2)); err == nil || !strings.Contains(err.Error(), "repetida") {
		t.Errorf("err = %v", err)
	}
}
