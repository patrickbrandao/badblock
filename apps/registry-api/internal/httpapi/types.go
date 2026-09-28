package httpapi

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/patrickbrandao/badblock/apps/registry-api/internal/store"
)

// Tipos das respostas JSON da v1. Campos opcionais saem como null (e não
// somem) para que o formato seja estável para quem consome.

// DatasetInfo identifica a versão do central que gerou a resposta.
type DatasetInfo struct {
	Version   int64  `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// PrefixInfo é o prefixo principal de uma resposta.
type PrefixInfo struct {
	CIDR        string  `json:"cidr"`
	Level       string  `json:"level"`
	RIR         *string `json:"rir"`
	Country     *string `json:"country"`
	Status      *string `json:"status"`
	Registered  *string `json:"registered"`
	Designation *string `json:"designation,omitempty"`
	RDAP        *string `json:"rdap"`
}

// HolderInfo é o titular de um recurso.
type HolderInfo struct {
	ID         string  `json:"id"`
	Name       *string `json:"name"`
	NameSource *string `json:"name_source"`
	Document   *string `json:"document"`
}

// ASNLink é um ASN ligado a um prefixo: pelo NIC.br (nicbr) ou pelo titular.
type ASNLink struct {
	ASN  int64   `json:"asn"`
	Name *string `json:"name"`
	Link string  `json:"link"`
}

// ChainItem é um nível da cadeia IANA → RIR → NIC.br.
type ChainItem struct {
	CIDR        string  `json:"cidr"`
	Level       string  `json:"level"`
	RIR         *string `json:"rir"`
	Country     *string `json:"country,omitempty"`
	Status      *string `json:"status"`
	Registered  *string `json:"registered"`
	Designation *string `json:"designation,omitempty"`
}

// Flags resume o que interessa para bloqueio.
type Flags struct {
	Bogon   bool    `json:"bogon"`
	Special *string `json:"special"`
}

// ipLookup é a parte da resposta de /v1/ip que depende só do bloco; é o que
// vai para o cache, compartilhado por todos os IPs do /24 ou /48.
type ipLookup struct {
	Prefix        *PrefixInfo `json:"prefix"`
	Holder        *HolderInfo `json:"holder"`
	ASNs          []ASNLink   `json:"asns"`
	ASNsTruncated bool        `json:"asns_truncated,omitempty"`
	Chain         []ChainItem `json:"chain"`
	Flags         Flags       `json:"flags"`
}

// IPResponse é a resposta de /v1/ip e /v1/ip/{ip}.
type IPResponse struct {
	IP string `json:"ip"`
	ipLookup
	Dataset DatasetInfo `json:"dataset"`
}

// ASNPrefix é um prefixo ligado a um ASN.
type ASNPrefix struct {
	CIDR    string  `json:"cidr"`
	Link    string  `json:"link"`
	Status  *string `json:"status"`
	Country *string `json:"country"`
}

// ASNChainItem é um bloco de ASNs da IANA ou de uso especial.
type ASNChainItem struct {
	First       int64   `json:"first"`
	Last        int64   `json:"last"`
	Level       string  `json:"level"`
	RIR         *string `json:"rir"`
	Designation string  `json:"designation"`
	Reference   *string `json:"reference,omitempty"`
}

// ASNResponse é a resposta de /v1/asn/{asn}.
type ASNResponse struct {
	ASN               int64          `json:"asn"`
	Name              *string        `json:"name"`
	NameSource        *string        `json:"name_source"`
	Handle            *string        `json:"handle"`
	RIR               *string        `json:"rir"`
	Country           *string        `json:"country"`
	Status            *string        `json:"status"`
	Registered        *string        `json:"registered"`
	RDAP              *string        `json:"rdap"`
	Holder            *HolderInfo    `json:"holder"`
	Prefixes          []ASNPrefix    `json:"prefixes"`
	PrefixesTotal     int            `json:"prefixes_total"`
	PrefixesTruncated bool           `json:"prefixes_truncated"`
	Chain             []ASNChainItem `json:"chain"`
	Flags             Flags          `json:"flags"`
	FirstSeen         *string        `json:"first_seen"`
	UpdatedAt         *string        `json:"updated_at"`
	Dataset           DatasetInfo    `json:"dataset"`
}

// ChildPrefix é uma delegação dentro do prefixo consultado.
type ChildPrefix struct {
	CIDR    string  `json:"cidr"`
	Level   string  `json:"level"`
	Status  *string `json:"status"`
	Country *string `json:"country"`
	Holder  *string `json:"holder"`
}

// PrefixResponse é a resposta de /v1/prefix/{ip}/{len}.
type PrefixResponse struct {
	Query             string        `json:"query"`
	Match             string        `json:"match"` // exact, covering ou none
	Prefix            *PrefixInfo   `json:"prefix"`
	Holder            *HolderInfo   `json:"holder"`
	ASNs              []ASNLink     `json:"asns"`
	Chain             []ChainItem   `json:"chain"`
	Children          []ChildPrefix `json:"children"`
	ChildrenTruncated bool          `json:"children_truncated"`
	Flags             Flags         `json:"flags"`
	Dataset           DatasetInfo   `json:"dataset"`
}

// HolderCounts são as contagens de um titular.
type HolderCounts struct {
	ASNs          int   `json:"asns"`
	IPv4Prefixes  int   `json:"ipv4_prefixes"`
	IPv6Prefixes  int   `json:"ipv6_prefixes"`
	IPv4Addresses int64 `json:"ipv4_addresses"`
}

// ListASNItem é um ASN nas listas.
type ListASNItem struct {
	ASN        int64   `json:"asn"`
	Name       *string `json:"name"`
	RIR        *string `json:"rir"`
	Country    *string `json:"country"`
	Status     *string `json:"status"`
	Registered *string `json:"registered"`
	Holder     *string `json:"holder,omitempty"`
}

// ListPrefixItem é um prefixo nas listas.
type ListPrefixItem struct {
	CIDR       string  `json:"cidr"`
	RIR        *string `json:"rir"`
	Country    *string `json:"country"`
	Status     *string `json:"status"`
	Registered *string `json:"registered"`
	Holder     *string `json:"holder,omitempty"`
}

// HolderResponse é a resposta de /v1/holder/{rir}/{id}.
type HolderResponse struct {
	ID         string           `json:"id"`
	RIR        *string          `json:"rir"`
	Name       *string          `json:"name"`
	NameSource *string          `json:"name_source"`
	Document   *string          `json:"document"`
	Country    *string          `json:"country"`
	Counts     HolderCounts     `json:"counts"`
	ASNs       []ListASNItem    `json:"asns"`
	Prefixes   []ListPrefixItem `json:"prefixes"`
	NextCursor *string          `json:"next_cursor"`
	FirstSeen  string           `json:"first_seen"`
	UpdatedAt  string           `json:"updated_at"`
	RemovedAt  *string          `json:"removed_at"`
	Dataset    DatasetInfo      `json:"dataset"`
}

// PrefixListResponse é a resposta paginada das listas de prefixos.
type PrefixListResponse struct {
	Filter     map[string]any   `json:"filter"`
	Items      []ListPrefixItem `json:"items"`
	NextCursor *string          `json:"next_cursor"`
	Dataset    DatasetInfo      `json:"dataset"`
}

// ASNListResponse é a resposta paginada das listas de ASNs.
type ASNListResponse struct {
	Filter     map[string]any `json:"filter"`
	Items      []ListASNItem  `json:"items"`
	NextCursor *string        `json:"next_cursor"`
	Dataset    DatasetInfo    `json:"dataset"`
}

// HistoryEvent é um evento do change_log.
type HistoryEvent struct {
	At             string          `json:"at"`
	DatasetVersion int64           `json:"dataset_version"`
	Action         string          `json:"action"`
	Level          *string         `json:"level,omitempty"`
	Before         json.RawMessage `json:"before"`
	After          json.RawMessage `json:"after"`
}

// HistoryResponse é a resposta dos endpoints de histórico.
type HistoryResponse struct {
	Entity  string         `json:"entity"`
	Key     string         `json:"key"`
	Events  []HistoryEvent `json:"events"`
	Dataset DatasetInfo    `json:"dataset"`
}

// SourceInfo é o estado de uma fonte em /v1/meta/sources.
type SourceInfo struct {
	ID            string  `json:"id"`
	URL           *string `json:"url"`
	FileDate      *string `json:"file_date"`
	Records       *int64  `json:"records"`
	LastCheckedAt *string `json:"last_checked_at"`
	LastChangedAt *string `json:"last_changed_at"`
	LastSuccessAt *string `json:"last_success_at"`
	LastError     *string `json:"last_error"`
	LastErrorAt   *string `json:"last_error_at"`
}

// SourcesResponse é a resposta de /v1/meta/sources.
type SourcesResponse struct {
	Sources []SourceInfo `json:"sources"`
	Dataset DatasetInfo  `json:"dataset"`
}

// LegacyASN é o formato exato do POC antigo (/asn/{asn}).
type LegacyASN struct {
	ASN     int64  `json:"asn"`
	Name    string `json:"name"`
	CID     string `json:"cid"`
	Country string `json:"country"`
	RIR     string `json:"rir"`
	Status  string `json:"status"`
	Score   string `json:"score"`
}

// --- conversões -------------------------------------------------------------

var rirNames = map[string]string{
	"afrinic": "AFRINIC", "apnic": "APNIC", "arin": "ARIN", "lacnic": "LACNIC", "ripencc": "RIPENCC",
}

// rirName devolve o RIR em maiúsculas, como no restante do BadBlock.
func rirName(id *string) *string {
	if id == nil {
		return nil
	}
	if n, ok := rirNames[*id]; ok {
		return &n
	}
	up := strings.ToUpper(*id)
	return &up
}

func dateStr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format("2006-01-02")
	return &s
}

func tsStr(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func tsPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := tsStr(*t)
	return &s
}

func strPtr(s string) *string { return &s }

func prefixInfo(p store.Prefix) *PrefixInfo {
	return &PrefixInfo{
		CIDR:        p.Prefix.String(),
		Level:       p.Level,
		RIR:         rirName(p.RIR),
		Country:     p.Country,
		Status:      p.Status,
		Registered:  dateStr(p.Registered),
		Designation: p.Designation,
		RDAP:        p.RDAPURL,
	}
}

func chainItem(p store.Prefix) ChainItem {
	return ChainItem{
		CIDR:        p.Prefix.String(),
		Level:       p.Level,
		RIR:         rirName(p.RIR),
		Country:     p.Country,
		Status:      p.Status,
		Registered:  dateStr(p.Registered),
		Designation: p.Designation,
	}
}

func holderFromPrefix(p store.Prefix) *HolderInfo {
	if p.HolderKey == nil {
		return nil
	}
	return &HolderInfo{ID: *p.HolderKey, Name: p.HolderName, NameSource: p.HolderNameSource, Document: p.HolderDocument}
}

func listASNItem(a store.ASNBrief) ListASNItem {
	rir := a.RIR
	status := a.Status
	return ListASNItem{
		ASN: a.ASN, Name: a.Name, RIR: rirName(&rir), Country: a.Country, Status: &status,
		Registered: dateStr(a.Registered), Holder: a.HolderKey,
	}
}

func isDelegated(status *string) bool {
	return status != nil && (*status == "allocated" || *status == "assigned")
}
