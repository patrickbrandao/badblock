package parse

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

const samplePath = "../../testdata/root-zone-sample.zone"

func readSample(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(samplePath)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func parseString(t *testing.T, s string) *Dataset {
	t.Helper()
	d, err := Parse(strings.NewReader(s))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return d
}

func find(d *Dataset, owner, typ string) []Record {
	var out []Record
	for _, r := range d.Records {
		if r.Owner == owner && r.Type == typ {
			out = append(out, r)
		}
	}
	return out
}

func tld(d *Dataset, name string) *TLD {
	for i := range d.TLDs {
		if d.TLDs[i].Name == name {
			return &d.TLDs[i]
		}
	}
	return nil
}

func TestParseSample(t *testing.T) {
	d := parseString(t, readSample(t))
	if d.Lines != 212 || len(d.Records) != 195 || d.RRSIGs != 17 || d.Skipped != 0 || d.WarningCount() != 0 {
		t.Fatalf("lines %d, records %d, rrsigs %d, skipped %d, warnings %v",
			d.Lines, len(d.Records), d.RRSIGs, d.Skipped, d.Warnings)
	}
	want := map[string]int{"A": 59, "AAAA": 55, "DNSKEY": 4, "DS": 6, "NS": 61, "NSEC": 8, "RRSIG": 17, "SOA": 1, "ZONEMD": 1}
	for k, v := range want {
		if d.TypeCounts[k] != v {
			t.Errorf("TypeCounts[%s] = %d, quero %d", k, d.TypeCounts[k], v)
		}
	}
	soa := SOA{TTL: 86400, MName: "a.root-servers.net", RName: "nstld.verisign-grs.com",
		Serial: 2026092901, Refresh: 1800, Retry: 900, Expire: 604800, Minimum: 86400}
	if d.SOA != soa {
		t.Errorf("SOA = %+v", d.SOA)
	}
	if r := find(d, ".", "SOA"); len(r) != 1 || r[0].RData != "a.root-servers.net nstld.verisign-grs.com 2026092901 1800 900 604800 86400" {
		t.Errorf("SOA em Records = %+v", r)
	}
	if n := len(find(d, ".", "NS")); n != 13 {
		t.Errorf("NS da raiz = %d", n)
	}
	if r := find(d, "a.root-servers.net", "A"); len(r) != 1 || r[0].RData != "198.41.0.4" || r[0].TTL != 518400 {
		t.Errorf("a.root-servers.net A = %+v", r)
	}

	tlds := []TLD{
		{"aaa", "aaa", 6, 6, 6, 1},
		{"bo", "bo", 4, 4, 3, 0},
		{"br", "br", 6, 6, 6, 1},
		{"com", "com", 13, 13, 13, 1},
		{"top", "top", 8, 6, 3, 2},
		{"xn--p1ai", "рф", 6, 6, 6, 1},
		{"zw", "zw", 5, 5, 5, 0},
	}
	if len(d.TLDs) != len(tlds) {
		t.Fatalf("TLDs = %+v", d.TLDs)
	}
	for i, w := range tlds {
		if d.TLDs[i] != w {
			t.Errorf("TLD %d = %+v, quero %+v", i, d.TLDs[i], w)
		}
	}

	checks := []struct{ owner, typ, rdata string }{
		{"br", "NS", "a.dns.br"},
		{"br", "DS", "38298 13 2 9F2D4993F47B0F2751DE0007D70A2754EE532FE373761154D9EA7A8CB9D8EA18"},
		{"br", "NSEC", "bradesco NS DS RRSIG NSEC"},
		{"a.dns.br", "A", "200.219.148.10"},
		{"a.dns.br", "AAAA", "2001:12f8:6::10"}, // no arquivo: 2001:12f8:6:0:0:0:0:10
		{"ns.dns.br", "AAAA", "2001:12ff:0:a20::5"},
		{"i.zdnscloud.cn", "AAAA", "2401:8d00:1::1"},
		{"zw", "NSEC", ". NS RRSIG NSEC"},
		{"xn--p1ai", "NS", "c.tld-servers.ru"},
		{".", "NSEC", "aaa NS SOA RRSIG NSEC DNSKEY ZONEMD"},
		{".", "ZONEMD", "2026092901 1 1 E80BFF012C499FB7532E91A1926E336362AE85DFD6715DB7390175BC8A800F10AE18369000A47566523182623B0FFF99"},
	}
	for _, c := range checks {
		ok := false
		for _, r := range find(d, c.owner, c.typ) {
			ok = ok || r.RData == c.rdata
		}
		if !ok {
			t.Errorf("sem %s %s %s; tem %+v", c.owner, c.typ, c.rdata, find(d, c.owner, c.typ))
		}
	}
	if k := find(d, ".", "DNSKEY"); len(k) != 4 || !strings.HasPrefix(k[2].RData, "257 3 8 AwEAAaz/tAm8yTn4Mfeh5eyI96WSVexTBAvkMgJzkKTOiW1vkIbzxeF3") {
		t.Errorf("DNSKEY = %+v", k)
	}
	if n := len(find(d, "i.zdnscloud.cn", "A")); n != 0 {
		t.Errorf("i.zdnscloud.cn não tem A na zona: %d", n)
	}
	for _, r := range d.Records {
		if r.Type == "RRSIG" {
			t.Fatal("RRSIG não é guardado")
		}
	}
}

func TestName(t *testing.T) {
	good := map[string]string{
		".":                           ".",
		"br.":                         "br",
		"A.DNS.BR.":                   "a.dns.br",
		"xn--p1ai.":                   "xn--p1ai",
		"_dmarc.example.":             "_dmarc.example",
		strings.Repeat("a", 63) + ".": strings.Repeat("a", 63),
	}
	for in, want := range good {
		if got, err := Name(in); err != nil || got != want {
			t.Errorf("Name(%q) = %q, %v; quero %q", in, got, err, want)
		}
	}
	for _, in := range []string{"br", "", "..", "a..br.", "a b.", "é.", strings.Repeat("a", 64) + ".",
		strings.Repeat("abcdefghi.", 26) + "."} {
		if got, err := Name(in); err == nil {
			t.Errorf("Name(%q) = %q; esperava erro", in, got)
		}
	}
}

// Variações de formato que o parser aceita e normaliza.
func TestParseEdgeCases(t *testing.T) {
	zone := "\uFEFF; comentário no início\r\n" +
		".\t86400\tIN\tSOA\tA.ROOT-SERVERS.NET. nstld.verisign-grs.com. 2026093001 1800 900 604800 86400 ; fim\r\n" +
		"\n" +
		". 518400 in ns a.root-servers.net.\n" +
		"A.Root-Servers.Net.   518400   IN   A   198.41.0.4\n" +
		"a.root-servers.net. 518400 IN AAAA 2001:503:BA3E:0:0:0:2:30\n" +
		"BR. 172800 IN NS A.DNS.BR.\n" +
		"br. 86400 IN DS 38298 13 2 9f2d4993f47b0f2751de0007d70a2754 ee532fe373761154d9ea7a8cb9d8ea18\n" +
		". 172800 IN DNSKEY 257 3 8 AwEAAaz/tAm8 yTn4Mfeh5eyI\n" +
		"br. 86400 IN nsec bradesco. ns ds rrsig nsec\n" +
		"br. 86400 IN RRSIG DS 8 1 86400 20261012170000 20260929160000 57780 . abc\n"
	d := parseString(t, zone)
	if d.Lines != 9 || d.Skipped != 0 || d.RRSIGs != 1 || len(d.Records) != 8 {
		t.Fatalf("lines %d skipped %d rrsigs %d records %+v warnings %v", d.Lines, d.Skipped, d.RRSIGs, d.Records, d.Warnings)
	}
	if d.SOA.MName != "a.root-servers.net" || d.SOA.Serial != 2026093001 {
		t.Errorf("SOA = %+v", d.SOA)
	}
	want := map[string]string{
		".|NS":                    "a.root-servers.net",
		"a.root-servers.net|A":    "198.41.0.4",
		"a.root-servers.net|AAAA": "2001:503:ba3e::2:30",
		"br|NS":                   "a.dns.br",
		"br|DS":                   "38298 13 2 9F2D4993F47B0F2751DE0007D70A2754EE532FE373761154D9EA7A8CB9D8EA18",
		".|DNSKEY":                "257 3 8 AwEAAaz/tAm8yTn4Mfeh5eyI",
		"br|NSEC":                 "bradesco NS DS RRSIG NSEC",
	}
	for k, v := range want {
		owner, typ, _ := strings.Cut(k, "|")
		if r := find(d, owner, typ); len(r) != 1 || r[0].RData != v {
			t.Errorf("%s = %+v, quero %q", k, r, v)
		}
	}
	if len(d.TLDs) != 1 || d.TLDs[0] != (TLD{"br", "br", 1, 0, 0, 1}) {
		t.Errorf("TLDs = %+v", d.TLDs)
	}
}

// Cada linha ruim é descartada com um aviso (uma por vez no recorte real,
// abaixo de 1%).
func TestParseDiscards(t *testing.T) {
	cases := map[string]string{
		"br 172800 IN NS a.dns.br.":                    "relativo",
		"br. -1 IN NS a.dns.br.":                       "TTL inválido",
		"br. 4294967295 IN NS a.dns.br.":               "TTL inválido",
		"br. 172800 CH NS a.dns.br.":                   `classe "CH"`,
		"br. 172800 IN TXT \"x\"":                      `tipo "TXT" desconhecido`,
		"br. 172800 IN NS":                             "4 campos",
		"$ORIGIN .":                                    "diretiva não suportada",
		"$TTL 86400":                                   "diretiva não suportada",
		"br. 172800 IN SOA ( a. b. 1 2 3 4 5 )":        "parênteses",
		" 172800 IN NS a.dns.br.":                      "sem dono",
		"a.dns.br. 172800 IN A 2001:db8::1":            "endereço inválido",
		"a.dns.br. 172800 IN A 300.1.1.1":              "endereço inválido",
		"a.dns.br. 172800 IN AAAA 200.219.148.10":      "endereço inválido",
		"a.dns.br. 172800 IN AAAA fe80::1%eth0":        "endereço inválido",
		"a.dns.br. 172800 IN A 200.219.148.10 1.2.3.4": "2 campos no rdata",
		"br. 86400 IN DS 38298 13 2 XYZ":               "digest em hex inválido",
		"br. 86400 IN DS 70000 13 2 9F2D":              "primeiro campo inválido",
		"br. 86400 IN DS 38298 13 2":                   "esperado 4 ou mais",
		". 86400 IN DS 20326 8 2 E06D":                 "DS de ., que não é um TLD",
		"a.dns.br. 86400 IN DS 20326 8 2 E06D":         "DS de a.dns.br, que não é um TLD",
		". 172800 IN DNSKEY 257 3 8 ***":               "base64 inválida",
		"dns.br. 172800 IN NS a.dns.br.":               "NS de dns.br, que não é a raiz nem um TLD",
		"br. 86400 IN SOA a. b. 1 2 3 4 5":             "SOA fora da raiz",
		"br. 86400 IN NSEC bradesco. NS D-S":           "tipo inválido",
		"br. 86400 IN RRSIG DS 8 1 86400":              "esperado 9 ou mais",
		"br. 86400 IN NS a.dns.br. extra.":             "2 campos no rdata",
		"br\xff. 172800 IN NS a.dns.br.":               "não é UTF-8",
		"a.root-servers.net. 518400 IN A 198.41.0.4":   "repetido",
	}
	base := readSample(t)
	for line, want := range cases {
		d, err := Parse(strings.NewReader(base + line + "\n"))
		if err != nil {
			t.Errorf("%q: arquivo recusado: %v", line, err)
			continue
		}
		if d.Skipped != 1 || len(d.Warnings) != 1 || !strings.Contains(d.Warnings[0], "linha 213: linha descartada: ") ||
			!strings.Contains(d.Warnings[0], want) {
			t.Errorf("%q: skipped %d, avisos %v; quero %q", line, d.Skipped, d.Warnings, want)
		}
	}
}

func TestParseRejectsBrokenFile(t *testing.T) {
	sample := readSample(t)
	soaLine := strings.SplitN(sample, "\n", 2)[0] + "\n"
	withoutRootNS := ""
	for line := range strings.Lines(sample) {
		if !strings.HasPrefix(line, ".\t\t\t518400\tIN\tNS\t") {
			withoutRootNS += line
		}
	}
	lines := strings.SplitAfter(sample, "\n")
	cases := map[string]struct{ body, want string }{
		"vazio":            {"", "arquivo sem nenhum registro"},
		"só comentários":   {"; nada\n\n", "arquivo sem nenhum registro"},
		"sem LF no fim":    {strings.TrimSuffix(sample, "\n"), "linha 212 sem fim de linha"},
		"cortado no meio":  {sample[:len(sample)/2], "sem fim de linha no fim do arquivo: arquivo cortado?"},
		"sem SOA":          {strings.TrimPrefix(sample, soaLine), "arquivo sem o SOA da raiz"},
		"dois SOA":         {sample + strings.Replace(soaLine, "2026092901", "2026092902", 1), "linha 213: mais de um SOA"},
		"sem NS da raiz":   {withoutRootNS, "arquivo sem os NS da raiz"},
		"formato novo":     {strings.ReplaceAll(sample, "\tIN\t", "\tCH\t"), "linhas descartadas"},
		"linha gigante":    {sample + "br. 1 IN NS " + strings.Repeat("a", MaxLineBytes) + ".\n", "com mais de 65536 bytes"},
		"linhas repetidas": {strings.Join(lines[:50], "") + strings.Join(lines[20:50], "") + strings.Join(lines[50:], ""), "linhas descartadas"},
	}
	for name, c := range cases {
		if _, err := Parse(strings.NewReader(c.body)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v; quero %q", name, err, c.want)
		}
	}
}

// 2 linhas ruins em 214 (0,93%) passam; 3 em 215 (1,4%) recusam o arquivo.
func TestParseSkippedLimit(t *testing.T) {
	base := readSample(t)
	bad := "br. 172800 CH NS a.dns.br.\n"
	d, err := Parse(strings.NewReader(base + bad + bad))
	if err != nil || d.Skipped != 2 {
		t.Fatalf("2 ruins: %v, %+v", err, d)
	}
	_, err = Parse(strings.NewReader(base + bad + bad + bad))
	if err == nil || !strings.Contains(err.Error(), "3 de 215 linhas descartadas (1.4%, limite 1.0%): formato mudou? primeiros avisos: linha 213:") {
		t.Fatalf("3 ruins: err = %v", err)
	}
}

func TestWarningsAreCapped(t *testing.T) {
	var b strings.Builder
	b.WriteString(readSample(t))
	for i := range 7000 { // glue sintético e único, para os 60 descartes ficarem abaixo de 1%
		fmt.Fprintf(&b, "h%d.nic.br. 172800 IN A 192.0.2.%d\n", i, i%256)
	}
	for range 60 {
		b.WriteString("br. 172800 CH NS a.dns.br.\n")
	}
	d := parseString(t, b.String())
	if len(d.Warnings) != MaxWarnings || d.WarningCount() != 60 || d.Skipped != 60 {
		t.Errorf("avisos guardados %d, total %d, descartes %d", len(d.Warnings), d.WarningCount(), d.Skipped)
	}
}

func TestPunycode(t *testing.T) {
	good := map[string]string{
		"p1ai":         "рф",
		"fiqs8s":       "中国",
		"90ais":        "бел",
		"mgbaam7a8h":   "امارات",
		"11b4c3d":      "कॉम",
		"1ck2e1b":      "セール",
		"3e0b707e":     "한국",
		"bcher-kva":    "bücher",
		"-> $1.00 <--": "-> $1.00 <-",
	}
	for in, want := range good {
		if got, err := Punycode(in); err != nil || got != want {
			t.Errorf("Punycode(%q) = %q, %v; quero %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "9", "p1a!", "zzzzzzzzzzzz", "é-p1ai"} {
		if got, err := Punycode(in); err == nil {
			t.Errorf("Punycode(%q) = %q; esperava erro", in, got)
		}
	}
}

func TestPunycodeInvalidTLDWarns(t *testing.T) {
	d := parseString(t, readSample(t)+"xn--zzzzzzzzzzzz. 172800 IN NS a.dns.br.\n")
	x := tld(d, "xn--zzzzzzzzzzzz")
	if x == nil || x.Unicode != "xn--zzzzzzzzzzzz" || d.WarningCount() != 1 || !strings.Contains(d.Warnings[0], "TLD xn--zzzzzzzzzzzz: punycode inválido") {
		t.Errorf("tld = %+v, avisos %v", x, d.Warnings)
	}
}

// TestParseRealFile lê o root.zone inteiro do dia quando ROOTZONE_REAL_FILE
// aponta para ele (pulado se ausente; make test-real).
func TestParseRealFile(t *testing.T) {
	path := os.Getenv("ROOTZONE_REAL_FILE")
	if path == "" {
		t.Skip("defina ROOTZONE_REAL_FILE com o caminho do root.zone")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	start := time.Now()
	d, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if d.Skipped != 0 || len(d.TLDs) < 1000 {
		t.Errorf("descartes %d, TLDs %d, avisos %v", d.Skipped, len(d.TLDs), d.Warnings)
	}
	idn, signed, v6 := 0, 0, 0
	for _, x := range d.TLDs {
		if x.Unicode != x.Name {
			idn++
		}
		if x.DSRecords > 0 {
			signed++
		}
		if x.NameserversIPv6 > 0 {
			v6++
		}
	}
	t.Logf("parser: %v; serial %d; %d linhas, %d registros guardados, %d RRSIG, %d TLDs (%d IDN, %d com DS, %d com IPv6); tipos %v; avisos %d",
		elapsed, d.SOA.Serial, d.Lines, len(d.Records), d.RRSIGs, len(d.TLDs), idn, signed, v6, d.TypeCounts, d.WarningCount())
}
