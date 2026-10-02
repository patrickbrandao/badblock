// Package parse interpreta o arquivo named.root da InterNIC (root hints).
//
// O arquivo está no formato de zona do BIND: um cabeçalho em comentários, com
// a data da última atualização e o serial da zona raiz relacionada, e um
// bloco por servidor raiz:
//
//	;       last update:     September 24, 2026
//	;       related version of root zone:     2026092401
//	;
//	; FORMERLY NS.INTERNIC.NET
//	;
//	.                        3600000      NS    A.ROOT-SERVERS.NET.
//	A.ROOT-SERVERS.NET.      3600000      A     198.41.0.4
//	A.ROOT-SERVERS.NET.      3600000      AAAA  2001:503:ba3e::2:30
//	...
//	; End of file
//
// São só 13 servidores: qualquer linha que não bata recusa o arquivo inteiro
// (não há linha descartada). As regras e as medições que as justificam estão
// em specs/fontes/roothints/fonte.md.
package parse

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Server é um servidor raiz: a linha ". NS" e os registros A e AAAA dele.
type Server struct {
	Name    string     // minúsculo, sem o ponto final: a.root-servers.net
	Letter  string     // primeiro rótulo do nome: a
	NSTTL   int64      // TTL da linha ". NS"
	IPv4    netip.Addr // inválido (zero) = sem registro A
	IPv4TTL int64
	IPv6    netip.Addr // inválido (zero) = sem registro AAAA
	IPv6TTL int64
	Note    string // comentário do bloco, sem o ";" ("" = sem comentário)
}

// Header é o cabeçalho do arquivo.
type Header struct {
	LastUpdate time.Time // data de "last update:", 00:00 UTC
	ZoneSerial int64     // "related version of root zone:"
}

// Dataset é o arquivo inteiro já validado.
type Dataset struct {
	Header    Header
	Servers   []Server // na ordem do arquivo
	Warnings  []string // primeiras MaxWarnings ocorrências
	warnCount int
}

// MaxWarnings limita os avisos guardados (o total fica em WarningCount).
const MaxWarnings = 50

// WarningCount é o total de avisos, inclusive os que não couberam em Warnings.
func (d *Dataset) WarningCount() int { return d.warnCount }

// IPv4Count e IPv6Count contam os servidores com registro A e AAAA.
func (d *Dataset) IPv4Count() int { return d.count(func(s Server) bool { return s.IPv4.IsValid() }) }

// IPv6Count conta os servidores com registro AAAA.
func (d *Dataset) IPv6Count() int { return d.count(func(s Server) bool { return s.IPv6.IsValid() }) }

func (d *Dataset) count(ok func(Server) bool) int {
	n := 0
	for _, s := range d.Servers {
		if ok(s) {
			n++
		}
	}
	return n
}

func (d *Dataset) warn(format string, args ...any) {
	d.warnCount++
	if len(d.Warnings) < MaxWarnings {
		d.Warnings = append(d.Warnings, fmt.Sprintf(format, args...))
	}
}

var (
	lastUpdateRe = regexp.MustCompile(`(?i)^last update:\s*(.*)$`)
	serialRe     = regexp.MustCompile(`(?i)^related version of root zone:\s*(.*)$`)
	serverNameRe = regexp.MustCompile(`^[a-z]\.root-servers\.net$`)
)

// DateLayout é o formato da data de "last update:" (September 24, 2026).
const DateLayout = "January 2, 2006"

// Parse lê o arquivo inteiro e devolve os servidores, ou o motivo da recusa.
func Parse(r io.Reader) (*Dataset, error) {
	d := &Dataset{}
	p := parser{d: d, byName: map[string]int{}, byAddr: map[netip.Addr]string{}}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Bytes()
		if lineNo == 1 {
			raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")) // BOM
		}
		if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
			return nil, fmt.Errorf("linha %d: UTF-8 inválido ou NUL", lineNo)
		}
		if err := p.line(strings.TrimSpace(string(raw))); err != nil {
			return nil, fmt.Errorf("linha %d: %w", lineNo, err)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("leitura: %w", err)
	}
	if err := p.header(); err != nil {
		return nil, err
	}
	if len(d.Servers) == 0 {
		return nil, errors.New(`arquivo sem nenhum servidor (linha ". NS")`)
	}
	if !p.endOfFile {
		return nil, errors.New(`arquivo sem a linha "; End of file" no fim: arquivo truncado?`)
	}
	for _, s := range d.Servers {
		switch {
		case !s.IPv4.IsValid() && !s.IPv6.IsValid():
			return nil, fmt.Errorf("servidor %s sem endereço (nem A nem AAAA)", s.Name)
		case !s.IPv4.IsValid():
			d.warn("servidor %s sem IPv4 (registro A)", s.Name)
		case !s.IPv6.IsValid():
			d.warn("servidor %s sem IPv6 (registro AAAA)", s.Name)
		}
	}
	return d, nil
}

type parser struct {
	d          *Dataset
	byName     map[string]int        // nome → índice em d.Servers
	byAddr     map[netip.Addr]string // endereço → servidor
	pending    []string              // comentários desde a última linha de dados
	seenData   bool
	endOfFile  bool // a última linha não vazia é "; End of file"
	lastUpdate string
	serial     string
}

func (p *parser) line(line string) error {
	if line == "" {
		return nil
	}
	if text, ok := strings.CutPrefix(line, ";"); ok {
		p.comment(strings.TrimSpace(text))
		return nil
	}
	p.endOfFile = false
	if !p.seenData {
		p.seenData = true
		if err := p.header(); err != nil {
			return err
		}
	}
	if i := strings.IndexByte(line, ';'); i >= 0 { // comentário no fim da linha
		line = line[:i]
	}
	f := strings.Fields(line)
	if len(f) == 5 && strings.EqualFold(f[2], "IN") {
		f = append(f[:2], f[3:]...)
	}
	if len(f) != 4 {
		return fmt.Errorf("esperava <nome> <ttl> [IN] <tipo> <dado>: %q", truncate(line, 80))
	}
	owner, typ, data := f[0], strings.ToUpper(f[2]), f[3]
	ttl, err := strconv.ParseUint(f[1], 10, 31) // RFC 2181: 0 a 2^31-1
	if err != nil {
		return fmt.Errorf("TTL inválido %q", truncate(f[1], 40))
	}
	if !strings.HasSuffix(owner, ".") {
		return fmt.Errorf("nome sem o ponto final %q", truncate(owner, 80))
	}
	defer func() { p.pending = nil }()

	switch typ {
	case "NS":
		return p.ns(owner, data, int64(ttl))
	case "A", "AAAA":
		return p.address(owner, typ, data, int64(ttl))
	default:
		return fmt.Errorf("tipo %q não esperado (só NS, A e AAAA)", truncate(f[2], 20))
	}
}

func (p *parser) comment(text string) {
	p.endOfFile = strings.EqualFold(text, "End of file")
	if !p.seenData {
		// Cabeçalho: tudo até a última linha "last update:" ou "related
		// version of root zone:"; o que vem depois é o comentário do bloco
		// do primeiro servidor.
		if m := lastUpdateRe.FindStringSubmatch(text); m != nil {
			p.lastUpdate, p.pending = strings.TrimSpace(m[1]), nil
			return
		}
		if m := serialRe.FindStringSubmatch(text); m != nil {
			p.serial, p.pending = strings.TrimSpace(m[1]), nil
			return
		}
	}
	p.pending = append(p.pending, text)
}

// header valida as duas linhas do cabeçalho; roda na primeira linha de dados
// e no fim do arquivo.
func (p *parser) header() error {
	if p.lastUpdate == "" {
		return errors.New(`cabeçalho sem a linha "last update:"`)
	}
	if p.serial == "" {
		return errors.New(`cabeçalho sem a linha "related version of root zone:"`)
	}
	t, err := time.Parse(DateLayout, p.lastUpdate)
	if err != nil {
		return fmt.Errorf(`cabeçalho: data de "last update:" inválida: %q`, truncate(p.lastUpdate, 40))
	}
	n, err := strconv.ParseUint(p.serial, 10, 32)
	if err != nil {
		return fmt.Errorf(`cabeçalho: serial de "related version of root zone:" inválido: %q`, truncate(p.serial, 40))
	}
	p.d.Header = Header{LastUpdate: t, ZoneSerial: int64(n)}
	return nil
}

func (p *parser) ns(owner, target string, ttl int64) error {
	if owner != "." {
		return fmt.Errorf(`NS de %q: só a raiz "." pode ter NS no named.root`, truncate(owner, 80))
	}
	if !strings.HasSuffix(target, ".") {
		return fmt.Errorf("nome sem o ponto final %q", truncate(target, 80))
	}
	name := normalize(target)
	if !serverNameRe.MatchString(name) {
		return fmt.Errorf("servidor %q fora do padrão <letra>.root-servers.net", truncate(name, 80))
	}
	if _, dup := p.byName[name]; dup {
		return fmt.Errorf("servidor %s repetido", name)
	}
	var note []string
	for _, c := range p.pending {
		if c != "" {
			note = append(note, c)
		}
	}
	p.byName[name] = len(p.d.Servers)
	p.d.Servers = append(p.d.Servers, Server{
		Name: name, Letter: name[:1], NSTTL: ttl, Note: strings.Join(note, " "),
	})
	return nil
}

func (p *parser) address(owner, typ, data string, ttl int64) error {
	name := normalize(owner)
	i, ok := p.byName[name]
	if !ok {
		return fmt.Errorf("registro %s de %s sem a linha NS dele antes", typ, truncate(name, 80))
	}
	s := &p.d.Servers[i]
	addr, err := netip.ParseAddr(data)
	family := "IPv4"
	if typ == "AAAA" {
		family = "IPv6"
	}
	switch {
	case err != nil,
		typ == "A" && !addr.Is4(),
		typ == "AAAA" && (!addr.Is6() || addr.Is4In6() || addr.Zone() != ""):
		return fmt.Errorf("endereço %s inválido em %s: %q", family, name, truncate(data, 60))
	case !addr.IsGlobalUnicast() || addr.IsPrivate():
		return fmt.Errorf("endereço %s de %s não é unicast global", addr, name)
	}
	if other, dup := p.byAddr[addr]; dup {
		return fmt.Errorf("endereço %s repetido (já é de %s)", addr, other)
	}
	if typ == "A" {
		if s.IPv4.IsValid() {
			return fmt.Errorf("segundo registro A de %s", name)
		}
		s.IPv4, s.IPv4TTL = addr, ttl
	} else {
		if s.IPv6.IsValid() {
			return fmt.Errorf("segundo registro AAAA de %s", name)
		}
		s.IPv6, s.IPv6TTL = addr, ttl
	}
	p.byAddr[addr] = name
	return nil
}

// normalize deixa o nome em minúsculas e sem o ponto final.
func normalize(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, "."))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}
