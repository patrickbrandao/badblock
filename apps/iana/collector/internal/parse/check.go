package parse

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/source"
)

// Limits são as travas de sanidade do dataset. DefaultLimits é o que roda em
// produção; os testes com recortes dos arquivos usam limites menores.
type Limits struct {
	// Complete exige as coberturas estruturais: ASNs de 16 bits cobrindo
	// 0–65535 e de 32 bits cobrindo 65536–4294967295 sem buracos, exatamente
	// 256 blocos /8 no IPv4, e os 5 RIRs presentes no IPv6 unicast e em cada
	// JSON RDAP.
	Complete bool

	MinIPv6Blocks  int // ipv6-unicast-address-assignments (medido: 51)
	MinSpecialIPv4 int // blocos do special registry IPv4 (medido: 26)
	MinSpecialIPv6 int // blocos do special registry IPv6 (medido: 25)
	MinSpecialASNs int // special-purpose-as-numbers (medido: 9)
	MinRDAPASN     int // entradas de asn.json (medido: 159)
	MinRDAPIPv4    int // entradas de ipv4.json (medido: 221)
	MinRDAPIPv6    int // entradas de ipv6.json (medido: 34)
}

// DefaultLimits são os mínimos de produção: cerca de 60% do medido em
// 2026-09-28. Os arquivos da IANA só crescem; uma queda desse tamanho é
// arquivo truncado ou formato novo.
func DefaultLimits() Limits {
	return Limits{
		Complete:       true,
		MinIPv6Blocks:  30,
		MinSpecialIPv4: 15,
		MinSpecialIPv6: 15,
		MinSpecialASNs: 6,
		MinRDAPASN:     100,
		MinRDAPIPv4:    150,
		MinRDAPIPv6:    20,
	}
}

// Faixas de ASN de cada arquivo.
const (
	maxASN16 = 65535
	maxASN32 = 4294967295
)

// Check confere o dataset inteiro. Devolve todas as falhas encontradas.
func Check(d *Dataset, lim Limits) error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	for _, f := range source.Files {
		if st := d.Files[f.Name]; st == nil || st.Rows == 0 {
			fail("%s: nenhum registro", f.Name)
		}
	}

	// ASNs: sem sobreposição; com Complete, cobertura total de cada arquivo.
	blocks := slices.Clone(d.ASNBlocks)
	slices.SortFunc(blocks, func(a, b ASNBlock) int { return cmp.Compare(a.Start, b.Start) })
	for i := 1; i < len(blocks); i++ {
		if blocks[i].Start <= blocks[i-1].End {
			fail("faixas de ASN sobrepostas: %s (%s) e %s (%s)",
				formatASNRange(blocks[i-1].Start, blocks[i-1].End), blocks[i-1].SourceFile,
				formatASNRange(blocks[i].Start, blocks[i].End), blocks[i].SourceFile)
		}
	}
	if lim.Complete {
		checkCoverage(blocks, source.ASNumbers1, 0, maxASN16, fail)
		checkCoverage(blocks, source.ASNumbers2, maxASN16+1, maxASN32, fail)
	}

	// Blocos IP da IANA.
	var v4, v6 int
	v6RIRs := map[string]bool{}
	for _, b := range d.PrefixBlocks {
		if b.Prefix.Addr().Is4() {
			v4++
			if lim.Complete && b.Prefix.Bits() != 8 {
				fail("%s: bloco %s não é /8", source.IPv4Space, b.Prefix)
			}
		} else {
			v6++
			v6RIRs[b.Registry] = true
		}
	}
	if lim.Complete && v4 != 256 {
		fail("%s: %d blocos /8, esperados exatamente 256", source.IPv4Space, v4)
	}
	if v6 < lim.MinIPv6Blocks {
		fail("%s: só %d blocos (mínimo %d)", source.IPv6Unicast, v6, lim.MinIPv6Blocks)
	}
	if lim.Complete {
		if missing := missingRIRs(v6RIRs); len(missing) > 0 {
			fail("%s: nenhum bloco de %s", source.IPv6Unicast, strings.Join(missing, ", "))
		}
	}

	// Special-purpose.
	var sp4, sp6 int
	for _, s := range d.SpecialPrefixes {
		if s.Prefix.Addr().Is4() {
			sp4++
		} else {
			sp6++
		}
	}
	if sp4 < lim.MinSpecialIPv4 {
		fail("%s: só %d blocos (mínimo %d)", source.SpecialIPv4, sp4, lim.MinSpecialIPv4)
	}
	if sp6 < lim.MinSpecialIPv6 {
		fail("%s: só %d blocos (mínimo %d)", source.SpecialIPv6, sp6, lim.MinSpecialIPv6)
	}
	if n := len(d.SpecialASNs); n < lim.MinSpecialASNs {
		fail("%s: só %d faixas (mínimo %d)", source.SpecialASN, n, lim.MinSpecialASNs)
	}

	// RDAP: mínimo de entradas e os 5 RIRs em cada arquivo.
	count := map[string]int{}
	rirs := map[string]map[string]bool{KindASN: {}, KindIPv4: {}, KindIPv6: {}}
	for _, s := range d.RDAPServices {
		count[s.Kind]++
		rirs[s.Kind][s.Registry] = true
	}
	for _, k := range []struct {
		kind, file string
		min        int
	}{{KindASN, source.RDAPASN, lim.MinRDAPASN}, {KindIPv4, source.RDAPIPv4, lim.MinRDAPIPv4}, {KindIPv6, source.RDAPIPv6, lim.MinRDAPIPv6}} {
		if count[k.kind] < k.min {
			fail("%s: só %d entradas (mínimo %d)", k.file, count[k.kind], k.min)
		}
		if lim.Complete {
			if missing := missingRIRs(rirs[k.kind]); len(missing) > 0 {
				fail("%s: sem servidor RDAP de %s", k.file, strings.Join(missing, ", "))
			}
		}
	}
	return errors.Join(errs...)
}

// checkCoverage confere que as faixas de um arquivo cobrem [from, to] sem
// buracos (blocks já ordenado e sem sobreposição).
func checkCoverage(blocks []ASNBlock, file string, from, to int64, fail func(string, ...any)) {
	next := from
	for _, b := range blocks {
		if b.SourceFile != file {
			continue
		}
		if b.Start < from || b.End > to {
			fail("%s: faixa %s fora de %d–%d", file, formatASNRange(b.Start, b.End), from, to)
			return
		}
		if b.Start != next {
			fail("%s: buraco entre %d e %d", file, next, b.Start-1)
			return
		}
		next = b.End + 1
	}
	if next != to+1 {
		fail("%s: faixas terminam em %d, esperado %d", file, next-1, to)
	}
}

func missingRIRs(seen map[string]bool) []string {
	var out []string
	for _, r := range Registries {
		if !seen[r] {
			out = append(out, r)
		}
	}
	return out
}
