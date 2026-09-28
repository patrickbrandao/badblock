package parse

import (
	"bufio"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// DelegatedHeader é a linha de versão de um arquivo delegated-extended:
// version|registry|serial|records|startdate|enddate|UTCoffset.
type DelegatedHeader struct {
	Version   string
	Registry  string
	Serial    string
	Records   int
	EndDate   *time.Time
	UTCOffset string
}

// Delegation é um registro registry|cc|type|start|value|date|status|opaque-id.
type Delegation struct {
	Registry string
	CC       string
	Type     string // asn, ipv4 ou ipv6
	Start    string // forma canônica: número do ASN ou IP normalizado
	Value    int64
	Date     *time.Time
	Status   string
	OpaqueID string

	ASNFirst int64          // só asn
	ASNLast  int64          // só asn
	CIDRs    []netip.Prefix // só ipv4 e ipv6
}

// DelegatedFile é o conteúdo validado de um arquivo delegated-extended.
type DelegatedFile struct {
	Header  DelegatedHeader
	Summary map[string]int
	Records []Delegation
	Stats   Stats
}

// ParseDelegated lê um arquivo delegated-extended e confere a integridade:
// o registry de cada linha, as linhas summary contra os registros lidos e o
// total do cabeçalho. Qualquer divergência vira erro, porque um arquivo
// truncado ou corrompido aplicado ao banco apagaria recursos válidos.
func ParseDelegated(r io.Reader, registry string) (*DelegatedFile, error) {
	f := &DelegatedFile{Summary: map[string]int{}}
	counted := map[string]int{}
	haveHeader := false

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "|")

		if !haveHeader {
			h, err := parseDelegatedHeader(fields)
			if err != nil {
				return nil, fmt.Errorf("linha %d: cabeçalho: %w", lineNo, err)
			}
			if h.Registry != registry {
				return nil, fmt.Errorf("cabeçalho do registry %q, esperado %q", h.Registry, registry)
			}
			f.Header = h
			haveHeader = true
			continue
		}

		if len(fields) >= 6 && fields[5] == "summary" {
			n, err := strconv.Atoi(fields[4])
			if err != nil {
				return nil, fmt.Errorf("linha %d: summary inválido: %q", lineNo, line)
			}
			f.Summary[fields[2]] = n
			continue
		}

		d, err := parseDelegation(fields, registry)
		if err != nil {
			f.Stats.skip(lineNo, "%v", err)
			continue
		}
		if d.warning != "" {
			f.Stats.warn(lineNo, "%s", d.warning)
		}
		counted[d.Type]++
		f.Records = append(f.Records, d.Delegation)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("leitura: %w", err)
	}
	if !haveHeader {
		return nil, fmt.Errorf("arquivo sem cabeçalho de versão")
	}
	f.Stats.Records = len(f.Records)

	if f.Stats.Skipped > 0 {
		return nil, &ErrTooManySkipped{Skipped: f.Stats.Skipped, Total: f.Stats.Skipped + f.Stats.Records, Samples: f.Stats.Samples}
	}
	for typ, want := range f.Summary {
		if counted[typ] != want {
			return nil, fmt.Errorf("summary de %s diz %d registros, o arquivo tem %d", typ, want, counted[typ])
		}
	}
	for typ, got := range counted {
		if _, ok := f.Summary[typ]; !ok {
			return nil, fmt.Errorf("arquivo tem %d registros %s sem linha summary", got, typ)
		}
	}
	if f.Header.Records > 0 && f.Header.Records != len(f.Records) {
		return nil, fmt.Errorf("cabeçalho diz %d registros, o arquivo tem %d", f.Header.Records, len(f.Records))
	}
	return f, nil
}

func parseDelegatedHeader(fields []string) (DelegatedHeader, error) {
	if len(fields) < 7 {
		return DelegatedHeader{}, fmt.Errorf("esperados 7 campos, há %d", len(fields))
	}
	if !strings.HasPrefix(fields[0], "2") {
		return DelegatedHeader{}, fmt.Errorf("versão desconhecida %q", fields[0])
	}
	records, err := strconv.Atoi(fields[3])
	if err != nil {
		return DelegatedHeader{}, fmt.Errorf("total de registros inválido %q", fields[3])
	}
	end, err := parseDate(fields[5])
	if err != nil {
		return DelegatedHeader{}, fmt.Errorf("enddate: %w", err)
	}
	return DelegatedHeader{
		Version:   fields[0],
		Registry:  fields[1],
		Serial:    fields[2],
		Records:   records,
		EndDate:   end,
		UTCOffset: fields[6],
	}, nil
}

type parsedDelegation struct {
	Delegation
	warning string
}

func parseDelegation(fields []string, registry string) (parsedDelegation, error) {
	var out parsedDelegation
	if len(fields) < 7 {
		return out, fmt.Errorf("esperados ao menos 7 campos, há %d", len(fields))
	}
	if fields[0] != registry {
		return out, fmt.Errorf("registry %q em arquivo de %q", fields[0], registry)
	}
	d := Delegation{
		Registry: fields[0],
		CC:       strings.ToUpper(strings.TrimSpace(fields[1])),
		Type:     strings.ToLower(fields[2]),
		Status:   strings.ToLower(strings.TrimSpace(fields[6])),
	}
	if len(fields) >= 8 {
		d.OpaqueID = strings.TrimSpace(fields[7])
	}
	if d.Status == "" {
		return out, fmt.Errorf("status vazio")
	}
	date, err := parseDate(fields[5])
	if err != nil {
		out.warning = err.Error()
	}
	d.Date = date

	value, err := strconv.ParseInt(fields[4], 10, 64)
	if err != nil || value <= 0 {
		return out, fmt.Errorf("valor inválido %q", fields[4])
	}
	d.Value = value

	switch d.Type {
	case "asn":
		first, err := strconv.ParseUint(fields[3], 10, 32)
		if err != nil {
			return out, fmt.Errorf("ASN inicial inválido %q", fields[3])
		}
		last := int64(first) + value - 1
		if last > 4294967295 {
			return out, fmt.Errorf("faixa de ASN passa de 4294967295")
		}
		d.Start = strconv.FormatUint(first, 10)
		d.ASNFirst, d.ASNLast = int64(first), last

	case "ipv4":
		addr, err := netip.ParseAddr(fields[3])
		if err != nil || !addr.Is4() {
			return out, fmt.Errorf("IPv4 inválido %q", fields[3])
		}
		cidrs, err := IPv4RangeToCIDRs(addr, uint64(value))
		if err != nil {
			return out, err
		}
		d.Start = addr.String()
		d.CIDRs = cidrs

	case "ipv6":
		addr, err := netip.ParseAddr(fields[3])
		if err != nil || !addr.Is6() || addr.Is4In6() {
			return out, fmt.Errorf("IPv6 inválido %q", fields[3])
		}
		if value > 128 {
			return out, fmt.Errorf("prefixo IPv6 /%d inválido", value)
		}
		prefix := netip.PrefixFrom(addr, int(value))
		if masked := prefix.Masked(); masked.Addr() != addr {
			out.warning = fmt.Sprintf("%s/%d não está alinhado; usando %s", addr, value, masked)
			prefix = masked
		}
		d.Start = addr.String()
		d.CIDRs = []netip.Prefix{prefix}

	default:
		return out, fmt.Errorf("tipo desconhecido %q", fields[2])
	}

	out.Delegation = d
	return out, nil
}
