package parse

import (
	"fmt"
	"net/netip"
	"os"
	"strings"
	"testing"

	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/source"
)

// relaxed são limites que os recortes de testdata/ satisfazem.
var relaxed = Limits{MinIPv6Blocks: 5, MinSpecialIPv4: 5, MinSpecialIPv6: 5, MinSpecialASNs: 3,
	MinRDAPASN: 5, MinRDAPIPv4: 5, MinRDAPIPv6: 5}

func TestCheckFixtures(t *testing.T) {
	d := mustParse(t, fixtures(t))
	if err := Check(d, relaxed); err != nil {
		t.Errorf("recortes com limites relaxados: %v", err)
	}
	// Com os limites de produção, os recortes são "arquivos truncados".
	err := Check(d, DefaultLimits())
	if err == nil {
		t.Fatal("recortes deveriam falhar com DefaultLimits")
	}
	for _, want := range []string{"buraco", "esperados exatamente 256", "só 14 blocos", "só 9 blocos", "só 5 faixas", "só 15 entradas"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("faltou %q em: %v", want, err)
		}
	}
}

// complete monta um dataset mínimo que passa em DefaultLimits.
func complete() *Dataset {
	d := &Dataset{Files: map[string]*FileStats{}}
	for _, f := range source.Files {
		d.Files[f.Name] = &FileStats{Records: 1, Rows: 1}
	}
	d.ASNBlocks = []ASNBlock{
		{Start: 0, End: 0, SourceFile: source.ASNumbers1},
		{Start: 1, End: 65535, SourceFile: source.ASNumbers1},
		{Start: 65536, End: 4294967295, SourceFile: source.ASNumbers2},
	}
	for i := range 256 {
		d.PrefixBlocks = append(d.PrefixBlocks, PrefixBlock{Prefix: netip.PrefixFrom(netip.AddrFrom4([4]byte{byte(i)}), 8)})
	}
	lim := DefaultLimits()
	for i := range lim.MinIPv6Blocks {
		d.PrefixBlocks = append(d.PrefixBlocks, PrefixBlock{
			Prefix:   netip.MustParsePrefix(fmt.Sprintf("2001:%x::/32", i)),
			Registry: Registries[i%len(Registries)],
		})
	}
	for i := range lim.MinSpecialIPv4 {
		d.SpecialPrefixes = append(d.SpecialPrefixes, SpecialPrefix{Prefix: netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(i)}), 16)})
	}
	for i := range lim.MinSpecialIPv6 {
		d.SpecialPrefixes = append(d.SpecialPrefixes, SpecialPrefix{Prefix: netip.MustParsePrefix(fmt.Sprintf("fc%02x::/16", i))})
	}
	for i := range lim.MinSpecialASNs {
		d.SpecialASNs = append(d.SpecialASNs, SpecialASN{Start: int64(i), End: int64(i)})
	}
	for _, k := range []struct {
		kind string
		n    int
	}{{KindASN, lim.MinRDAPASN}, {KindIPv4, lim.MinRDAPIPv4}, {KindIPv6, lim.MinRDAPIPv6}} {
		for i := range k.n {
			d.RDAPServices = append(d.RDAPServices, RDAPService{Kind: k.kind, Registry: Registries[i%len(Registries)]})
		}
	}
	return d
}

func TestCheckComplete(t *testing.T) {
	if err := Check(complete(), DefaultLimits()); err != nil {
		t.Fatalf("dataset completo: %v", err)
	}

	breaks := map[string]func(d *Dataset){
		"buraco": func(d *Dataset) { d.ASNBlocks[1].Start = 2 },
		"sobrepostas": func(d *Dataset) {
			d.ASNBlocks = append(d.ASNBlocks, ASNBlock{Start: 100, End: 200, SourceFile: source.ASNumbers2})
		},
		"terminam em":              func(d *Dataset) { d.ASNBlocks[2].End = 4294967294 },
		"fora de":                  func(d *Dataset) { d.ASNBlocks[2].SourceFile = source.ASNumbers1 },
		"esperados exatamente 256": func(d *Dataset) { d.PrefixBlocks = d.PrefixBlocks[1:] },
		"não é /8": func(d *Dataset) {
			d.PrefixBlocks[3].Prefix = netip.MustParsePrefix("3.0.0.0/9")
		},
		"nenhum bloco de ripencc": func(d *Dataset) {
			for i := range d.PrefixBlocks {
				if d.PrefixBlocks[i].Registry == "ripencc" {
					d.PrefixBlocks[i].Registry = "arin"
				}
			}
		},
		"sem servidor RDAP de lacnic": func(d *Dataset) {
			for i := range d.RDAPServices {
				if d.RDAPServices[i].Kind == KindIPv4 && d.RDAPServices[i].Registry == "lacnic" {
					d.RDAPServices[i].Registry = ""
				}
			}
		},
		"só 14 blocos":    func(d *Dataset) { d.SpecialPrefixes = d.SpecialPrefixes[1:] },
		"nenhum registro": func(d *Dataset) { d.Files[source.RDAPIPv6].Rows = 0 },
	}
	for want, brk := range breaks {
		d := complete()
		brk(d)
		err := Check(d, DefaultLimits())
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v", want, err)
		}
	}
}

// TestRealFiles roda o parser e a sanidade de produção nos arquivos reais
// inteiros. Opcional: IANA_REAL_DIR aponta para uma pasta com os 10 arquivos
// com o nome do servidor (make test-real baixa e roda).
func TestRealFiles(t *testing.T) {
	dir := os.Getenv("IANA_REAL_DIR")
	if dir == "" {
		t.Skip("IANA_REAL_DIR não definida")
	}
	d := mustParse(t, loadDir(t, dir))
	for _, f := range source.Files {
		st := d.Files[f.Name]
		t.Logf("%-34s linhas %4d  registros %4d  descartadas %d  %s", f.Name, st.Records, st.Rows, st.Skipped, st.Publication)
		if st.Skipped != 0 {
			t.Errorf("%s: %d linhas descartadas", f.Name, st.Skipped)
		}
	}
	if d.WarningCount() != 0 {
		t.Errorf("avisos inesperados (%d): %v", d.WarningCount(), d.Warnings)
	}
	if err := Check(d, DefaultLimits()); err != nil {
		t.Errorf("sanidade: %v", err)
	}
	t.Logf("totais: %d faixas de ASN, %d blocos IP, %d special IP, %d special ASN, %d entradas RDAP",
		len(d.ASNBlocks), len(d.PrefixBlocks), len(d.SpecialPrefixes), len(d.SpecialASNs), len(d.RDAPServices))
}
