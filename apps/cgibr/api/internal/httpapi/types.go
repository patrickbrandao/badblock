package httpapi

import (
	"net/netip"
	"time"

	"github.com/patrickbrandao/badblock/apps/cgibr/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/cgibr/api/internal/store"
)

// DatasetInfo acompanha toda resposta de dados.
type DatasetInfo struct {
	Version   string `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// ASNRef é um ASN resumido dentro de outras respostas.
type ASNRef struct {
	ASN          int64  `json:"asn"`
	Name         string `json:"name"`
	Document     string `json:"document"`
	DocumentType string `json:"document_type"`
}

// Prefixes separa os blocos por família.
type Prefixes struct {
	IPv4 []string `json:"ipv4"`
	IPv6 []string `json:"ipv6"`
}

// ASNResponse é GET /asn/{asn}.
type ASNResponse struct {
	ASNRef
	Prefixes  Prefixes    `json:"prefixes"`
	FirstSeen string      `json:"first_seen"`
	UpdatedAt string      `json:"updated_at"`
	Dataset   DatasetInfo `json:"dataset"`
}

// IPResponse é GET /ip/{ip}.
type IPResponse struct {
	IP      string      `json:"ip"`
	Prefix  string      `json:"prefix"`
	ASN     ASNRef      `json:"asn"`
	Dataset DatasetInfo `json:"dataset"`
}

// PrefixResponse é GET /prefix/{ip}/{len}.
type PrefixResponse struct {
	Query   string      `json:"query"`
	Prefix  string      `json:"prefix"`
	Exact   bool        `json:"exact"`
	ASN     ASNRef      `json:"asn"`
	Dataset DatasetInfo `json:"dataset"`
}

// DocumentResponse é GET /document/{doc}.
type DocumentResponse struct {
	Document       string      `json:"document"`
	DocumentDigits string      `json:"document_digits"`
	DocumentType   string      `json:"document_type"`
	Name           string      `json:"name"`
	Count          int         `json:"count"`
	ASNs           []ASNName   `json:"asns"`
	Dataset        DatasetInfo `json:"dataset"`
}

// ASNName é um ASN só com número e nome.
type ASNName struct {
	ASN  int64  `json:"asn"`
	Name string `json:"name"`
}

// ASNListResponse é GET /asns.
type ASNListResponse struct {
	Count   int         `json:"count"`
	ASNs    []ASNRef    `json:"asns"`
	Dataset DatasetInfo `json:"dataset"`
}

// MetaResponse é GET /meta.
type MetaResponse struct {
	App       string         `json:"app"`
	Version   string         `json:"version"`
	Dataset   *MetaDataset   `json:"dataset"`
	Collector *MetaCollector `json:"collector"`
}

// MetaDataset descreve o arquivo aplicado.
type MetaDataset struct {
	Version    string `json:"version"`
	UpdatedAt  string `json:"updated_at"`
	Source     string `json:"source"`
	SHA256     string `json:"sha256"`
	ASNs       int    `json:"asns"`
	PrefixesV4 int    `json:"prefixes_v4"`
	PrefixesV6 int    `json:"prefixes_v6"`
}

// MetaCollector é a linha do collector-cgibr em jobs.
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

// documentType: 14 dígitos = CNPJ; o NIC.br usa 8 dígitos para titulares
// estrangeiros.
func documentType(digits string) string {
	if len(digits) == 14 {
		return "cnpj"
	}
	return "foreign"
}

func asnRef(a store.ASNBrief) ASNRef {
	return ASNRef{ASN: a.ASN, Name: a.Name, Document: a.Document, DocumentType: documentType(a.DocumentDigits)}
}

func splitPrefixes(ps []netip.Prefix) Prefixes {
	out := Prefixes{IPv4: []string{}, IPv6: []string{}}
	for _, p := range ps {
		if p.Addr().Is4() {
			out.IPv4 = append(out.IPv4, p.String())
		} else {
			out.IPv6 = append(out.IPv6, p.String())
		}
	}
	return out
}
