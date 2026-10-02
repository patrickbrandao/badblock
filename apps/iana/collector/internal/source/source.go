// Package source lista os 10 arquivos da IANA que formam o dataset, na ordem
// fixa usada em todo o app (downloads, hash combinado, iana_run.files).
//
// Os caminhos são fixos no código; só as bases vêm da configuração
// (IANA_BASE_URL e RDAP_BASE_URL), para os testes apontarem para um
// httptest.Server.
package source

import "strings"

// Bases padrão das URLs.
const (
	DefaultIANABaseURL = "https://www.iana.org/assignments"
	DefaultRDAPBaseURL = "https://data.iana.org/rdap"
)

// Nomes dos arquivos: chave em iana_run.files e valor de source_file.
const (
	ASNumbers1  = "as-numbers-1"
	ASNumbers2  = "as-numbers-2"
	IPv4Space   = "ipv4-address-space"
	IPv6Unicast = "ipv6-unicast-address-assignments"
	SpecialIPv4 = "iana-ipv4-special-registry-1"
	SpecialIPv6 = "iana-ipv6-special-registry-1"
	SpecialASN  = "special-purpose-as-numbers"
	RDAPASN     = "rdap-asn"
	RDAPIPv4    = "rdap-ipv4"
	RDAPIPv6    = "rdap-ipv6"
)

// File é um arquivo do dataset.
type File struct {
	Name string
	RDAP bool   // true = relativo a RDAP_BASE_URL; false = IANA_BASE_URL
	Path string // caminho relativo à base
}

// Files são os 10 arquivos, na ordem fixa do dataset.
var Files = []File{
	{ASNumbers1, false, "as-numbers/as-numbers-1.csv"},
	{ASNumbers2, false, "as-numbers/as-numbers-2.csv"},
	{IPv4Space, false, "ipv4-address-space/ipv4-address-space.csv"},
	{IPv6Unicast, false, "ipv6-unicast-address-assignments/ipv6-unicast-address-assignments.csv"},
	{SpecialIPv4, false, "iana-ipv4-special-registry/iana-ipv4-special-registry-1.csv"},
	{SpecialIPv6, false, "iana-ipv6-special-registry/iana-ipv6-special-registry-1.csv"},
	{SpecialASN, false, "iana-as-numbers-special-registry/special-purpose-as-numbers.csv"},
	{RDAPASN, true, "asn.json"},
	{RDAPIPv4, true, "ipv4.json"},
	{RDAPIPv6, true, "ipv6.json"},
}

// URL monta a URL do arquivo a partir das bases configuradas.
func (f File) URL(ianaBase, rdapBase string) string {
	base := ianaBase
	if f.RDAP {
		base = rdapBase
	}
	return strings.TrimRight(base, "/") + "/" + f.Path
}

// Basename é o nome do arquivo no servidor (ex.: asn.json). Os testes com os
// arquivos reais procuram cada arquivo por esse nome numa pasta.
func (f File) Basename() string {
	return f.Path[strings.LastIndex(f.Path, "/")+1:]
}
