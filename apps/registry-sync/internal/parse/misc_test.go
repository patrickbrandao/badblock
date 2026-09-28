package parse

import (
	"strings"
	"testing"
)

func TestParseRDAPBootstrap(t *testing.T) {
	asn, _, err := ParseRDAPBootstrap(open(t, "iana-rdap-asn"), "asn")
	if err != nil {
		t.Fatal(err)
	}
	var lacnic bool
	for _, s := range asn {
		if s.ASNFirst <= 61613 && 61613 <= s.ASNLast {
			lacnic = s.BaseURL == "https://rdap.lacnic.net/rdap/"
		}
		if !strings.HasPrefix(s.BaseURL, "https://") || !strings.HasSuffix(s.BaseURL, "/") {
			t.Errorf("URL base deveria ser https com barra final: %q", s.BaseURL)
		}
	}
	if !lacnic {
		t.Error("AS61613 deveria cair no RDAP da LACNIC")
	}

	v4, _, err := ParseRDAPBootstrap(open(t, "iana-rdap-ipv4"), "ip")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range v4 {
		if s.Prefix.String() == "200.0.0.0/8" && s.BaseURL != "https://rdap.lacnic.net/rdap/" {
			t.Errorf("200/8 = %q", s.BaseURL)
		}
	}

	if _, _, err := ParseRDAPBootstrap(strings.NewReader(`{"services":[]}`), "asn"); err == nil {
		t.Error("bootstrap vazio deveria gerar erro")
	}
}

func TestParseNICBR(t *testing.T) {
	recs, _, err := ParseNICBR(open(t, "nicbr"))
	if err != nil {
		t.Fatal(err)
	}
	byASN := map[int64]NICBRRecord{}
	for _, r := range recs {
		byASN[r.ASN] = r
	}
	tm := byASN[61613]
	if tm.Name != "TMSoft Solucoes em Informatica Ltda" || tm.Document != "08.030.063/0001-00" {
		t.Errorf("AS61613 = %+v", tm)
	}
	if len(tm.Prefixes) != 3 {
		t.Errorf("AS61613 deveria ter 3 blocos, tem %v", tm.Prefixes)
	}
	if byASN[174].Name != "COGENT BRASIL TELECOMUNICAÇÕES LTDA." {
		t.Errorf("acentos deveriam ser preservados: %q", byASN[174].Name)
	}

	sample := "AS1|A|1|10.0.0.0/8\nAS1|A|1|11.0.0.0/8|10.0.0.0/8\nAS2|B|2\nXX3|C|3\n"
	recs, st, err := ParseNICBR(strings.NewReader(sample))
	if err == nil {
		t.Errorf("1 linha ruim em 4 passa de 1%%: esperado erro (stats %+v)", st)
	}
	_ = recs

	sample = "AS1|A|1|10.0.0.0/8\nAS1|A|1|11.0.0.0/8|10.0.0.0/8\nAS2|B|2|10.0.0.1/8\n"
	recs, _, err = ParseNICBR(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || len(recs[0].Prefixes) != 2 {
		t.Errorf("ASN repetido deveria juntar blocos sem duplicar: %+v", recs)
	}
	if recs[1].Prefixes[0].String() != "10.0.0.0/8" {
		t.Errorf("bits de host deveriam ser zerados: %v", recs[1].Prefixes)
	}
}

func TestParseASNames(t *testing.T) {
	cases := map[string]ASName{
		"61613 AS61613 - TMSoft Solucoes em Informatica Ltda, BR":          {61613, "AS61613", "TMSoft Solucoes em Informatica Ltda", "BR"},
		"1 LVLT-1 - Level 3 Parent, LLC, US":                               {1, "LVLT-1", "Level 3 Parent, LLC", "US"},
		"28 DFVLR-SYS Deutsches Zentrum fuer Luft- und Raumfahrt e.V., DE": {28, "DFVLR-SYS", "Deutsches Zentrum fuer Luft- und Raumfahrt e.V.", "DE"},
		"37100 SEACOM Limited - SEACOM Limited, MU":                        {37100, "SEACOM Limited", "SEACOM Limited", "MU"},
		"23456 AS_TRANS": {23456, "AS_TRANS", "", ""},
	}
	for line, want := range cases {
		got, _, err := ParseASNames(strings.NewReader(line + "\n"))
		if err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		if len(got) != 1 || got[0] != want {
			t.Errorf("%q = %+v, esperado %+v", line, got, want)
		}
	}

	all, st, err := ParseASNames(open(t, "asnames"))
	if err != nil || st.Skipped != 0 || len(all) == 0 {
		t.Errorf("fixture asnames: %v %+v", err, st)
	}
}
