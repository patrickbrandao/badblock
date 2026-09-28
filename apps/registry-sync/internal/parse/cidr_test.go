package parse

import (
	"net/netip"
	"reflect"
	"testing"
)

func TestIPv4RangeToCIDRs(t *testing.T) {
	cases := []struct {
		start string
		count uint64
		want  []string
	}{
		{"45.171.60.0", 1024, []string{"45.171.60.0/22"}},
		{"62.122.208.0", 1280, []string{"62.122.208.0/22", "62.122.212.0/24"}},
		{"164.146.0.0", 393216, []string{"164.146.0.0/15", "164.148.0.0/14"}},
		{"23.128.1.0", 768, []string{"23.128.1.0/24", "23.128.2.0/23"}},
		{"10.0.0.1", 1, []string{"10.0.0.1/32"}},
		{"0.0.0.0", 1 << 32, []string{"0.0.0.0/0"}},
		{"255.255.255.254", 2, []string{"255.255.255.254/31"}},
	}
	for _, c := range cases {
		got, err := IPv4RangeToCIDRs(netip.MustParseAddr(c.start), c.count)
		if err != nil {
			t.Fatalf("%s+%d: erro inesperado: %v", c.start, c.count, err)
		}
		var gotStr []string
		for _, p := range got {
			gotStr = append(gotStr, p.String())
		}
		if !reflect.DeepEqual(gotStr, c.want) {
			t.Errorf("%s+%d = %v, esperado %v", c.start, c.count, gotStr, c.want)
		}
	}
}

func TestIPv4RangeToCIDRsErrors(t *testing.T) {
	if _, err := IPv4RangeToCIDRs(netip.MustParseAddr("255.255.255.255"), 2); err == nil {
		t.Error("esperado erro de estouro do espaço IPv4")
	}
	if _, err := IPv4RangeToCIDRs(netip.MustParseAddr("10.0.0.0"), 0); err == nil {
		t.Error("esperado erro para quantidade zero")
	}
	if _, err := IPv4RangeToCIDRs(netip.MustParseAddr("2001:db8::"), 1); err == nil {
		t.Error("esperado erro para IPv6")
	}
}
