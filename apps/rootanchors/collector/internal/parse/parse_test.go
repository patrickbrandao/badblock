package parse

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"
)

func readFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustParse(t *testing.T, s string) *Dataset {
	t.Helper()
	ds, err := Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

// replace troca exatamente uma ocorrência (o teste falha se o trecho sumiu da
// fixture).
func replace(t *testing.T, s, old, repl string) string {
	t.Helper()
	if strings.Count(s, old) != 1 {
		t.Fatalf("trecho %q aparece %d vezes na fixture", old, strings.Count(s, old))
	}
	return strings.Replace(s, old, repl, 1)
}

func TestParseSample(t *testing.T) {
	ds := mustParse(t, readFile(t, "root-anchors.xml"))
	if ds.AnchorID != "0C05FDD6-422C-4910-8ED6-430ED15E11C2" || ds.Zone != "." ||
		ds.Source != "http://data.iana.org/root-anchors/root-anchors.xml" {
		t.Errorf("anchor = %q / %q / %q", ds.AnchorID, ds.Zone, ds.Source)
	}
	if len(ds.Keys) != 3 || len(ds.Warnings) != 0 || ds.WarningCount() != 0 {
		t.Fatalf("keys = %d, warnings = %v", len(ds.Keys), ds.Warnings)
	}
	date := func(s string) time.Time { return mustTime(t, s) }

	old := ds.Keys[0]
	if old.ID != "Kjqmt7v" || old.KeyTag != 19036 || old.Algorithm != 8 || old.DigestType != 2 ||
		old.Digest != "49AAC11D7B6F6446702E54A1607371607A1A41855200FD2CE1CDDE32F24E8FB5" ||
		old.PublicKey != "" || old.Flags != nil || old.ValidUntil == nil ||
		!old.ValidFrom.Equal(date("2010-07-15T00:00:00Z")) || !old.ValidUntil.Equal(date("2019-01-11T00:00:00Z")) {
		t.Errorf("19036 = %+v", old)
	}
	for i, want := range []struct {
		id   string
		tag  int
		from string
		dig  string
	}{
		{"Klajeyz", 20326, "2017-02-02T00:00:00Z", "E06D44B80B8F1D39A95C0B0D7C65D08458E880409BBC683457104237C7F8EC8D"},
		{"Kmyv6jo", 38696, "2024-07-18T00:00:00Z", "683D2D0ACB8C9B712A1948B27F741219298D0A450D612C483AF444A4C0FB2B16"},
	} {
		k := ds.Keys[i+1]
		if k.ID != want.id || k.KeyTag != want.tag || k.Digest != want.dig || k.Flags == nil || *k.Flags != 257 ||
			k.ValidUntil != nil || !k.ValidFrom.Equal(date(want.from)) || !strings.HasPrefix(k.PublicKey, "AwEAA") {
			t.Errorf("%d = %+v", want.tag, k)
		}
		if k.ValidFrom.Location() != time.UTC {
			t.Errorf("%d: validFrom fora de UTC", want.tag)
		}
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// As duas KSKs com PublicKey reproduzem o key tag e o digest publicados; e os
// outros DigestType aceitos (SHA-1, SHA-384) conferem pelo mesmo caminho.
func TestKeyTagAndDigestOfRealKeys(t *testing.T) {
	ds := mustParse(t, readFile(t, "root-anchors.xml"))
	for _, k := range ds.Keys[1:] {
		raw, _ := base64.StdEncoding.DecodeString(k.PublicKey)
		rdata := DNSKEYRData(*k.Flags, k.Algorithm, raw)
		if got := KeyTag(rdata); got != k.KeyTag {
			t.Errorf("KeyTag = %d, quero %d", got, k.KeyTag)
		}
		if got := DSDigest(2, rdata); got != k.Digest {
			t.Errorf("%d: digest = %s", k.KeyTag, got)
		}
		if n := len(DSDigest(1, rdata)); n != 40 {
			t.Errorf("SHA-1 com %d dígitos", n)
		}
		if n := len(DSDigest(4, rdata)); n != 96 {
			t.Errorf("SHA-384 com %d dígitos", n)
		}
	}

	// Um KeyDigest SHA-384 da 38696, com o digest calculado, é aceito.
	src := readFile(t, "root-anchors.xml")
	k := ds.Keys[2]
	raw, _ := base64.StdEncoding.DecodeString(k.PublicKey)
	d384 := DSDigest(4, DNSKEYRData(257, 8, raw))
	src = replace(t, src, "<DigestType>2</DigestType>\n        <Digest>683D", "<DigestType>4</DigestType>\n        <Digest>683D")
	src = replace(t, src, k.Digest, strings.ToLower(d384)) // minúsculas também valem
	ds = mustParse(t, src)
	if got := ds.Keys[2]; got.DigestType != 4 || got.Digest != d384 {
		t.Errorf("SHA-384 = %+v", got)
	}
}

func TestKeyTagAlgorithm1(t *testing.T) {
	// RSA/MD5: os 16 bits mais significativos dos 24 menos significativos do
	// módulo (os três últimos bytes da chave).
	rdata := DNSKEYRData(257, 1, []byte{1, 2, 3, 0xAB, 0xCD, 0xEF})
	if got := KeyTag(rdata); got != 0xABCD {
		t.Errorf("KeyTag = %#x", got)
	}
}

func TestParseRejectsFixtures(t *testing.T) {
	for file, want := range map[string]string{
		"root-anchors-bad-digest.xml": "KeyDigest Klajeyz: digest não bate",
		"root-anchors-bad-keytag.xml": "KeyDigest Klajeyz: key tag 20327 não bate com o calculado da PublicKey (20326)",
		"root-anchors-bad-zone.xml":   `zona "com.": só a raiz`,
		"root-anchors-truncated.xml":  "XML: ",
	} {
		_, err := Parse(strings.NewReader(readFile(t, file)))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, quero %q", file, err, want)
		}
	}
}

func TestParseRejects(t *testing.T) {
	src := readFile(t, "root-anchors.xml")
	k20326 := `<KeyDigest id="Klajeyz" validFrom="2017-02-02T00:00:00+00:00">`
	cases := []struct {
		name, xml, want string
	}{
		{"vazio", "", "XML: arquivo vazio"},
		{"só a declaração", `<?xml version="1.0" encoding="UTF-8"?>` + "\n", "XML: arquivo vazio"},
		{"outra raiz", `<Anchors><Zone>.</Zone></Anchors>`, "XML: "},
		{"texto depois", src + "lixo", "texto depois de </TrustAnchor>"},
		{"elemento depois", src + "<TrustAnchor/>", "elemento <TrustAnchor> depois"},
		{"sem id do TrustAnchor", replace(t, src, `id="0C05FDD6-422C-4910-8ED6-430ED15E11C2" `, ""), "sem o atributo id no TrustAnchor"},
		{"sem Zone", replace(t, src, "<Zone>.</Zone>", ""), "sem o elemento Zone no TrustAnchor"},
		{"duas Zone", replace(t, src, "<Zone>.</Zone>", "<Zone>.</Zone><Zone>.</Zone>"), "2 elementos Zone no TrustAnchor"},
		{"sem KeyDigest", `<TrustAnchor id="x"><Zone>.</Zone></TrustAnchor>`, "nenhum KeyDigest"},
		{"id repetido", replace(t, src, `id="Kmyv6jo"`, `id="Klajeyz"`), "KeyDigest Klajeyz: id repetido"},
		{"sem id", replace(t, src, `id="Kjqmt7v" `, ""), "KeyDigest 1: sem o atributo id"},
		{"sem validFrom", replace(t, src, k20326, `<KeyDigest id="Klajeyz">`), "sem o atributo validFrom"},
		{"validFrom ruim", replace(t, src, `validFrom="2017-02-02T00:00:00+00:00"`, `validFrom="2017-02-02"`), `validFrom inválido: "2017-02-02"`},
		{"validUntil vazio", replace(t, src, k20326, `<KeyDigest id="Klajeyz" validFrom="2017-02-02T00:00:00+00:00" validUntil="">`), "validUntil inválido"},
		{"validUntil antes", replace(t, src, `validUntil="2019-01-11T00:00:00+00:00"`, `validUntil="2009-01-11T00:00:00+00:00"`), "validUntil 2009-01-11T00:00:00Z anterior a validFrom 2010-07-15T00:00:00Z"},
		{"sem KeyTag", replace(t, src, "<KeyTag>19036</KeyTag>", ""), "KeyDigest Kjqmt7v: sem o elemento KeyTag"},
		{"KeyTag repetido", replace(t, src, "<KeyTag>19036</KeyTag>", "<KeyTag>19036</KeyTag><KeyTag>1</KeyTag>"), "elemento KeyTag repetido"},
		{"KeyTag acima de 16 bits", replace(t, src, "<KeyTag>19036</KeyTag>", "<KeyTag>70000</KeyTag>"), `KeyTag inválido: "70000"`},
		{"KeyTag negativo", replace(t, src, "<KeyTag>19036</KeyTag>", "<KeyTag>-1</KeyTag>"), "KeyTag inválido"},
		{"Algorithm acima de 8 bits", replace(t, src, "<Algorithm>8</Algorithm>\n        <DigestType>2</DigestType>\n        <Digest>49AA", "<Algorithm>300</Algorithm>\n        <DigestType>2</DigestType>\n        <Digest>49AA"), "Algorithm inválido"},
		{"DigestType 3", replace(t, src, "<DigestType>2</DigestType>\n        <Digest>49AA", "<DigestType>3</DigestType>\n        <Digest>49AA"), "tipo de digest 3 não suportado"},
		{"digest curto", replace(t, src, "49AAC11D7B6F6446702E54A1607371607A1A41855200FD2CE1CDDE32F24E8FB5", "49AAC11D"), "digest com 8 dígitos hex; DigestType 2 pede 64"},
		{"digest não hex", replace(t, src, "49AAC11D7B6F6446702E54A1607371607A1A41855200FD2CE1CDDE32F24E8FB5", "ZZAAC11D7B6F6446702E54A1607371607A1A41855200FD2CE1CDDE32F24E8FB5"), "digest não é hexadecimal"},
		{"PublicKey sem Flags", replace(t, src, "<Flags>257</Flags>\n    </KeyDigest>\n    <KeyDigest id=\"Kmyv6jo\"", "</KeyDigest>\n    <KeyDigest id=\"Kmyv6jo\""), "KeyDigest Klajeyz: elemento PublicKey sem Flags"},
		{"Flags sem PublicKey", replace(t, src, "<Digest>49AAC11D7B6F6446702E54A1607371607A1A41855200FD2CE1CDDE32F24E8FB5</Digest>", "<Digest>49AAC11D7B6F6446702E54A1607371607A1A41855200FD2CE1CDDE32F24E8FB5</Digest><Flags>257</Flags>"), "KeyDigest Kjqmt7v: elemento Flags sem PublicKey"},
		{"Flags sem Zone Key", replace(t, src, "<Flags>257</Flags>\n    </KeyDigest>\n    <KeyDigest id=\"Kmyv6jo\"", "<Flags>1</Flags>\n    </KeyDigest>\n    <KeyDigest id=\"Kmyv6jo\""), "flags 1 sem o bit Zone Key (256)"},
		{"Flags 256 muda o key tag", replace(t, src, "<Flags>257</Flags>\n    </KeyDigest>\n    <KeyDigest id=\"Kmyv6jo\"", "<Flags>256</Flags>\n    </KeyDigest>\n    <KeyDigest id=\"Kmyv6jo\""), "key tag 20326 não bate"},
		{"PublicKey ruim", replace(t, src, "<PublicKey>AwEAAaz/", "<PublicKey>!!AwEAAaz/"), "PublicKey: não é base64 válido"},
		{"PublicKey trocada", replace(t, src, "<PublicKey>AwEAAaz/", "<PublicKey>AwEAAbz/"), "key tag 20326 não bate"},
	}
	for _, c := range cases {
		_, err := Parse(strings.NewReader(c.xml))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, quero %q", c.name, err, c.want)
		}
	}
}

func TestParseTolerates(t *testing.T) {
	src := readFile(t, "root-anchors.xml")
	// Chave pública quebrada em linhas, digest com espaços e em minúsculas,
	// validUntil igual a validFrom: tudo aceito.
	src = replace(t, src, "<PublicKey>AwEAAaz/tAm8yTn4", "<PublicKey>\n  AwEAAaz/\n  tAm8yTn4")
	src = replace(t, src, "<Digest>E06D44B80B8F1D39", "<Digest> e06d44b8 0b8f1d39")
	src = replace(t, src, `validUntil="2019-01-11T00:00:00+00:00"`, `validUntil="2010-07-15T00:00:00+00:00"`)
	ds := mustParse(t, src)
	if k := ds.Keys[1]; !strings.HasPrefix(k.PublicKey, "AwEAAaz/tAm8yTn4") || strings.ContainsAny(k.PublicKey, " \n") ||
		k.Digest != "E06D44B80B8F1D39A95C0B0D7C65D08458E880409BBC683457104237C7F8EC8D" {
		t.Errorf("20326 = %+v", k)
	}
	if k := ds.Keys[0]; !k.ValidUntil.Equal(k.ValidFrom) {
		t.Errorf("19036 = %+v", k)
	}
}

func TestParseWarnsUnknown(t *testing.T) {
	src := readFile(t, "root-anchors.xml")
	src = replace(t, src, "<Zone>.</Zone>", `<Zone>.</Zone><Comment>novo</Comment>`)
	src = replace(t, src, `<KeyDigest id="Kmyv6jo"`, `<KeyDigest status="x" id="Kmyv6jo"`)
	src = replace(t, src, "<KeyTag>38696</KeyTag>", "<KeyTag>38696</KeyTag><Note/>")
	src = replace(t, src, `<TrustAnchor id=`, `<TrustAnchor version="2" id=`)
	ds := mustParse(t, src)
	want := []string{
		"TrustAnchor: atributo desconhecido version ignorado",
		"TrustAnchor: elemento desconhecido <Comment> ignorado",
		"KeyDigest Kmyv6jo: atributo desconhecido status ignorado",
		"KeyDigest Kmyv6jo: elemento desconhecido <Note> ignorado",
	}
	if len(ds.Keys) != 3 || strings.Join(ds.Warnings, "|") != strings.Join(want, "|") {
		t.Errorf("warnings = %q", ds.Warnings)
	}
}

func TestWarningsAreCapped(t *testing.T) {
	src := readFile(t, "root-anchors.xml")
	src = replace(t, src, "<Zone>.</Zone>", "<Zone>.</Zone>"+strings.Repeat("<X/>", 60))
	ds := mustParse(t, src)
	if len(ds.Warnings) != MaxWarnings || ds.WarningCount() != 60 {
		t.Errorf("guardados %d, total %d", len(ds.Warnings), ds.WarningCount())
	}
}
