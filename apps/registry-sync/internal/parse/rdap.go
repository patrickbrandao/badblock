package parse

import (
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
)

// RDAPService associa uma faixa de ASNs ou um prefixo ao servidor RDAP que
// responde por ela, conforme o bootstrap da IANA (RFC 9224).
type RDAPService struct {
	Entry    string // como publicado: "1-1876" ou "41.0.0.0/8"
	ASNFirst int64
	ASNLast  int64
	Prefix   netip.Prefix
	BaseURL  string
}

type rdapBootstrap struct {
	Version     string       `json:"version"`
	Publication string       `json:"publication"`
	Services    [][][]string `json:"services"`
}

// ParseRDAPBootstrap lê asn.json, ipv4.json ou ipv6.json. kind é "asn" ou "ip".
func ParseRDAPBootstrap(r io.Reader, kind string) ([]RDAPService, Stats, error) {
	var st Stats
	var doc rdapBootstrap
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, st, fmt.Errorf("json: %w", err)
	}
	if len(doc.Services) == 0 {
		return nil, st, fmt.Errorf("bootstrap sem services")
	}
	var out []RDAPService
	for i, svc := range doc.Services {
		if len(svc) < 2 {
			st.skip(i+1, "service sem faixas ou URLs")
			continue
		}
		base := PreferHTTPS(svc[1])
		if base == "" {
			st.skip(i+1, "service sem URL")
			continue
		}
		for _, entry := range svc[0] {
			s := RDAPService{Entry: entry, BaseURL: base}
			switch kind {
			case "asn":
				first, last, err := parseASNRange(entry)
				if err != nil {
					st.skip(i+1, "%v", err)
					continue
				}
				s.ASNFirst, s.ASNLast = first, last
			case "ip":
				p, err := netip.ParsePrefix(entry)
				if err != nil {
					st.skip(i+1, "prefixo inválido %q", entry)
					continue
				}
				s.Prefix = p.Masked()
			default:
				return nil, st, fmt.Errorf("tipo de bootstrap desconhecido %q", kind)
			}
			out = append(out, s)
		}
	}
	st.Records = len(out)
	return out, st, checkSkipped(st, 0.01)
}
