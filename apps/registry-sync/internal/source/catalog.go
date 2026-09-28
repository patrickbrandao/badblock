// Package source define o catálogo de fontes públicas que o registry-sync
// acompanha: de onde baixar, como interpretar e quais limites de sanidade valem
// para cada uma.
package source

import (
	"fmt"
	"slices"
	"strings"
)

// Kind diz qual parser e quais tabelas de ingest uma fonte usa.
type Kind string

const (
	KindDelegated  Kind = "delegated"        // delegated-extended de um RIR
	KindIANAASN    Kind = "iana-asn"         // as-numbers-1.csv / as-numbers-2.csv
	KindIANAIPv4   Kind = "iana-ipv4"        // ipv4-address-space.csv
	KindIANAIPv6   Kind = "iana-ipv6"        // ipv6-unicast-address-assignments.csv
	KindSpecialIP  Kind = "iana-special-ip"  // registros special-purpose IPv4/IPv6
	KindSpecialASN Kind = "iana-special-asn" // special-purpose-as-numbers.csv
	KindRDAPASN    Kind = "rdap-asn"         // bootstrap RDAP de ASNs
	KindRDAPIP     Kind = "rdap-ip"          // bootstrap RDAP de IPv4/IPv6
	KindNICBR      Kind = "nicbr"            // nicbr-asn-blk
	KindASNames    Kind = "asnames"          // asn.txt do RIPE
)

// Source é uma fonte do catálogo.
type Source struct {
	ID          string
	Kind        Kind
	RIR         string   // só delegated: afrinic, apnic, arin, lacnic, ripencc
	URLs        []string // primária primeiro; as demais são espelhos de fallback
	MD5         bool     // o RIR publica <url>.md5 ao lado do arquivo
	MinRecords  int      // abaixo disso o arquivo é tratado como truncado
	Description string
}

const (
	lacnicStats = "https://ftp.lacnic.net/pub/stats/"
	ripeStats   = "https://ftp.ripe.net/pub/stats/"
	apnicStats  = "https://ftp.apnic.net/stats/"
	ianaAssign  = "https://www.iana.org/assignments/"
)

// Catalog devolve todas as fontes, na ordem em que são aplicadas.
//
// Para cada RIR, a URL primária é a do próprio RIR. Os espelhos entram só como
// fallback: em 2026-09-28 o espelho do APNIC na LACNIC estava um dia atrasado.
// Os mínimos de registros ficam bem abaixo do tamanho real (medido na mesma
// data) e só pegam arquivos claramente truncados; a trava de remoção cuida do
// resto.
func Catalog() []Source {
	return []Source{
		{
			ID: "rir-afrinic", Kind: KindDelegated, RIR: "afrinic", MD5: true, MinRecords: 10000,
			URLs: []string{
				"https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest",
				lacnicStats + "afrinic/delegated-afrinic-extended-latest",
			},
			Description: "AFRINIC delegated-extended",
		},
		{
			ID: "rir-apnic", Kind: KindDelegated, RIR: "apnic", MD5: true, MinRecords: 100000,
			URLs: []string{
				apnicStats + "apnic/delegated-apnic-extended-latest",
				lacnicStats + "apnic/delegated-apnic-extended-latest",
			},
			Description: "APNIC delegated-extended",
		},
		{
			ID: "rir-arin", Kind: KindDelegated, RIR: "arin", MD5: true, MinRecords: 100000,
			URLs: []string{
				"https://ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest",
				lacnicStats + "arin/delegated-arin-extended-latest",
			},
			Description: "ARIN delegated-extended",
		},
		{
			ID: "rir-lacnic", Kind: KindDelegated, RIR: "lacnic", MD5: true, MinRecords: 50000,
			URLs: []string{
				lacnicStats + "lacnic/delegated-lacnic-extended-latest",
				ripeStats + "lacnic/delegated-lacnic-extended-latest",
				apnicStats + "lacnic/delegated-lacnic-extended-latest",
			},
			Description: "LACNIC delegated-extended",
		},
		{
			ID: "rir-ripencc", Kind: KindDelegated, RIR: "ripencc", MD5: true, MinRecords: 150000,
			URLs: []string{
				ripeStats + "ripencc/delegated-ripencc-extended-latest",
				lacnicStats + "ripencc/delegated-ripencc-extended-latest",
			},
			Description: "RIPE NCC delegated-extended",
		},
		{
			ID: "iana-asn-16", Kind: KindIANAASN, MinRecords: 50,
			URLs:        []string{ianaAssign + "as-numbers/as-numbers-1.csv"},
			Description: "IANA: blocos de ASN de 16 bits",
		},
		{
			ID: "iana-asn-32", Kind: KindIANAASN, MinRecords: 50,
			URLs:        []string{ianaAssign + "as-numbers/as-numbers-2.csv"},
			Description: "IANA: blocos de ASN de 32 bits",
		},
		{
			ID: "iana-ipv4", Kind: KindIANAIPv4, MinRecords: 256,
			URLs:        []string{ianaAssign + "ipv4-address-space/ipv4-address-space.csv"},
			Description: "IANA: espaço IPv4 (/8)",
		},
		{
			ID: "iana-ipv6", Kind: KindIANAIPv6, MinRecords: 30,
			URLs:        []string{ianaAssign + "ipv6-unicast-address-assignments/ipv6-unicast-address-assignments.csv"},
			Description: "IANA: blocos IPv6 unicast",
		},
		{
			ID: "iana-special-ipv4", Kind: KindSpecialIP, MinRecords: 15,
			URLs:        []string{ianaAssign + "iana-ipv4-special-registry/iana-ipv4-special-registry-1.csv"},
			Description: "IANA: IPv4 de uso especial",
		},
		{
			ID: "iana-special-ipv6", Kind: KindSpecialIP, MinRecords: 15,
			URLs:        []string{ianaAssign + "iana-ipv6-special-registry/iana-ipv6-special-registry-1.csv"},
			Description: "IANA: IPv6 de uso especial",
		},
		{
			ID: "iana-special-asn", Kind: KindSpecialASN, MinRecords: 5,
			URLs:        []string{ianaAssign + "iana-as-numbers-special-registry/special-purpose-as-numbers.csv"},
			Description: "IANA: ASNs de uso especial",
		},
		{
			ID: "iana-rdap-asn", Kind: KindRDAPASN, MinRecords: 50,
			URLs:        []string{"https://data.iana.org/rdap/asn.json"},
			Description: "IANA: bootstrap RDAP de ASNs",
		},
		{
			ID: "iana-rdap-ipv4", Kind: KindRDAPIP, MinRecords: 100,
			URLs:        []string{"https://data.iana.org/rdap/ipv4.json"},
			Description: "IANA: bootstrap RDAP de IPv4",
		},
		{
			ID: "iana-rdap-ipv6", Kind: KindRDAPIP, MinRecords: 10,
			URLs:        []string{"https://data.iana.org/rdap/ipv6.json"},
			Description: "IANA: bootstrap RDAP de IPv6",
		},
		{
			ID: "nicbr", Kind: KindNICBR, MinRecords: 5000,
			URLs:        []string{"https://ftp.registro.br/pub/numeracao/origin/nicbr-asn-blk-latest.txt"},
			Description: "NIC.br: ASNs brasileiros, titulares e blocos",
		},
		{
			ID: "asnames", Kind: KindASNames, MinRecords: 80000,
			URLs:        []string{"https://ftp.ripe.net/ripe/asnames/asn.txt"},
			Description: "RIPE: nomes de todos os AS do mundo",
		},
	}
}

// Select filtra o catálogo pelos ids informados. Lista vazia devolve tudo.
// Um id desconhecido é erro, para não ignorar erro de digitação em silêncio.
func Select(ids []string) ([]Source, error) {
	all := Catalog()
	if len(ids) == 0 {
		return all, nil
	}
	var out []Source
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		i := slices.IndexFunc(all, func(s Source) bool { return s.ID == id })
		if i < 0 {
			return nil, fmt.Errorf("fonte desconhecida %q (conhecidas: %s)", id, strings.Join(IDs(), ", "))
		}
		out = append(out, all[i])
	}
	// Mantém a ordem do catálogo, que é a ordem de aplicação.
	slices.SortStableFunc(out, func(a, b Source) int {
		return slices.IndexFunc(all, func(s Source) bool { return s.ID == a.ID }) -
			slices.IndexFunc(all, func(s Source) bool { return s.ID == b.ID })
	})
	return out, nil
}

// IDs devolve os ids de todas as fontes.
func IDs() []string {
	var ids []string
	for _, s := range Catalog() {
		ids = append(ids, s.ID)
	}
	return ids
}
