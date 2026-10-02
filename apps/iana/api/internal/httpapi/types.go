package httpapi

import (
	"time"

	"github.com/patrickbrandao/badblock/apps/iana/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/iana/api/internal/store"
)

// DatasetInfo acompanha toda resposta de dados.
type DatasetInfo struct {
	Version   string `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// ASNBlock é uma faixa de ASN do registro da IANA.
type ASNBlock struct {
	Start            int64    `json:"start"`
	End              int64    `json:"end"`
	Description      string   `json:"description"`
	Registry         *string  `json:"registry"`
	Whois            *string  `json:"whois"`
	RDAPURLs         []string `json:"rdap_urls"`
	Reference        *string  `json:"reference"`
	RegistrationDate *string  `json:"registration_date"`
}

// SpecialASN é uma faixa de ASN de uso especial.
type SpecialASN struct {
	Start     int64   `json:"start"`
	End       int64   `json:"end"`
	Reason    string  `json:"reason"`
	Reference *string `json:"reference"`
}

// PrefixBlock é um bloco IP do registro da IANA (/8 do IPv4 ou bloco IPv6).
type PrefixBlock struct {
	Prefix         string   `json:"prefix"`
	Designation    string   `json:"designation"`
	Registry       *string  `json:"registry"`
	Status         string   `json:"status"`
	Whois          *string  `json:"whois"`
	RDAPURLs       []string `json:"rdap_urls"`
	AllocationDate *string  `json:"allocation_date"`
	Note           *string  `json:"note"`
}

// SpecialPrefix é um bloco IP de uso especial. Flags null = a IANA publica
// vazio ou N/A.
type SpecialPrefix struct {
	Prefix             string  `json:"prefix"`
	Name               string  `json:"name"`
	RFC                *string `json:"rfc"`
	AllocationDate     *string `json:"allocation_date"`
	TerminationDate    *string `json:"termination_date"`
	Source             *bool   `json:"source"`
	Destination        *bool   `json:"destination"`
	Forwardable        *bool   `json:"forwardable"`
	GloballyReachable  *bool   `json:"globally_reachable"`
	ReservedByProtocol *bool   `json:"reserved_by_protocol"`
}

// RDAPService é o servidor RDAP de uma faixa de ASN ou bloco IP.
type RDAPService struct {
	Resource string   `json:"resource"`
	Registry *string  `json:"registry"`
	URLs     []string `json:"urls"`
}

// ASNResponse é GET /asn/{asn}.
type ASNResponse struct {
	ASN     int64        `json:"asn"`
	Block   *ASNBlock    `json:"block"`
	Special []SpecialASN `json:"special"`
	RDAP    *RDAPService `json:"rdap"`
	Dataset DatasetInfo  `json:"dataset"`
}

// IPResponse é GET /ip/{ip}.
type IPResponse struct {
	IP      string          `json:"ip"`
	Block   *PrefixBlock    `json:"block"`
	Special []SpecialPrefix `json:"special"`
	Bogon   bool            `json:"bogon"`
	RDAP    *RDAPService    `json:"rdap"`
	Dataset DatasetInfo     `json:"dataset"`
}

// PrefixResponse é GET /prefix/{ip}/{len}.
type PrefixResponse struct {
	Query   string          `json:"query"`
	Block   *PrefixBlock    `json:"block"`
	Special []SpecialPrefix `json:"special"`
	Bogon   bool            `json:"bogon"`
	RDAP    *RDAPService    `json:"rdap"`
	Dataset DatasetInfo     `json:"dataset"`
}

// ASNBlocksResponse é GET /asns.
type ASNBlocksResponse struct {
	Count   int         `json:"count"`
	Blocks  []ASNBlock  `json:"blocks"`
	Dataset DatasetInfo `json:"dataset"`
}

// PrefixBlocksResponse é GET /ipv4 e GET /ipv6.
type PrefixBlocksResponse struct {
	Count   int           `json:"count"`
	Blocks  []PrefixBlock `json:"blocks"`
	Dataset DatasetInfo   `json:"dataset"`
}

// SpecialResponse é GET /special.
type SpecialResponse struct {
	IPv4    []SpecialPrefix `json:"ipv4"`
	IPv6    []SpecialPrefix `json:"ipv6"`
	ASN     []SpecialASN    `json:"asn"`
	Dataset DatasetInfo     `json:"dataset"`
}

// RDAPPublication é o campo publication de cada JSON de bootstrap RDAP.
type RDAPPublication struct {
	ASN  *string `json:"asn"`
	IPv4 *string `json:"ipv4"`
	IPv6 *string `json:"ipv6"`
}

// RDAPResponse é GET /rdap.
type RDAPResponse struct {
	Publication RDAPPublication `json:"publication"`
	ASN         []RDAPService   `json:"asn"`
	IPv4        []RDAPService   `json:"ipv4"`
	IPv6        []RDAPService   `json:"ipv6"`
	Dataset     DatasetInfo     `json:"dataset"`
}

// MetaResponse é GET /meta.
type MetaResponse struct {
	App       string         `json:"app"`
	Version   string         `json:"version"`
	Dataset   *MetaDataset   `json:"dataset"`
	Collector *MetaCollector `json:"collector"`
}

// MetaDataset descreve o dataset aplicado.
type MetaDataset struct {
	Version   string     `json:"version"`
	UpdatedAt string     `json:"updated_at"`
	SHA256    string     `json:"sha256"`
	Files     []MetaFile `json:"files"`
}

// MetaFile é um dos 10 arquivos da IANA do dataset aplicado.
type MetaFile struct {
	Name         string  `json:"name"`
	URL          string  `json:"url"`
	SHA256       string  `json:"sha256"`
	Bytes        int64   `json:"bytes"`
	Rows         *int    `json:"rows"`
	LastModified *string `json:"last_modified"`
	Publication  *string `json:"publication"`
}

// MetaCollector é a linha do collector-iana em jobs.
type MetaCollector struct {
	App          string  `json:"app"`
	LastSyncAt   *string `json:"last_sync_at"`
	LastCheckAt  *string `json:"last_check_at"`
	Consolidated bool    `json:"consolidated"`
}

// StatusResponse é /health e /status.
type StatusResponse struct {
	Success   bool              `json:"success"`
	Status    string            `json:"status"`
	Timestamp string            `json:"timestamp"`
	Message   string            `json:"message"`
	Checks    map[string]string `json:"checks"`
}

// IndexResponse é GET {base}/.
type IndexResponse struct {
	App       string   `json:"app"`
	Version   string   `json:"version"`
	BasePath  string   `json:"base_path"`
	Versions  []string `json:"versions"`
	Endpoints []string `json:"endpoints"`
	Source    string   `json:"source"`
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func tsPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := ts(*t)
	return &s
}

func datasetInfo(s dataset.Snapshot) DatasetInfo {
	return DatasetInfo{Version: s.Version, UpdatedAt: ts(s.AppliedAt)}
}

// strs garante [] (e não null) no JSON.
func strs(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func asnBlock(b store.ASNBlock) ASNBlock {
	return ASNBlock{
		Start: b.Start, End: b.End, Description: b.Description, Registry: b.Registry, Whois: b.Whois,
		RDAPURLs: strs(b.RDAPURLs), Reference: b.Reference, RegistrationDate: b.RegistrationDate,
	}
}

func asnBlockPtr(b *store.ASNBlock) *ASNBlock {
	if b == nil {
		return nil
	}
	v := asnBlock(*b)
	return &v
}

func asnBlocks(list []store.ASNBlock) []ASNBlock {
	out := make([]ASNBlock, 0, len(list))
	for _, b := range list {
		out = append(out, asnBlock(b))
	}
	return out
}

func specialASNs(list []store.SpecialASN) []SpecialASN {
	out := make([]SpecialASN, 0, len(list))
	for _, s := range list {
		out = append(out, SpecialASN{Start: s.Start, End: s.End, Reason: s.Reason, Reference: s.Reference})
	}
	return out
}

func prefixBlock(b store.PrefixBlock) PrefixBlock {
	return PrefixBlock{
		Prefix: b.Prefix.String(), Designation: b.Designation, Registry: b.Registry, Status: b.Status,
		Whois: b.Whois, RDAPURLs: strs(b.RDAPURLs), AllocationDate: b.AllocationDate, Note: b.Note,
	}
}

func prefixBlockPtr(b *store.PrefixBlock) *PrefixBlock {
	if b == nil {
		return nil
	}
	v := prefixBlock(*b)
	return &v
}

func prefixBlocks(list []store.PrefixBlock) []PrefixBlock {
	out := make([]PrefixBlock, 0, len(list))
	for _, b := range list {
		out = append(out, prefixBlock(b))
	}
	return out
}

func specialPrefix(s store.SpecialPrefix) SpecialPrefix {
	return SpecialPrefix{
		Prefix: s.Prefix.String(), Name: s.Name, RFC: s.RFC, AllocationDate: s.AllocationDate,
		TerminationDate: s.TerminationDate, Source: s.Source, Destination: s.Destination,
		Forwardable: s.Forwardable, GloballyReachable: s.GloballyReachable, ReservedByProtocol: s.ReservedByProtocol,
	}
}

func specialPrefixes(list []store.SpecialPrefix) []SpecialPrefix {
	out := make([]SpecialPrefix, 0, len(list))
	for _, s := range list {
		out = append(out, specialPrefix(s))
	}
	return out
}

func rdapService(r store.RDAPService) RDAPService {
	return RDAPService{Resource: r.Resource, Registry: r.Registry, URLs: strs(r.URLs)}
}

func rdapPtr(r *store.RDAPService) *RDAPService {
	if r == nil {
		return nil
	}
	v := rdapService(*r)
	return &v
}
