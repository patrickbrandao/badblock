// Package parse interpreta o arquivo asn.txt do RIPE NCC (nomes de AS).
//
// Formato: uma linha por ASN, "<asn> <descrição>", com a descrição
// terminando em ", <CC>". O RIPE monta a descrição de três jeitos, conforme o
// RIR de origem:
//
//	15169 GOOGLE - Google LLC, US                                  (ARIN/APNIC/LACNIC)
//	29571 Orange Côte d'Ivoire - Orange Côte d'Ivoire, CI          (AFRINIC: "X - X")
//	28 DFVLR-SYS Deutsches Zentrum fuer Luft- und Raumfahrt e.V., DE (RIPE NCC)
//
// A descrição é guardada como veio; handle, name e country são derivados por
// Derive. As regras e as medições que as justificam estão em
// specs/fontes/ripe/asnames/fonte.md.
package parse

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ASN é uma linha do arquivo.
type ASN struct {
	Number      int64
	Description string // texto depois do ASN, como publicado
	Handle      string // "" = sem handle (NULL no banco)
	Name        string // "" = sem nome (NULL no banco)
	Country     string // "" = sem sufixo ", CC" (NULL no banco)
}

// Dataset é o arquivo inteiro já validado.
type Dataset struct {
	ASNs      []ASN
	Lines     int      // linhas com conteúdo (sem vazias e comentários)
	Skipped   int      // linhas descartadas inteiras
	Warnings  []string // primeiras MaxWarnings ocorrências
	warnCount int
}

// MaxWarnings limita os avisos guardados (o total fica em WarningCount).
const MaxWarnings = 50

// MaxSkippedRatio é a fração de linhas descartadas acima da qual o arquivo
// inteiro é recusado: um formato novo ou um arquivo corrompido não pode virar
// uma remoção em massa.
const MaxSkippedRatio = 0.01

// WarningCount é o total de avisos, inclusive os que não couberam em Warnings.
func (d *Dataset) WarningCount() int { return d.warnCount }

func (d *Dataset) warn(line int, format string, args ...any) {
	d.warnCount++
	if len(d.Warnings) < MaxWarnings {
		d.Warnings = append(d.Warnings, fmt.Sprintf("linha %d: ", line)+fmt.Sprintf(format, args...))
	}
}

// Parse lê o arquivo. Linha sem ASN válido, sem descrição ou com ASN repetido
// é descartada (vale a primeira ocorrência do ASN). Mais de MaxSkippedRatio de
// linhas descartadas recusa o arquivo inteiro.
func Parse(r io.Reader) (*Dataset, error) {
	d := &Dataset{}
	seen := map[int64]struct{}{}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Bytes()
		if lineNo == 1 {
			raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")) // BOM
		}
		line := strings.TrimSpace(clean(raw))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		d.Lines++

		numStr, desc := line, ""
		if i := strings.IndexFunc(line, unicode.IsSpace); i >= 0 {
			numStr, desc = line[:i], strings.TrimSpace(line[i:])
		}
		n, err := strconv.ParseUint(numStr, 10, 32)
		if err != nil {
			d.Skipped++
			d.warn(lineNo, "linha descartada: ASN inválido %q", truncate(numStr, 40))
			continue
		}
		number := int64(n)
		if desc == "" {
			d.Skipped++
			d.warn(lineNo, "linha descartada: AS%d sem descrição", number)
			continue
		}
		if _, dup := seen[number]; dup {
			d.Skipped++
			d.warn(lineNo, "linha descartada: AS%d repetido; vale a primeira ocorrência", number)
			continue
		}
		seen[number] = struct{}{}

		a := ASN{Number: number, Description: desc}
		a.Handle, a.Name, a.Country = Derive(desc)
		d.ASNs = append(d.ASNs, a)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("leitura: %w", err)
	}
	if d.Lines == 0 {
		return nil, fmt.Errorf("arquivo sem nenhuma linha de dados")
	}
	if ratio := float64(d.Skipped) / float64(d.Lines); ratio > MaxSkippedRatio {
		return nil, fmt.Errorf("%d de %d linhas descartadas (%.1f%%, limite %.1f%%): formato mudou? primeiros avisos: %s",
			d.Skipped, d.Lines, ratio*100, MaxSkippedRatio*100, strings.Join(first(d.Warnings, 3), "; "))
	}
	return d, nil
}

// Derive separa a descrição em handle, name e country (vazio = ausente).
//
//  1. country: sufixo ", XX" com duas letras maiúsculas; o resto é o corpo.
//  2. Corpo "X - X" (as duas metades iguais, formato AFRINIC): handle e name
//     são X. Pega também X com " - " dentro ("TMCEL - Moçambique Telecom, SA").
//  3. Corpo com " - " e sem espaço antes do primeiro separador (formato
//     ARIN/APNIC/LACNIC): handle é o que vem antes, name o que vem depois.
//  4. Senão (formato RIPE NCC, sem separador; ou com " - " dentro do nome da
//     organização): handle é a primeira palavra, name o resto.
func Derive(desc string) (handle, name, country string) {
	body := desc
	if n := len(desc); n >= 4 && desc[n-4:n-2] == ", " && isUpper(desc[n-2]) && isUpper(desc[n-1]) {
		country, body = desc[n-2:], desc[:n-4]
	}

	trimmed := strings.TrimSpace(body)
	if x, ok := halves(trimmed); ok {
		return x, x, country
	}
	// Os espaços nas pontas fazem "- , NZ" (handle vazio) e "AS4745-138 - , KR"
	// (nome vazio) caírem na regra 3 como qualquer "handle - nome".
	padded := " " + body + " "
	if before, after, ok := strings.Cut(padded, " - "); ok {
		if h := strings.TrimSpace(before); !strings.ContainsFunc(h, unicode.IsSpace) {
			return h, strings.TrimSpace(after), country
		}
	}
	if i := strings.IndexFunc(trimmed, unicode.IsSpace); i >= 0 {
		return trimmed[:i], strings.TrimSpace(trimmed[i:]), country
	}
	return trimmed, "", country
}

// halves reconhece "X - X" e devolve X.
func halves(s string) (string, bool) {
	if len(s) < 5 || (len(s)-3)%2 != 0 {
		return "", false
	}
	k := (len(s) - 3) / 2
	if s[k:k+3] != " - " || s[:k] != s[k+3:] {
		return "", false
	}
	return s[:k], true
}

// clean converte a linha em texto aceito pelo Postgres: UTF-8 inválido e NUL
// viram U+FFFD. A medição mostrou que a fonte é UTF-8 (sem Latin-1).
func clean(raw []byte) string {
	s := strings.ToValidUTF8(string(raw), "�")
	if strings.IndexByte(s, 0) >= 0 {
		s = strings.ReplaceAll(s, "\x00", "�")
	}
	return s
}

func isUpper(b byte) bool { return b >= 'A' && b <= 'Z' }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

func first(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
