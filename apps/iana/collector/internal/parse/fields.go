package parse

import (
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// footnoteRe acha as marcas de nota de rodapé da IANA: "192.0.0.0/24 [2]",
// "False [1]", "N/A [3]". Só dígitos entre colchetes, então referências como
// [RFC1918] não são tocadas.
var footnoteRe = regexp.MustCompile(`\s*\[\d+\]`)

// stripFootnotes tira as notas de rodapé e os espaços das pontas.
func stripFootnotes(s string) string {
	return strings.TrimSpace(footnoteRe.ReplaceAllString(s, ""))
}

// clean junta espaços, tabs e quebras de linha em um espaço só: os CSVs da
// IANA quebram células longas (notas, referências) no meio das frases.
func clean(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// parseASNRange lê "1-1876", "2043" ou "AS2043" (com espaços e nota de
// rodapé tolerados).
func parseASNRange(s string) (start, end int64, err error) {
	s = strings.ReplaceAll(stripFootnotes(s), " ", "")
	if len(s) > 2 && strings.EqualFold(s[:2], "AS") {
		s = s[2:]
	}
	a, b, isRange := strings.Cut(s, "-")
	if !isRange {
		b = a
	}
	x, err1 := strconv.ParseUint(a, 10, 32)
	y, err2 := strconv.ParseUint(b, 10, 32)
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("faixa de ASN inválida: %q", s)
	}
	if x > y {
		return 0, 0, fmt.Errorf("faixa de ASN invertida: %q", s)
	}
	return int64(x), int64(y), nil
}

// formatASNRange é a forma canônica de uma faixa: "2043" ou "1-1876".
func formatASNRange(start, end int64) string {
	if start == end {
		return strconv.FormatInt(start, 10)
	}
	return strconv.FormatInt(start, 10) + "-" + strconv.FormatInt(end, 10)
}

// parsePrefix lê um bloco CIDR. Aceita a forma da IANA para os /8 do IPv4
// ("000/8", "045/8": só o primeiro octeto, com zeros à esquerda). Devolve a
// forma canônica e se havia bits de host.
func parsePrefix(s string) (p netip.Prefix, hostBits bool, err error) {
	s = strings.ReplaceAll(stripFootnotes(s), " ", "")
	addr, bits, ok := strings.Cut(s, "/")
	if ok && addr != "" && !strings.ContainsAny(addr, ".:") {
		octet, err1 := strconv.ParseUint(addr, 10, 8)
		n, err2 := strconv.Atoi(bits)
		if err1 != nil || err2 != nil || n < 0 || n > 32 {
			return p, false, fmt.Errorf("bloco inválido: %q", s)
		}
		p = netip.PrefixFrom(netip.AddrFrom4([4]byte{byte(octet)}), n)
	} else {
		p, err = netip.ParsePrefix(s)
		if err != nil {
			return p, false, fmt.Errorf("bloco inválido: %q", s)
		}
	}
	if m := p.Masked(); m != p {
		return m, true, nil
	}
	return p, false, nil
}

// splitCell separa uma célula com vários valores ("192.0.0.170/32,
// 192.0.0.171/32").
func splitCell(s string) []string {
	return strings.FieldsFunc(stripFootnotes(s), func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\r' || r == '\t'
	})
}

// parseBool lê as flags dos special registries. Vazio e N/A viram nil
// (NULL); ok = false quando o valor não é reconhecido.
func parseBool(s string) (v *bool, ok bool) {
	switch strings.ToLower(stripFootnotes(s)) {
	case "true", "yes":
		t := true
		return &t, true
	case "false", "no":
		f := false
		return &f, true
	case "", "n/a", "na", "-":
		return nil, true
	}
	return nil, false
}

// statusRe é o formato aceito na coluna Status (ALLOCATED, LEGACY, RESERVED...).
var statusRe = regexp.MustCompile(`^[A-Z][A-Z _-]*$`)

var (
	monthRe = regexp.MustCompile(`^\d{4}-\d{2}$`)
	dayRe   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// parseDate aceita AAAA-MM e AAAA-MM-DD (as duas formas que a IANA usa) e
// devolve o texto como publicado. Vazio e N/A viram ""; ok = false quando a
// data não é reconhecida.
func parseDate(s string) (string, bool) {
	s = stripFootnotes(s)
	switch {
	case s == "" || strings.EqualFold(s, "n/a"):
		return "", true
	case monthRe.MatchString(s):
		_, err := time.Parse("2006-01", s)
		return s, err == nil
	case dayRe.MatchString(s):
		_, err := time.Parse("2006-01-02", s)
		return s, err == nil
	}
	return "", false
}

var urlStartRe = regexp.MustCompile(`(?i)https?://`)

// splitURLs separa a coluna RDAP. A IANA às vezes cola duas URLs sem
// separador ("https://rdap.arin.net/registryhttp://rdap.arin.net/registry"),
// então cada "http://" ou "https://" começa uma URL nova. ok = false quando a
// célula tem texto que não é URL.
func splitURLs(s string) (urls []string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true
	}
	starts := urlStartRe.FindAllStringIndex(s, -1)
	if len(starts) == 0 || strings.Trim(s[:starts[0][0]], " ,;") != "" {
		return nil, false
	}
	seen := map[string]bool{}
	for i, st := range starts {
		end := len(s)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		u := strings.Trim(s[st[0]:end], " ,;\t\r\n")
		if !validURL(u) {
			return nil, false
		}
		if !seen[u] {
			seen[u] = true
			urls = append(urls, u)
		}
	}
	return urls, true
}

func validURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// rirDomains liga o domínio de WHOIS/RDAP ao RIR (iana_*.registry).
var rirDomains = []struct{ domain, registry string }{
	{"afrinic.net", "afrinic"},
	{"apnic.net", "apnic"},
	{"arin.net", "arin"},
	{"lacnic.net", "lacnic"},
	{"ripe.net", "ripencc"},
}

// Registries são os valores possíveis de registry, em ordem alfabética.
var Registries = []string{"afrinic", "apnic", "arin", "lacnic", "ripencc"}

// registryFromHost: whois.arin.net → arin, rdap.db.ripe.net → ripencc.
func registryFromHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, ok := strings.Cut(host, ":"); ok {
		host = h
	}
	for _, r := range rirDomains {
		if host == r.domain || strings.HasSuffix(host, "."+r.domain) {
			return r.registry
		}
	}
	return ""
}

// registryFromURLs usa a primeira URL cujo domínio é de um RIR.
func registryFromURLs(urls []string) string {
	for _, s := range urls {
		if u, err := url.Parse(s); err == nil {
			if r := registryFromHost(u.Host); r != "" {
				return r
			}
		}
	}
	return ""
}

var rirNameRe = regexp.MustCompile(`(?i)^(?:(?:assigned|administered) by\s+)?(afrinic|apnic|arin|lacnic|ripe\s*ncc)$`)

// registryFromName: "Assigned by RIPE NCC", "Administered by ARIN", "LACNIC".
func registryFromName(s string) string {
	m := rirNameRe.FindStringSubmatch(clean(s))
	if m == nil {
		return ""
	}
	name := strings.ToLower(strings.ReplaceAll(m[1], " ", ""))
	return name
}
