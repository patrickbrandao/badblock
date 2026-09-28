package parse

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestParseDelegatedFixture(t *testing.T) {
	f, err := os.Open("../../testdata/sources/rir-lacnic")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	file, err := ParseDelegated(f, "lacnic")
	if err != nil {
		t.Fatalf("ParseDelegated: %v", err)
	}
	if file.Header.EndDate == nil || file.Header.EndDate.Format("2006-01-02") != "2026-09-25" {
		t.Errorf("enddate = %v", file.Header.EndDate)
	}
	if file.Header.Records != len(file.Records) {
		t.Errorf("header %d != registros %d", file.Header.Records, len(file.Records))
	}

	var tmsoft *Delegation
	for i := range file.Records {
		if file.Records[i].Type == "asn" && file.Records[i].Start == "61613" {
			tmsoft = &file.Records[i]
		}
	}
	if tmsoft == nil {
		t.Fatal("AS61613 não encontrado")
	}
	if tmsoft.OpaqueID != "258500" || tmsoft.CC != "BR" || tmsoft.Status != "allocated" {
		t.Errorf("AS61613 = %+v", tmsoft)
	}
	if tmsoft.ASNFirst != 61613 || tmsoft.ASNLast != 61613 {
		t.Errorf("faixa = %d-%d", tmsoft.ASNFirst, tmsoft.ASNLast)
	}
}

func TestParseDelegatedNonCIDRAndRanges(t *testing.T) {
	f, err := os.Open("../../testdata/sources/rir-ripencc")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	file, err := ParseDelegated(f, "ripencc")
	if err != nil {
		t.Fatalf("ParseDelegated: %v", err)
	}
	for _, d := range file.Records {
		if d.Start == "62.122.208.0" {
			if len(d.CIDRs) != 2 || d.CIDRs[0].String() != "62.122.208.0/22" || d.CIDRs[1].String() != "62.122.212.0/24" {
				t.Errorf("CIDRs de 62.122.208.0+1280 = %v", d.CIDRs)
			}
			return
		}
	}
	t.Fatal("registro 62.122.208.0 não encontrado")
}

const delegatedSample = `2.3|lacnic|20260927|4|19870101|20260925|-0300
lacnic|*|asn|*|2|summary
lacnic|*|ipv4|*|1|summary
lacnic|*|ipv6|*|1|summary
lacnic|BR|asn|64000|3|20230505|allocated|42
lacnic||asn|6064|1||available
lacnic|BR|ipv4|45.171.60.0|1024|00000000|ASSIGNED|42
lacnic|BR|ipv6|2804:5964::|32|20190211|allocated|42
`

func TestParseDelegatedSample(t *testing.T) {
	file, err := ParseDelegated(strings.NewReader(delegatedSample), "lacnic")
	if err != nil {
		t.Fatalf("ParseDelegated: %v", err)
	}
	if len(file.Records) != 4 {
		t.Fatalf("registros = %d", len(file.Records))
	}
	asn := file.Records[0]
	if asn.ASNFirst != 64000 || asn.ASNLast != 64002 || asn.OpaqueID != "42" {
		t.Errorf("faixa de ASN = %+v", asn)
	}
	if file.Records[1].OpaqueID != "" || file.Records[1].Status != "available" {
		t.Errorf("registro sem opaque-id = %+v", file.Records[1])
	}
	v4 := file.Records[2]
	if v4.Date != nil {
		t.Errorf("data 00000000 deveria virar nil, veio %v", v4.Date)
	}
	if v4.Status != "assigned" {
		t.Errorf("status deveria ser normalizado para minúsculas, veio %q", v4.Status)
	}
	if v6 := file.Records[3]; len(v6.CIDRs) != 1 || v6.CIDRs[0].String() != "2804:5964::/32" {
		t.Errorf("IPv6 = %+v", v6)
	}
}

func TestParseDelegatedIntegrity(t *testing.T) {
	cases := map[string]string{
		"summary divergente":     strings.Replace(delegatedSample, "lacnic|*|ipv4|*|1|summary", "lacnic|*|ipv4|*|2|summary", 1),
		"cabeçalho divergente":   strings.Replace(delegatedSample, "20260927|4|", "20260927|5|", 1),
		"registry trocado":       strings.Replace(delegatedSample, "lacnic|BR|ipv6", "arin|BR|ipv6", 1),
		"linha truncada":         strings.Replace(delegatedSample, "lacnic|BR|ipv6|2804:5964::|32|20190211|allocated|42\n", "lacnic|BR|ipv6|2804:5964::\n", 1),
		"sem cabeçalho":          "",
		"cabeçalho de outro RIR": strings.Replace(delegatedSample, "2.3|lacnic|", "2.3|arin|", 1),
	}
	for name, content := range cases {
		if _, err := ParseDelegated(strings.NewReader(content), "lacnic"); err == nil {
			t.Errorf("%s: esperado erro", name)
		}
	}

	_, err := ParseDelegated(strings.NewReader(cases["linha truncada"]), "lacnic")
	var tooMany *ErrTooManySkipped
	if !errors.As(err, &tooMany) {
		t.Errorf("linha truncada deveria gerar ErrTooManySkipped, veio %v", err)
	}
}
