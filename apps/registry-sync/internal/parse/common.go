// Package parse lê os arquivos publicados pela IANA, pelos RIRs, pelo NIC.br
// e pelo RIPE (asn.txt) e devolve registros tipados e validados.
//
// Os parsers não acessam rede nem banco: recebem um io.Reader e devolvem
// structs. Toda a validação de integridade de conteúdo (contagens, formato,
// linhas inválidas) acontece aqui, antes de qualquer escrita no Postgres.
package parse

import (
	"fmt"
	"strings"
	"time"
)

// maxSampleErrors limita quantas mensagens de linha inválida são guardadas.
const maxSampleErrors = 10

// Stats resume uma leitura: quantos registros entraram e quais linhas foram
// descartadas, com algumas mensagens de exemplo para o log.
type Stats struct {
	Records  int
	Skipped  int
	Samples  []string
	Warnings int
}

// skip registra uma linha descartada.
func (s *Stats) skip(line int, format string, args ...any) {
	s.Skipped++
	if len(s.Samples) < maxSampleErrors {
		s.Samples = append(s.Samples, fmt.Sprintf("linha %d: %s", line, fmt.Sprintf(format, args...)))
	}
}

// warn registra um problema que não descarta a linha.
func (s *Stats) warn(line int, format string, args ...any) {
	s.Warnings++
	if len(s.Samples) < maxSampleErrors {
		s.Samples = append(s.Samples, fmt.Sprintf("linha %d (aviso): %s", line, fmt.Sprintf(format, args...)))
	}
}

// ErrTooManySkipped indica que a proporção de linhas inválidas passou do limite.
type ErrTooManySkipped struct {
	Skipped, Total int
	Samples        []string
}

func (e *ErrTooManySkipped) Error() string {
	return fmt.Sprintf("%d de %d linhas inválidas (exemplos: %s)", e.Skipped, e.Total, strings.Join(e.Samples, "; "))
}

// checkSkipped falha quando mais de maxRatio das linhas foram descartadas.
func checkSkipped(st Stats, maxRatio float64) error {
	total := st.Records + st.Skipped
	if st.Skipped > 0 && total > 0 && float64(st.Skipped)/float64(total) > maxRatio {
		return &ErrTooManySkipped{Skipped: st.Skipped, Total: total, Samples: st.Samples}
	}
	return nil
}

// parseDate aceita os formatos de data das fontes: AAAAMMDD (delegated),
// AAAA-MM-DD e AAAA-MM (IANA). Vazio, "00000000" e "N/A" viram nil.
func parseDate(v string) (*time.Time, error) {
	v = strings.TrimSpace(v)
	switch v {
	case "", "00000000", "N/A":
		return nil, nil
	}
	for _, layout := range []string{"20060102", "2006-01-02", "2006-01"} {
		if len(v) != len(layout) {
			continue
		}
		t, err := time.Parse(layout, v)
		if err == nil {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("data inválida %q", v)
}

// splitURLs separa as URLs de uma célula da IANA, que às vezes vêm coladas
// ("https://rdap.arin.net/registryhttp://rdap.arin.net/registry").
func splitURLs(v string) []string {
	var out []string
	rest := strings.TrimSpace(v)
	for rest != "" {
		start := indexScheme(rest)
		if start < 0 {
			break
		}
		rest = rest[start:]
		next := indexScheme(rest[1:])
		if next < 0 {
			out = append(out, strings.TrimSpace(rest))
			break
		}
		out = append(out, strings.TrimSpace(rest[:next+1]))
		rest = rest[next+1:]
	}
	return out
}

// indexScheme devolve a posição do próximo "http://" ou "https://".
func indexScheme(s string) int {
	a := strings.Index(s, "https://")
	b := strings.Index(s, "http://")
	switch {
	case a < 0:
		return b
	case b < 0:
		return a
	case a < b:
		return a
	default:
		return b
	}
}

// PreferHTTPS escolhe a primeira URL https (ou a primeira de todas) e garante
// a barra final, formato que o RDAP espera para concatenar caminhos.
func PreferHTTPS(urls []string) string {
	chosen := ""
	for _, u := range urls {
		if strings.HasPrefix(u, "https://") {
			chosen = u
			break
		}
	}
	if chosen == "" && len(urls) > 0 {
		chosen = urls[0]
	}
	if chosen != "" && !strings.HasSuffix(chosen, "/") {
		chosen += "/"
	}
	return chosen
}
