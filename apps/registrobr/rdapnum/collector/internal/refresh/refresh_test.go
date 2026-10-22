package refresh

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		payload      string
		kind, target string
		ok           bool
	}{
		{`{"kind":"asn","target":"61610","at":"2026-10-21T21:10:00.123Z"}`, "asn", "61610", true},
		{`{"kind":"prefix","target":"200.225.48.0/21","at":"2026-10-21T21:10:00Z"}`, "prefix", "200.225.48.0/21", true},
		{`{"kind":"prefix","target":"2804:8ae0::/32"}`, "prefix", "2804:8ae0::/32", true},
		{`{"kind":"prefix","target":"200.225.48.1/21"}`, "", "", false}, // não canônico
		{`{"kind":"asn","target":"4294967296"}`, "", "", false},
		{`{"kind":"asn","target":"AS61610"}`, "", "", false},
		{`{"kind":"domain","target":"x"}`, "", "", false},
		{`não é json`, "", "", false},
	}
	for _, c := range cases {
		k, tg, ok := Parse(c.payload)
		if k != c.kind || tg != c.target || ok != c.ok {
			t.Errorf("%s = %q %q %v", c.payload, k, tg, ok)
		}
	}
}
