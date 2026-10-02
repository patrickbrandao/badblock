package parse

import (
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"
)

// fixture é o named.root inteiro de 2026-09-30 (last update September 24,
// 2026; serial 2026092401), copiado sem alteração.
func fixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../testdata/named.root")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustParse(t *testing.T, s string) *Dataset {
	t.Helper()
	d, err := Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestParseFixture(t *testing.T) {
	d := mustParse(t, fixture(t))
	if d.Header.ZoneSerial != 2026092401 || !d.Header.LastUpdate.Equal(time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("cabeçalho = %+v", d.Header)
	}
	if len(d.Servers) != 13 || d.IPv4Count() != 13 || d.IPv6Count() != 13 || d.WarningCount() != 0 || len(d.Warnings) != 0 {
		t.Fatalf("servidores = %d, v4 = %d, v6 = %d, avisos = %v", len(d.Servers), d.IPv4Count(), d.IPv6Count(), d.Warnings)
	}
	for i, s := range d.Servers {
		letter := string(rune('a' + i))
		if s.Letter != letter || s.Name != letter+".root-servers.net" || s.NSTTL != 3600000 ||
			s.IPv4TTL != 3600000 || s.IPv6TTL != 3600000 || s.Note == "" {
			t.Errorf("servidor %d = %+v", i, s)
		}
	}
	want := map[string]Server{
		"a": {IPv4: netip.MustParseAddr("198.41.0.4"), IPv6: netip.MustParseAddr("2001:503:ba3e::2:30"), Note: "FORMERLY NS.INTERNIC.NET"},
		"b": {IPv4: netip.MustParseAddr("170.247.170.2"), IPv6: netip.MustParseAddr("2801:1b8:10::b"), Note: "FORMERLY NS1.ISI.EDU"},
		"g": {IPv4: netip.MustParseAddr("192.112.36.4"), IPv6: netip.MustParseAddr("2001:500:12::d0d"), Note: "FORMERLY NS.NIC.DDN.MIL"},
		"j": {IPv4: netip.MustParseAddr("192.58.128.30"), IPv6: netip.MustParseAddr("2001:503:c27::2:30"), Note: "OPERATED BY VERISIGN, INC."},
		"k": {IPv4: netip.MustParseAddr("193.0.14.129"), IPv6: netip.MustParseAddr("2001:7fd::1"), Note: "OPERATED BY RIPE NCC"},
		"m": {IPv4: netip.MustParseAddr("202.12.27.33"), IPv6: netip.MustParseAddr("2001:dc3::35"), Note: "OPERATED BY WIDE"},
	}
	for _, s := range d.Servers {
		w, ok := want[s.Letter]
		if ok && (s.IPv4 != w.IPv4 || s.IPv6 != w.IPv6 || s.Note != w.Note) {
			t.Errorf("%s = %s / %s / %q", s.Name, s.IPv4, s.IPv6, s.Note)
		}
	}
}

// TestParseRealFile lê o arquivo do dia quando ROOTHINTS_REAL_FILE aponta para
// uma cópia de https://www.internic.net/domain/named.root.
func TestParseRealFile(t *testing.T) {
	path := os.Getenv("ROOTHINTS_REAL_FILE")
	if path == "" {
		t.Skip("defina ROOTHINTS_REAL_FILE para testar com o arquivo real")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	d := mustParse(t, string(b))
	if len(d.Servers) < 13 {
		t.Errorf("servidores = %d", len(d.Servers))
	}
	t.Logf("%d bytes: %d servidores (%d IPv4, %d IPv6), last update %s, serial %d, %d avisos, em %s",
		len(b), len(d.Servers), d.IPv4Count(), d.IPv6Count(), d.Header.LastUpdate.Format("2006-01-02"),
		d.Header.ZoneSerial, d.WarningCount(), time.Since(start))
}

// Variações toleradas: CRLF, BOM, classe IN, maiúsculas/minúsculas, espaços,
// comentário no fim da linha, "; End of file" com LF final, linhas vazias.
func TestParseTolerated(t *testing.T) {
	base := fixture(t)
	cases := map[string]string{
		"crlf":                   strings.ReplaceAll(base, "\n", "\r\n"),
		"bom":                    "\xef\xbb\xbf" + base,
		"LF no fim":              base + "\n",
		"linhas vazias":          strings.ReplaceAll(base, "; \n", "\n\n"),
		"classe IN":              strings.ReplaceAll(base, "3600000      A     ", "3600000  IN  A  "),
		"tab":                    strings.ReplaceAll(base, "      AAAA  ", "\tAAAA\t"),
		"minúsculas":             strings.ToLower(base),
		"comentário na linha":    strings.Replace(base, "198.41.0.4", "198.41.0.4 ; Verisign", 1),
		"end of file sem espaço": strings.Replace(base, "; End of file", ";End of file", 1),
	}
	for name, s := range cases {
		d, err := Parse(strings.NewReader(s))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(d.Servers) != 13 || d.Servers[0].Name != "a.root-servers.net" || d.Servers[0].IPv4 != netip.MustParseAddr("198.41.0.4") {
			t.Errorf("%s: servidores = %d, a = %+v", name, len(d.Servers), d.Servers[0])
		}
	}
}

// Servidor sem um dos endereços: aceito, com aviso; a coluna fica NULL.
func TestParseMissingFamilyWarns(t *testing.T) {
	s := strings.Replace(fixture(t), "B.ROOT-SERVERS.NET.      3600000      AAAA  2801:1b8:10::b\n", "", 1)
	s = strings.Replace(s, "C.ROOT-SERVERS.NET.      3600000      A     192.33.4.12\n", "", 1)
	d := mustParse(t, s)
	if d.IPv4Count() != 12 || d.IPv6Count() != 12 {
		t.Errorf("v4 = %d, v6 = %d", d.IPv4Count(), d.IPv6Count())
	}
	want := []string{"servidor b.root-servers.net sem IPv6 (registro AAAA)", "servidor c.root-servers.net sem IPv4 (registro A)"}
	if strings.Join(d.Warnings, "|") != strings.Join(want, "|") {
		t.Errorf("avisos = %q", d.Warnings)
	}
}

// Bloco sem comentário: note vazio (NULL no banco), sem herdar o cabeçalho.
func TestParseBlockWithoutComment(t *testing.T) {
	s := strings.Replace(fixture(t), "; FORMERLY NS.INTERNIC.NET \n", "", 1)
	s = strings.Replace(s, "; OPERATED BY WIDE\n", "", 1)
	d := mustParse(t, s)
	if d.Servers[0].Note != "" || d.Servers[12].Note != "" || d.Servers[1].Note != "FORMERLY NS1.ISI.EDU" {
		t.Errorf("notas = %q / %q / %q", d.Servers[0].Note, d.Servers[1].Note, d.Servers[12].Note)
	}
}

func TestParseRejects(t *testing.T) {
	base := fixture(t)
	cases := []struct{ name, body, err string }{
		{"vazio", "", `cabeçalho sem a linha "last update:"`},
		{"html", "<html>manutenção</html>\n", `linha 1: cabeçalho sem a linha "last update:"`},
		{"sem last update", strings.Replace(base, ";       last update:     September 24, 2026\n", "", 1), `cabeçalho sem a linha "last update:"`},
		{"sem serial", strings.Replace(base, ";       related version of root zone:     2026092401\n", "", 1), `cabeçalho sem a linha "related version of root zone:"`},
		{"data inválida", strings.Replace(base, "September 24, 2026", "2026-09-24", 1), `data de "last update:" inválida: "2026-09-24"`},
		{"serial inválido", strings.Replace(base, "2026092401", "20260924x", 1), `serial de "related version of root zone:" inválido`},
		{"serial acima de 32 bits", strings.Replace(base, "2026092401", "4294967296", 1), `serial de "related version of root zone:" inválido`},
		{"truncado entre blocos", base[:strings.Index(base, "; FORMERLY NS.NIC.DDN.MIL")], `sem a linha "; End of file"`},
		{"truncado no meio da linha", base[:len(base)/2], `esperava <nome> <ttl> [IN] <tipo> <dado>: "."`},
		{"sem end of file", strings.Replace(base, "; End of file", "", 1), `sem a linha "; End of file"`},
		{"dados depois do end of file", base + "\nN.ROOT-SERVERS.NET. 3600000 A 192.0.2.1\n", `sem a linha NS dele antes`},
		{"só o cabeçalho", base[:strings.Index(base, ".                        3600000")] + "; End of file", `arquivo sem nenhum servidor`},
		{"NS de outro nome", strings.Replace(base, ".                        3600000      NS    B.ROOT", "net.                     3600000      NS    B.ROOT", 1), `NS de "net.": só a raiz`},
		{"NS fora de root-servers.net", strings.Replace(base, "NS    B.ROOT-SERVERS.NET.", "NS    NS1.EXAMPLE.COM.", 1), `fora do padrão <letra>.root-servers.net`},
		{"NS sem ponto final", strings.Replace(base, "NS    B.ROOT-SERVERS.NET.", "NS    B.ROOT-SERVERS.NET", 1), `nome sem o ponto final "B.ROOT-SERVERS.NET"`},
		{"NS repetido", strings.Replace(base, "NS    B.ROOT-SERVERS.NET.", "NS    A.ROOT-SERVERS.NET.", 1), `servidor a.root-servers.net repetido`},
		{"A de nome sem NS", strings.Replace(base, "B.ROOT-SERVERS.NET.      3600000      A ", "Z.ROOT-SERVERS.NET.      3600000      A ", 1), `registro A de z.root-servers.net sem a linha NS dele antes`},
		{"A antes do NS", strings.Replace(base, ".                        3600000      NS    M.ROOT-SERVERS.NET.\nM.ROOT-SERVERS.NET.      3600000      A     202.12.27.33\n", "M.ROOT-SERVERS.NET.      3600000      A     202.12.27.33\n.                        3600000      NS    M.ROOT-SERVERS.NET.\n", 1), `registro A de m.root-servers.net sem a linha NS dele antes`},
		{"IPv4 inválido", strings.Replace(base, "198.41.0.4", "198.41.0.256", 1), `endereço IPv4 inválido em a.root-servers.net: "198.41.0.256"`},
		{"IPv6 no A", strings.Replace(base, "A     198.41.0.4", "A     2001:db8::1", 1), `endereço IPv4 inválido`},
		{"IPv4 no AAAA", strings.Replace(base, "2001:503:ba3e::2:30", "198.41.0.4", 1), `endereço IPv6 inválido`},
		{"IPv4 mapeado no AAAA", strings.Replace(base, "2001:503:ba3e::2:30", "::ffff:198.41.0.5", 1), `endereço IPv6 inválido`},
		{"IPv6 com zona", strings.Replace(base, "2001:503:ba3e::2:30", "fe80::1%eth0", 1), `endereço IPv6 inválido`},
		{"IPv4 privado", strings.Replace(base, "198.41.0.4", "10.0.0.1", 1), `endereço 10.0.0.1 de a.root-servers.net não é unicast global`},
		{"loopback", strings.Replace(base, "2001:503:ba3e::2:30", "::1", 1), `não é unicast global`},
		{"endereço repetido", strings.Replace(base, "170.247.170.2", "198.41.0.4", 1), `endereço 198.41.0.4 repetido (já é de a.root-servers.net)`},
		{"segundo A", strings.Replace(base, "A.ROOT-SERVERS.NET.      3600000      AAAA", "A.ROOT-SERVERS.NET.      3600000      A     198.41.0.5\nA.ROOT-SERVERS.NET.      3600000      AAAA", 1), `segundo registro A de a.root-servers.net`},
		{"segundo AAAA", strings.Replace(base, "; \n; FORMERLY NS1.ISI.EDU", "A.ROOT-SERVERS.NET. 3600000 AAAA 2001:503:ba3e::2:31\n; \n; FORMERLY NS1.ISI.EDU", 1), `segundo registro AAAA de a.root-servers.net`},
		{"sem endereço", strings.Replace(strings.Replace(base, "C.ROOT-SERVERS.NET.      3600000      A     192.33.4.12\n", "", 1), "C.ROOT-SERVERS.NET.      3600000      AAAA  2001:500:2::c\n", "", 1), `servidor c.root-servers.net sem endereço`},
		{"tipo inesperado", strings.Replace(base, "AAAA  2001:500:2::c", "TXT   oi", 1), `tipo "TXT" não esperado`},
		{"TTL inválido", strings.Replace(base, "3600000      A     198.41.0.4", "1h      A     198.41.0.4", 1), `TTL inválido "1h"`},
		{"TTL acima de 2^31-1", strings.Replace(base, "3600000      A     198.41.0.4", "2147483648      A     198.41.0.4", 1), `TTL inválido`},
		{"classe CH", strings.Replace(base, "3600000      A     198.41.0.4", "3600000 CH A 198.41.0.4", 1), `esperava <nome> <ttl> [IN] <tipo> <dado>`},
		{"campos a mais", strings.Replace(base, "198.41.0.4", "198.41.0.4 extra", 1), `esperava <nome> <ttl> [IN] <tipo> <dado>`},
		{"nome relativo", strings.Replace(base, "A.ROOT-SERVERS.NET.      3600000      A ", "A.ROOT-SERVERS.NET      3600000      A ", 1), `nome sem o ponto final "A.ROOT-SERVERS.NET"`},
		{"UTF-8 inválido", strings.Replace(base, "FORMERLY NS.INTERNIC.NET", "FORMERLY \xff", 1), `UTF-8 inválido ou NUL`},
		{"NUL", strings.Replace(base, "OPERATED BY WIDE", "OPERATED\x00BY WIDE", 1), `UTF-8 inválido ou NUL`},
		{"linha gigante", strings.Replace(base, "; FORMERLY NS.INTERNIC.NET", "; "+strings.Repeat("x", 2<<20), 1), `leitura:`},
	}
	for _, c := range cases {
		_, err := Parse(strings.NewReader(c.body))
		if err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s: err = %v, quero %q", c.name, err, c.err)
		}
	}
}
