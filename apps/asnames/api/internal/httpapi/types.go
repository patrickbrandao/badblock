package httpapi

import (
	"time"

	"github.com/patrickbrandao/badblock/apps/asnames/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/asnames/api/internal/store"
)

// DatasetInfo acompanha toda resposta de dados.
type DatasetInfo struct {
	Version   string `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// ASNResponse é GET /asn/{asn}. handle, name e country são null quando a
// linha da fonte não os traz.
type ASNResponse struct {
	ASN         int64       `json:"asn"`
	Handle      *string     `json:"handle"`
	Name        *string     `json:"name"`
	Country     *string     `json:"country"`
	Description string      `json:"description"`
	FirstSeen   string      `json:"first_seen"`
	UpdatedAt   string      `json:"updated_at"`
	Dataset     DatasetInfo `json:"dataset"`
}

// ASNBrief é um ASN nas listas por país.
type ASNBrief struct {
	ASN    int64   `json:"asn"`
	Handle *string `json:"handle"`
	Name   *string `json:"name"`
}

// ASNEntry é um ASN nas listas por handle e na busca.
type ASNEntry struct {
	ASN         int64   `json:"asn"`
	Handle      *string `json:"handle"`
	Name        *string `json:"name"`
	Country     *string `json:"country"`
	Description string  `json:"description"`
}

// CountryResponse é GET /country/{cc}.
type CountryResponse struct {
	Country string      `json:"country"`
	Count   int         `json:"count"`
	ASNs    []ASNBrief  `json:"asns"`
	Dataset DatasetInfo `json:"dataset"`
}

// HandleResponse é GET /handle/{handle}.
type HandleResponse struct {
	Handle  string      `json:"handle"`
	Count   int         `json:"count"`
	ASNs    []ASNEntry  `json:"asns"`
	Dataset DatasetInfo `json:"dataset"`
}

// SearchResponse é GET /search?q=.
type SearchResponse struct {
	Query     string      `json:"query"`
	Count     int         `json:"count"`
	Truncated bool        `json:"truncated"`
	ASNs      []ASNEntry  `json:"asns"`
	Dataset   DatasetInfo `json:"dataset"`
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
	Version   string `json:"version"`
	UpdatedAt string `json:"updated_at"`
	Source    string `json:"source"`
	SHA256    string `json:"sha256"`
	ASNs      int    `json:"asns"`
}

// MetaCollector é a linha do collector-asnames em jobs.
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

func asnEntry(e store.Entry) ASNEntry {
	return ASNEntry{ASN: e.ASN, Handle: e.Handle, Name: e.Name, Country: e.Country, Description: e.Description}
}
