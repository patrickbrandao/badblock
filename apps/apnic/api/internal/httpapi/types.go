package httpapi

import (
	"cmp"
	"net/netip"
	"slices"
	"time"

	"github.com/patrickbrandao/badblock/apps/apnic/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/apnic/api/internal/store"
)

// DatasetInfo acompanha toda resposta de dados.
type DatasetInfo struct {
	Version   string `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// Delegation são os campos de delegação do registro (iguais em ASNs e
// blocos). cc, reg_date e opaque_id são null quando vazios na fonte.
type Delegation struct {
	CC       *string `json:"cc"`
	RegDate  *string `json:"reg_date"`
	Status   string  `json:"status"`
	OpaqueID *string `json:"opaque_id"`
}

// Range é a faixa de ASNs de um registro.
type Range struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	Count int64 `json:"count"`
}

// Record é o registro original de um bloco: IPv4 = endereço inicial e
// quantidade de endereços; IPv6 = endereço inicial e tamanho do prefixo.
type Record struct {
	Start string `json:"start"`
	Value int64  `json:"value"`
}

// ASNResponse é GET /asn/{asn}.
type ASNResponse struct {
	ASN   int64 `json:"asn"`
	Range Range `json:"range"`
	Delegation
	FirstSeen string      `json:"first_seen"`
	UpdatedAt string      `json:"updated_at"`
	Dataset   DatasetInfo `json:"dataset"`
}

// IPResponse é GET /ip/{ip}.
type IPResponse struct {
	IP     string `json:"ip"`
	Prefix string `json:"prefix"`
	Delegation
	Record  Record      `json:"record"`
	Dataset DatasetInfo `json:"dataset"`
}

// PrefixResponse é GET /prefix/{ip}/{len}.
type PrefixResponse struct {
	Query  string `json:"query"`
	Prefix string `json:"prefix"`
	Exact  bool   `json:"exact"`
	Delegation
	Record  Record      `json:"record"`
	Dataset DatasetInfo `json:"dataset"`
}

// HolderResponse é GET /holder/{opaque_id}.
type HolderResponse struct {
	OpaqueID string         `json:"opaque_id"`
	CC       *string        `json:"cc"`
	CCs      []string       `json:"ccs"`
	Counts   HolderCounts   `json:"counts"`
	ASNs     []HolderASN    `json:"asns"`
	Prefixes HolderPrefixes `json:"prefixes"`
	Dataset  DatasetInfo    `json:"dataset"`
}

// HolderCounts é o tamanho de cada lista do titular.
type HolderCounts struct {
	ASNs int `json:"asns"` // registros (faixas) de ASN
	IPv4 int `json:"ipv4"` // blocos IPv4
	IPv6 int `json:"ipv6"` // blocos IPv6
}

// HolderASN é um registro de ASN do titular.
type HolderASN struct {
	Start   int64   `json:"start"`
	End     int64   `json:"end"`
	Count   int64   `json:"count"`
	CC      *string `json:"cc"`
	Status  string  `json:"status"`
	RegDate *string `json:"reg_date"`
}

// HolderPrefix é um bloco do titular.
type HolderPrefix struct {
	Prefix  string  `json:"prefix"`
	CC      *string `json:"cc"`
	Status  string  `json:"status"`
	RegDate *string `json:"reg_date"`
}

// HolderPrefixes separa os blocos do titular por família.
type HolderPrefixes struct {
	IPv4 []HolderPrefix `json:"ipv4"`
	IPv6 []HolderPrefix `json:"ipv6"`
}

// MetaResponse é GET /meta.
type MetaResponse struct {
	App       string         `json:"app"`
	Version   string         `json:"version"`
	Dataset   *MetaDataset   `json:"dataset"`
	Collector *MetaCollector `json:"collector"`
}

// MetaDataset descreve o arquivo aplicado (última linha status = 1 da
// tabela de execuções do collector).
type MetaDataset struct {
	Version     string  `json:"version"`
	UpdatedAt   string  `json:"updated_at"`
	Source      string  `json:"source"`
	SHA256      *string `json:"sha256"`
	MD5         *string `json:"md5"`
	Serial      *string `json:"serial"`
	StartDate   *string `json:"start_date"`
	EndDate     *string `json:"end_date"`
	ASNRecords  *int    `json:"asn_records"`
	IPv4Records *int    `json:"ipv4_records"`
	IPv6Records *int    `json:"ipv6_records"`
	PrefixesV4  *int    `json:"prefixes_v4"`
	PrefixesV6  *int    `json:"prefixes_v6"`
}

// MetaCollector é a linha do collector em jobs.
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
	Registry  string   `json:"registry"`
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

// datePtr formata uma coluna date como AAAA-MM-DD (null se NULL).
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

func delegation(i store.Info) Delegation {
	return Delegation{CC: i.CC, RegDate: datePtr(i.RegDate), Status: i.Status, OpaqueID: i.OpaqueID}
}

func record(b *store.Block) Record {
	return Record{Start: b.RecordStart.String(), Value: b.RecordValue}
}

// holderResponse monta a resposta do titular. cc é o país mais frequente
// entre os recursos listados (cada registro de ASN e cada bloco conta 1;
// empate = ordem alfabética) e ccs lista todos os países, do mais frequente
// ao menos; recursos sem país não contam.
func holderResponse(h *store.Holder, snap dataset.Snapshot) HolderResponse {
	resp := HolderResponse{
		OpaqueID: h.OpaqueID,
		CCs:      []string{},
		ASNs:     make([]HolderASN, 0, len(h.ASNs)),
		Prefixes: HolderPrefixes{IPv4: []HolderPrefix{}, IPv6: []HolderPrefix{}},
		Dataset:  datasetInfo(snap),
	}
	freq := map[string]int{}
	for _, a := range h.ASNs {
		resp.ASNs = append(resp.ASNs, HolderASN{
			Start: a.Start, End: a.End, Count: a.Count,
			CC: a.Info.CC, Status: a.Info.Status, RegDate: datePtr(a.Info.RegDate),
		})
		if a.Info.CC != nil {
			freq[*a.Info.CC]++
		}
	}
	for _, b := range h.Blocks {
		p := HolderPrefix{Prefix: b.Prefix.String(), CC: b.Info.CC, Status: b.Info.Status, RegDate: datePtr(b.Info.RegDate)}
		if is4(b.Prefix) {
			resp.Prefixes.IPv4 = append(resp.Prefixes.IPv4, p)
		} else {
			resp.Prefixes.IPv6 = append(resp.Prefixes.IPv6, p)
		}
		if b.Info.CC != nil {
			freq[*b.Info.CC]++
		}
	}
	for cc := range freq {
		resp.CCs = append(resp.CCs, cc)
	}
	slices.SortFunc(resp.CCs, func(x, y string) int {
		return cmp.Or(cmp.Compare(freq[y], freq[x]), cmp.Compare(x, y))
	})
	if len(resp.CCs) > 0 {
		cc := resp.CCs[0]
		resp.CC = &cc
	}
	resp.Counts = HolderCounts{ASNs: len(resp.ASNs), IPv4: len(resp.Prefixes.IPv4), IPv6: len(resp.Prefixes.IPv6)}
	return resp
}

func is4(p netip.Prefix) bool { return p.Addr().Is4() }
