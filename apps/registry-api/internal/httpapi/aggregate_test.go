package httpapi

import (
	"net/netip"
	"testing"
)

func prefixes(ss ...string) []netip.Prefix {
	var out []netip.Prefix
	for _, s := range ss {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

func strs(ps []netip.Prefix) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.String())
	}
	return out
}

func TestAggregate(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"10.0.0.0/24", "10.0.1.0/24"}, []string{"10.0.0.0/23"}},
		{[]string{"10.0.1.0/24", "10.0.0.0/24", "10.0.2.0/23"}, []string{"10.0.0.0/22"}},
		{[]string{"10.0.0.0/8", "10.1.2.0/24"}, []string{"10.0.0.0/8"}},
		{[]string{"10.0.1.0/24", "10.0.2.0/24"}, []string{"10.0.1.0/24", "10.0.2.0/24"}},
		{[]string{"45.171.60.0/22", "45.171.60.0/22"}, []string{"45.171.60.0/22"}},
		{[]string{"2804:5964::/33", "2804:5964:8000::/33", "10.0.0.0/25", "10.0.0.128/25"},
			[]string{"10.0.0.0/24", "2804:5964::/32"}},
		{nil, nil},
	}
	for _, c := range cases {
		got := strs(Aggregate(prefixes(c.in...)))
		if len(got) != len(c.want) {
			t.Errorf("%v = %v, esperado %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%v = %v, esperado %v", c.in, got, c.want)
				break
			}
		}
	}
}
