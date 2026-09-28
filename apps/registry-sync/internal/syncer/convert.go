package syncer

import (
	"bytes"
	"fmt"
	"net/netip"
	"time"

	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/parse"
	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/source"
	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/store"
)

// Payload é o conteúdo de uma fonte pronto para gravar.
type Payload struct {
	Tables   []store.TableData
	Records  int
	FileDate *time.Time // só delegated: data do arquivo segundo o cabeçalho
	Stats    parse.Stats
}

// Convert interpreta o conteúdo baixado de uma fonte e monta as linhas das
// tabelas de ingest, na ordem de colunas de store.Table.
func Convert(src source.Source, body []byte) (*Payload, error) {
	r := bytes.NewReader(body)
	switch src.Kind {
	case source.KindDelegated:
		f, err := parse.ParseDelegated(r, src.RIR)
		if err != nil {
			return nil, err
		}
		rows := make([][]any, 0, len(f.Records))
		for _, d := range f.Records {
			var asnFirst, asnLast, cidrs any
			if d.Type == "asn" {
				asnFirst, asnLast = d.ASNFirst, d.ASNLast
			} else {
				cidrs = d.CIDRs
			}
			rows = append(rows, []any{d.Type, d.Start, src.RIR, d.Value, d.CC, date(d.Date), d.Status,
				d.OpaqueID, asnFirst, asnLast, cidrs})
		}
		return &Payload{
			Tables:   []store.TableData{{Table: store.TableDelegation, Rows: rows}},
			Records:  len(rows),
			FileDate: f.Header.EndDate,
			Stats:    f.Stats,
		}, nil

	case source.KindIANAASN:
		blocks, st, err := parse.ParseIANAASN(r)
		if err != nil {
			return nil, err
		}
		var rows [][]any
		for _, b := range blocks {
			rows = append(rows, []any{b.First, b.Last, b.Description, b.WHOIS, b.RDAP, b.Reference, date(b.Date)})
		}
		return single(store.TableIANAASNBlock, rows, st), nil

	case source.KindIANAIPv4, source.KindIANAIPv6:
		parseFn := parse.ParseIANAIPv4
		if src.Kind == source.KindIANAIPv6 {
			parseFn = parse.ParseIANAIPv6
		}
		blocks, st, err := parseFn(r)
		if err != nil {
			return nil, err
		}
		var rows [][]any
		for _, b := range blocks {
			rows = append(rows, []any{b.Prefix, b.Designation, date(b.Date), b.WHOIS, b.RDAP, b.Status, b.Note})
		}
		return single(store.TableIANAIPBlock, rows, st), nil

	case source.KindSpecialIP:
		blocks, st, err := parse.ParseIANASpecialIP(r)
		if err != nil {
			return nil, err
		}
		seen := map[netip.Prefix]bool{}
		var rows [][]any
		for _, b := range blocks {
			// Um bloco listado duas vezes fica com a primeira linha.
			if seen[b.Prefix] {
				continue
			}
			seen[b.Prefix] = true
			rows = append(rows, []any{b.Prefix, b.Name, b.RFC, date(b.AllocDate), b.Termination,
				boolPtr(b.Source), boolPtr(b.Destination), boolPtr(b.Forwardable),
				boolPtr(b.GloballyReachable), boolPtr(b.ReservedByProtocol)})
		}
		return single(store.TableIANASpecialIP, rows, st), nil

	case source.KindSpecialASN:
		blocks, st, err := parse.ParseIANASpecialASN(r)
		if err != nil {
			return nil, err
		}
		var rows [][]any
		for _, b := range blocks {
			rows = append(rows, []any{b.First, b.Last, b.Reason, b.Reference})
		}
		return single(store.TableIANASpecialASN, rows, st), nil

	case source.KindRDAPASN, source.KindRDAPIP:
		kind := "asn"
		if src.Kind == source.KindRDAPIP {
			kind = "ip"
		}
		services, st, err := parse.ParseRDAPBootstrap(r, kind)
		if err != nil {
			return nil, err
		}
		var rows [][]any
		for _, s := range services {
			if kind == "asn" {
				rows = append(rows, []any{s.Entry, s.ASNFirst, s.ASNLast, nil, s.BaseURL})
			} else {
				rows = append(rows, []any{s.Entry, nil, nil, s.Prefix, s.BaseURL})
			}
		}
		return single(store.TableRDAPService, rows, st), nil

	case source.KindNICBR:
		recs, st, err := parse.ParseNICBR(r)
		if err != nil {
			return nil, err
		}
		var asns, prefixes [][]any
		for _, rec := range recs {
			asns = append(asns, []any{rec.ASN, rec.Name, rec.Document})
			for _, p := range rec.Prefixes {
				prefixes = append(prefixes, []any{rec.ASN, p})
			}
		}
		return &Payload{
			Tables: []store.TableData{
				{Table: store.TableNICBRASN, Rows: asns},
				{Table: store.TableNICBRPrefix, Rows: prefixes},
			},
			Records: len(asns),
			Stats:   st,
		}, nil

	case source.KindASNames:
		names, st, err := parse.ParseASNames(r)
		if err != nil {
			return nil, err
		}
		var rows [][]any
		for _, n := range names {
			rows = append(rows, []any{n.ASN, n.Handle, n.Name, n.CC})
		}
		return single(store.TableASName, rows, st), nil
	}
	return nil, fmt.Errorf("tipo de fonte sem conversor: %s", src.Kind)
}

func single(t store.Table, rows [][]any, st parse.Stats) *Payload {
	return &Payload{Tables: []store.TableData{{Table: t, Rows: rows}}, Records: len(rows), Stats: st}
}

// date converte datas opcionais para o COPY (nil vira NULL).
func date(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func boolPtr(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}
