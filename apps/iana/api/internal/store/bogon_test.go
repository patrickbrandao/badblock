package store

import (
	"net/netip"
	"testing"
)

func special(prefix string, gr *bool, terminated bool) SpecialPrefix {
	s := SpecialPrefix{Prefix: netip.MustParsePrefix(prefix), GloballyReachable: gr}
	if terminated {
		d := "2015-03"
		s.TerminationDate = &d
	}
	return s
}

func TestBogonRule(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name       string
		special    []SpecialPrefix // do mais específico para o menos
		unreserved bool
		want       bool
	}{
		{"8.8.8.8: sem special, /8 LEGACY", nil, true, false},
		{"10.0.0.1: special false", []SpecialPrefix{special("10.0.0.0/8", &no, false)}, false, true},
		{"100.64.0.1: special false vale sobre /8 ALLOCATED", []SpecialPrefix{special("100.64.0.0/10", &no, false)}, true, true},
		{"224.0.0.1: sem special, /8 RESERVED", nil, false, true},
		{"ff02::1: sem special, fora dos blocos", nil, false, true},
		{"192.0.0.9: exceção true dentro de /24 false",
			[]SpecialPrefix{special("192.0.0.9/32", &yes, false), special("192.0.0.0/24", &no, false)}, true, false},
		{"64:ff9b::1: special true fora dos blocos", []SpecialPrefix{special("64:ff9b::/96", &yes, false)}, false, false},
		{"2001::1 TEREDO: N/A não decide, bloco ALLOCATED",
			[]SpecialPrefix{special("2001::/32", nil, false), special("2001::/23", &no, false)}, true, false},
		{"2001:10::1: ORCHID encerrado não conta, vale 2001::/23",
			[]SpecialPrefix{special("2001:10::/28", nil, true), special("2001::/23", &no, false)}, true, true},
		{"192.88.99.1: só bloco encerrado, /8 LEGACY", []SpecialPrefix{special("192.88.99.0/24", nil, true)}, true, false},
		{"192.88.99.2: /32 false dentro do encerrado",
			[]SpecialPrefix{special("192.88.99.2/32", &no, false), special("192.88.99.0/24", nil, true)}, true, true},
	}
	for _, c := range cases {
		l := PrefixLookup{Special: c.special, Unreserved: c.unreserved}
		if got := l.Bogon(); got != c.want {
			t.Errorf("%s: bogon = %v, quero %v", c.name, got, c.want)
		}
	}
}
