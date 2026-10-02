package parse

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func sampleDataset(t *testing.T) *Dataset {
	t.Helper()
	f, err := os.Open("../../testdata/asn-sample.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestParseSample(t *testing.T) {
	d := sampleDataset(t)
	if got := len(d.ASNs); got != 37 {
		t.Errorf("ASNs = %d, quero 37", got)
	}
	if d.WarningCount() != 0 || d.Skipped != 0 {
		t.Errorf("avisos inesperados: %v", d.Warnings)
	}

	byNum := map[int64]ASN{}
	for _, a := range d.ASNs {
		byNum[a.Number] = a
	}
	cases := []struct {
		asn                   int64
		description           string
		handle, name, country string
	}{
		// ARIN/APNIC/LACNIC: "handle - nome, CC"; vírgulas no nome ficam.
		{1, "LVLT-1 - Level 3 Parent, LLC, US", "LVLT-1", "Level 3 Parent, LLC", "US"},
		{15169, "GOOGLE - Google LLC, US", "GOOGLE", "Google LLC", "US"},
		{28000, "AS28000 - LACNIC - Latin American and Caribbean IP address, UY", "AS28000", "LACNIC - Latin American and Caribbean IP address", "UY"},
		// ASN de 32 bits.
		{262287, "AS262287 - Latitude.sh LTDA, BR", "AS262287", "Latitude.sh LTDA", "BR"},
		{403009, "LOREM-IPSUM - Lorem, US", "LOREM-IPSUM", "Lorem", "US"},
		// RIPE NCC: sem " - "; handle é a primeira palavra.
		{28, "DFVLR-SYS Deutsches Zentrum fuer Luft- und Raumfahrt e.V., DE", "DFVLR-SYS", "Deutsches Zentrum fuer Luft- und Raumfahrt e.V.", "DE"},
		// RIPE NCC com " - " dentro do nome da organização.
		{13040, "ASN-FIZ FIZ Karlsruhe - Leibniz-Institut fuer Informationsinfrastruktur GmbH, DE", "ASN-FIZ", "FIZ Karlsruhe - Leibniz-Institut fuer Informationsinfrastruktur GmbH", "DE"},
		{2047, "ASN-ROCHE-BASLE Hoffmann - La Roche Ltd., CH", "ASN-ROCHE-BASLE", "Hoffmann - La Roche Ltd.", "CH"},
		{6794, "ASN-HRTNET  HRT - Croatian Radio Television, HR", "ASN-HRTNET", "HRT - Croatian Radio Television", "HR"},
		// Handle ARIN com espaço: ambíguo com o formato RIPE; vale a regra 4.
		{511, "PRISMA HEALTH - Prisma Health, US", "PRISMA", "HEALTH - Prisma Health", "US"},
		// RIPE NCC só com o handle.
		{2799, "Polismyndigheten, SE", "Polismyndigheten", "", "SE"},
		// AFRINIC "X - X", com espaços e com " - " dentro de X.
		{29571, "Orange Côte d'Ivoire - Orange Côte d'Ivoire, CI", "Orange Côte d'Ivoire", "Orange Côte d'Ivoire", "CI"},
		{30619, "TMCEL - Moçambique Telecom, SA - TMCEL - Moçambique Telecom, SA, MZ", "TMCEL - Moçambique Telecom, SA", "TMCEL - Moçambique Telecom, SA", "MZ"},
		{5536, "Xyberdata - Xyberdata, MU", "Xyberdata", "Xyberdata", "MU"},
		{328289, "CEE DEE INVESTMENT Company Limited - CEE DEE INVESTMENT Company Limited, SL", "CEE DEE INVESTMENT Company Limited", "CEE DEE INVESTMENT Company Limited", "SL"},
		// Nome vazio e handle vazio.
		{4745, "AS4745-138 - , KR", "AS4745-138", "", "KR"},
		{7901, "- , NZ", "", "", "NZ"},
		// Códigos regionais e mojibake/controles C1 da fonte, preservados.
		{248, "IDDQD-AS - IDDQD-AS, EU", "IDDQD-AS", "IDDQD-AS", "EU"},
		{136209, "CYBERFORESTLLC-AS-AP - CyberForest LLC., AP", "CYBERFORESTLLC-AS-AP", "CyberForest LLC.", "AP"},
		{56122, "VALE-SA-AP - Avenida GraÃƒÂ§a Aranha, 26 Castelo, SG", "VALE-SA-AP", "Avenida GraÃƒÂ§a Aranha, 26 Castelo", "SG"},
		{59265, "CT-CNGI - China telecom â\u0080\u0093 China Next Generation Internet, CN", "CT-CNGI", "China telecom â\u0080\u0093 China Next Generation Internet", "CN"},
	}
	for _, c := range cases {
		a, ok := byNum[c.asn]
		if !ok {
			t.Errorf("AS%d ausente", c.asn)
			continue
		}
		if a.Description != c.description || a.Handle != c.handle || a.Name != c.name || a.Country != c.country {
			t.Errorf("AS%d = %+v\nquero description=%q handle=%q name=%q country=%q",
				c.asn, a, c.description, c.handle, c.name, c.country)
		}
	}
}

func TestDerive(t *testing.T) {
	cases := []struct{ in, handle, name, country string }{
		{"FOO - Bar, BR", "FOO", "Bar", "BR"},
		{"FOO - Bar", "FOO", "Bar", ""},               // sem país
		{"FOO - Bar, br", "FOO", "Bar, br", ""},       // país em minúsculas não conta
		{"FOO - Bar, BRA", "FOO", "Bar, BRA", ""},     // três letras não conta
		{"FOO Bar Baz, DE", "FOO", "Bar Baz", "DE"},   // formato RIPE
		{"FOO, DE", "FOO", "", "DE"},                  // só handle
		{"FOO", "FOO", "", ""},                        // só handle, sem país
		{", US", "", "", "US"},                        // só país
		{"A - A, ZA", "A", "A", "ZA"},                 // AFRINIC mínimo
		{"A - B - A - B, ZA", "A - B", "A - B", "ZA"}, // AFRINIC com separador dentro
		{"X -Y - Z, KR", "X", "-Y - Z", "KR"},
		// Linha real (AS401635): espaço duplo antes do separador.
		{"WALSWORTH  - EAU CLAIRE - WALSWORTH PUBLISHING COMPANY, US", "WALSWORTH", "EAU CLAIRE - WALSWORTH PUBLISHING COMPANY", "US"}, // antes do separador tem espaço: regra 4
	}
	for _, c := range cases {
		h, n, cc := Derive(c.in)
		if h != c.handle || n != c.name || cc != c.country {
			t.Errorf("Derive(%q) = %q, %q, %q; quero %q, %q, %q", c.in, h, n, cc, c.handle, c.name, c.country)
		}
	}
}

func TestParseEdgeCases(t *testing.T) {
	in := "\xef\xbb\xbf# comentário\n" +
		"1 FOO - Foo\xff Inc, US\r\n" + // UTF-8 inválido vira U+FFFD
		"\n" +
		"2\tBAR - Bar\x00Corp, BR\n" + // tab como separador; NUL vira U+FFFD
		"1 FOO2 - Outro, US\n" + // repetido: vale o primeiro
		"4294967295 MAX - Max, ZZ\n" +
		"3 X - Y, DE\n" +
		lines(10, 209)

	d, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.ASNs) != 204 || d.Skipped != 1 {
		t.Fatalf("ASNs = %d, Skipped = %d", len(d.ASNs), d.Skipped)
	}
	if a := d.ASNs[0]; a.Number != 1 || a.Handle != "FOO" || a.Name != "Foo� Inc" {
		t.Errorf("AS1 = %+v", a)
	}
	if a := d.ASNs[1]; a.Number != 2 || a.Handle != "BAR" || a.Name != "Bar�Corp" || a.Country != "BR" {
		t.Errorf("AS2 = %+v", a)
	}
	if a := d.ASNs[2]; a.Number != 4294967295 {
		t.Errorf("ASN máximo de 32 bits = %+v", a)
	}
	if d.WarningCount() != 1 || !strings.Contains(d.Warnings[0], "AS1 repetido") {
		t.Errorf("avisos = %v", d.Warnings)
	}
}

// lines gera linhas válidas "<n> H<n> - N, DE" de from até to.
func lines(from, to int) string {
	var b strings.Builder
	for i := from; i <= to; i++ {
		n := strconv.Itoa(i)
		b.WriteString(n + " H" + n + " - N, DE\n")
	}
	return b.String()
}

func TestParseRejectsBrokenFile(t *testing.T) {
	cases := map[string]string{
		"vazio":                "\n# só comentário\n",
		"formato mudou":        "asn;handle;nome\n1;x;y\n",
		"ASN com AS":           strings.Repeat("AS1 X - Y, US\n", 10),
		"ASN acima de 32 bits": "4294967296 X - Y, US\n",
		"ASN negativo":         "-1 X - Y, US\n",
		"sem descrição":        "1\n2 \n",
		"muitos repetidos":     strings.Repeat("1 X - Y, US\n", 10),
	}
	for name, in := range cases {
		if _, err := Parse(strings.NewReader(in)); err == nil {
			t.Errorf("%s: esperava erro", name)
		}
	}
}

func TestParseToleratesFewBadLines(t *testing.T) {
	d, err := Parse(strings.NewReader(lines(1, 200) + "ASX ruim\n"))
	if err != nil {
		t.Fatalf("0,5%% de linhas ruins deveria passar: %v", err)
	}
	if d.Skipped != 1 || len(d.ASNs) != 200 {
		t.Errorf("Skipped = %d, ASNs = %d", d.Skipped, len(d.ASNs))
	}
}

func TestParseRejectsJustOverLimit(t *testing.T) {
	in := lines(1, 98) + "x ruim\ny ruim\n" // 2 de 100 = 2%
	if _, err := Parse(strings.NewReader(in)); err == nil || !strings.Contains(err.Error(), "descartadas") {
		t.Fatalf("2%% de linhas ruins deveria recusar: %v", err)
	}
}

// TestParseRealFile lê o arquivo real inteiro, quando RIPE_ASNAMES_REAL_FILE aponta
// para uma cópia de https://ftp.ripe.net/ripe/asnames/asn.txt.
func TestParseRealFile(t *testing.T) {
	path := os.Getenv("RIPE_ASNAMES_REAL_FILE")
	if path == "" {
		t.Skip("defina RIPE_ASNAMES_REAL_FILE para testar com o arquivo real")
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
	if d.Skipped != 0 || d.WarningCount() != 0 {
		t.Errorf("descartes inesperados: %d (%v)", d.Skipped, d.Warnings)
	}
	if len(d.ASNs) < 100000 {
		t.Errorf("só %d ASNs", len(d.ASNs))
	}
	var noCountry, noHandle, noName int
	for _, a := range d.ASNs {
		if a.Country == "" {
			noCountry++
		}
		if a.Handle == "" {
			noHandle++
		}
		if a.Name == "" {
			noName++
		}
	}
	t.Logf("%d ASNs em %s; sem país %d, sem handle %d, sem nome %d", len(d.ASNs), elapsed, noCountry, noHandle, noName)
}
