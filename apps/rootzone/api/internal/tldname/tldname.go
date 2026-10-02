// Package tldname normaliza o nome de um TLD pedido na URL para a forma que
// o collector-rootzone grava em rootzone_tld.tld: ASCII, minúsculas, sem o
// ponto final, um rótulo só de [a-z0-9_-] com 1 a 63 caracteres. Um nome em
// Unicode (рф) vira o rótulo IDNA "xn--" (xn--p1ai) pelo punycode da
// RFC 3492, feito à mão (a biblioteca padrão não o exporta e o projeto não
// traz dependência para isso; o decodificador é cópia do
// apps/rootzone/collector/internal/parse/punycode.go).
package tldname

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxLabel é o tamanho máximo de um rótulo DNS (RFC 1035).
const MaxLabel = 63

// ErrInvalid é devolvido para um nome que não é um rótulo de TLD válido.
var ErrInvalid = errors.New("TLD inválido")

// Normalize devolve o TLD na forma gravada pelo coletor. Aceita qualquer
// caixa, com ou sem um ponto final, em ASCII (br, xn--p1ai) ou em Unicode
// (рф, РФ). Um nome em Unicode — só letras, marcas e dígitos fora do
// ASCII — é passado para minúsculas (strings.ToLower, sem o mapeamento
// completo da UTS #46 nem a normalização NFC) e codificado em punycode com o
// prefixo "xn--"; o resultado passa pela mesma regra do rótulo ASCII.
func Normalize(raw string) (string, error) {
	name := strings.TrimSuffix(raw, ".")
	if name == "" || !utf8.ValidString(name) {
		return "", ErrInvalid
	}
	if !isASCII(name) {
		// Fora do ASCII, só letras, marcas e dígitos (o ASCII é conferido
		// depois, pela regra do rótulo).
		if strings.ContainsFunc(name, func(r rune) bool {
			return r >= utf8.RuneSelf && !unicode.In(r, unicode.L, unicode.M, unicode.Nd)
		}) {
			return "", ErrInvalid
		}
		enc, err := Encode(strings.ToLower(name))
		if err != nil {
			return "", ErrInvalid
		}
		name = "xn--" + enc
	}
	name = strings.ToLower(name)
	if !validLabel(name) {
		return "", ErrInvalid
	}
	return name, nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// validLabel é a regra de chk_rootzone_tld_tld: ^[a-z0-9_-]{1,63}$.
func validLabel(s string) bool {
	if len(s) < 1 || len(s) > MaxLabel {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}
