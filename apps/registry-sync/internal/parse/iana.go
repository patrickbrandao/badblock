package parse

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// IANAASNBlock é uma linha de as-numbers-1.csv ou as-numbers-2.csv.
type IANAASNBlock struct {
	First, Last int64
	Description string
	WHOIS       string
	RDAP        string
	Reference   string
	Date        *time.Time
}

// IANAIPBlock é uma linha de ipv4-address-space.csv ou de
// ipv6-unicast-address-assignments.csv.
type IANAIPBlock struct {
	Prefix      netip.Prefix
	Designation string
	Date        *time.Time
	WHOIS       string
	RDAP        string
	Status      string
	Note        string
}

// SpecialIP é um bloco dos registros special-purpose de IPv4 e IPv6.
type SpecialIP struct {
	Prefix             netip.Prefix
	Name               string
	RFC                string
	AllocDate          *time.Time
	Termination        string
	Source             *bool
	Destination        *bool
	Forwardable        *bool
	GloballyReachable  *bool
	ReservedByProtocol *bool
}

// SpecialASN é uma linha de special-purpose-as-numbers.csv.
type SpecialASN struct {
	First, Last int64
	Reason      string
	Reference   string
}

// footnoteRe casa as notas de rodapé da IANA, como " [2]".
var footnoteRe = regexp.MustCompile(`\s*\[\d+\]`)

// stripFootnotes remove notas de rodapé e espaços das pontas.
func stripFootnotes(v string) string {
	return strings.TrimSpace(footnoteRe.ReplaceAllString(v, ""))
}

// readCSV lê um CSV da IANA e confere o cabeçalho: se a IANA mudar as colunas,
// o parser falha em vez de gravar dados na coluna errada.
func readCSV(r io.Reader, header []string) ([][]string, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csv: %w", err)
	}
	if len(rows) == 0 {
		return nil, errors.New("csv vazio")
	}
	got := rows[0]
	if len(got) < len(header) {
		return nil, fmt.Errorf("cabeçalho com %d colunas, esperadas %d (%v)", len(got), len(header), got)
	}
	for i, want := range header {
		if !strings.EqualFold(stripFootnotes(got[i]), want) {
			return nil, fmt.Errorf("coluna %d é %q, esperada %q", i+1, got[i], want)
		}
	}
	return rows[1:], nil
}

// cell devolve a coluna i ou "" quando a linha é curta.
func cell(row []string, i int) string {
	if i < len(row) {
		return strings.TrimSpace(row[i])
	}
	return ""
}

// parseASNRange lê "64512" ou "64512-65534".
func parseASNRange(v string) (int64, int64, error) {
	v = strings.TrimSpace(v)
	a, b, isRange := strings.Cut(v, "-")
	first, err := strconv.ParseUint(strings.TrimSpace(a), 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("ASN inválido %q", v)
	}
	last := first
	if isRange {
		last, err = strconv.ParseUint(strings.TrimSpace(b), 10, 32)
		if err != nil || last < first {
			return 0, 0, fmt.Errorf("faixa de ASN inválida %q", v)
		}
	}
	return int64(first), int64(last), nil
}

// ParseIANAASN lê as-numbers-1.csv (16 bits) ou as-numbers-2.csv (32 bits).
// A linha "See Sub-registry 16-bit AS numbers" do arquivo de 32 bits aponta
// para o outro arquivo e é ignorada.
func ParseIANAASN(r io.Reader) ([]IANAASNBlock, Stats, error) {
	var st Stats
	rows, err := readCSV(r, []string{"Number", "Description", "WHOIS", "RDAP", "Reference", "Registration Date"})
	if err != nil {
		return nil, st, err
	}
	var out []IANAASNBlock
	for i, row := range rows {
		line := i + 2
		desc := cell(row, 1)
		if strings.HasPrefix(strings.ToLower(desc), "see sub-registry") {
			continue
		}
		first, last, err := parseASNRange(cell(row, 0))
		if err != nil {
			st.skip(line, "%v", err)
			continue
		}
		date, err := parseDate(cell(row, 5))
		if err != nil {
			st.warn(line, "%v", err)
		}
		out = append(out, IANAASNBlock{
			First:       first,
			Last:        last,
			Description: desc,
			WHOIS:       cell(row, 2),
			RDAP:        PreferHTTPS(splitURLs(cell(row, 3))),
			Reference:   cell(row, 4),
			Date:        date,
		})
	}
	st.Records = len(out)
	return out, st, checkSkipped(st, 0.01)
}

// ParseIANAIPv4 lê ipv4-address-space.csv, cujo prefixo vem como "001/8".
func ParseIANAIPv4(r io.Reader) ([]IANAIPBlock, Stats, error) {
	return parseIANAIPBlocks(r, func(v string) (netip.Prefix, error) {
		octet, bitsStr, ok := strings.Cut(v, "/")
		if !ok || bitsStr != "8" {
			return netip.Prefix{}, fmt.Errorf("prefixo IPv4 da IANA inesperado %q", v)
		}
		n, err := strconv.Atoi(octet)
		if err != nil || n < 0 || n > 255 {
			return netip.Prefix{}, fmt.Errorf("octeto inválido %q", v)
		}
		return netip.PrefixFrom(netip.AddrFrom4([4]byte{byte(n), 0, 0, 0}), 8), nil
	})
}

// ParseIANAIPv6 lê ipv6-unicast-address-assignments.csv.
func ParseIANAIPv6(r io.Reader) ([]IANAIPBlock, Stats, error) {
	return parseIANAIPBlocks(r, func(v string) (netip.Prefix, error) {
		p, err := netip.ParsePrefix(v)
		if err != nil || !p.Addr().Is6() {
			return netip.Prefix{}, fmt.Errorf("prefixo IPv6 inválido %q", v)
		}
		return p.Masked(), nil
	})
}

func parseIANAIPBlocks(r io.Reader, parsePrefix func(string) (netip.Prefix, error)) ([]IANAIPBlock, Stats, error) {
	var st Stats
	rows, err := readCSV(r, []string{"Prefix", "Designation", "Date", "WHOIS", "RDAP", "Status", "Note"})
	if err != nil {
		return nil, st, err
	}
	var out []IANAIPBlock
	for i, row := range rows {
		line := i + 2
		prefix, err := parsePrefix(cell(row, 0))
		if err != nil {
			st.skip(line, "%v", err)
			continue
		}
		date, err := parseDate(cell(row, 2))
		if err != nil {
			st.warn(line, "%v", err)
		}
		status := strings.ToLower(stripFootnotes(cell(row, 5)))
		if status == "" {
			st.skip(line, "status vazio")
			continue
		}
		out = append(out, IANAIPBlock{
			Prefix:      prefix,
			Designation: cell(row, 1),
			Date:        date,
			WHOIS:       cell(row, 3),
			RDAP:        PreferHTTPS(splitURLs(cell(row, 4))),
			Status:      status,
			Note:        cell(row, 6),
		})
	}
	st.Records = len(out)
	return out, st, checkSkipped(st, 0.01)
}

// parseTriBool lê True/False dos registros special-purpose; N/A e vazio são nil.
func parseTriBool(v string) *bool {
	switch strings.ToLower(stripFootnotes(v)) {
	case "true":
		b := true
		return &b
	case "false":
		b := false
		return &b
	default:
		return nil
	}
}

// ParseIANASpecialIP lê iana-ipv4-special-registry-1.csv ou
// iana-ipv6-special-registry-1.csv. Uma célula pode trazer vários blocos
// ("192.0.0.170/32, 192.0.0.171/32") e notas de rodapé ("192.0.0.0/24 [2]").
func ParseIANASpecialIP(r io.Reader) ([]SpecialIP, Stats, error) {
	var st Stats
	rows, err := readCSV(r, []string{
		"Address Block", "Name", "RFC", "Allocation Date", "Termination Date",
		"Source", "Destination", "Forwardable", "Globally Reachable", "Reserved-by-Protocol",
	})
	if err != nil {
		return nil, st, err
	}
	var out []SpecialIP
	for i, row := range rows {
		line := i + 2
		date, err := parseDate(cell(row, 3))
		if err != nil {
			st.warn(line, "%v", err)
		}
		for _, raw := range strings.Split(stripFootnotes(cell(row, 0)), ",") {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			prefix, err := netip.ParsePrefix(raw)
			if err != nil {
				st.skip(line, "bloco inválido %q", raw)
				continue
			}
			out = append(out, SpecialIP{
				Prefix:             prefix.Masked(),
				Name:               strings.TrimSpace(strings.Trim(cell(row, 1), `"`)),
				RFC:                cell(row, 2),
				AllocDate:          date,
				Termination:        cell(row, 4),
				Source:             parseTriBool(cell(row, 5)),
				Destination:        parseTriBool(cell(row, 6)),
				Forwardable:        parseTriBool(cell(row, 7)),
				GloballyReachable:  parseTriBool(cell(row, 8)),
				ReservedByProtocol: parseTriBool(cell(row, 9)),
			})
		}
	}
	st.Records = len(out)
	return out, st, checkSkipped(st, 0.01)
}

// ParseIANASpecialASN lê special-purpose-as-numbers.csv.
func ParseIANASpecialASN(r io.Reader) ([]SpecialASN, Stats, error) {
	var st Stats
	rows, err := readCSV(r, []string{"AS Number", "Reason for Reservation", "Reference"})
	if err != nil {
		return nil, st, err
	}
	var out []SpecialASN
	for i, row := range rows {
		first, last, err := parseASNRange(cell(row, 0))
		if err != nil {
			st.skip(i+2, "%v", err)
			continue
		}
		out = append(out, SpecialASN{First: first, Last: last, Reason: cell(row, 1), Reference: cell(row, 2)})
	}
	st.Records = len(out)
	return out, st, checkSkipped(st, 0.01)
}
