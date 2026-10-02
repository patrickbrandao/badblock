// Package parse interpreta o arquivo "delegated-extended" de um RIR, formato
// comum a AFRINIC, APNIC, ARIN, LACNIC e RIPE NCC. Nada aqui é específico de
// um RIR: o registry esperado vem de quem chama.
//
// Campos separados por "|". Linhas vazias e começadas por "#" são ignoradas
// (o APNIC abre o arquivo com um bloco de comentários). A primeira linha de
// dados é o cabeçalho:
//
//	version|registry|serial|records|startdate|enddate|UTCoffset
//
// seguido de uma linha de resumo por tipo:
//
//	registry|*|type|*|count|summary
//
// e dos registros:
//
//	registry|cc|type|start|value|date|status[|opaque-id[|extensões...]]
//
// type é asn, ipv4 ou ipv6. value é a quantidade de ASNs (asn), a quantidade
// de endereços (ipv4, nem sempre um CIDR) ou o tamanho do prefixo (ipv6).
// date é AAAAMMDD, vazia ou 00000000; cc é o país (ZZ ou vazio = sem país);
// opaque-id identifica o titular dentro do RIR e fica vazio em available e
// reserved (alguns RIRs omitem o campo nessas linhas). Campos depois do
// opaque-id são extensões sem definição e são ignorados.
package parse

import (
	"bufio"
	"bytes"
	"cmp"
	"fmt"
	"io"
	"math"
	"math/bits"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// MaxWarnings limita os avisos guardados (o total fica em WarningCount).
const MaxWarnings = 50

// MaxSkippedRatio é a fração de registros descartados acima da qual o arquivo
// inteiro é recusado: um formato novo ou um arquivo corrompido não pode virar
// uma remoção em massa.
const MaxSkippedRatio = 0.01

// Statuses são os valores aceitos no campo status (os únicos que aparecem nos
// arquivos dos cinco RIRs). Outro valor descarta a linha, com aviso.
var Statuses = []string{"allocated", "assigned", "available", "reserved"}

// Header é a linha de versão do arquivo.
type Header struct {
	Version   string    // versão do formato: 2 ou 2.3
	Registry  string    // registry que gerou o arquivo, em minúsculas
	Serial    string    // série do arquivo, como publicada (data AAAAMMDD ou época Unix)
	Records   int       // registros declarados (sem cabeçalho, summary e comentários)
	StartDate time.Time // zero = vazia ou 00000000
	EndDate   time.Time // zero = vazia ou 00000000
	UTCOffset string
}

// ASN é um registro type = asn: a faixa Start..End().
type ASN struct {
	Start    int64
	Count    int64
	CC       string    // "" = vazio na fonte
	Date     time.Time // zero = vazia ou 00000000
	Status   string
	OpaqueID string // "" = vazio na fonte
}

// End é o último ASN da faixa.
func (a ASN) End() int64 { return a.Start + a.Count - 1 }

// Prefix é um bloco CIDR vindo de um registro ipv4 ou ipv6. Um registro IPv4
// que não forma um CIDR vira vários Prefix com os mesmos dados.
type Prefix struct {
	Prefix      netip.Prefix
	CC          string
	Date        time.Time
	Status      string
	OpaqueID    string
	RecordStart netip.Addr // campo start do registro de origem
	RecordValue int64      // campo value do registro de origem
}

// Dataset é o arquivo inteiro já validado.
type Dataset struct {
	Header      Header
	ASNs        []ASN
	Prefixes    []Prefix
	ASNRecords  int // registros aceitos por tipo
	IPv4Records int
	IPv6Records int
	PrefixesV4  int // blocos depois da divisão em CIDRs
	PrefixesV6  int
	Lines       int      // linhas de registro (sem cabeçalho, summary, vazias e comentários)
	Skipped     int      // linhas de registro descartadas
	Warnings    []string // primeiras MaxWarnings ocorrências
	warnCount   int
}

// Records é o total de registros aceitos.
func (d *Dataset) Records() int { return d.ASNRecords + d.IPv4Records + d.IPv6Records }

// WarningCount é o total de avisos, inclusive os que não couberam em Warnings.
func (d *Dataset) WarningCount() int { return d.warnCount }

// warn registra um aviso; line = 0 para avisos do arquivo como um todo.
func (d *Dataset) warn(line int, format string, args ...any) {
	d.warnCount++
	if len(d.Warnings) >= MaxWarnings {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if line > 0 {
		msg = fmt.Sprintf("linha %d: %s", line, msg)
	}
	d.Warnings = append(d.Warnings, msg)
}

var versionRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)

// Parse lê o arquivo de um RIR cujo campo registry deve ser registry.
//
// Recusa o arquivo inteiro (erro) quando: falta o cabeçalho ou ele é ilegível;
// a versão do formato não é 2.x; o registry do cabeçalho é outro; a contagem
// do cabeçalho ou de uma linha summary não bate com os registros lidos
// (arquivo truncado ou corrompido); mais de 1% dos registros é descartado.
//
// Um registro inválido (campos faltando, registry, país, tipo, início, valor
// ou status desconhecidos) é descartado com aviso. Data inválida vira "sem
// data", com aviso. Registro repetido (mesmo início de ASN, mesmo bloco) fica
// com a primeira ocorrência, com aviso.
func Parse(r io.Reader, registry string) (*Dataset, error) {
	registry = strings.ToLower(registry)
	d := &Dataset{}
	var haveHeader bool
	summaries := map[string]int{}
	typeLines := map[string]int{}
	seenASN := map[int64]struct{}{}
	seenPrefix := map[netip.Prefix]struct{}{}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Bytes()
		if lineNo == 1 {
			raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")) // BOM
		}
		line := strings.TrimSpace(strings.ToValidUTF8(string(raw), "�"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "|")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}

		if !haveHeader {
			h, err := parseHeader(fields)
			if err != nil {
				return nil, fmt.Errorf("linha %d: %w", lineNo, err)
			}
			if h.Registry != registry {
				return nil, fmt.Errorf("linha %d: cabeçalho do registry %q, esperado %q", lineNo, h.Registry, registry)
			}
			for _, w := range h.warnings {
				d.warn(lineNo, "%s", w)
			}
			d.Header, haveHeader = h.Header, true
			continue
		}

		if len(fields) >= 6 && fields[5] == "summary" {
			t := strings.ToLower(fields[2])
			n, err := strconv.Atoi(fields[4])
			if err != nil || n < 0 {
				return nil, fmt.Errorf("linha %d: summary ilegível: %q", lineNo, line)
			}
			if _, dup := summaries[t]; dup {
				d.warn(lineNo, "summary de %s repetido; vale o primeiro", t)
				continue
			}
			summaries[t] = n
			continue
		}

		d.Lines++
		if len(fields) >= 3 {
			typeLines[strings.ToLower(fields[2])]++
		}
		if err := d.record(fields, registry, seenASN, seenPrefix, lineNo); err != nil {
			d.Skipped++
			d.warn(lineNo, "registro descartado: %v", err)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("leitura: %w", err)
	}
	if !haveHeader {
		return nil, fmt.Errorf("arquivo sem cabeçalho nem registros")
	}

	// Contagens: o cabeçalho e as linhas summary têm que bater com o que foi
	// lido. Divergência = arquivo truncado ou corrompido.
	if d.Header.Records != d.Lines {
		return nil, fmt.Errorf("o cabeçalho declara %d registros e o arquivo tem %d: arquivo truncado ou corrompido?",
			d.Header.Records, d.Lines)
	}
	for _, t := range sortedKeys(summaries) {
		if summaries[t] != typeLines[t] {
			return nil, fmt.Errorf("a linha summary declara %d registros %s e o arquivo tem %d: arquivo truncado ou corrompido?",
				summaries[t], t, typeLines[t])
		}
	}
	for _, t := range sortedKeys(typeLines) {
		if _, ok := summaries[t]; !ok {
			d.warn(0, "sem linha summary para %s (%d registros)", t, typeLines[t])
		}
	}
	if d.Lines == 0 {
		return nil, fmt.Errorf("arquivo sem nenhum registro")
	}
	if ratio := float64(d.Skipped) / float64(d.Lines); ratio > MaxSkippedRatio {
		return nil, fmt.Errorf("%d de %d registros descartados (%.1f%%, limite %.1f%%): formato mudou? primeiros avisos: %s",
			d.Skipped, d.Lines, ratio*100, MaxSkippedRatio*100, strings.Join(first(d.Warnings, 3), "; "))
	}
	d.checkASNOverlaps()
	return d, nil
}

type header struct {
	Header
	warnings []string
}

func parseHeader(f []string) (header, error) {
	var h header
	if !versionRe.MatchString(f[0]) {
		return h, fmt.Errorf("cabeçalho ausente ou ilegível (esperado version|registry|serial|records|startdate|enddate|UTCoffset): %q",
			strings.Join(f, "|"))
	}
	if major, _, _ := strings.Cut(f[0], "."); major != "2" {
		return h, fmt.Errorf("versão de formato %q não suportada (esperada 2.x)", f[0])
	}
	if len(f) < 6 {
		return h, fmt.Errorf("cabeçalho com %d campos, esperados 7: %q", len(f), strings.Join(f, "|"))
	}
	h.Version = f[0]
	h.Registry = strings.ToLower(f[1])
	h.Serial = f[2]
	if h.Serial == "" {
		return h, fmt.Errorf("cabeçalho sem serial")
	}
	n, err := strconv.Atoi(f[3])
	if err != nil || n < 0 {
		return h, fmt.Errorf("cabeçalho com contagem de registros inválida: %q", f[3])
	}
	h.Records = n
	var ok bool
	if h.StartDate, ok = parseDate(f[4]); !ok {
		h.warnings = append(h.warnings, fmt.Sprintf("startdate inválida no cabeçalho: %q", f[4]))
	}
	if h.EndDate, ok = parseDate(f[5]); !ok {
		h.warnings = append(h.warnings, fmt.Sprintf("enddate inválida no cabeçalho: %q", f[5]))
	}
	if len(f) > 6 {
		h.UTCOffset = f[6]
	}
	return h, nil
}

// record valida uma linha de registro e a acrescenta ao dataset. Erro =
// linha descartada.
func (d *Dataset) record(f []string, registry string, seenASN map[int64]struct{}, seenPrefix map[netip.Prefix]struct{}, lineNo int) error {
	if len(f) < 7 {
		return fmt.Errorf("esperados ao menos 7 campos, vieram %d", len(f))
	}
	if reg := strings.ToLower(f[0]); reg != registry {
		return fmt.Errorf("registry %q, esperado %q", f[0], registry)
	}
	cc := strings.ToUpper(f[1])
	if cc != "" && !isCountry(cc) {
		return fmt.Errorf("país inválido %q", f[1])
	}
	status := strings.ToLower(f[6])
	if !slices.Contains(Statuses, status) {
		return fmt.Errorf("status desconhecido %q", f[6])
	}
	date, ok := parseDate(f[5])
	if !ok {
		d.warn(lineNo, "data inválida %q; gravada como vazia", f[5])
	}
	var opaque string
	if len(f) > 7 {
		opaque = f[7]
	}

	switch t := strings.ToLower(f[2]); t {
	case "asn":
		start, err := strconv.ParseUint(f[3], 10, 32)
		if err != nil {
			return fmt.Errorf("ASN inicial inválido %q", f[3])
		}
		count, err := strconv.ParseUint(f[4], 10, 64)
		if err != nil || count < 1 || count > math.MaxUint32-start+1 {
			return fmt.Errorf("quantidade de ASNs inválida %q a partir de %d", f[4], start)
		}
		if _, dup := seenASN[int64(start)]; dup {
			d.warn(lineNo, "registro do AS%d repetido; vale a primeira ocorrência", start)
			return nil
		}
		seenASN[int64(start)] = struct{}{}
		d.ASNs = append(d.ASNs, ASN{Start: int64(start), Count: int64(count), CC: cc, Date: date, Status: status, OpaqueID: opaque})
		d.ASNRecords++

	case "ipv4":
		addr, err := netip.ParseAddr(f[3])
		if err != nil || !addr.Is4() {
			return fmt.Errorf("endereço IPv4 inválido %q", f[3])
		}
		count, err := strconv.ParseUint(f[4], 10, 64)
		if err != nil {
			return fmt.Errorf("quantidade de endereços inválida %q", f[4])
		}
		pfxs, err := SplitIPv4(addr, count)
		if err != nil {
			return err
		}
		added := 0
		for _, p := range pfxs {
			if _, dup := seenPrefix[p]; dup {
				d.warn(lineNo, "bloco %s repetido; vale a primeira ocorrência", p)
				continue
			}
			seenPrefix[p] = struct{}{}
			d.Prefixes = append(d.Prefixes, Prefix{Prefix: p, CC: cc, Date: date, Status: status, OpaqueID: opaque,
				RecordStart: addr, RecordValue: int64(count)})
			added++
		}
		if added > 0 {
			d.IPv4Records++
			d.PrefixesV4 += added
		}

	case "ipv6":
		addr, err := netip.ParseAddr(f[3])
		if err != nil || !addr.Is6() || addr.Is4In6() || addr.Zone() != "" {
			return fmt.Errorf("endereço IPv6 inválido %q", f[3])
		}
		length, err := strconv.Atoi(f[4])
		if err != nil || length < 1 || length > 128 {
			return fmt.Errorf("tamanho de prefixo IPv6 inválido %q", f[4])
		}
		p := netip.PrefixFrom(addr, length)
		if m := p.Masked(); m != p {
			d.warn(lineNo, "bloco %s com bits de host; usando %s", p, m)
			p = m
		}
		if _, dup := seenPrefix[p]; dup {
			d.warn(lineNo, "bloco %s repetido; vale a primeira ocorrência", p)
			return nil
		}
		seenPrefix[p] = struct{}{}
		d.Prefixes = append(d.Prefixes, Prefix{Prefix: p, CC: cc, Date: date, Status: status, OpaqueID: opaque,
			RecordStart: addr, RecordValue: int64(length)})
		d.IPv6Records++
		d.PrefixesV6++

	default:
		return fmt.Errorf("tipo desconhecido %q", f[2])
	}
	return nil
}

// SplitIPv4 divide a faixa de count endereços a partir de start nos CIDRs
// mínimos que a cobrem exatamente, em ordem. Ex.: 62.122.208.0 + 1280 →
// 62.122.208.0/22 e 62.122.212.0/24.
func SplitIPv4(start netip.Addr, count uint64) ([]netip.Prefix, error) {
	if !start.Is4() {
		return nil, fmt.Errorf("endereço IPv4 inválido %s", start)
	}
	b := start.As4()
	cur := uint64(b[0])<<24 | uint64(b[1])<<16 | uint64(b[2])<<8 | uint64(b[3])
	if count < 1 || cur+count > 1<<32 {
		return nil, fmt.Errorf("quantidade de endereços inválida %d a partir de %s", count, start)
	}
	var out []netip.Prefix
	for count > 0 {
		// Maior bloco alinhado em cur (bits zerados à direita) que cabe no resto.
		size := uint64(1) << 32
		if cur != 0 {
			size = cur & -cur
		}
		for size > count {
			size >>= 1
		}
		addr := netip.AddrFrom4([4]byte{byte(cur >> 24), byte(cur >> 16), byte(cur >> 8), byte(cur)})
		out = append(out, netip.PrefixFrom(addr, 32-bits.TrailingZeros64(size)))
		cur += size
		count -= size
	}
	return out, nil
}

// checkASNOverlaps avisa se duas faixas de ASN se sobrepõem: a api-ripencc acha
// a faixa de um ASN pelo maior início <= ASN, o que supõe faixas disjuntas
// (nunca vistas sobrepostas nos arquivos dos cinco RIRs).
func (d *Dataset) checkASNOverlaps() {
	r := slices.Clone(d.ASNs)
	slices.SortFunc(r, func(a, b ASN) int { return cmp.Compare(a.Start, b.Start) })
	for i := 1; i < len(r); i++ {
		if r[i].Start <= r[i-1].End() {
			d.warn(0, "faixa AS%d-AS%d sobrepõe AS%d-AS%d", r[i].Start, r[i].End(), r[i-1].Start, r[i-1].End())
		}
	}
}

// parseDate lê AAAAMMDD. Vazia ou 00000000 = sem data (zero, ok). Data
// inválida devolve ok = false (e zero).
func parseDate(s string) (time.Time, bool) {
	if s == "" || s == "00000000" {
		return time.Time{}, true
	}
	if len(s) != 8 {
		return time.Time{}, false
	}
	t, err := time.Parse("20060102", s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func isCountry(cc string) bool {
	return len(cc) == 2 && cc[0] >= 'A' && cc[0] <= 'Z' && cc[1] >= 'A' && cc[1] <= 'Z'
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func first(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
