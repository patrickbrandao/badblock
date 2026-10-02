package tldname

import (
	"strings"
	"testing"
)

// idn são pares reais de rootzone_tld (arquivo de 2026-09-29): o rótulo
// "xn--" e a forma Unicode que o coletor grava.
var idn = map[string]string{
	"xn--p1ai":                 "рф",
	"xn--90ae":                 "бг",
	"xn--80asehdb":             "онлайн",
	"xn--fiqs8s":               "中国",
	"xn--zfr164b":              "政务",
	"xn--kcrx77d1x4a":          "飞利浦",
	"xn--3e0b707e":             "한국",
	"xn--11b4c3d":              "कॉम",
	"xn--mgbaam7a8h":           "امارات",
	"xn--mgbah1a3hjkrd":        "موريتانيا",
	"xn--clchc0ea0b2g2a9gcd":   "சிங்கப்பூர்",
	"xn--w4r85el8fhu5dnra":     "嘉里大酒店",
	"xn--vermgensberater-ctb":  "vermögensberater",
	"xn--vermgensberatung-pwb": "vermögensberatung",
}

func TestPunycodeRoundTrip(t *testing.T) {
	for ascii, uni := range idn {
		enc, err := Encode(uni)
		if err != nil || "xn--"+enc != ascii {
			t.Errorf("Encode(%q) = %q, %v; quero %q", uni, enc, err, ascii)
		}
		dec, err := Decode(strings.TrimPrefix(ascii, "xn--"))
		if err != nil || dec != uni {
			t.Errorf("Decode(%q) = %q, %v; quero %q", ascii, dec, err, uni)
		}
	}
	// Exemplos da RFC 3492, seção 7.1 (A: árabe; L: japonês com ASCII).
	for uni, want := range map[string]string{
		"ليهمابتكلموشعربي؟": "egbpdaj6bu4bxfgehfvwxn",
		"3年B組金八先生":          "3B-ww4c5e180e575a65lsy2b",
	} {
		if got, err := Encode(uni); err != nil || got != want {
			t.Errorf("Encode(%q) = %q, %v; quero %q", uni, got, err, want)
		}
		if got, err := Decode(want); err != nil || got != uni {
			t.Errorf("Decode(%q) = %q, %v; quero %q", want, got, err, uni)
		}
	}
	if _, err := Encode(""); err == nil {
		t.Error("Encode vazio deveria falhar")
	}
	if _, err := Decode("99999999999"); err == nil {
		t.Error("Decode com estouro deveria falhar")
	}
}

func TestNormalize(t *testing.T) {
	ok := map[string]string{
		"br":                    "br",
		"BR":                    "br",
		"Br.":                   "br",
		"com.":                  "com",
		"xn--p1ai":              "xn--p1ai",
		"XN--P1AI.":             "xn--p1ai",
		"рф":                    "xn--p1ai",
		"РФ":                    "xn--p1ai",
		"рф.":                   "xn--p1ai",
		"中国":                    "xn--fiqs8s",
		"Vermögensberater":      "xn--vermgensberater-ctb",
		"a":                     "a",
		"under_score":           "under_score",
		"x-y":                   "x-y",
		strings.Repeat("a", 63): strings.Repeat("a", 63),
		// Rótulo válido que não existe na zona: a normalização não confere
		// o punycode (o coletor também aceita qualquer rótulo xn--).
		"xn--zz": "xn--zz",
	}
	for in, want := range ok {
		if got, err := Normalize(in); err != nil || got != want {
			t.Errorf("Normalize(%q) = %q, %v; quero %q", in, got, err, want)
		}
	}
	bad := []string{
		"", ".", "..", "br..", "a.b", "a.br.", " br", "br ", "b r", "b\tr", "b\x00r",
		"b%r", "b*r", "b/r", "b:r", "b+r", "\xff", "b\xffr",
		strings.Repeat("a", 64),
		"рф.рф", "р ф", "р\u0000ф", "рф!", "\u200b",
		strings.Repeat("ф", 60), // o punycode passa de 63 caracteres
	}
	for _, in := range bad {
		if got, err := Normalize(in); err == nil {
			t.Errorf("Normalize(%q) = %q, quero erro", in, got)
		}
	}
}
