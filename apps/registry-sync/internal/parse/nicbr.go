package parse

import (
	"bufio"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"
)

// NICBRRecord é uma linha do nicbr-asn-blk: ASN|nome|documento|bloco|bloco...
//
// O nome e o documento (CNPJ, ou id estrangeiro de 8 dígitos) são do titular
// brasileiro dos blocos. Em 21 linhas (2026-09-28) o ASN é estrangeiro
// (AS8075, AS174...): o vínculo vale para os blocos, não para o nome do ASN.
type NICBRRecord struct {
	ASN      int64
	Name     string
	Document string
	Prefixes []netip.Prefix
}

// ParseNICBR lê o arquivo do NIC.br. Linhas com ASN inválido são descartadas;
// um bloco inválido descarta só o bloco. Mais de 1% de linhas ruins é erro.
func ParseNICBR(r io.Reader) ([]NICBRRecord, Stats, error) {
	var st Stats
	var out []NICBRRecord
	seen := map[int64]int{}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) < 3 {
			st.skip(lineNo, "esperados ao menos 3 campos")
			continue
		}
		num, ok := strings.CutPrefix(strings.ToUpper(strings.TrimSpace(fields[0])), "AS")
		if !ok {
			st.skip(lineNo, "ASN sem prefixo AS: %q", fields[0])
			continue
		}
		asn, err := strconv.ParseUint(num, 10, 32)
		if err != nil {
			st.skip(lineNo, "ASN inválido %q", fields[0])
			continue
		}
		rec := NICBRRecord{
			ASN:      int64(asn),
			Name:     strings.TrimSpace(fields[1]),
			Document: strings.TrimSpace(fields[2]),
		}
		dup := map[netip.Prefix]bool{}
		for _, raw := range fields[3:] {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			p, err := netip.ParsePrefix(raw)
			if err != nil {
				st.warn(lineNo, "bloco inválido %q", raw)
				continue
			}
			if p.Masked() != p {
				st.warn(lineNo, "bloco %s com bits de host; usando %s", p, p.Masked())
				p = p.Masked()
			}
			if dup[p] {
				continue
			}
			dup[p] = true
			rec.Prefixes = append(rec.Prefixes, p)
		}
		if idx, ok := seen[rec.ASN]; ok {
			// ASN repetido: junta os blocos na primeira ocorrência.
			st.warn(lineNo, "AS%d repetido", rec.ASN)
			prev := &out[idx]
			have := map[netip.Prefix]bool{}
			for _, p := range prev.Prefixes {
				have[p] = true
			}
			for _, p := range rec.Prefixes {
				if !have[p] {
					prev.Prefixes = append(prev.Prefixes, p)
				}
			}
			continue
		}
		seen[rec.ASN] = len(out)
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, st, fmt.Errorf("leitura: %w", err)
	}
	st.Records = len(out)
	return out, st, checkSkipped(st, 0.01)
}
