package tldname

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
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

// hugeCJK devolve n ideogramas CJK distintos (unificados, a partir de
// U+4E00, e da extensão A, U+3400): o pior caso do Encode, Θ(distintos ×
// tamanho).
func hugeCJK(n int) string {
	var b strings.Builder
	b.Grow(3 * n)
	r := rune(0x4E00)
	for range n {
		b.WriteRune(r)
		r++
		if r == 0x9FFF+1 {
			r = 0x3400 // extensão A
		}
		if r == 0x4DBF+1 {
			r = 0x4E00 // volta ao começo: repete, mas o tamanho é o que pesa
		}
	}
	return b.String()
}

// TestNormalizeEarlyReject confere que os nomes que não cabem num rótulo
// são recusados antes do punycode (o encode de teste falha se chamado), e
// que o limite não recusa um IDN válido.
func TestNormalizeEarlyReject(t *testing.T) {
	orig := encode
	t.Cleanup(func() { encode = orig })
	encode = func(s string) (string, error) {
		t.Errorf("encode chamado com %d runas", utf8.RuneCountInString(s))
		return orig(s)
	}
	for name, in := range map[string]string{
		"300 mil CJK distintos": hugeCJK(300_000),
		"60 runas":              strings.Repeat("ф", 60),
		"59 ф + ASCII":          strings.Repeat("ф", 59) + "a",
		"253 bytes ASCII":       strings.Repeat("a", 253),
		"253 bytes com ponto":   strings.Repeat("a", 253) + ".",
		"1 MiB ASCII":           strings.Repeat("a", 1<<20),
	} {
		if got, err := Normalize(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: Normalize = %q, %v; quero ErrInvalid", name, got, err)
		}
	}

	// No limite (59 runas) o punycode roda e decide pelo tamanho final.
	encode = orig
	if _, err := Normalize(strings.Repeat("ф", 59)); !errors.Is(err, ErrInvalid) {
		t.Errorf("59 ф: %v; quero ErrInvalid (o punycode passa de 63)", err)
	}
	// Nenhum IDN real passa do limite de runas: a checagem não tem falso
	// negativo (os 151 da zona são conferidos no teste com o arquivo real).
	for ascii, uni := range idn {
		if n := utf8.RuneCountInString(uni); n > maxRunes || len(ascii) < len("xn--")+n {
			t.Errorf("%s: %d runas, rótulo de %d caracteres", uni, n, len(ascii))
		}
		if got, err := Normalize(uni); err != nil || got != ascii {
			t.Errorf("Normalize(%q) = %q, %v; quero %q", uni, got, err, ascii)
		}
	}
}

func BenchmarkNormalizeHuge(b *testing.B) {
	in := hugeCJK(300_000)
	for b.Loop() {
		if _, err := Normalize(in); !errors.Is(err, ErrInvalid) {
			b.Fatal(err)
		}
	}
}
