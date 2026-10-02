// Package parse interpreta o arquivo nicbr-asn-blk do NIC.br.
//
// Formato: uma linha por ASN, campos separados por "|":
//
//	AS61610|ELEA DATA CENTERS|35.980.592/0001-30|187.87.28.0/22|2804:8ae0::/32
//
// ASN (com o prefixo AS), nome do titular, documento (CNPJ formatado ou
// identificador estrangeiro de 8 dígitos) e zero ou mais blocos CIDR.
package parse

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"
)

// ASN é uma linha do arquivo.
type ASN struct {
	Number   int64
	Name     string
	Document string
	Prefixes []netip.Prefix
}

// Dataset é o arquivo inteiro já validado.
type Dataset struct {
	ASNs       []ASN
	PrefixesV4 int
	PrefixesV6 int
	Lines      int      // linhas com conteúdo (sem vazias e comentários)
	Skipped    int      // linhas descartadas inteiras
	Warnings   []string // primeiras MaxWarnings ocorrências
	warnCount  int
}

// MaxWarnings limita os avisos guardados (o total fica em WarningCount).
const MaxWarnings = 50

// MaxSkippedRatio é a fração de linhas descartadas acima da qual o arquivo
// inteiro é recusado: um formato novo ou um arquivo corrompido não pode virar
// uma remoção em massa.
const MaxSkippedRatio = 0.01

// WarningCount é o total de avisos, inclusive os que não couberam em Warnings.
func (d *Dataset) WarningCount() int { return d.warnCount }

// Prefixes é o total de blocos.
func (d *Dataset) Prefixes() int { return d.PrefixesV4 + d.PrefixesV6 }

func (d *Dataset) warn(line int, format string, args ...any) {
	d.warnCount++
	if len(d.Warnings) < MaxWarnings {
		d.Warnings = append(d.Warnings, fmt.Sprintf("linha %d: ", line)+fmt.Sprintf(format, args...))
	}
}

// Parse lê o arquivo. Uma linha sem ASN válido é descartada; um bloco
// inválido descarta só o bloco. ASN repetido junta os blocos na primeira
// ocorrência; bloco repetido (na mesma linha ou em outra) fica com a primeira.
func Parse(r io.Reader) (*Dataset, error) {
	d := &Dataset{}
	asnIndex := map[int64]int{}
	seenPrefix := map[netip.Prefix]int64{}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Bytes()
		if lineNo == 1 {
			raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")) // BOM
		}
		line := strings.TrimSpace(strings.ToValidUTF8(string(raw), "�"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		d.Lines++

		fields := strings.Split(line, "|")
		if len(fields) < 3 {
			d.Skipped++
			d.warn(lineNo, "linha descartada: esperados ao menos 3 campos, vieram %d", len(fields))
			continue
		}
		number, err := parseASN(fields[0])
		if err != nil {
			d.Skipped++
			d.warn(lineNo, "linha descartada: %v", err)
			continue
		}

		idx, dup := asnIndex[number]
		if dup {
			d.warn(lineNo, "AS%d repetido; blocos somados à primeira ocorrência", number)
		} else {
			idx = len(d.ASNs)
			asnIndex[number] = idx
			d.ASNs = append(d.ASNs, ASN{
				Number:   number,
				Name:     strings.TrimSpace(fields[1]),
				Document: strings.TrimSpace(fields[2]),
			})
		}

		for _, f := range fields[3:] {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			p, err := netip.ParsePrefix(f)
			if err != nil {
				d.warn(lineNo, "bloco inválido %q descartado", f)
				continue
			}
			if m := p.Masked(); m != p {
				d.warn(lineNo, "bloco %s com bits de host; usando %s", p, m)
				p = m
			}
			if owner, ok := seenPrefix[p]; ok {
				if owner != number {
					d.warn(lineNo, "bloco %s já pertence a AS%d; ocorrência em AS%d descartada", p, owner, number)
				}
				continue
			}
			seenPrefix[p] = number
			d.ASNs[idx].Prefixes = append(d.ASNs[idx].Prefixes, p)
			if p.Addr().Is4() {
				d.PrefixesV4++
			} else {
				d.PrefixesV6++
			}
		}
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

func parseASN(field string) (int64, error) {
	field = strings.TrimSpace(field)
	num, ok := strings.CutPrefix(strings.ToUpper(field), "AS")
	if !ok {
		return 0, fmt.Errorf("ASN sem o prefixo AS: %q", field)
	}
	n, err := strconv.ParseUint(num, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("ASN inválido: %q", field)
	}
	return int64(n), nil
}

func first(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
