package store

import (
	"fmt"
	"net/netip"
	"testing"

	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/resolver"
)

func TestDecide(t *testing.T) {
	ip := netip.MustParseAddr("200.160.2.3")
	ptr := func(names ...string) resolver.Result {
		return resolver.Result{IP: ip, Status: resolver.PTR, Names: names}
	}
	fail := func(s resolver.Status) resolver.Result { return resolver.Result{IP: ip, Status: s} }
	noPTR := resolver.Result{IP: ip, Status: resolver.NoPTR}
	rowPTR := &Row{Status: resolver.PTR, Names: []string{"registro.br"}}
	rowFail := &Row{Status: resolver.Timeout}

	cases := []struct {
		name string
		cur  *Row
		res  resolver.Result
		want Op
	}{
		{"sem linha, ptr", nil, ptr("registro.br"), Insert},
		{"sem linha, falha", nil, fail(resolver.ServFail), Insert},
		{"sem linha, no_ptr", nil, noPTR, None},
		{"ptr, outros nomes", rowPTR, ptr("novo.registro.br"), Update},
		{"ptr, um nome a mais", rowPTR, ptr("a.registro.br", "registro.br"), Update},
		{"ptr, falha (não apaga o PTR)", rowPTR, fail(resolver.Timeout), None},
		{"ptr, refused", rowPTR, fail(resolver.Refused), None},
		{"falha, ptr", rowFail, ptr("registro.br"), Update},
		{"falha, outra falha", rowFail, fail(resolver.ServFail), Update},
		{"falha, a mesma falha", rowFail, fail(resolver.Timeout), None},
		{"ptr, no_ptr", rowPTR, noPTR, Delete},
		{"falha, no_ptr", rowFail, noPTR, Delete},
		{"ptr igual", rowPTR, ptr("registro.br"), None},
	}
	for _, c := range cases {
		if got := Decide(c.cur, c.res); got != c.want {
			t.Errorf("%s: %v, quero %v", c.name, got, c.want)
		}
	}
}

func TestTally(t *testing.T) {
	got := Tally([]resolver.Result{
		{Status: resolver.PTR}, {Status: resolver.PTR}, {Status: resolver.NoPTR},
		{Status: resolver.ServFail}, {Status: resolver.Refused}, {Status: resolver.Timeout},
	})
	if got != (Counts{PTR: 2, NoPTR: 1, Failed: 3}) {
		t.Errorf("Tally = %+v", got)
	}
}

func TestCapWarnings(t *testing.T) {
	if got := capWarnings(nil); got == nil || len(got) != 0 {
		t.Errorf("sem avisos = %#v", got)
	}
	var many []string
	for i := range 53 {
		many = append(many, fmt.Sprintf("aviso %d", i))
	}
	got := capWarnings(many)
	if len(got) != 51 || got[50] != "... e mais 3 avisos" || got[49] != "aviso 49" {
		t.Errorf("capWarnings = %d itens, último %q", len(got), got[len(got)-1])
	}
}
