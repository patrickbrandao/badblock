package parse

import (
	"fmt"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/afrinic/collector/internal/rir"
)

const sampleFile = "../../testdata/delegated-extended-sample.txt"

func parseFile(t *testing.T, path, registry string) *Dataset {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, err := Parse(f, registry)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func date(s string) time.Time {
	t, _ := time.Parse("20060102", s)
	return t
}

// A fixture é um recorte real do arquivo da AFRINIC (15 registros, cabeçalho e
// summaries ajustados ao recorte).
func TestParseSample(t *testing.T) {
	d := parseFile(t, sampleFile, rir.Registry)
	h := d.Header
	if h.Version != "2" || h.Registry != rir.Registry || h.Serial != "20260928" || h.Records != 15 ||
		!h.StartDate.IsZero() || !h.EndDate.Equal(date("20260928")) || h.UTCOffset != "00000" {
		t.Errorf("cabeçalho = %+v", h)
	}
	if d.ASNRecords != 5 || d.IPv4Records != 6 || d.IPv6Records != 4 || d.Records() != 15 {
		t.Errorf("registros = %d/%d/%d", d.ASNRecords, d.IPv4Records, d.IPv6Records)
	}
	if d.PrefixesV4 != 9 || d.PrefixesV6 != 4 || len(d.Prefixes) != 13 || len(d.ASNs) != 5 {
		t.Errorf("blocos = %d/%d", d.PrefixesV4, d.PrefixesV6)
	}
	if d.WarningCount() != 0 || d.Skipped != 0 {
		t.Errorf("avisos inesperados: %v", d.Warnings)
	}

	asn := map[int64]ASN{}
	for _, a := range d.ASNs {
		asn[a.Start] = a
	}
	if a := asn[329814]; a.Count != 1 || a.CC != "NE" || a.Status != "allocated" || a.OpaqueID != "F3626E7D" || !a.Date.Equal(date("20260918")) {
		t.Errorf("AS329814 = %+v", a)
	}
	if a := asn[6187]; !a.Date.Equal(date("19840101")) || a.OpaqueID != "F36771FA" {
		t.Errorf("AS6187 (data mais antiga do arquivo) = %+v", a)
	}
	// A AFRINIC publica available/reserved com cc ZZ, data vazia e opaque-id
	// vazio no fim da linha (8 campos).
	if a := asn[8770]; a.Status != "available" || a.CC != "ZZ" || !a.Date.IsZero() || a.OpaqueID != "" || a.End() != 8770 {
		t.Errorf("AS8770 available = %+v", a)
	}
	if a := asn[10803]; a.Status != "reserved" || a.CC != "ZZ" || a.OpaqueID != "" {
		t.Errorf("AS10803 reservado = %+v", a)
	}

	// Registros IPv4 que não formam CIDR: 393216 endereços = /15 + /14;
	// 2560 = /22 + /22 + /23.
	want := "160.115.0.0/16 164.146.0.0/15 164.148.0.0/14 196.4.20.0/22 196.4.24.0/22 196.4.28.0/23 " +
		"102.201.36.0/22 41.57.112.0/21 102.201.0.0/20"
	if got := strings.Join(prefixStrings(d, 4), " "); got != want {
		t.Errorf("blocos IPv4 = %s", got)
	}
	pfx := map[string]Prefix{}
	for _, p := range d.Prefixes {
		pfx[p.Prefix.String()] = p
	}
	if p := pfx["196.4.28.0/23"]; p.RecordStart.String() != "196.4.20.0" || p.RecordValue != 2560 || p.OpaqueID != "F369838C" {
		t.Errorf("pedaço do registro não-CIDR = %+v", p)
	}
	if p := pfx["102.201.36.0/22"]; p.OpaqueID != "F3626E7D" || p.Status != "assigned" || p.RecordValue != 1024 || p.RecordStart.String() != "102.201.36.0" {
		t.Errorf("102.201.36.0/22 = %+v", p)
	}
	if p := pfx["41.57.112.0/21"]; p.Status != "reserved" || p.CC != "ZZ" || !p.Date.IsZero() || p.OpaqueID != "" {
		t.Errorf("IPv4 reservado = %+v", p)
	}
	if p := pfx["2001:4208::/29"]; p.Status != "available" || p.RecordValue != 29 || p.CC != "ZZ" {
		t.Errorf("IPv6 available = %+v", p)
	}
	if p := pfx["2001:43fe:3800::/48"]; p.Status != "assigned" || p.OpaqueID != "F3626E7D" {
		t.Errorf("IPv6 /48 = %+v", p)
	}
}

// Recortes reais dos outros quatro RIRs: o mesmo parser atende todos, trocando
// só o registry esperado.
func TestParseOtherRIRFormats(t *testing.T) {
	cases := []struct {
		registry         string
		version, serial  string
		start, end       string // startdate e enddate do cabeçalho ("" = vazia/00000000)
		asn, v4, v6, pv4 int
		check            func(t *testing.T, d *Dataset)
	}{
		{"lacnic", "2.3", "20260927", "19870101", "20260925", 3, 2, 2, 2, func(t *testing.T, d *Dataset) {
			// available/reserved com cc vazio (não ZZ); available com 7 campos.
			if a := d.ASNs[1]; a.Start != 28003 || a.Count != 3 || a.End() != 28005 || a.CC != "" || a.OpaqueID != "" {
				t.Errorf("faixa available AS28003 = %+v", a)
			}
			if a := d.ASNs[2]; a.OpaqueID != "258500" || d.Prefixes[1].OpaqueID != "258500" || d.Prefixes[3].OpaqueID != "258500" {
				t.Errorf("titular numérico em ASN e blocos = %+v / %+v", a, d.Prefixes)
			}
			if p := d.Prefixes[2]; p.Prefix.String() != "2001:1201:20::/43" || p.Status != "available" || p.OpaqueID != "" {
				t.Errorf("IPv6 available de 7 campos = %+v", p)
			}
			if d.Header.UTCOffset != "-0300" {
				t.Errorf("UTCOffset = %q", d.Header.UTCOffset)
			}
		}},
		{"apnic", "2.3", "20260929", "", "20260928", 1, 3, 1, 3, func(t *testing.T, d *Dataset) {
			if a := d.ASNs[0]; a.Start != 1768 || a.Count != 2 || a.End() != 1769 {
				t.Errorf("faixa de ASN = %+v", a)
			}
			if got := prefixStrings(d, 4); !contains(got, "163.61.160.0/26") {
				t.Errorf("bloco menor que /24 = %v", got)
			}
			if got := prefixStrings(d, 6); !contains(got, "2001:7fa::/64") {
				t.Errorf("IPv6 /64 = %v", got)
			}
		}},
		{"arin", "2.3", "1790600421096", "19700101", "20260928", 2, 1, 1, 2, func(t *testing.T, d *Dataset) {
			if a := d.ASNs[0]; a.Start != 3 || !a.Date.IsZero() || a.OpaqueID != "d98c567cda2db06e693f2b574eafe848" {
				t.Errorf("AS3 com data 00000000 = %+v", a)
			}
			if got := prefixStrings(d, 4); strings.Join(got, " ") != "23.128.1.0/24 23.128.2.0/23" {
				t.Errorf("768 endereços = %v", got)
			}
		}},
		{"ripencc", "2", "1790632799", "19700101", "20260928", 1, 2, 2, 3, func(t *testing.T, d *Dataset) {
			if got := prefixStrings(d, 4); strings.Join(got, " ") != "62.122.208.0/22 62.122.212.0/24 156.67.6.0/29" {
				t.Errorf("blocos IPv4 = %v", got)
			}
			for _, p := range d.Prefixes {
				if p.Prefix.String() == "62.122.212.0/24" && (p.RecordStart.String() != "62.122.208.0" || p.RecordValue != 1280) {
					t.Errorf("registro de origem do pedaço = %+v", p)
				}
			}
			if a := d.ASNs[0]; a.Status != "available" || a.OpaqueID != "" || a.CC != "" {
				t.Errorf("linha de 7 campos = %+v", a)
			}
			if p := d.Prefixes[3]; p.OpaqueID != "a71dd866-c717-4529-950b-2040370c754a" {
				t.Errorf("opaque-id UUID = %+v", p)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.registry, func(t *testing.T) {
			d := parseFile(t, "../../testdata/formats/"+c.registry+".txt", c.registry)
			if d.Header.Version != c.version || d.Header.Serial != c.serial || !d.Header.EndDate.Equal(date(c.end)) {
				t.Errorf("cabeçalho = %+v", d.Header)
			}
			if (c.start == "") != d.Header.StartDate.IsZero() {
				t.Errorf("startdate = %v, quero %q", d.Header.StartDate, c.start)
			}
			if d.ASNRecords != c.asn || d.IPv4Records != c.v4 || d.IPv6Records != c.v6 || d.PrefixesV4 != c.pv4 {
				t.Errorf("registros = %d/%d/%d, blocos v4 = %d", d.ASNRecords, d.IPv4Records, d.IPv6Records, d.PrefixesV4)
			}
			if d.WarningCount() != 0 {
				t.Errorf("avisos: %v", d.Warnings)
			}
			c.check(t, d)
		})
	}
}

func TestParseWrongRegistry(t *testing.T) {
	f, err := os.Open("../../testdata/formats/arin.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := Parse(f, "ripencc"); err == nil || !strings.Contains(err.Error(), `registry "arin"`) {
		t.Fatalf("err = %v", err)
	}
}

// file monta um arquivo coerente (cabeçalho e summaries batendo) com os
// registros dados, do registry "rir".
func file(records ...string) string {
	counts := map[string]int{}
	for _, r := range records {
		counts[strings.Split(r, "|")[2]]++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "2.3|rir|20260927|%d|19870101|20260925|-0300\n", len(records))
	for _, t := range []string{"asn", "ipv4", "ipv6"} {
		fmt.Fprintf(&b, "rir|*|%s|*|%d|summary\n", t, counts[t])
	}
	for _, r := range records {
		b.WriteString(r + "\n")
	}
	return b.String()
}

func good(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("rir|BR|asn|%d|1|20200101|allocated|%d", 64512+i, i)
	}
	return out
}

func TestParseRejectsBrokenFile(t *testing.T) {
	cases := map[string]string{
		"vazio":                 "\n# só comentário\n",
		"sem cabeçalho":         strings.Join(good(3), "\n"),
		"versão 3":              strings.Replace(file(good(3)...), "2.3|", "3.0|", 1),
		"registry errado":       strings.Replace(file(good(3)...), "|rir|", "|outro|", 1),
		"cabeçalho curto":       "2.3|rir|20260927\n" + strings.Join(good(3), "\n"),
		"contagem de registros": strings.Replace(file(good(3)...), "|3|19870101", "|4|19870101", 1),
		"summary divergente":    strings.Replace(file(good(3)...), "asn|*|3|summary", "asn|*|2|summary", 1),
		"truncado":              strings.TrimSuffix(file(good(3)...), good(3)[2]+"\n"),
		"summary ilegível":      strings.Replace(file(good(3)...), "asn|*|3|summary", "asn|*|x|summary", 1),
		"sem registros":         file(),
		"mais de 1% ruim":       file(append(good(98), "rir|BR|asn|x|1|20200101|allocated|1", "rir|BR|asn|64999|1|20200101|transferred|1")...),
	}
	for name, in := range cases {
		if _, err := Parse(strings.NewReader(in), "rir"); err == nil {
			t.Errorf("%s: esperava erro", name)
		}
	}
}

func TestParseToleratesFewBadLines(t *testing.T) {
	recs := append(good(199), "rir|BR|asn|x|1|20200101|allocated|1")
	d, err := Parse(strings.NewReader(file(recs...)), "rir")
	if err != nil {
		t.Fatalf("0,5%% de linhas ruins deveria passar: %v", err)
	}
	if d.Skipped != 1 || d.ASNRecords != 199 {
		t.Errorf("Skipped = %d, ASNs = %d", d.Skipped, d.ASNRecords)
	}
}

func TestParseRecordRules(t *testing.T) {
	recs := append(good(1000),
		"rir|br|ipv4|192.0.2.0|256|20200101|ALLOCATED|x1",        // cc e status normalizados
		"rir|BR|ipv6|2001:db8::1|32|20200101|assigned|x2",        // bits de host → 2001:db8::/32
		"rir|BR|ipv6|2001:db8::|32|20200101|assigned|x3",         // repetido → descartado
		"rir|BR|asn|64512|1|20200101|allocated|dup",              // ASN repetido → fica o primeiro
		"rir|BR|asn|70000|1|20201340|allocated|y",                // data inválida → sem data
		"rir|BR|asn|80000|1|20200101|transferred|z",              // status desconhecido → descartado
		"rir|B1|asn|80001|1|20200101|allocated|z",                // país inválido → descartado
		"rir|BR|asn|4294967295|2|20200101|allocated|z",           // faixa estoura 32 bits → descartado
		"rir|BR|ipv4|255.255.255.0|512|20200101|allocated|z",     // faixa estoura → descartado
		"rir|BR|ipv6|2001:db8:1::|129|20200101|allocated|z",      // prefixo inválido → descartado
		"rir||ipv4|198.51.100.0|256|00000000|reserved|",          // vazios → NULL
		"rir|BR|ipv4|203.0.113.0|256|20200101|allocated|o|ext|x", // extensões ignoradas
	)
	d, err := Parse(strings.NewReader(file(recs...)), "RIR")
	if err != nil {
		t.Fatal(err)
	}
	if d.Skipped != 5 {
		t.Errorf("Skipped = %d, quero 5: %v", d.Skipped, d.Warnings)
	}
	if d.ASNRecords != 1001 || d.IPv4Records != 3 || d.IPv6Records != 1 {
		t.Errorf("registros = %d/%d/%d", d.ASNRecords, d.IPv4Records, d.IPv6Records)
	}
	byPfx := map[string]Prefix{}
	for _, p := range d.Prefixes {
		byPfx[p.Prefix.String()] = p
	}
	if p := byPfx["192.0.2.0/24"]; p.CC != "BR" || p.Status != "allocated" {
		t.Errorf("normalização = %+v", p)
	}
	if p := byPfx["2001:db8::/32"]; p.OpaqueID != "x2" || p.RecordStart.String() != "2001:db8::1" {
		t.Errorf("primeira ocorrência = %+v", p)
	}
	if p := byPfx["198.51.100.0/24"]; p.CC != "" || !p.Date.IsZero() || p.OpaqueID != "" {
		t.Errorf("vazios = %+v", p)
	}
	if p := byPfx["203.0.113.0/24"]; p.OpaqueID != "o" {
		t.Errorf("extensões = %+v", p)
	}
	for _, a := range d.ASNs {
		if a.Start == 64512 && a.OpaqueID != "0" {
			t.Errorf("ASN repetido deveria manter o primeiro: %+v", a)
		}
		if a.Start == 70000 && !a.Date.IsZero() {
			t.Errorf("data inválida deveria ficar vazia: %+v", a)
		}
	}
	if d.WarningCount() < 9 {
		t.Errorf("avisos = %d: %v", d.WarningCount(), d.Warnings)
	}
}

func TestParseToleratesCommentsBOMAndCRLF(t *testing.T) {
	in := "\xef\xbb\xbf# comentário\r\n#\r\n" + strings.ReplaceAll(file(good(3)...), "\n", "\r\n") + "\r\n"
	d, err := Parse(strings.NewReader(in), "rir")
	if err != nil {
		t.Fatal(err)
	}
	if d.ASNRecords != 3 || d.ASNs[2].OpaqueID != "2" {
		t.Errorf("ASNs = %+v", d.ASNs)
	}
}

func TestParseWarnsOverlapsAndMissingSummary(t *testing.T) {
	recs := append(good(10), "rir|BR|asn|64000|600|20200101|allocated|big") // cobre 64512..
	in := strings.Replace(file(recs...), "rir|*|ipv6|*|0|summary\n", "", 1)
	in = strings.Replace(in, "rir|*|asn|*|11|summary\n", "", 1)
	d, err := Parse(strings.NewReader(in), "rir")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(d.Warnings, "\n")
	if !strings.Contains(joined, "sobrepõe") || !strings.Contains(joined, "sem linha summary para asn") {
		t.Errorf("avisos = %v", d.Warnings)
	}
}

func TestSplitIPv4(t *testing.T) {
	cases := []struct {
		start string
		count uint64
		want  string
	}{
		{"10.0.0.0", 256, "10.0.0.0/24"},
		{"10.0.0.0", 1, "10.0.0.0/32"},
		{"62.122.208.0", 1280, "62.122.208.0/22 62.122.212.0/24"},
		{"23.128.5.0", 1792, "23.128.5.0/24 23.128.6.0/23 23.128.8.0/22"},
		{"87.116.83.0", 2304, "87.116.83.0/24 87.116.84.0/22 87.116.88.0/22"},
		{"164.146.0.0", 393216, "164.146.0.0/15 164.148.0.0/14"},
		{"10.0.0.1", 6, "10.0.0.1/32 10.0.0.2/31 10.0.0.4/31 10.0.0.6/32"},
		{"192.0.2.48", 48, "192.0.2.48/28 192.0.2.64/27"},
		{"0.0.0.0", 1 << 32, "0.0.0.0/0"},
		{"128.0.0.0", 1 << 31, "128.0.0.0/1"},
		{"255.255.255.255", 1, "255.255.255.255/32"},
		{"1.0.0.0", 3 << 24, "1.0.0.0/8 2.0.0.0/7"},
		{"0.0.0.1", (1 << 32) - 1, "0.0.0.1/32 0.0.0.2/31 0.0.0.4/30 0.0.0.8/29 0.0.0.16/28 0.0.0.32/27 0.0.0.64/26 " +
			"0.0.0.128/25 0.0.1.0/24 0.0.2.0/23 0.0.4.0/22 0.0.8.0/21 0.0.16.0/20 0.0.32.0/19 0.0.64.0/18 0.0.128.0/17 " +
			"0.1.0.0/16 0.2.0.0/15 0.4.0.0/14 0.8.0.0/13 0.16.0.0/12 0.32.0.0/11 0.64.0.0/10 0.128.0.0/9 1.0.0.0/8 " +
			"2.0.0.0/7 4.0.0.0/6 8.0.0.0/5 16.0.0.0/4 32.0.0.0/3 64.0.0.0/2 128.0.0.0/1"},
	}
	for _, c := range cases {
		got, err := SplitIPv4(netip.MustParseAddr(c.start), c.count)
		if err != nil {
			t.Errorf("%s + %d: %v", c.start, c.count, err)
			continue
		}
		var s []string
		var total uint64
		for _, p := range got {
			s = append(s, p.String())
			total += 1 << (32 - p.Bits())
		}
		if strings.Join(s, " ") != c.want || total != c.count {
			t.Errorf("%s + %d = %v (total %d), quero %s", c.start, c.count, s, total, c.want)
		}
	}
	for _, bad := range []struct {
		start string
		count uint64
	}{{"10.0.0.0", 0}, {"255.255.255.0", 257}, {"0.0.0.0", 1<<32 + 1}, {"2001:db8::", 1}} {
		if _, err := SplitIPv4(netip.MustParseAddr(bad.start), bad.count); err == nil {
			t.Errorf("%s + %d: esperava erro", bad.start, bad.count)
		}
	}
}

func prefixStrings(d *Dataset, family int) []string {
	var out []string
	for _, p := range d.Prefixes {
		if (family == 4) == p.Prefix.Addr().Is4() {
			out = append(out, p.Prefix.String())
		}
	}
	return out
}

func contains(s []string, v string) bool {
	return slices.Contains(s, v)
}

// TestParseRealFile lê o arquivo real inteiro do RIR, baixado à mão, quando
// AFRINIC_REAL_FILE aponta para ele (pulado se ausente):
//
//	curl -o /tmp/delegated https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest
//	AFRINIC_REAL_FILE=/tmp/delegated go test -run RealFile -v ./internal/parse/
func TestParseRealFile(t *testing.T) {
	path := os.Getenv("AFRINIC_REAL_FILE")
	if path == "" {
		t.Skip("defina AFRINIC_REAL_FILE com o caminho do arquivo real")
	}
	start := time.Now()
	d := parseFile(t, path, rir.Registry)
	t.Logf("%v: cabeçalho %+v; registros asn/ipv4/ipv6 = %d/%d/%d; blocos v4/v6 = %d/%d; descartes %d; avisos %d %v",
		time.Since(start), d.Header, d.ASNRecords, d.IPv4Records, d.IPv6Records, d.PrefixesV4, d.PrefixesV6,
		d.Skipped, d.WarningCount(), d.Warnings)
	if d.Skipped != 0 {
		t.Errorf("%d registros descartados no arquivo real: %v", d.Skipped, d.Warnings)
	}
	if d.Records() < rir.DefaultMinRecords {
		t.Errorf("%d registros, abaixo do mínimo padrão %d", d.Records(), rir.DefaultMinRecords)
	}
}
