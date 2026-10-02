package httpapi

import (
	"time"

	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/dataset"
)

// DatasetInfo acompanha toda resposta de dados.
type DatasetInfo struct {
	Version   string `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// ZoneInfo é a versão da zona raiz nas rotas de dados: o serial do SOA da
// execução aplicada que é a versão do dataset (null só numa linha fora do
// padrão).
type ZoneInfo struct {
	Serial *int64 `json:"serial"`
}

// TLDsResponse é GET /tlds.
type TLDsResponse struct {
	Zone    ZoneInfo     `json:"zone"`
	Count   int          `json:"count"`
	TLDs    []TLDSummary `json:"tlds"`
	Dataset DatasetInfo  `json:"dataset"`
}

// TLDSummary é um TLD na lista, com as contagens de rootzone_tld.
type TLDSummary struct {
	TLD             string `json:"tld"`
	TLDUnicode      string `json:"tld_unicode"`
	Nameservers     int    `json:"nameservers"`
	NameserversIPv4 int    `json:"nameservers_ipv4"`
	NameserversIPv6 int    `json:"nameservers_ipv6"`
	DSRecords       int    `json:"ds_records"`
}

// TLDResponse é GET /tld/{tld}: a delegação de um TLD.
type TLDResponse struct {
	TLD         string       `json:"tld"`
	TLDUnicode  string       `json:"tld_unicode"`
	Nameservers []Nameserver `json:"nameservers"`
	DS          []DS         `json:"ds"`
	FirstSeen   string       `json:"first_seen"`
	UpdatedAt   string       `json:"updated_at"`
	Zone        ZoneInfo     `json:"zone"`
	Dataset     DatasetInfo  `json:"dataset"`
}

// Nameserver é um NS da delegação, com o glue que a zona raiz traz dele.
type Nameserver struct {
	Name string    `json:"name"`
	TTL  int64     `json:"ttl"`
	IPv4 []Address `json:"ipv4"`
	IPv6 []Address `json:"ipv6"`
}

// Address é um registro de glue (A ou AAAA), com o TTL dele.
type Address struct {
	Address string `json:"address"`
	TTL     int64  `json:"ttl"`
}

// DS é um registro DS da delegação, decomposto.
type DS struct {
	KeyTag     int    `json:"key_tag"`
	Algorithm  int    `json:"algorithm"`
	DigestType int    `json:"digest_type"`
	Digest     string `json:"digest"`
	TTL        int64  `json:"ttl"`
}

// MetaResponse é GET /meta.
type MetaResponse struct {
	App       string         `json:"app"`
	Version   string         `json:"version"`
	Dataset   *MetaDataset   `json:"dataset"`
	Collector *MetaCollector `json:"collector"`
}

// MetaDataset descreve a zona aplicada.
type MetaDataset struct {
	Version   string   `json:"version"`
	UpdatedAt string   `json:"updated_at"`
	Source    string   `json:"source"`
	SHA256    string   `json:"sha256"`
	Serial    *int64   `json:"serial"`
	SOA       *MetaSOA `json:"soa"`
	TLDs      *int     `json:"tlds"`
	Records   *int     `json:"records"`
	RRSIGs    *int     `json:"rrsigs"`
}

// MetaSOA é o SOA da zona aplicada (o serial fica em MetaDataset.Serial).
type MetaSOA struct {
	MName   string  `json:"mname"`
	RName   *string `json:"rname"`
	Refresh *int64  `json:"refresh"`
	Retry   *int64  `json:"retry"`
	Expire  *int64  `json:"expire"`
	Minimum *int64  `json:"minimum"`
}

// MetaCollector é a linha do collector-rootzone em jobs.
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

func zoneInfo(s dataset.Snapshot) ZoneInfo { return ZoneInfo{Serial: s.Serial} }
