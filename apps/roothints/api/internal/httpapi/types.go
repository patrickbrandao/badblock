package httpapi

import (
	"net/netip"
	"time"

	"github.com/patrickbrandao/badblock/apps/roothints/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/roothints/api/internal/store"
)

// DatasetInfo acompanha toda resposta de dados.
type DatasetInfo struct {
	Version   string `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// SourceInfo é o cabeçalho do named.root da versão usada na resposta
// (roothints_run): a data de "last update:" e o serial da zona raiz.
type SourceInfo struct {
	LastUpdate *string `json:"last_update"`
	ZoneSerial *int64  `json:"zone_serial"`
}

// Server é um servidor raiz. Endereços sem máscara; ipv4/ipv6 e o TTL de
// cada um são null quando o arquivo não traz aquela família, e note é null
// quando o bloco não tem comentário.
type Server struct {
	Name    string  `json:"name"`
	Letter  string  `json:"letter"`
	IPv4    *string `json:"ipv4"`
	IPv6    *string `json:"ipv6"`
	NSTTL   int     `json:"ns_ttl"`
	IPv4TTL *int    `json:"ipv4_ttl"`
	IPv6TTL *int    `json:"ipv6_ttl"`
	Note    *string `json:"note"`
}

// ServersResponse é GET /servers.
type ServersResponse struct {
	Source  SourceInfo  `json:"source"`
	Count   int         `json:"count"`
	Servers []Server    `json:"servers"`
	Dataset DatasetInfo `json:"dataset"`
}

// ServerResponse é GET /server/{server}: os campos de Server mais as datas
// deste banco.
type ServerResponse struct {
	Server
	FirstSeen string      `json:"first_seen"`
	UpdatedAt string      `json:"updated_at"`
	Source    SourceInfo  `json:"source"`
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
	Version    string  `json:"version"`
	UpdatedAt  string  `json:"updated_at"`
	Source     string  `json:"source"`
	MD5        string  `json:"md5"`
	SHA256     string  `json:"sha256"`
	LastUpdate *string `json:"last_update"`
	ZoneSerial *int64  `json:"zone_serial"`
	Servers    int     `json:"servers"`
}

// MetaCollector é a linha do collector-roothints em jobs.
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

func datePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.DateOnly)
	return &s
}

func datasetInfo(s dataset.Snapshot) DatasetInfo {
	return DatasetInfo{Version: s.Version, UpdatedAt: ts(s.AppliedAt)}
}

func sourceInfo(s dataset.Snapshot) SourceInfo {
	info := SourceInfo{ZoneSerial: s.ZoneSerial}
	if s.LastUpdate != "" {
		info.LastUpdate = &s.LastUpdate
	}
	return info
}

func serverOf(s store.Server) Server {
	return Server{
		Name: s.Name, Letter: s.Letter, IPv4: addrString(s.IPv4), IPv6: addrString(s.IPv6),
		NSTTL: s.NSTTL, IPv4TTL: s.IPv4TTL, IPv6TTL: s.IPv6TTL, Note: s.Note,
	}
}

func addrString(a *netip.Addr) *string {
	if a == nil {
		return nil
	}
	s := a.String()
	return &s
}
