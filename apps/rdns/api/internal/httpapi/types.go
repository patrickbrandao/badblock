package httpapi

import (
	"net/netip"
	"time"

	"github.com/patrickbrandao/badblock/apps/rdns/api/internal/store"
)

// Scan é o prefixo de rdns_prefix que contém o pedido e a data da varredura
// dele (substitui o bloco dataset das outras APIs).
type Scan struct {
	Prefix    string  `json:"prefix"`
	ASN       int64   `json:"asn"`
	Source    string  `json:"source"`
	Active    bool    `json:"active"`
	CheckedAt *string `json:"checked_at"`
}

// LiveInfo é o bloco live de /ip com live=true.
type LiveInfo struct {
	Status    string  `json:"status"`
	Result    *string `json:"result"`
	CheckedAt *string `json:"checked_at"`
}

// IPResponse é GET /ip/{ip}.
type IPResponse struct {
	IP        string    `json:"ip"`
	Status    string    `json:"status"`
	Name      *string   `json:"name"`
	Names     []string  `json:"names"`
	FirstSeen *string   `json:"first_seen"`
	UpdatedAt *string   `json:"updated_at"`
	Scan      Scan      `json:"scan"`
	Live      *LiveInfo `json:"live,omitempty"`
}

// PrefixItem é um endereço de GET /prefix/{ip}/{len}.
type PrefixItem struct {
	IP     string   `json:"ip"`
	Status string   `json:"status"`
	Name   *string  `json:"name"`
	Names  []string `json:"names"`
}

// PrefixCounts conta os itens em cada estado.
type PrefixCounts struct {
	PTR        int `json:"ptr"`
	NoPTR      int `json:"no_ptr"`
	ServFail   int `json:"servfail"`
	Refused    int `json:"refused"`
	Timeout    int `json:"timeout"`
	NotScanned int `json:"not_scanned"`
}

func (c *PrefixCounts) add(status string) {
	switch status {
	case "ptr":
		c.PTR++
	case "no_ptr":
		c.NoPTR++
	case "servfail":
		c.ServFail++
	case "refused":
		c.Refused++
	case "timeout":
		c.Timeout++
	case "not_scanned":
		c.NotScanned++
	}
}

// PrefixResponse é GET /prefix/{ip}/{len}.
type PrefixResponse struct {
	Prefix string       `json:"prefix"`
	Scan   Scan         `json:"scan"`
	Counts PrefixCounts `json:"counts"`
	Items  []PrefixItem `json:"items"`
}

// MetaResponse é GET /meta.
type MetaResponse struct {
	App         string           `json:"app"`
	Version     string           `json:"version"`
	Scan        MetaScan         `json:"scan"`
	Config      MetaConfig       `json:"config"`
	Derivations []MetaDerivation `json:"derivations"`
	Workers     []MetaWorker     `json:"workers"`
	Collector   *MetaCollector   `json:"collector"`
	Live        MetaLive         `json:"live"`
}

// MetaScan resume rdns_prefix.
type MetaScan struct {
	Prefixes     int     `json:"prefixes"`
	Active       int     `json:"active"`
	Static       int     `json:"static"`
	OutOfRule    int     `json:"out_of_rule"`
	Addresses    int64   `json:"addresses"`
	NeverScanned int     `json:"never_scanned"`
	Overdue      int     `json:"overdue"`
	Rescan       int     `json:"rescan"`
	Scanning     int     `json:"scanning"`
	OldestScanAt *string `json:"oldest_scan_at"`
	PTR          int64   `json:"ptr"`
	NoPTR        int64   `json:"no_ptr"`
	Failed       int64   `json:"failed"`
}

// MetaConfig é rdns_config (null = cada coletor usa a reserva do ambiente).
type MetaConfig struct {
	RateLimit    *int    `json:"rate_limit"`
	ScanInterval *string `json:"scan_interval"`
}

// MetaDerivation é a última derivação aplicada de uma origem.
type MetaDerivation struct {
	Source          string `json:"source"`
	Version         string `json:"version"`
	SourceVersion   string `json:"source_version"`
	SourceUpdatedAt string `json:"source_updated_at"`
	CreatedAt       string `json:"created_at"`
	Blocks          int    `json:"blocks"`
	Units           int    `json:"units"`
	Inserted        int    `json:"inserted"`
	ASNUpdated      int    `json:"asn_updated"`
	Covered         int    `json:"covered"`
	NotInSource     int    `json:"not_in_source"`
}

// MetaWorker é uma instância do collector-rdns.
type MetaWorker struct {
	Host        string `json:"host"`
	Version     string `json:"version"`
	StartedAt   string `json:"started_at"`
	SeenAt      string `json:"seen_at"`
	Alive       bool   `json:"alive"`
	RateLimit   int    `json:"rate_limit"`
	Queries     int64  `json:"queries"`
	LiveQueries int64  `json:"live_queries"`
	Prefixes    int64  `json:"prefixes"`
}

// MetaCollector é a linha do collector-rdns em jobs.
type MetaCollector struct {
	App          string  `json:"app"`
	LastSyncAt   *string `json:"last_sync_at"`
	LastCheckAt  *string `json:"last_check_at"`
	Consolidated bool    `json:"consolidated"`
}

// MetaLive é o estado da fila de tempo real.
type MetaLive struct {
	Queue *int64 `json:"queue"`
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
	App         string   `json:"app"`
	Version     string   `json:"version"`
	BasePath    string   `json:"base_path"`
	Versions    []string `json:"versions"`
	Endpoints   []string `json:"endpoints"`
	Source      string   `json:"source"`
	DerivedFrom []string `json:"derived_from"`
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func tsPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := ts(*t)
	return &s
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// statusName traduz rdns_ptr.status.
func statusName(s int16) string {
	switch s {
	case 1:
		return "ptr"
	case 2:
		return "servfail"
	case 3:
		return "refused"
	case 4:
		return "timeout"
	}
	return "unknown"
}

func scanInfo(p store.Prefix) Scan {
	return Scan{Prefix: p.Prefix.String(), ASN: p.ASN, Source: p.Source, Active: p.Active, CheckedAt: tsPtr(p.LastScanAt)}
}

// noRowStatus é o estado de um IP sem linha em rdns_ptr: no_ptr se o prefixo
// já foi varrido, not_scanned se nunca foi.
func noRowStatus(p store.Prefix) string {
	if p.LastScanAt != nil {
		return "no_ptr"
	}
	return "not_scanned"
}

func ipResponse(ip netip.Addr, row *store.IPRow) IPResponse {
	resp := IPResponse{IP: ip.String(), Status: noRowStatus(row.Prefix), Names: []string{}, Scan: scanInfo(row.Prefix)}
	if p := row.PTR; p != nil {
		resp.Status = statusName(p.Status)
		resp.FirstSeen, resp.UpdatedAt = tsPtr(&p.CreatedAt), tsPtr(&p.UpdatedAt)
		if p.Status == 1 {
			resp.Name = p.Name
			resp.Names = p.Names
		}
	}
	return resp
}

func prefixResponse(q netip.Prefix, rows *store.PrefixRows) PrefixResponse {
	resp := PrefixResponse{Prefix: q.String(), Scan: scanInfo(rows.Prefix)}
	n := 1 << (32 - q.Bits())
	resp.Items = make([]PrefixItem, 0, n)
	missing := noRowStatus(rows.Prefix)
	next := 0
	for a, i := q.Addr(), 0; i < n; a, i = a.Next(), i+1 {
		item := PrefixItem{IP: a.String(), Status: missing, Names: []string{}}
		for next < len(rows.PTRs) && rows.PTRs[next].IP.Less(a) {
			next++
		}
		if next < len(rows.PTRs) && rows.PTRs[next].IP == a {
			p := rows.PTRs[next]
			item.Status = statusName(p.Status)
			if p.Status == 1 {
				item.Name, item.Names = p.Name, p.Names
			}
		}
		resp.Counts.add(item.Status)
		resp.Items = append(resp.Items, item)
	}
	return resp
}
