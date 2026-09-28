package httpapi

import (
	"net/netip"
	"slices"
)

// Aggregate junta prefixos contíguos e remove os contidos em outros, devolvendo
// a menor lista de CIDRs que cobre exatamente o mesmo espaço. Serve às listas
// para firewall (?format=txt&aggregate=true): menos regras, mesmo efeito.
func Aggregate(in []netip.Prefix) []netip.Prefix {
	var v4, v6 []netip.Prefix
	for _, p := range in {
		p = p.Masked()
		if p.Addr().Is4() {
			v4 = append(v4, p)
		} else {
			v6 = append(v6, p)
		}
	}
	return append(aggregateFamily(v4), aggregateFamily(v6)...)
}

func aggregateFamily(ps []netip.Prefix) []netip.Prefix {
	slices.SortFunc(ps, func(a, b netip.Prefix) int {
		if c := a.Addr().Compare(b.Addr()); c != 0 {
			return c
		}
		return a.Bits() - b.Bits()
	})
	var out []netip.Prefix
	for _, p := range ps {
		// Contido no último mantido: descarta. Como a lista está ordenada por
		// endereço e, no empate, do maior bloco para o menor, basta olhar o topo.
		if n := len(out); n > 0 && out[n-1].Overlaps(p) && out[n-1].Bits() <= p.Bits() {
			continue
		}
		out = append(out, p)
		// Enquanto os dois do topo forem irmãos (metades do mesmo bloco pai),
		// troca os dois pelo pai.
		for len(out) >= 2 {
			a, b := out[len(out)-2], out[len(out)-1]
			parent, ok := siblingParent(a, b)
			if !ok {
				break
			}
			out = append(out[:len(out)-2], parent)
		}
	}
	return out
}

// siblingParent diz se a e b são as duas metades de um mesmo bloco e devolve o pai.
func siblingParent(a, b netip.Prefix) (netip.Prefix, bool) {
	if a.Bits() != b.Bits() || a.Bits() == 0 {
		return netip.Prefix{}, false
	}
	parent := netip.PrefixFrom(a.Addr(), a.Bits()-1).Masked()
	if parent.Addr() != a.Addr() || !parent.Contains(b.Addr()) || a == b {
		return netip.Prefix{}, false
	}
	return parent, true
}
