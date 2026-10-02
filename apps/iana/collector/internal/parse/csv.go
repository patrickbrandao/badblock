package parse

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/source"
)

// table é um CSV da IANA lido inteiro: cabeçalho normalizado e registros.
type table struct {
	cols    map[string]int // nome normalizado → índice
	records []record
}

type record struct {
	line   int
	fields []string
	err    error // registro que o encoding/csv não conseguiu ler
}

// get devolve a coluna pelo nome normalizado ("" se ausente).
func (t *table) get(r record, col string) string {
	i, ok := t.cols[col]
	if !ok || i >= len(r.fields) {
		return ""
	}
	return r.fields[i]
}

// normHeader: "Status [1]" → "status", "Reserved-by-Protocol" → "reserved-by-protocol".
func normHeader(s string) string {
	return strings.ToLower(clean(stripFootnotes(s)))
}

// readTable lê o CSV. Os arquivos têm campos com aspas, aspas dobradas
// ("""This network""") e quebras de linha dentro das células; LazyQuotes e
// FieldsPerRecord = -1 toleram variações sem perder o arquivo. required são as
// colunas sem as quais o arquivo é recusado; optional, as que só geram aviso
// se sumirem.
func (d *Dataset) readTable(file string, body []byte, required, optional []string) (*table, error) {
	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
	body = bytes.ToValidUTF8(body, []byte("�"))
	r := csv.NewReader(bytes.NewReader(body))
	r.LazyQuotes = true
	r.FieldsPerRecord = -1

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("cabeçalho ilegível: %w", err)
	}
	t := &table{cols: map[string]int{}}
	for i, h := range header {
		if n := normHeader(h); n != "" {
			if _, dup := t.cols[n]; !dup {
				t.cols[n] = i
			}
		}
	}
	for _, c := range required {
		if _, ok := t.cols[c]; !ok {
			return nil, fmt.Errorf("coluna obrigatória %q ausente no cabeçalho %q: formato mudou?", c, header)
		}
	}
	for _, c := range optional {
		if _, ok := t.cols[c]; !ok {
			d.warn(file, 1, "coluna %q ausente no cabeçalho; fica vazia", c)
		}
	}

	for errs := 0; ; {
		fields, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			line := 0
			if pe, ok := errors.AsType[*csv.ParseError](err); ok {
				line = pe.StartLine
			}
			if errs++; errs > 1000 {
				return nil, fmt.Errorf("CSV ilegível: %w", err)
			}
			t.records = append(t.records, record{line: line, err: err})
			continue
		}
		if allEmpty(fields) {
			continue
		}
		line, _ := r.FieldPos(0)
		t.records = append(t.records, record{line: line, fields: fields})
	}
	return t, nil
}

func allEmpty(fields []string) bool {
	for _, f := range fields {
		if strings.TrimSpace(f) != "" {
			return false
		}
	}
	return true
}

// registryOf decide o RIR de uma linha: o domínio do WHOIS vale; sem WHOIS,
// o nome ("Assigned by ARIN", "RIPE NCC"). Divergência vira aviso.
func (d *Dataset) registryOf(file string, line int, whois, name string) string {
	byWhois, byName := registryFromHost(whois), registryFromName(name)
	if byWhois != "" && byName != "" && byWhois != byName {
		d.warn(file, line, "WHOIS %s indica %s mas a descrição %q indica %s; vale o WHOIS", whois, byWhois, name, byName)
	}
	if byWhois != "" {
		return byWhois
	}
	return byName
}

// rdapURLs lê a coluna RDAP de um CSV, com aviso se não reconhecer.
func (d *Dataset) rdapURLs(file string, line int, cell string) []string {
	urls, ok := splitURLs(cell)
	if !ok {
		d.warn(file, line, "coluna RDAP não reconhecida %q; fica vazia", clean(cell))
	}
	if urls == nil {
		urls = []string{}
	}
	return urls
}

func (d *Dataset) date(file string, line int, col, cell string) string {
	v, ok := parseDate(cell)
	if !ok {
		d.warn(file, line, "%s não reconhecida %q; fica NULL", col, clean(cell))
	}
	return v
}

// skip descarta a linha inteira.
func (d *Dataset) skip(st *FileStats, file string, line int, format string, args ...any) {
	st.Skipped++
	d.warn(file, line, "linha descartada: "+format, args...)
}

// parseASNBlocks lê as-numbers-1.csv e as-numbers-2.csv:
//
//	Number,Description,WHOIS,RDAP,Reference,Registration Date
//	1-1876,Assigned by ARIN,whois.arin.net,https://rdap.arin.net/registryhttp://rdap.arin.net/registry,,
//
// A linha "0-65535,See Sub-registry 16-bit AS numbers" do as-numbers-2 é
// ignorada (as faixas de 16 bits vêm do as-numbers-1).
func (d *Dataset) parseASNBlocks(file string, body []byte, st *FileStats, seen *keys) error {
	t, err := d.readTable(file, body, []string{"number", "description"},
		[]string{"whois", "rdap", "reference", "registration date"})
	if err != nil {
		return err
	}
	for _, r := range t.records {
		desc := clean(t.get(r, "description"))
		if r.err == nil && strings.HasPrefix(strings.ToLower(desc), "see sub-registry") {
			continue
		}
		st.Records++
		if r.err != nil {
			d.skip(st, file, r.line, "%v", r.err)
			continue
		}
		start, end, err := parseASNRange(t.get(r, "number"))
		if err != nil {
			d.skip(st, file, r.line, "%v", err)
			continue
		}
		if other, dup := seen.asnBlock[start]; dup {
			d.skip(st, file, r.line, "faixa começando em %d repetida (já vista em %s)", start, other)
			continue
		}
		seen.asnBlock[start] = file
		whois := strings.ToLower(clean(t.get(r, "whois")))
		d.ASNBlocks = append(d.ASNBlocks, ASNBlock{
			Start:            start,
			End:              end,
			Description:      desc,
			Registry:         d.registryOf(file, r.line, whois, desc),
			WHOIS:            whois,
			RDAPURLs:         d.rdapURLs(file, r.line, t.get(r, "rdap")),
			Reference:        clean(t.get(r, "reference")),
			RegistrationDate: d.date(file, r.line, "Registration Date", t.get(r, "registration date")),
			SourceFile:       file,
		})
		st.Rows++
	}
	return nil
}

// knownStatus são os status medidos; outro valor bem formado entra com aviso.
var knownStatus = map[string]bool{"ALLOCATED": true, "LEGACY": true, "RESERVED": true}

// parsePrefixBlocks lê ipv4-address-space.csv e
// ipv6-unicast-address-assignments.csv:
//
//	Prefix,Designation,Date,WHOIS,RDAP,Status [1],Note
//	045/8,Administered by ARIN,1995-01,whois.arin.net,https://rdap.arin.net/registryhttp://rdap.arin.net/registry,LEGACY,
func (d *Dataset) parsePrefixBlocks(file string, body []byte, st *FileStats, seen *keys) error {
	t, err := d.readTable(file, body, []string{"prefix", "designation", "status"},
		[]string{"date", "whois", "rdap", "note"})
	if err != nil {
		return err
	}
	wantV4 := file == source.IPv4Space
	for _, r := range t.records {
		st.Records++
		if r.err != nil {
			d.skip(st, file, r.line, "%v", r.err)
			continue
		}
		p, hostBits, err := parsePrefix(t.get(r, "prefix"))
		if err != nil {
			d.skip(st, file, r.line, "%v", err)
			continue
		}
		if p.Addr().Is4() != wantV4 {
			d.skip(st, file, r.line, "bloco %s da família errada para este arquivo", p)
			continue
		}
		if hostBits {
			d.warn(file, r.line, "bloco %q com bits de host; usando %s", t.get(r, "prefix"), p)
		}
		status := strings.ToUpper(clean(stripFootnotes(t.get(r, "status"))))
		if !statusRe.MatchString(status) {
			d.skip(st, file, r.line, "status inválido %q", status)
			continue
		}
		if !knownStatus[status] {
			d.warn(file, r.line, "status desconhecido %q (aceito)", status)
		}
		if other, dup := seen.prefixBlock[p]; dup {
			d.skip(st, file, r.line, "bloco %s repetido (já visto em %s)", p, other)
			continue
		}
		seen.prefixBlock[p] = file
		designation := clean(t.get(r, "designation"))
		whois := strings.ToLower(clean(t.get(r, "whois")))
		d.PrefixBlocks = append(d.PrefixBlocks, PrefixBlock{
			Prefix:         p,
			Designation:    designation,
			Registry:       d.registryOf(file, r.line, whois, designation),
			WHOIS:          whois,
			RDAPURLs:       d.rdapURLs(file, r.line, t.get(r, "rdap")),
			Status:         status,
			AllocationDate: d.date(file, r.line, "Date", t.get(r, "date")),
			Note:           clean(t.get(r, "note")),
			SourceFile:     file,
		})
		st.Rows++
	}
	return nil
}

// parseSpecialPrefixes lê os special-purpose registries de IPv4 e IPv6:
//
//	Address Block,Name,RFC,Allocation Date,Termination Date,Source,Destination,Forwardable,Globally Reachable,Reserved-by-Protocol
//	"192.0.0.170/32, 192.0.0.171/32",NAT64/DNS64 Discovery,"[RFC8880][RFC7050], Section 2.2",2013-02,N/A,False,False,False,False,True
//	192.0.0.0/24 [2],IETF Protocol Assignments,"[RFC6890], Section 2.1",2010-01,N/A,False,False,False,False,False
func (d *Dataset) parseSpecialPrefixes(file string, body []byte, st *FileStats, seen *keys) error {
	flags := []string{"source", "destination", "forwardable", "globally reachable", "reserved-by-protocol"}
	t, err := d.readTable(file, body, []string{"address block", "name"},
		append([]string{"rfc", "allocation date", "termination date"}, flags...))
	if err != nil {
		return err
	}
	wantV4 := file == source.SpecialIPv4
	for _, r := range t.records {
		st.Records++
		if r.err != nil {
			d.skip(st, file, r.line, "%v", r.err)
			continue
		}
		base := SpecialPrefix{
			Name:            clean(t.get(r, "name")),
			RFC:             clean(t.get(r, "rfc")),
			AllocationDate:  d.date(file, r.line, "Allocation Date", t.get(r, "allocation date")),
			TerminationDate: d.date(file, r.line, "Termination Date", t.get(r, "termination date")),
		}
		dst := []**bool{&base.Source, &base.Destination, &base.Forwardable, &base.GloballyReachable, &base.ReservedByProtocol}
		for i, col := range flags {
			v, ok := parseBool(t.get(r, col))
			if !ok {
				d.warn(file, r.line, "%s não reconhecido %q; fica NULL", col, clean(t.get(r, col)))
			}
			*dst[i] = v
		}

		added := 0
		for _, cell := range splitCell(t.get(r, "address block")) {
			p, hostBits, err := parsePrefix(cell)
			if err != nil {
				d.warn(file, r.line, "%v; bloco ignorado", err)
				continue
			}
			if p.Addr().Is4() != wantV4 {
				d.warn(file, r.line, "bloco %s da família errada para este arquivo; ignorado", p)
				continue
			}
			if hostBits {
				d.warn(file, r.line, "bloco %q com bits de host; usando %s", cell, p)
			}
			if other, dup := seen.specialPrefix[p]; dup {
				d.warn(file, r.line, "bloco %s repetido (já visto em %s); ignorado", p, other)
				continue
			}
			seen.specialPrefix[p] = file
			sp := base
			sp.Prefix = p
			d.SpecialPrefixes = append(d.SpecialPrefixes, sp)
			added++
		}
		if added == 0 {
			d.skip(st, file, r.line, "nenhum bloco válido em %q", clean(t.get(r, "address block")))
			continue
		}
		st.Rows += added
	}
	return nil
}

// parseSpecialASNs lê special-purpose-as-numbers.csv:
//
//	AS Number,Reason for Reservation,Reference
//	64512-65534,For private use; reserved by [RFC6996],[RFC6996]
func (d *Dataset) parseSpecialASNs(file string, body []byte, st *FileStats, seen *keys) error {
	t, err := d.readTable(file, body, []string{"as number", "reason for reservation"}, []string{"reference"})
	if err != nil {
		return err
	}
	for _, r := range t.records {
		st.Records++
		if r.err != nil {
			d.skip(st, file, r.line, "%v", r.err)
			continue
		}
		start, end, err := parseASNRange(t.get(r, "as number"))
		if err != nil {
			d.skip(st, file, r.line, "%v", err)
			continue
		}
		if seen.specialASN[start] {
			d.skip(st, file, r.line, "faixa começando em %d repetida", start)
			continue
		}
		seen.specialASN[start] = true
		d.SpecialASNs = append(d.SpecialASNs, SpecialASN{
			Start:     start,
			End:       end,
			Reason:    clean(t.get(r, "reason for reservation")),
			Reference: clean(t.get(r, "reference")),
		})
		st.Rows++
	}
	return nil
}
