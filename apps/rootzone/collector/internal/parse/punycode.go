package parse

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Parâmetros do punycode para IDNA (RFC 3492, seção 5).
const (
	pcBase        = 36
	pcTMin        = 1
	pcTMax        = 26
	pcSkew        = 38
	pcDamp        = 700
	pcInitialBias = 72
	pcInitialN    = 128
	pcMaxInt      = 1<<31 - 1
)

// Punycode decodifica um rótulo punycode sem o prefixo "xn--" (RFC 3492,
// seção 6.2), ex.: "p1ai" → "рф". Feito à mão para não trazer dependência
// (a biblioteca padrão não exporta punycode).
func Punycode(s string) (string, error) {
	if s == "" {
		return "", errors.New("rótulo vazio")
	}
	var out []rune
	if b := strings.LastIndexByte(s, '-'); b >= 0 {
		for i := range b {
			if s[i] >= 0x80 {
				return "", fmt.Errorf("caractere não-ASCII na parte básica")
			}
			out = append(out, rune(s[i]))
		}
		s = s[b+1:]
	}
	n, i, bias := pcInitialN, 0, pcInitialBias
	for pos := 0; pos < len(s); {
		oldi, w := i, 1
		for k := pcBase; ; k += pcBase {
			if pos >= len(s) {
				return "", errors.New("sequência cortada")
			}
			digit := pcDigit(s[pos])
			pos++
			if digit < 0 {
				return "", fmt.Errorf("dígito inválido %q", s[pos-1])
			}
			if digit > (pcMaxInt-i)/w {
				return "", errors.New("estouro")
			}
			i += digit * w
			t := k - bias
			if t < pcTMin {
				t = pcTMin
			} else if t > pcTMax {
				t = pcTMax
			}
			if digit < t {
				break
			}
			if w > pcMaxInt/(pcBase-t) {
				return "", errors.New("estouro")
			}
			w *= pcBase - t
		}
		size := len(out) + 1
		bias = pcAdapt(i-oldi, size, oldi == 0)
		if i/size > pcMaxInt-n {
			return "", errors.New("estouro")
		}
		n += i / size
		i %= size
		if n < 0x80 || n > utf8.MaxRune || (n >= 0xD800 && n <= 0xDFFF) {
			return "", fmt.Errorf("ponto de código inválido U+%04X", n)
		}
		out = append(out, 0)
		copy(out[i+1:], out[i:])
		out[i] = rune(n)
		i++
	}
	return string(out), nil
}

func pcDigit(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c-'0') + 26
	case c >= 'a' && c <= 'z':
		return int(c - 'a')
	case c >= 'A' && c <= 'Z':
		return int(c - 'A')
	}
	return -1
}

func pcAdapt(delta, numPoints int, first bool) int {
	if first {
		delta /= pcDamp
	} else {
		delta /= 2
	}
	delta += delta / numPoints
	k := 0
	for delta > ((pcBase-pcTMin)*pcTMax)/2 {
		delta /= pcBase - pcTMin
		k += pcBase
	}
	return k + (pcBase-pcTMin+1)*delta/(delta+pcSkew)
}
