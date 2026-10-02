// Package parse lê o arquivo root.zone da InterNIC (a zona raiz do DNS em
// formato de arquivo mestre, RFC 1035) e monta o dataset que o coletor aplica.
//
// O arquivo publicado é regular: uma RR por linha, no formato
// "<dono> <ttl> IN <tipo> <rdata>", sem $ORIGIN/$TTL, sem comentários e sem
// parênteses. O parser aceita exatamente esse formato (campos separados por
// qualquer espaço em branco) e descarta, com aviso, a linha que foge dele;
// mais de 1% de linhas descartadas recusa o arquivo inteiro.
//
// Normalização (specs/fontes/rootzone/fonte.md): nomes em minúsculas e sem o
// ponto final (a raiz é "."), endereços na forma canônica do net/netip
// (RFC 5952 no IPv6), hex de DS e ZONEMD em maiúsculas, base64 do DNSKEY sem
// espaços, o resto do rdata com um espaço entre os campos. RRSIG são só
// contados: não entram em Records.
package parse

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// MaxWarnings é quantos avisos o dataset guarda (o total é contado).
	MaxWarnings = 50
	// MaxSkippedRatio é a fração máxima de linhas descartadas.
	MaxSkippedRatio = 0.01
	// MaxLineBytes é o tamanho máximo de uma linha (a maior do arquivo real
	// tem ~450 bytes: um RRSIG).
	MaxLineBytes = 64 << 10
	// maxTTL é o maior TTL aceito (RFC 2181, seção 8: o bit mais alto é zero).
	maxTTL = 1<<31 - 1
)

// Root é o dono das RRs do ápice da zona.
const Root = "."

// Tipos de RR aceitos. RRSIG é aceito, mas só contado.
var knownTypes = map[string]bool{
	"SOA": true, "NS": true, "A": true, "AAAA": true, "DS": true,
	"DNSKEY": true, "NSEC": true, "ZONEMD": true, "RRSIG": true,
}

// SOA é o registro SOA da raiz.
type SOA struct {
	TTL     uint32
	MName   string
	RName   string
	Serial  uint32
	Refresh uint32
	Retry   uint32
	Expire  uint32
	Minimum uint32
}

// Record é uma RR guardada (todas menos RRSIG), já normalizada.
type Record struct {
	Owner string // minúsculas, sem o ponto final; a raiz é "."
	Type  string // mnemônico em maiúsculas (NS, A, AAAA, DS...)
	TTL   uint32
	RData string // normalizado (ver o pacote)
}

// TLD é um domínio de topo delegado (dono de NS que não é a raiz).
type TLD struct {
	Name            string // ex.: "br", "xn--p1ai"
	Unicode         string // Name decodificado de punycode (igual a Name nos ASCII)
	Nameservers     int    // NS da delegação
	NameserversIPv4 int    // servidores de nome da delegação com glue A na zona
	NameserversIPv6 int    // idem, com glue AAAA
	DSRecords       int    // DS da delegação (0 = TLD sem DNSSEC)
}

// Dataset é o arquivo interpretado.
type Dataset struct {
	SOA        SOA
	Records    []Record       // na ordem do arquivo, sem RRSIG
	TLDs       []TLD          // em ordem de nome
	TypeCounts map[string]int // linhas aceitas por tipo, inclusive RRSIG
	RRSIGs     int            // RRSIG aceitos (não guardados)
	Lines      int            // linhas com conteúdo (sem vazias e comentários)
	Skipped    int            // linhas descartadas
	Warnings   []string       // os primeiros MaxWarnings avisos

	warningCount int
}

// WarningCount é o total de avisos, inclusive os que não foram guardados.
func (d *Dataset) WarningCount() int { return d.warningCount }

// Warn acrescenta um aviso (guardado se ainda couber em MaxWarnings).
func (d *Dataset) Warn(format string, args ...any) {
	d.warningCount++
	if len(d.Warnings) < MaxWarnings {
		d.Warnings = append(d.Warnings, fmt.Sprintf(format, args...))
	}
}

func (d *Dataset) skip(n int, format string, args ...any) {
	d.Skipped++
	d.Warn("linha %d: linha descartada: %s", n, fmt.Sprintf(format, args...))
}

// Parse lê o arquivo inteiro. Devolve erro (arquivo recusado) quando a leitura
// falha, a última linha não termina em LF (arquivo cortado), não há SOA da
// raiz ou há mais de um, não há NS da raiz, não há nenhuma linha de dados ou
// mais de MaxSkippedRatio das linhas foram descartadas.
func Parse(r io.Reader) (*Dataset, error) {
	d := &Dataset{TypeCounts: map[string]int{}}
	seen := map[string]bool{}
	soaSeen := false
	br := bufio.NewReaderSize(r, 64<<10)

	for n := 1; ; n++ {
		raw, err := readLine(br)
		if errors.Is(err, io.EOF) && raw == "" {
			break
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("leitura: %w", err)
		}
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("linha %d sem fim de linha no fim do arquivo: arquivo cortado?", n)
		}
		if len(raw) > MaxLineBytes {
			return nil, fmt.Errorf("linha %d com mais de %d bytes", n, MaxLineBytes)
		}
		if n == 1 {
			raw = strings.TrimPrefix(raw, "\uFEFF")
		}
		line := strings.TrimRight(raw, "\r\n")
		if i := strings.IndexByte(line, ';'); i >= 0 {
			line = line[:i] // comentário
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		d.Lines++

		rec, isSOA, soa, reason := parseLine(line)
		if reason != "" {
			d.skip(n, "%s", reason)
			continue
		}
		if rec.Type == "RRSIG" {
			d.TypeCounts[rec.Type]++
			d.RRSIGs++
			continue
		}
		key := rec.Owner + "\x00" + rec.Type + "\x00" + rec.RData
		if seen[key] {
			d.skip(n, "%s %s %s repetido; vale a primeira ocorrência", rec.Owner, rec.Type, cut(rec.RData, 60))
			continue
		}
		if isSOA {
			if soaSeen {
				return nil, fmt.Errorf("linha %d: mais de um SOA no arquivo", n)
			}
			soaSeen, d.SOA = true, soa
		}
		seen[key] = true
		d.TypeCounts[rec.Type]++
		d.Records = append(d.Records, rec)
	}

	if d.Lines == 0 {
		return nil, errors.New("arquivo sem nenhum registro")
	}
	if float64(d.Skipped) > MaxSkippedRatio*float64(d.Lines) {
		return nil, fmt.Errorf("%d de %d linhas descartadas (%.1f%%, limite %.1f%%): formato mudou? primeiros avisos: %s",
			d.Skipped, d.Lines, 100*float64(d.Skipped)/float64(d.Lines), 100*MaxSkippedRatio,
			strings.Join(d.Warnings[:min(3, len(d.Warnings))], "; "))
	}
	if !soaSeen {
		return nil, errors.New("arquivo sem o SOA da raiz")
	}
	d.buildTLDs()
	if d.rootNS() == 0 {
		return nil, errors.New("arquivo sem os NS da raiz")
	}
	return d, nil
}

// readLine lê até o LF (inclusive). Uma linha acima do limite é lida até o
// fim só para ser recusada pelo chamador.
func readLine(br *bufio.Reader) (string, error) {
	var sb strings.Builder
	for {
		chunk, err := br.ReadSlice('\n')
		if sb.Len() <= MaxLineBytes {
			sb.Write(chunk)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return sb.String(), err
	}
}

// parseLine interpreta uma linha de dados. reason != "" = linha descartada.
func parseLine(line string) (rec Record, isSOA bool, soa SOA, reason string) {
	if !utf8.ValidString(line) {
		return rec, false, soa, "texto que não é UTF-8"
	}
	if c := line[0]; c == ' ' || c == '\t' {
		return rec, false, soa, "linha sem dono (começa com espaço; dono herdado não é aceito)"
	}
	if line[0] == '$' {
		return rec, false, soa, fmt.Sprintf("diretiva não suportada %q", cut(line, 40))
	}
	if strings.ContainsAny(line, "()") {
		return rec, false, soa, "parênteses (registro em várias linhas) não são aceitos"
	}
	f := strings.Fields(line)
	if len(f) < 5 {
		return rec, false, soa, fmt.Sprintf("%d campos, esperado <dono> <ttl> IN <tipo> <rdata>: %q", len(f), cut(line, 60))
	}
	owner, err := Name(f[0])
	if err != nil {
		return rec, false, soa, fmt.Sprintf("dono %s", err)
	}
	ttl, err := strconv.ParseUint(f[1], 10, 32)
	if err != nil || ttl > maxTTL {
		return rec, false, soa, fmt.Sprintf("TTL inválido %q", cut(f[1], 20))
	}
	if !strings.EqualFold(f[2], "IN") {
		return rec, false, soa, fmt.Sprintf("classe %q (só IN é aceita)", cut(f[2], 20))
	}
	typ := strings.ToUpper(f[3])
	if !knownTypes[typ] {
		return rec, false, soa, fmt.Sprintf("tipo %q desconhecido", cut(f[3], 20))
	}
	rec = Record{Owner: owner, Type: typ, TTL: uint32(ttl)}
	rd := f[4:]
	switch typ {
	case "SOA":
		if owner != Root {
			return rec, false, soa, fmt.Sprintf("SOA fora da raiz (%s)", owner)
		}
		soa, err = parseSOA(rd)
		soa.TTL = uint32(ttl)
		if err == nil {
			rec.RData = fmt.Sprintf("%s %s %d %d %d %d %d", soa.MName, soa.RName,
				soa.Serial, soa.Refresh, soa.Retry, soa.Expire, soa.Minimum)
			isSOA = true
		}
	case "NS":
		if strings.Contains(owner, ".") && owner != Root {
			return rec, false, soa, fmt.Sprintf("NS de %s, que não é a raiz nem um TLD", owner)
		}
		rec.RData, err = oneName(rd)
	case "A", "AAAA":
		rec.RData, err = address(typ, rd)
	case "DS":
		if owner == Root || strings.Contains(owner, ".") {
			return rec, false, soa, fmt.Sprintf("DS de %s, que não é um TLD", owner)
		}
		rec.RData, err = digestRR(rd, 16, "digest")
	case "ZONEMD":
		rec.RData, err = digestRR(rd, 32, "digest")
	case "DNSKEY":
		rec.RData, err = dnskey(rd)
	case "NSEC":
		rec.RData, err = nsec(rd)
	case "RRSIG":
		err = rrsig(rd)
	}
	if err != nil {
		return rec, false, soa, fmt.Sprintf("%s %s: %s", owner, typ, err)
	}
	return rec, isSOA, soa, ""
}

// Name normaliza um nome absoluto do arquivo: exige o ponto final (o arquivo
// não tem $ORIGIN, então nome relativo é erro), passa para minúsculas e tira o
// ponto. A raiz vira ".". Rótulos de 1 a 63 caracteres [a-z0-9_-], nome de
// até 253.
func Name(s string) (string, error) {
	if s == "." {
		return Root, nil
	}
	if !strings.HasSuffix(s, ".") {
		return "", fmt.Errorf("relativo (sem o ponto final): %q", cut(s, 60))
	}
	s = strings.ToLower(strings.TrimSuffix(s, "."))
	if len(s) > 253 {
		return "", fmt.Errorf("com mais de 253 caracteres: %q", cut(s, 60))
	}
	for label := range strings.SplitSeq(s, ".") {
		if label == "" || len(label) > 63 {
			return "", fmt.Errorf("com rótulo vazio ou acima de 63 caracteres: %q", cut(s, 60))
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' {
				return "", fmt.Errorf("com caractere inválido: %q", cut(s, 60))
			}
		}
	}
	return s, nil
}

func oneName(rd []string) (string, error) {
	if len(rd) != 1 {
		return "", fmt.Errorf("%d campos no rdata, esperado 1", len(rd))
	}
	return Name(rd[0])
}

func address(typ string, rd []string) (string, error) {
	if len(rd) != 1 {
		return "", fmt.Errorf("%d campos no rdata, esperado 1", len(rd))
	}
	a, err := netip.ParseAddr(rd[0])
	if err != nil || a.Zone() != "" || (typ == "A") != a.Is4() {
		return "", fmt.Errorf("endereço inválido %q", cut(rd[0], 60))
	}
	return a.String(), nil
}

func uintField(s, what string, bits int) (uint64, error) {
	v, err := strconv.ParseUint(s, 10, bits)
	if err != nil {
		return 0, fmt.Errorf("%s inválido %q", what, cut(s, 20))
	}
	return v, nil
}

func parseSOA(rd []string) (SOA, error) {
	var s SOA
	if len(rd) != 7 {
		return s, fmt.Errorf("%d campos no rdata, esperado 7", len(rd))
	}
	var err error
	if s.MName, err = Name(rd[0]); err != nil {
		return s, fmt.Errorf("mname %w", err)
	}
	if s.RName, err = Name(rd[1]); err != nil {
		return s, fmt.Errorf("rname %w", err)
	}
	dst := []*uint32{&s.Serial, &s.Refresh, &s.Retry, &s.Expire, &s.Minimum}
	names := []string{"serial", "refresh", "retry", "expire", "minimum"}
	for i, p := range dst {
		v, err := uintField(rd[2+i], names[i], 32)
		if err != nil {
			return s, err
		}
		*p = uint32(v)
	}
	return s, nil
}

// digestRR normaliza DS (key tag, algoritmo, tipo do digest, digest) e ZONEMD
// (serial, esquema, algoritmo, digest): números em decimal e o digest em hex
// maiúsculo, sem espaços (a apresentação permite quebrá-lo).
func digestRR(rd []string, firstBits int, what string) (string, error) {
	if len(rd) < 4 {
		return "", fmt.Errorf("%d campos no rdata, esperado 4 ou mais", len(rd))
	}
	a, err := uintField(rd[0], "primeiro campo", firstBits)
	if err != nil {
		return "", err
	}
	b, err := uintField(rd[1], "segundo campo", 8)
	if err != nil {
		return "", err
	}
	c, err := uintField(rd[2], "terceiro campo", 8)
	if err != nil {
		return "", err
	}
	digest := strings.Join(rd[3:], "")
	if _, err := hex.DecodeString(digest); err != nil || digest == "" {
		return "", fmt.Errorf("%s em hex inválido %q", what, cut(digest, 40))
	}
	return fmt.Sprintf("%d %d %d %s", a, b, c, strings.ToUpper(digest)), nil
}

func dnskey(rd []string) (string, error) {
	if len(rd) < 4 {
		return "", fmt.Errorf("%d campos no rdata, esperado 4 ou mais", len(rd))
	}
	flags, err := uintField(rd[0], "flags", 16)
	if err != nil {
		return "", err
	}
	proto, err := uintField(rd[1], "protocolo", 8)
	if err != nil {
		return "", err
	}
	alg, err := uintField(rd[2], "algoritmo", 8)
	if err != nil {
		return "", err
	}
	key := strings.Join(rd[3:], "")
	if _, err := base64.StdEncoding.DecodeString(key); err != nil {
		return "", fmt.Errorf("chave em base64 inválida %q", cut(key, 40))
	}
	return fmt.Sprintf("%d %d %d %s", flags, proto, alg, key), nil
}

func typeMnemonic(s string) (string, bool) {
	s = strings.ToUpper(s)
	if s == "" || s[0] < 'A' || s[0] > 'Z' {
		return "", false
	}
	for i := 1; i < len(s); i++ {
		if (s[i] < 'A' || s[i] > 'Z') && (s[i] < '0' || s[i] > '9') {
			return "", false
		}
	}
	return s, true
}

func nsec(rd []string) (string, error) {
	next, err := Name(rd[0])
	if err != nil {
		return "", fmt.Errorf("próximo nome %w", err)
	}
	out := []string{next}
	for _, t := range rd[1:] {
		m, ok := typeMnemonic(t)
		if !ok {
			return "", fmt.Errorf("tipo inválido %q no bitmap", cut(t, 20))
		}
		out = append(out, m)
	}
	return strings.Join(out, " "), nil
}

// rrsig só confere o formato (9 campos, o primeiro um tipo): o RRSIG não é
// guardado.
func rrsig(rd []string) error {
	if len(rd) < 9 {
		return fmt.Errorf("%d campos no rdata, esperado 9 ou mais", len(rd))
	}
	if _, ok := typeMnemonic(rd[0]); !ok {
		return fmt.Errorf("tipo coberto inválido %q", cut(rd[0], 20))
	}
	return nil
}

// buildTLDs monta os TLDs a partir dos NS (donos que não são a raiz), com as
// contagens de servidores, glue e DS.
func (d *Dataset) buildTLDs() {
	hasA, hasAAAA := map[string]bool{}, map[string]bool{}
	ns := map[string][]string{}
	ds := map[string]int{}
	for _, r := range d.Records {
		switch r.Type {
		case "A":
			hasA[r.Owner] = true
		case "AAAA":
			hasAAAA[r.Owner] = true
		case "NS":
			if r.Owner != Root {
				ns[r.Owner] = append(ns[r.Owner], r.RData)
			}
		case "DS":
			ds[r.Owner]++
		}
	}
	d.TLDs = make([]TLD, 0, len(ns))
	for name, servers := range ns {
		t := TLD{Name: name, Unicode: name, Nameservers: len(servers), DSRecords: ds[name]}
		for _, s := range servers {
			if hasA[s] {
				t.NameserversIPv4++
			}
			if hasAAAA[s] {
				t.NameserversIPv6++
			}
		}
		if strings.HasPrefix(name, "xn--") {
			u, err := Punycode(name[4:])
			if err != nil {
				d.Warn("TLD %s: punycode inválido (%s); unicode fica igual ao nome", name, err)
			} else {
				t.Unicode = u
			}
		}
		d.TLDs = append(d.TLDs, t)
	}
	sort.Slice(d.TLDs, func(i, j int) bool { return d.TLDs[i].Name < d.TLDs[j].Name })
	orphans := []string{}
	for name := range ds {
		if _, ok := ns[name]; !ok {
			orphans = append(orphans, name)
		}
	}
	sort.Strings(orphans)
	for _, name := range orphans {
		d.Warn("DS de %s sem NS (não é uma delegação)", name)
	}
}

func (d *Dataset) rootNS() int {
	n := 0
	for _, r := range d.Records {
		if r.Owner == Root && r.Type == "NS" {
			n++
		}
	}
	return n
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}
