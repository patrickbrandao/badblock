package parse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/source"
)

// bootstrap é o JSON do bootstrap RDAP (RFC 9224):
//
//	{"description": "...", "publication": "2026-06-01T20:00:01Z", "version": "1.0",
//	 "services": [[["1-1876", "1902-2042"], ["https://rdap.arin.net/registry/", "http://rdap.arin.net/registry/"]], ...]}
type bootstrap struct {
	Description string       `json:"description"`
	Publication string       `json:"publication"`
	Version     string       `json:"version"`
	Services    [][][]string `json:"services"`
}

// parseRDAP lê asn.json, ipv4.json ou ipv6.json: uma RDAPService por entrada
// de services[i][0], com as URLs de services[i][1].
func (d *Dataset) parseRDAP(file string, body []byte, st *FileStats, seen *keys) error {
	kind := map[string]string{source.RDAPASN: KindASN, source.RDAPIPv4: KindIPv4, source.RDAPIPv6: KindIPv6}[file]
	var b bootstrap
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))))
	if err := dec.Decode(&b); err != nil {
		return fmt.Errorf("JSON inválido: %w", err)
	}
	if b.Services == nil {
		return fmt.Errorf("JSON sem a lista services: formato mudou?")
	}
	if b.Publication != "" {
		if t, err := time.Parse(time.RFC3339, b.Publication); err == nil {
			st.Publication = t.UTC().Format(time.RFC3339)
		} else {
			d.warn(file, 0, "publication não reconhecida %q", b.Publication)
		}
	}

	for i, svc := range b.Services {
		n := i + 1
		if len(svc) < 2 {
			entries := 1
			if len(svc) == 1 {
				entries = max(len(svc[0]), 1)
			}
			st.Records += entries
			st.Skipped += entries
			d.warn(file, 0, "serviço %d descartado: esperava [entradas, URLs]", n)
			continue
		}
		var urls []string
		for _, u := range svc[1] {
			u = strings.TrimSpace(u)
			if !validURL(u) {
				d.warn(file, 0, "serviço %d: URL inválida %q ignorada", n, u)
				continue
			}
			urls = append(urls, u)
		}
		registry := registryFromURLs(urls)
		for _, e := range svc[0] {
			st.Records++
			if len(urls) == 0 {
				d.skip(st, file, 0, "serviço %d sem URL válida; entrada %q", n, e)
				continue
			}
			s := RDAPService{Kind: kind, Registry: registry, URLs: urls}
			if kind == KindASN {
				start, end, err := parseASNRange(e)
				if err != nil {
					d.skip(st, file, 0, "serviço %d: %v", n, err)
					continue
				}
				s.Start, s.End, s.Resource = start, end, formatASNRange(start, end)
			} else {
				p, hostBits, err := parsePrefix(e)
				if err != nil {
					d.skip(st, file, 0, "serviço %d: %v", n, err)
					continue
				}
				if p.Addr().Is4() != (kind == KindIPv4) {
					d.skip(st, file, 0, "serviço %d: bloco %s da família errada", n, p)
					continue
				}
				if hostBits {
					d.warn(file, 0, "serviço %d: bloco %q com bits de host; usando %s", n, e, p)
				}
				s.Prefix, s.Resource = p, p.String()
			}
			key := kind + " " + s.Resource
			if seen.rdap[key] {
				d.skip(st, file, 0, "serviço %d: entrada %s repetida", n, s.Resource)
				continue
			}
			seen.rdap[key] = true
			d.RDAPServices = append(d.RDAPServices, s)
			st.Rows++
		}
	}
	return nil
}
