package tldname

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

// Encode codifica um rótulo em punycode, sem o prefixo "xn--" (RFC 3492,
// seção 6.3), ex.: "рф" → "p1ai". Os caracteres ASCII ficam como vieram
// (a caixa é problema de quem chama).
func Encode(s string) (string, error) {
	if s == "" || !utf8.ValidString(s) {
		return "", errors.New("rótulo vazio ou UTF-8 inválido")
	}
	input := []rune(s)
	var out strings.Builder
	for _, r := range input {
		if r < pcInitialN {
			out.WriteByte(byte(r))
		}
	}
	b := out.Len()
	h := b
	if b > 0 {
		out.WriteByte('-')
	}
	n, delta, bias := pcInitialN, 0, pcInitialBias
	for h < len(input) {
		m := pcMaxInt
		for _, r := range input {
			if int(r) >= n && int(r) < m {
				m = int(r)
			}
		}
		if m-n > (pcMaxInt-delta)/(h+1) {
			return "", errors.New("estouro")
		}
		delta += (m - n) * (h + 1)
		n = m
		for _, r := range input {
			if int(r) < n {
				if delta++; delta > pcMaxInt {
					return "", errors.New("estouro")
				}
			}
			if int(r) != n {
				continue
			}
			q := delta
			for k := pcBase; ; k += pcBase {
				t := k - bias
				if t < pcTMin {
					t = pcTMin
				} else if t > pcTMax {
					t = pcTMax
				}
				if q < t {
					break
				}
				out.WriteByte(pcEncodeDigit(t + (q-t)%(pcBase-t)))
				q = (q - t) / (pcBase - t)
			}
			out.WriteByte(pcEncodeDigit(q))
			bias = pcAdapt(delta, h+1, h == b)
			delta = 0
			h++
		}
		delta++
		n++
	}
	return out.String(), nil
}

// Decode decodifica um rótulo punycode sem o prefixo "xn--" (RFC 3492,
// seção 6.2), ex.: "p1ai" → "рф". Cópia de parse.Punycode do
// collector-rootzone, que grava rootzone_tld.tld_unicode com ela.
func Decode(s string) (string, error) {
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

// pcEncodeDigit: 0–25 → a–z, 26–35 → 0–9.
func pcEncodeDigit(d int) byte {
	if d < 26 {
		return byte('a' + d)
	}
	return byte('0' + d - 26)
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
