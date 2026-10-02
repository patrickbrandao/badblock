package parse

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/anatel/pst/collector/internal/archive"
)

const bom = "\xef\xbb\xbf"

func parseSample(t *testing.T) *Dataset {
	t.Helper()
	f, err := os.Open("../../testdata/pst-sample.csv")
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

func find(d *Dataset, doc string) *Provider {
	for i := range d.Providers {
		if d.Providers[i].Document == doc {
			return &d.Providers[i]
		}
	}
	return nil
}

func date(y, m, day int) time.Time { return time.Date(y, time.Month(m), day, 0, 0, 0, 0, time.UTC) }

func TestParseSample(t *testing.T) {
	d := parseSample(t)
	if d.Rows != 158 || d.RowsCNPJ != 155 || d.RowsCPF != 3 || d.Duplicates != 103 || d.Skipped != 0 ||
		len(d.Providers) != 8 || d.Services() != 52 || d.WarningCount() != 0 {
		t.Fatalf("contagens: rows %d, cnpj %d, cpf %d, dup %d, skip %d, prestadoras %d, serviços %d, avisos %v",
			d.Rows, d.RowsCNPJ, d.RowsCPF, d.Duplicates, d.Skipped, len(d.Providers), d.Services(), d.Warnings)
	}

	// Ordem do arquivo; nenhuma pessoa física.
	var docs []string
	for _, p := range d.Providers {
		docs = append(docs, p.Document)
		if strings.Contains(p.Name, "PESSOA FISICA") {
			t.Errorf("linha de CPF virou prestadora: %+v", p)
		}
	}
	want := "72063654000256 02558157000162 07807833000108 02883607000192 01763250000146 01600200001110 02839640000115 07157343000103"
	if strings.Join(docs, " ") != want {
		t.Errorf("ordem = %v", docs)
	}

	// Prestadora grande: 145 linhas, 42 serviços distintos.
	tel := find(d, "02558157000162")
	if tel.Name != "TELEFONICA BRASIL S.A." || tel.TradeName != "" || tel.Complement != "Telefônica Brasil S/A" ||
		tel.CityIBGECode != 3550308 || tel.City != "São Paulo" || tel.State != "SP" || tel.PostalCode != "04571936" ||
		tel.Email != "cadastro.fiscal.br@telefonica.com" || len(tel.Services) != 42 {
		t.Errorf("telefonica = %+v (%d serviços)", tel, len(tel.Services))
	}
	s := tel.Services[0]
	if s.ServiceCode != "176" || s.ServiceName != "STFC/RADIOTELEFONICO - ESTACOES TERRENAS" || s.NotificationFistel != "50402628500" ||
		s.GrantFistel != "50423150120" || !s.GrantedOn.Equal(date(2021, 1, 5)) {
		t.Errorf("primeiro serviço da telefonica = %+v", s)
	}

	// SCM com nome fantasia.
	whim := find(d, "07807833000108")
	if whim.TradeName != "GRUPO EMPRESARIAL ACESSO" || whim.Complement != "" || len(whim.Services) != 1 {
		t.Errorf("whim = %+v", whim)
	}
	if s := whim.Services[0]; s.ServiceCode != "045" || s.ServiceName != "Serviço de Comunicação Multimídia" ||
		s.ServiceGroup != "Banda Larga Fixa" || s.EntityType != "Outorgada" ||
		s.GrantType != "Serviços de Interesse Coletivo e Restrito - SIC" || !s.NotifiedOn.Equal(date(2025, 8, 20)) {
		t.Errorf("serviço da whim = %+v", s)
	}

	// Duas outorgas (SIR e SIC), o mesmo Fistel de notificação sob as duas.
	vig := find(d, "02883607000192")
	if len(vig.Services) != 4 || vig.Services[0].GrantFistel != "50422928020" || vig.Services[1].GrantFistel != "50450923320" ||
		vig.Services[0].NotificationFistel != vig.Services[1].NotificationFistel {
		t.Errorf("vigillare = %+v", vig.Services)
	}

	// Aspas: "" dentro do campo e ; dentro do campo.
	if p := find(d, "01763250000146"); p.Name != `TRANS "S" LTDA` || p.Email != "" {
		t.Errorf("trans = %+v", p)
	}
	if p := find(d, "01600200001110"); p.Complement != ": KM 11; GALPAO: 1;" || p.Number != "S.N." || p.TradeName != "CLE BRASIL" {
		t.Errorf("veolia = %+v", p)
	}

	// Ausentes: "-", vazio, "N/I" e espaço viram NULL; "." fica.
	if p := find(d, "02839640000115"); p.Number != "." || p.Complement != "" || p.Phone != "" || p.Email != "" || p.Street != "RUA G , N.º 01" {
		t.Errorf("paraguacu = %+v", p)
	}
	ab := find(d, "72063654000256")
	if ab.Complement != "" || ab.Services[0].NotificationProcess != "" || ab.Number != "." {
		t.Errorf("abrigo = %+v", ab)
	}

	// Dispensada de outorga: colunas 7 a 9 = N/A → NULL.
	cr := find(d, "07157343000103")
	if s := cr.Services[0]; s.EntityType != "Dispensada de Outorga" || s.GrantType != "Dispensada de Outorga" ||
		s.GrantFistel != "" || s.GrantProcess != "" || !s.GrantedOn.IsZero() || s.ServiceCode != "401" ||
		s.ServiceName != "Rádio do Cidadão - Dispensa de Autorização" {
		t.Errorf("cremosino = %+v", s)
	}
}

func TestParseSampleZip(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/pst-sample.zip")
	if err != nil {
		t.Fatal(err)
	}
	c, err := archive.Extract(raw, archive.MaxCSVBytes)
	if err != nil {
		t.Fatal(err)
	}
	d, err := Parse(bytes.NewReader(c.Data))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Providers) != 8 || d.Services() != 52 {
		t.Errorf("zip: %d prestadoras, %d serviços", len(d.Providers), d.Services())
	}
}

// csvOf monta um CSV com o cabeçalho real e as linhas dadas (CRLF).
func csvOf(lines ...string) string {
	return bom + strings.Join(Header[:], ";") + "\r\n" + strings.Join(lines, "\r\n") + "\r\n"
}

// row monta uma linha de CNPJ válida, trocando as colunas de over (posição a partir de 1).
func row(over map[int]string) string {
	f := []string{"CNPJ", "11222333000181", "EMPRESA EXEMPLO LTDA", "EXEMPLO", "Outorgada",
		"Serviços de Interesse Coletivo e Restrito - SIC", "50400000001", "53500000000000001", "05/01/2021",
		"Banda Larga Fixa", "045 - Serviço de Comunicação Multimídia", "50400000002", "53500000000000002", "13/02/2003",
		"RUA EXEMPLO", "10", "SALA 1", "CENTRO", "01001000", "3550308", "São Paulo", "SP", "(11) 1234-5678", "contato@exemplo.com.br"}
	for k, v := range over {
		f[k-1] = v
	}
	return strings.Join(f, ";")
}

func mustParse(t *testing.T, s string) *Dataset {
	t.Helper()
	d, err := Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestParseWithoutBOMAndLF(t *testing.T) {
	s := strings.TrimPrefix(strings.ReplaceAll(csvOf(row(nil)), "\r\n", "\n"), bom)
	d := mustParse(t, s)
	if len(d.Providers) != 1 || d.Services() != 1 {
		t.Errorf("d = %+v", d)
	}
}

func TestParseTrimAndInvalidUTF8(t *testing.T) {
	d := mustParse(t, csvOf(row(map[int]string{3: "  EMPRESA \xff LTDA ", 4: " N/I ", 17: " ", 23: "", 24: "-"})))
	p := d.Providers[0]
	if p.Name != "EMPRESA � LTDA" || p.TradeName != "" || p.Complement != "" || p.Phone != "" || p.Email != "" {
		t.Errorf("p = %+v", p)
	}
}

func TestParseKeepsLiteralValues(t *testing.T) {
	d := mustParse(t, csvOf(row(map[int]string{13: "0", 16: "S/N"})))
	if d.Providers[0].Number != "S/N" || d.Providers[0].Services[0].NotificationProcess != "0" {
		t.Errorf("p = %+v", d.Providers[0])
	}
}

func TestParseCPFIgnored(t *testing.T) {
	cpf := row(map[int]string{1: "CPF", 2: "***12345**", 3: "PESSOA FISICA EXEMPLO"})
	short := "CPF;***12345**;PESSOA FISICA EXEMPLO" // CPF não é conferido
	d := mustParse(t, csvOf(cpf, short, row(nil)))
	if d.RowsCPF != 2 || d.RowsCNPJ != 1 || d.Skipped != 0 || len(d.Providers) != 1 || d.WarningCount() != 0 {
		t.Errorf("d = %+v", d)
	}
}

func TestParseExactCopy(t *testing.T) {
	r := row(nil)
	d := mustParse(t, csvOf(r, r, " "+strings.ReplaceAll(r, ";", " ; ")))
	if d.Duplicates != 2 || d.Services() != 1 || d.WarningCount() != 0 {
		t.Errorf("d = %+v", d)
	}
}

func TestParseRepeatedService(t *testing.T) {
	d := mustParse(t, csvOf(
		row(nil),
		row(map[int]string{14: "14/02/2003"}), // mesma chave, data diferente
		row(map[int]string{7: "50400000009"}), // outra outorga: outro serviço
		row(map[int]string{3: "OUTRO NOME LTDA", 12: "50400000003"}),                       // outro serviço, prestadora diferente
		row(map[int]string{3: "TERCEIRO NOME LTDA", 12: "50400000004"}),                    // um aviso por CNPJ
		row(map[int]string{2: "99888777000166", 5: "Dispensada de Outorga", 7: "N/A"}),     // dispensada
		row(map[int]string{2: "99888777000166", 5: "Dispensada de Outorga", 7: "", 9: ""}), // mesma chave (NULL = NULL)
	))
	want := []string{
		"linha 3: serviço repetido (CNPJ 11222333000181, Fistel 50400000002, código 045); vale a primeira ocorrência",
		"linha 5: CNPJ 11222333000181 com dados da prestadora diferentes da primeira linha; vale a primeira",
		"linha 8: serviço repetido (CNPJ 99888777000166, Fistel 50400000002, código 045); vale a primeira ocorrência",
	}
	if strings.Join(d.Warnings, "\n") != strings.Join(want, "\n") {
		t.Errorf("avisos:\n%s", strings.Join(d.Warnings, "\n"))
	}
	if len(d.Providers) != 2 || d.Services() != 5 || d.Providers[0].Name != "EMPRESA EXEMPLO LTDA" ||
		!d.Providers[0].Services[0].NotifiedOn.Equal(date(2003, 2, 13)) {
		t.Errorf("prestadoras = %+v", d.Providers)
	}
}

func TestParseInvalidCityAndState(t *testing.T) {
	d := mustParse(t, csvOf(row(map[int]string{20: "355030", 22: "sp"}), row(map[int]string{2: "99888777000166", 20: "N/I", 22: "-"})))
	want := "linha 2: código IBGE inválido: \"355030\"\nlinha 2: UF inválida: \"sp\""
	if strings.Join(d.Warnings, "\n") != want || d.Skipped != 0 {
		t.Errorf("avisos = %v", d.Warnings)
	}
	for _, p := range d.Providers {
		if p.CityIBGECode != 0 || p.State != "" {
			t.Errorf("p = %+v", p)
		}
	}
}

func TestParseSkippedLines(t *testing.T) {
	good := make([]string, 0, 1000)
	for i := range 999 {
		good = append(good, row(map[int]string{12: "5" + strings.Repeat("0", 6) + pad4(i)}))
	}
	cases := []struct {
		line string
		msg  string
	}{
		{strings.Join(strings.Split(row(nil), ";")[:23], ";"), "esperados 24 campos, vieram 23"},
		{row(map[int]string{1: "RG"}), `tipo de identificação desconhecido: "RG"`},
		{row(map[int]string{2: "11.222.333/0001-81"}), `CNPJ inválido: "11.222.333/0001-81"`},
		{row(map[int]string{11: "045 Serviço"}), `serviço inválido: "045 Serviço"`},
		{row(map[int]string{12: "N/I"}), `Fistel da notificação inválido: "N/I"`},
		{row(map[int]string{7: "123"}), `Fistel da outorga inválido: "123"`},
		{row(map[int]string{9: "31/02/2021"}), `data inválida em "Data Inclusão da Outorga": "31/02/2021"`},
		{row(map[int]string{14: "2003-02-13"}), `data inválida em "Data Inclusão da Notificação": "2003-02-13"`},
		{row(map[int]string{3: "N/I"}), "CNPJ 11222333000181 sem nome"},
		{row(map[int]string{10: ""}), `CNPJ 11222333000181 sem "Serviço da Notificação"`},
	}
	for _, tc := range cases {
		// 1 linha ruim em 1000 de CNPJ (0,1%): passa, com aviso.
		d, err := Parse(strings.NewReader(csvOf(append([]string{tc.line}, good...)...)))
		if err != nil {
			t.Errorf("%s: %v", tc.msg, err)
			continue
		}
		if d.Skipped != 1 || d.RowsCNPJ != 1000 || len(d.Warnings) == 0 || d.Warnings[0] != "linha 2: linha descartada: "+tc.msg {
			t.Errorf("%s: skipped %d, avisos %v", tc.msg, d.Skipped, first(d.Warnings, 1))
		}
	}
}

func pad4(i int) string { return fmt.Sprintf("%04d", i) }

func TestParseSkippedLimit(t *testing.T) {
	// 2 ruins em 100 linhas de CNPJ (2%): recusa.
	lines := []string{row(map[int]string{2: "x"}), row(map[int]string{2: "y"})}
	for i := range 98 {
		lines = append(lines, row(map[int]string{12: "5000000" + pad4(i)}))
	}
	_, err := Parse(strings.NewReader(csvOf(lines...)))
	if err == nil || !strings.HasPrefix(err.Error(), "2 de 100 linhas de CNPJ descartadas (2.0%, limite 1.0%)") {
		t.Fatalf("err = %v", err)
	}
	// 1 em 100 (1%): passa.
	if _, err := Parse(strings.NewReader(csvOf(append(lines[1:], row(map[int]string{12: "50000009999"}))...))); err != nil {
		t.Errorf("1%% deveria passar: %v", err)
	}
}

func TestParseRejects(t *testing.T) {
	wrong := Header
	wrong[10] = "Serviço"
	cases := []struct {
		name, csv, msg string
	}{
		{"vazio", "", "arquivo vazio"},
		{"só BOM", bom, "arquivo vazio"},
		{"só cabeçalho", csvOf()[:len(csvOf())-2], "arquivo sem nenhuma linha de dados"},
		{"coluna trocada", strings.Join(wrong[:], ";") + "\r\n" + row(nil) + "\r\n",
			`cabeçalho inesperado na coluna 11: "Serviço" (esperado "Código e Nome do Serviço da Notificação")`},
		{"coluna a mais", strings.Join(Header[:], ";") + ";Extra\r\n", "cabeçalho com 25 colunas (esperadas 24)"},
		{"separador vírgula", strings.Join(Header[:], ",") + "\r\n", "cabeçalho com 1 colunas (esperadas 24)"},
		{"aspas quebradas", csvOf(row(nil), row(map[int]string{3: `EMPRESA "X" LTDA`})), `csv: linha 3: bare " in non-quoted-field`},
		{"aspas sem fechar", csvOf(row(nil), row(map[int]string{3: `"EMPRESA X LTDA`})), "csv: linha 3: extraneous or missing \" in quoted-field"},
	}
	for _, tc := range cases {
		_, err := Parse(strings.NewReader(tc.csv))
		if err == nil || err.Error() != tc.msg {
			t.Errorf("%s: err = %v, quero %q", tc.name, err, tc.msg)
		}
	}
}

func TestParseWarningsCap(t *testing.T) {
	var lines []string
	for i := range 60 {
		lines = append(lines, row(map[int]string{2: "1122233300" + pad4(i), 22: "xx"}))
	}
	d := mustParse(t, csvOf(lines...))
	if len(d.Warnings) != MaxWarnings || d.WarningCount() != 60 {
		t.Errorf("avisos guardados %d, total %d", len(d.Warnings), d.WarningCount())
	}
}

// TestParseRealFile lê o ZIP real inteiro do dia, baixado à mão, quando
// ANATEL_PST_REAL_FILE aponta para ele (pulado se ausente):
//
//	curl -o /tmp/pst.zip https://www.anatel.gov.br/dadosabertos/paineis_de_dados/outorga_e_licenciamento/prestadoras_servicos_telecomunicacoes.zip
//	make test-real FILE=/tmp/pst.zip
func TestParseRealFile(t *testing.T) {
	path := os.Getenv("ANATEL_PST_REAL_FILE")
	if path == "" {
		t.Skip("defina ANATEL_PST_REAL_FILE com o caminho do ZIP real")
	}
	data := realCSV(t, path)
	start := time.Now()
	d, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("parser: %v; linhas %d (CNPJ %d, CPF %d), cópias %d, descartadas %d, prestadoras %d, serviços %d, avisos %d %v",
		time.Since(start), d.Rows, d.RowsCNPJ, d.RowsCPF, d.Duplicates, d.Skipped, len(d.Providers), d.Services(),
		d.WarningCount(), d.Warnings)
	if d.Skipped != 0 {
		t.Errorf("%d linhas descartadas no arquivo real: %v", d.Skipped, d.Warnings)
	}
	if len(d.Providers) < 30000 {
		t.Errorf("%d prestadoras, abaixo do mínimo padrão 30000", len(d.Providers))
	}
	counts := map[string]int{}
	for _, p := range d.Providers {
		seen := map[string]bool{}
		for _, s := range p.Services {
			if !seen[s.ServiceCode] {
				seen[s.ServiceCode] = true
				counts[s.ServiceCode]++
			}
		}
	}
	t.Logf("prestadoras por serviço: SCM (045) %d, STFC (171) %d, SeAC (750) %d, SMP (010) %d; %d códigos",
		counts["045"], counts["171"], counts["750"], counts["010"], len(counts))
}

// realCSV lê o arquivo real: um ZIP (extraído com o pacote archive, com o
// tempo no log) ou o CSV já extraído.
func realCSV(t testing.TB, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.ToLower(path), ".zip") {
		return raw
	}
	start := time.Now()
	c, err := archive.Extract(raw, archive.MaxCSVBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ZIP de %d bytes: %s com %d bytes, sha256 %s, modificado em %s; extração %v",
		len(raw), c.Name, len(c.Data), c.SHA256, c.ModifiedAt.Format(time.RFC3339), time.Since(start))
	return c.Data
}
