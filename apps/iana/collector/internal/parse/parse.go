// Package parse interpreta os 10 arquivos da IANA e monta o dataset que o
// collector aplica nas tabelas iana_*.
//
// Sete arquivos são CSV (com cabeçalho, campos com aspas e quebras de linha,
// notas de rodapé como "192.0.0.0/24 [2]" e "False [1]") e três são o JSON do
// bootstrap RDAP (RFC 9224). As regras de cada arquivo estão em
// specs/fontes/iana/fonte.md; a checagem de sanidade do dataset inteiro fica
// em Check.
package parse

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/source"
)

// MaxWarnings limita os avisos guardados (o total fica em WarningCount).
const MaxWarnings = 50

// MaxSkippedRatio é a fração de linhas descartadas, em cada arquivo, acima da
// qual o dataset inteiro é recusado: um formato novo ou um arquivo corrompido
// não pode virar uma remoção em massa. Nos arquivos pequenos (special
// registries, ~25 linhas) isso quer dizer que qualquer linha com a chave
// ilegível recusa o dataset.
const MaxSkippedRatio = 0.01

// ASNBlock é uma linha de as-numbers-1.csv ou as-numbers-2.csv.
type ASNBlock struct {
	Start            int64
	End              int64
	Description      string
	Registry         string // "" = NULL
	WHOIS            string
	RDAPURLs         []string
	Reference        string
	RegistrationDate string
	SourceFile       string
}

// PrefixBlock é uma linha de ipv4-address-space.csv ou
// ipv6-unicast-address-assignments.csv.
type PrefixBlock struct {
	Prefix         netip.Prefix
	Designation    string
	Registry       string
	WHOIS          string
	RDAPURLs       []string
	Status         string
	AllocationDate string
	Note           string
	SourceFile     string
}

// SpecialPrefix é um bloco dos special-purpose registries (uma célula com
// vários blocos vira vários SpecialPrefix com os mesmos atributos).
type SpecialPrefix struct {
	Prefix             netip.Prefix
	Name               string
	RFC                string
	AllocationDate     string
	TerminationDate    string
	Source             *bool // nil = NULL (vazio ou N/A)
	Destination        *bool
	Forwardable        *bool
	GloballyReachable  *bool
	ReservedByProtocol *bool
}

// SpecialASN é uma linha de special-purpose-as-numbers.csv.
type SpecialASN struct {
	Start     int64
	End       int64
	Reason    string
	Reference string
}

// Tipos de entrada RDAP (iana_rdap_service.kind).
const (
	KindASN  = "asn"
	KindIPv4 = "ipv4"
	KindIPv6 = "ipv6"
)

// RDAPService é uma entrada de services[][0] de um JSON de bootstrap RDAP.
type RDAPService struct {
	Kind     string
	Resource string       // forma canônica: "1-1876", "2043" ou o CIDR
	Start    int64        // só para KindASN
	End      int64        // só para KindASN
	Prefix   netip.Prefix // só para KindIPv4/KindIPv6
	Registry string
	URLs     []string
}

// FileStats resume um arquivo.
type FileStats struct {
	Records     int    // linhas (ou entradas RDAP) com dados, sem cabeçalho nem linhas ignoradas
	Rows        int    // registros gerados para as tabelas
	Skipped     int    // linhas descartadas inteiras
	Publication string // só RDAP: campo publication (RFC 3339, UTC)
}

// Dataset são os 10 arquivos já interpretados.
type Dataset struct {
	ASNBlocks       []ASNBlock
	PrefixBlocks    []PrefixBlock
	SpecialPrefixes []SpecialPrefix
	SpecialASNs     []SpecialASN
	RDAPServices    []RDAPService
	Files           map[string]*FileStats
	Warnings        []string // primeiras MaxWarnings ocorrências
	warnCount       int
}

// WarningCount é o total de avisos, inclusive os que não couberam em Warnings.
func (d *Dataset) WarningCount() int { return d.warnCount }

func (d *Dataset) warn(file string, line int, format string, args ...any) {
	d.warnCount++
	if len(d.Warnings) >= MaxWarnings {
		return
	}
	where := file
	if line > 0 {
		where = fmt.Sprintf("%s linha %d", file, line)
	}
	d.Warnings = append(d.Warnings, where+": "+fmt.Sprintf(format, args...))
}

// Parse interpreta os 10 arquivos (nome → conteúdo, nomes de source.Files).
// Devolve erro se faltar arquivo, se um cabeçalho não tiver as colunas
// obrigatórias, se um JSON for inválido ou se mais de MaxSkippedRatio das
// linhas de um arquivo forem descartadas.
func Parse(files map[string][]byte) (*Dataset, error) {
	d := &Dataset{Files: map[string]*FileStats{}}
	seen := &keys{
		asnBlock:      map[int64]string{},
		prefixBlock:   map[netip.Prefix]string{},
		specialPrefix: map[netip.Prefix]string{},
		specialASN:    map[int64]bool{},
		rdap:          map[string]bool{},
	}
	for _, f := range source.Files {
		body, ok := files[f.Name]
		if !ok {
			return nil, fmt.Errorf("%s: arquivo ausente", f.Name)
		}
		st := &FileStats{}
		d.Files[f.Name] = st
		var err error
		switch f.Name {
		case source.ASNumbers1, source.ASNumbers2:
			err = d.parseASNBlocks(f.Name, body, st, seen)
		case source.IPv4Space, source.IPv6Unicast:
			err = d.parsePrefixBlocks(f.Name, body, st, seen)
		case source.SpecialIPv4, source.SpecialIPv6:
			err = d.parseSpecialPrefixes(f.Name, body, st, seen)
		case source.SpecialASN:
			err = d.parseSpecialASNs(f.Name, body, st, seen)
		case source.RDAPASN, source.RDAPIPv4, source.RDAPIPv6:
			err = d.parseRDAP(f.Name, body, st, seen)
		default:
			err = fmt.Errorf("arquivo desconhecido")
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		if st.Records == 0 {
			return nil, fmt.Errorf("%s: arquivo sem nenhuma linha de dados", f.Name)
		}
		if ratio := float64(st.Skipped) / float64(st.Records); ratio > MaxSkippedRatio {
			return nil, fmt.Errorf("%s: %d de %d linhas descartadas (%.1f%%, limite %.1f%%): formato mudou? avisos: %s",
				f.Name, st.Skipped, st.Records, ratio*100, MaxSkippedRatio*100, strings.Join(d.warningsOf(f.Name, 3), "; "))
		}
	}
	return d, nil
}

// keys guarda as chaves naturais já vistas, para descartar duplicados.
type keys struct {
	asnBlock      map[int64]string // asn_start → arquivo
	prefixBlock   map[netip.Prefix]string
	specialPrefix map[netip.Prefix]string
	specialASN    map[int64]bool
	rdap          map[string]bool // kind + " " + resource
}

func (d *Dataset) warningsOf(file string, n int) []string {
	var out []string
	for _, w := range d.Warnings {
		if strings.HasPrefix(w, file+" ") || strings.HasPrefix(w, file+":") {
			out = append(out, w)
			if len(out) == n {
				break
			}
		}
	}
	return out
}
