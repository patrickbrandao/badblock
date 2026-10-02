// Package parse interpreta o CSV de prestadoras de serviços de
// telecomunicações da Anatel (já extraído do ZIP).
//
// Formato: UTF-8 com BOM, CRLF, separador ";", aspas RFC 4180, cabeçalho com
// 24 colunas e uma linha por serviço notificado. Só as linhas de pessoa
// jurídica (CNPJ) são guardadas; as de pessoa física (CPF, mascarado pela
// Anatel) são contadas e ignoradas. Regras completas em
// specs/fontes/anatel/pst/fonte.md.
package parse

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Header são as 24 colunas do CSV, com os nomes exatos e na ordem.
var Header = [24]string{
	"Tipo de Identificação",
	"CNPJ ou CPF",
	"Nome Entidade Prestadora de Serviço",
	"Nome Fantasia",
	"Tipo de Entidade",
	"Tipo de Outorga",
	"Fistel da Outorga",
	"Processo SEI da Outorga",
	"Data Inclusão da Outorga",
	"Serviço da Notificação",
	"Código e Nome do Serviço da Notificação",
	"Fistel da Notificação",
	"Processo SEI da Notificação",
	"Data Inclusão da Notificação",
	"Logradouro do Endereço Sede",
	"Número do Endereço Sede",
	"Complemento do Endereço Sede",
	"Bairro do Endereço Sede",
	"CEP do Endereço Sede",
	"Código IBGE do Munícipio do Endereço Sede",
	"Nome do Munícipio do Endereço Sede",
	"UF do Endereço Sede",
	"Telefone Principal",
	"Endereço Eletrônico",
}

// Posição (a partir de 0) de cada coluna.
const (
	colType = iota
	colDocument
	colName
	colTradeName
	colEntityType
	colGrantType
	colGrantFistel
	colGrantProcess
	colGrantedOn
	colServiceGroup
	colService
	colNotificationFistel
	colNotificationProcess
	colNotifiedOn
	colStreet
	colNumber
	colComplement
	colDistrict
	colPostalCode
	colCityIBGE
	colCity
	colState
	colPhone
	colEmail
	numCols
)

// Provider é uma prestadora (CNPJ), com os dados da primeira linha dela.
// Texto vazio é NULL no banco; CityIBGECode zero também.
type Provider struct {
	Document     string
	Name         string
	TradeName    string
	Street       string
	Number       string
	Complement   string
	District     string
	PostalCode   string
	CityIBGECode int
	City         string
	State        string
	Phone        string
	Email        string
	Services     []Service
}

// Service é um serviço notificado por uma prestadora. Texto vazio e data zero
// são NULL no banco.
type Service struct {
	EntityType          string
	GrantType           string
	GrantFistel         string
	GrantProcess        string
	GrantedOn           time.Time
	ServiceGroup        string
	ServiceCode         string
	ServiceName         string
	NotificationFistel  string
	NotificationProcess string
	NotifiedOn          time.Time
}

// Dataset é o CSV inteiro já validado.
type Dataset struct {
	Providers  []Provider // na ordem do arquivo
	Rows       int        // linhas de dados (sem o cabeçalho)
	RowsCNPJ   int        // linhas que não são de CPF (inclusive cópias e descartadas)
	RowsCPF    int        // linhas de pessoa física, ignoradas
	Duplicates int        // linhas de CNPJ que são cópia exata de uma anterior
	Skipped    int        // linhas de CNPJ descartadas
	Warnings   []string   // primeiras MaxWarnings ocorrências
	warnCount  int
	services   int
}

// MaxWarnings limita os avisos guardados (o total fica em WarningCount).
const MaxWarnings = 50

// MaxSkippedRatio é a fração de linhas de CNPJ descartadas acima da qual o
// arquivo inteiro é recusado: um formato novo ou um arquivo corrompido não
// pode virar uma remoção em massa.
const MaxSkippedRatio = 0.01

// WarningCount é o total de avisos, inclusive os que não couberam em Warnings.
func (d *Dataset) WarningCount() int { return d.warnCount }

// Services é o total de serviços aceitos.
func (d *Dataset) Services() int { return d.services }

func (d *Dataset) warn(line int, format string, args ...any) {
	d.warnCount++
	if len(d.Warnings) < MaxWarnings {
		d.Warnings = append(d.Warnings, fmt.Sprintf("linha %d: ", line)+fmt.Sprintf(format, args...))
	}
}

func (d *Dataset) skip(line int, format string, args ...any) {
	d.Skipped++
	d.warn(line, "linha descartada: "+format, args...)
}

var (
	reCNPJ    = regexp.MustCompile(`^[0-9]{14}$`)
	reFistel  = regexp.MustCompile(`^[0-9]{11}$`)
	reService = regexp.MustCompile(`^([0-9]{3}) - (.+)$`)
	reDate    = regexp.MustCompile(`^[0-9]{2}/[0-9]{2}/[0-9]{4}$`)
	reIBGE    = regexp.MustCompile(`^[1-9][0-9]{6}$`)
	reState   = regexp.MustCompile(`^[A-Z]{2}$`)
)

// providerCols são as colunas com os dados da prestadora (iguais em todas as
// linhas do mesmo CNPJ).
var providerCols = []int{colName, colTradeName, colStreet, colNumber, colComplement, colDistrict,
	colPostalCode, colCityIBGE, colCity, colState, colPhone, colEmail}

type serviceKey struct {
	document, grantFistel, notificationFistel, code string
}

type providerState struct {
	index  int
	raw    string // colunas da prestadora da primeira linha, já normalizadas
	warned bool
}

// Parse lê o CSV. Erro de sintaxe do CSV ou cabeçalho diferente recusa o
// arquivo; uma linha inválida é descartada (com aviso), até MaxSkippedRatio
// das linhas de CNPJ.
func Parse(r io.Reader) (*Dataset, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	if bom, _ := br.Peek(3); bytes.Equal(bom, []byte("\xef\xbb\xbf")) {
		_, _ = br.Discard(3)
	}
	cr := csv.NewReader(br)
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = false
	cr.ReuseRecord = true

	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("arquivo vazio")
	}
	if err != nil {
		return nil, csvError(err)
	}
	if len(header) != numCols {
		return nil, fmt.Errorf("cabeçalho com %d colunas (esperadas %d)", len(header), numCols)
	}
	for i, h := range header {
		if h = strings.TrimSpace(h); h != Header[i] {
			return nil, fmt.Errorf("cabeçalho inesperado na coluna %d: %q (esperado %q)", i+1, h, Header[i])
		}
	}

	d := &Dataset{}
	seenLine := map[string]struct{}{}
	providers := map[string]*providerState{}
	services := map[serviceKey]Service{}
	fields := make([]string, numCols)

	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, csvError(err)
		}
		line, _ := cr.FieldPos(0)
		d.Rows++
		if strings.TrimSpace(rec[0]) == "CPF" {
			d.RowsCPF++
			continue
		}
		d.RowsCNPJ++
		if len(rec) != numCols {
			d.skip(line, "esperados %d campos, vieram %d", numCols, len(rec))
			continue
		}
		for i, v := range rec {
			fields[i] = strings.TrimSpace(strings.ToValidUTF8(v, "�"))
		}
		whole := strings.Join(fields, "\x00")
		if _, dup := seenLine[whole]; dup {
			d.Duplicates++
			continue
		}
		seenLine[whole] = struct{}{}

		svc, ok := d.service(line, fields)
		if !ok {
			continue
		}
		doc := fields[colDocument]

		ps, known := providers[doc]
		raw := providerRaw(fields)
		if !known {
			ps = &providerState{index: len(d.Providers), raw: raw}
			providers[doc] = ps
			d.Providers = append(d.Providers, d.provider(line, fields))
		} else if raw != ps.raw && !ps.warned {
			ps.warned = true
			d.warn(line, "CNPJ %s com dados da prestadora diferentes da primeira linha; vale a primeira", doc)
		}

		key := serviceKey{doc, svc.GrantFistel, svc.NotificationFistel, svc.ServiceCode}
		if first, dup := services[key]; dup {
			if first != svc {
				d.warn(line, "serviço repetido (CNPJ %s, Fistel %s, código %s); vale a primeira ocorrência",
					doc, svc.NotificationFistel, svc.ServiceCode)
			}
			continue
		}
		services[key] = svc
		p := &d.Providers[ps.index]
		p.Services = append(p.Services, svc)
		d.services++
	}

	if d.Rows == 0 {
		return nil, fmt.Errorf("arquivo sem nenhuma linha de dados")
	}
	if d.RowsCNPJ > 0 {
		if ratio := float64(d.Skipped) / float64(d.RowsCNPJ); ratio > MaxSkippedRatio {
			return nil, fmt.Errorf("%d de %d linhas de CNPJ descartadas (%.1f%%, limite %.1f%%): formato mudou? primeiros avisos: %s",
				d.Skipped, d.RowsCNPJ, ratio*100, MaxSkippedRatio*100, strings.Join(first(d.Warnings, 3), "; "))
		}
	}
	return d, nil
}

// service valida a linha e devolve o serviço dela; false = linha descartada.
func (d *Dataset) service(line int, f []string) (Service, bool) {
	if f[colType] != "CNPJ" {
		d.skip(line, "tipo de identificação desconhecido: %q", f[colType])
		return Service{}, false
	}
	doc := f[colDocument]
	if !reCNPJ.MatchString(doc) {
		d.skip(line, "CNPJ inválido: %q", doc)
		return Service{}, false
	}
	m := reService.FindStringSubmatch(f[colService])
	if m == nil || strings.TrimSpace(m[2]) == "" {
		d.skip(line, "serviço inválido: %q", f[colService])
		return Service{}, false
	}
	if !reFistel.MatchString(f[colNotificationFistel]) {
		d.skip(line, "Fistel da notificação inválido: %q", f[colNotificationFistel])
		return Service{}, false
	}
	grantFistel := value(f[colGrantFistel])
	if grantFistel != "" && !reFistel.MatchString(grantFistel) {
		d.skip(line, "Fistel da outorga inválido: %q", grantFistel)
		return Service{}, false
	}
	var dates [2]time.Time
	for i, col := range []int{colGrantedOn, colNotifiedOn} {
		v := value(f[col])
		if v == "" {
			continue
		}
		t, err := time.Parse("02/01/2006", v)
		if err != nil || !reDate.MatchString(v) {
			d.skip(line, "data inválida em %q: %q", Header[col], v)
			return Service{}, false
		}
		dates[i] = t
	}
	if value(f[colName]) == "" {
		d.skip(line, "CNPJ %s sem nome", doc)
		return Service{}, false
	}
	for _, col := range []int{colEntityType, colGrantType, colServiceGroup} {
		if value(f[col]) == "" {
			d.skip(line, "CNPJ %s sem %q", doc, Header[col])
			return Service{}, false
		}
	}
	return Service{
		EntityType:          f[colEntityType],
		GrantType:           f[colGrantType],
		GrantFistel:         grantFistel,
		GrantProcess:        value(f[colGrantProcess]),
		GrantedOn:           dates[0],
		ServiceGroup:        f[colServiceGroup],
		ServiceCode:         m[1],
		ServiceName:         strings.TrimSpace(m[2]),
		NotificationFistel:  f[colNotificationFistel],
		NotificationProcess: value(f[colNotificationProcess]),
		NotifiedOn:          dates[1],
	}, true
}

// provider monta a prestadora a partir da primeira linha do CNPJ.
func (d *Dataset) provider(line int, f []string) Provider {
	p := Provider{
		Document:   f[colDocument],
		Name:       value(f[colName]),
		TradeName:  value(f[colTradeName]),
		Street:     value(f[colStreet]),
		Number:     value(f[colNumber]),
		Complement: value(f[colComplement]),
		District:   value(f[colDistrict]),
		PostalCode: value(f[colPostalCode]),
		City:       value(f[colCity]),
		Phone:      value(f[colPhone]),
		Email:      value(f[colEmail]),
	}
	if v := value(f[colCityIBGE]); v != "" {
		if reIBGE.MatchString(v) {
			p.CityIBGECode, _ = strconv.Atoi(v)
		} else {
			d.warn(line, "código IBGE inválido: %q", v)
		}
	}
	if v := value(f[colState]); v != "" {
		if reState.MatchString(v) {
			p.State = v
		} else {
			d.warn(line, "UF inválida: %q", v)
		}
	}
	return p
}

func providerRaw(f []string) string {
	parts := make([]string, len(providerCols))
	for i, c := range providerCols {
		parts[i] = value(f[c])
	}
	return strings.Join(parts, "\x00")
}

// value normaliza um campo já sem espaços nas pontas: os marcadores de valor
// ausente viram texto vazio (NULL no banco).
func value(s string) string {
	switch s {
	case "N/I", "N/A", "-":
		return ""
	}
	return s
}

func csvError(err error) error {
	if pe, ok := errors.AsType[*csv.ParseError](err); ok {
		return fmt.Errorf("csv: linha %d: %w", pe.Line, pe.Err)
	}
	return fmt.Errorf("csv: %w", err)
}

func first(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
