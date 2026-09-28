package parse

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
)

// ASName é uma linha do asn.txt publicado pelo RIPE com o nome de todos os AS
// do mundo, nos formatos:
//
//	61613 AS61613 - TMSoft Solucoes em Informatica Ltda, BR
//	28 DFVLR-SYS Deutsches Zentrum fuer Luft- und Raumfahrt e.V., DE
type ASName struct {
	ASN    int64
	Handle string // primeiro token depois do número (AS-NAME no whois)
	Name   string // descrição da organização
	CC     string
}

// ParseASNames lê o asn.txt. Mais de 1% de linhas inválidas é erro.
func ParseASNames(r io.Reader) ([]ASName, Stats, error) {
	var st Stats
	var out []ASName
	seen := map[int64]bool{}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		num, rest, _ := strings.Cut(line, " ")
		asn, err := strconv.ParseUint(num, 10, 32)
		if err != nil {
			st.skip(lineNo, "ASN inválido %q", num)
			continue
		}
		if seen[int64(asn)] {
			st.warn(lineNo, "AS%d repetido", asn)
			continue
		}
		seen[int64(asn)] = true
		out = append(out, splitASName(int64(asn), strings.TrimSpace(rest)))
	}
	if err := sc.Err(); err != nil {
		return nil, st, fmt.Errorf("leitura: %w", err)
	}
	st.Records = len(out)
	return out, st, checkSkipped(st, 0.01)
}

// splitASName separa handle, nome e país do texto depois do número.
//
// Quando há " - ", o handle é o que vem antes do primeiro separador, mesmo
// com espaços: na AFRINIC o as-name pode ter espaços ("SEACOM Limited -
// SEACOM Limited"), e cortar no primeiro espaço estragaria o nome. Sem o
// separador (~39 mil linhas em 2026-09), o handle é o primeiro token.
func splitASName(asn int64, rest string) ASName {
	a := ASName{ASN: asn}
	// O país é o último item, depois da última vírgula, quando tem 2 letras.
	if i := strings.LastIndex(rest, ","); i >= 0 {
		cc := strings.TrimSpace(rest[i+1:])
		if len(cc) == 2 && isUpperAlpha(cc) {
			a.CC = cc
			rest = strings.TrimSpace(rest[:i])
		}
	}
	if handle, name, ok := strings.Cut(rest, " - "); ok {
		a.Handle = strings.TrimSpace(handle)
		a.Name = strings.TrimSpace(name)
		return a
	}
	handle, name, _ := strings.Cut(rest, " ")
	a.Handle = strings.TrimSpace(handle)
	a.Name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "-"))
	return a
}

func isUpperAlpha(s string) bool {
	for _, r := range s {
		if !unicode.IsUpper(r) || r > unicode.MaxASCII {
			return false
		}
	}
	return true
}
