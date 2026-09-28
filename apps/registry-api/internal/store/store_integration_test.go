//go:build integration

package store_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/patrickbrandao/badblock/apps/registry-api/internal/store"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/testdb"
)

// Os testes usam o role badblock_api: além das consultas, provam que as views
// do schema api bastam (a API não enxerga as tabelas).
func open(t *testing.T) *store.Store {
	t.Helper()
	db := testdb.Start(t)
	testdb.Seed(t, db, "../../testdata/seed.sql")
	st, err := store.Open(context.Background(), db.APIURL, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st
}

func TestQueries(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	chain, err := st.Chain(ctx, netip.MustParseAddr("45.171.60.1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) != 2 || chain[0].Level != "rir" || chain[1].Level != "iana" || *chain[0].HolderKey != "lacnic:258500" {
		t.Fatalf("cadeia = %+v", chain)
	}
	if len(chain[0].NICBRASNs) != 1 || chain[0].NICBRASNs[0] != 61613 {
		t.Errorf("nicbr_asns = %v", chain[0].NICBRASNs)
	}

	// Mesmo tamanho de prefixo: special antes de iana.
	chain, _ = st.Chain(ctx, netip.MustParseAddr("10.1.1.1"))
	if len(chain) != 2 || chain[0].Level != "special" || *chain[0].GloballyReachable {
		t.Errorf("cadeia de 10.1.1.1 = %+v", chain)
	}
	// Linha removida não aparece.
	if chain, _ = st.Chain(ctx, netip.MustParseAddr("200.1.2.3")); len(chain) != 0 {
		t.Errorf("prefixo removido apareceu: %+v", chain)
	}

	children, err := st.Children(ctx, netip.MustParsePrefix("45.0.0.0/8"), 10)
	if err != nil || len(children) != 1 || children[0].Prefix.String() != "45.171.60.0/22" {
		t.Errorf("filhos de 45/8 = %+v %v", children, err)
	}

	a, err := st.ASN(ctx, 61613)
	if err != nil || *a.Name != "TMSoft Solucoes em Informatica Ltda" || *a.HolderDocument != "08.030.063/0001-00" {
		t.Errorf("AS61613 = %+v %v", a, err)
	}
	if _, err := st.ASN(ctx, 1); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("AS1 deveria ser ErrNotFound, veio %v", err)
	}
	blocks, _ := st.ASNBlocks(ctx, 64512)
	if len(blocks) != 2 {
		t.Errorf("blocos de 64512 = %+v", blocks)
	}

	links, total, err := st.PrefixesForASN(ctx, 61613, a.HolderID, 2)
	if err != nil || total != 3 || len(links) != 2 || links[0].Link != "nicbr" {
		t.Errorf("prefixos do AS61613 = %+v total=%d %v", links, total, err)
	}
	google, _ := st.ASN(ctx, 15169)
	links, total, _ = st.PrefixesForASN(ctx, 15169, google.HolderID, 10)
	if total != 1 || links[0].Link != "holder" || links[0].Prefix.String() != "8.8.8.0/24" {
		t.Errorf("prefixos do AS15169 = %+v", links)
	}

	h, err := st.Holder(ctx, "lacnic", "258500")
	if err != nil || h.Key != "lacnic:258500" {
		t.Fatalf("titular = %+v %v", h, err)
	}
	page, _ := st.HolderPrefixes(ctx, h.ID, nil, 2)
	if len(page) != 2 {
		t.Fatalf("página 1 = %+v", page)
	}
	rest, _ := st.HolderPrefixes(ctx, h.ID, &page[1].Prefix, 10)
	if len(rest) != 1 {
		t.Errorf("página 2 = %+v", rest)
	}

	var br []string
	err = st.ListPrefixes(ctx, store.ListFilter{Country: "BR", Statuses: []string{"allocated"}}, nil, 0,
		func(p store.ListPrefix) error { br = append(br, p.Prefix.String()); return nil })
	if err != nil || len(br) != 3 {
		t.Errorf("prefixos BR = %v %v", br, err)
	}
	var v6 int
	_ = st.ListPrefixes(ctx, store.ListFilter{RIR: "lacnic", Family: 6}, nil, 10,
		func(store.ListPrefix) error { v6++; return nil })
	if v6 != 1 {
		t.Errorf("IPv6 da LACNIC = %d", v6)
	}
	var asns []int64
	_ = st.ListASNs(ctx, store.ListFilter{Country: "BR"}, -1, 0, func(b store.ASNBrief) error {
		asns = append(asns, b.ASN)
		return nil
	})
	if len(asns) != 1 || asns[0] != 61613 {
		t.Errorf("ASNs BR = %v", asns)
	}

	hist, err := st.History(ctx, "asn", "61613", 10)
	if err != nil || len(hist) != 1 || hist[0].Action != "update" {
		t.Errorf("histórico = %+v %v", hist, err)
	}
	src, err := st.Sources(ctx)
	if err != nil || len(src) != 1 || src[0].SourceID != "rir-lacnic" {
		t.Errorf("fontes = %+v %v", src, err)
	}
	d, err := st.CurrentDataset(ctx)
	if err != nil || d.Version != 1 {
		t.Errorf("dataset = %+v %v", d, err)
	}
	exc, err := st.CacheExceptions(ctx)
	if err != nil || len(exc) != 1 || exc[0].String() != "192.0.0.0/24" {
		t.Errorf("exceções = %v %v", exc, err)
	}
}
