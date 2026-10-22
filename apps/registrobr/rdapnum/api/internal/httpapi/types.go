package httpapi

import (
	"cmp"
	"net/netip"
	"slices"
	"strconv"
	"time"

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/store"
)

// Scan é o alvo que gerou o dado e o estado da coleta dele (substitui o bloco
// dataset das outras APIs).
type Scan struct {
	Kind        string  `json:"kind"`
	Target      string  `json:"target"`
	Source      string  `json:"source"`
	Active      bool    `json:"active"`
	CheckedAt   *string `json:"checked_at"`
	AttemptedAt *string `json:"attempted_at"`
	LastResult  *string `json:"last_result"`
	RetryAt     *string `json:"retry_at"`
	Response    *string `json:"response"`
}

// Entity é o titular ou um contato.
type Entity struct {
	Handle              string  `json:"handle"`
	Kind                *string `json:"kind"`
	Name                *string `json:"name"`
	Email               *string `json:"email"`
	Country             *string `json:"country"`
	Document            *string `json:"document"`
	DocumentType        *string `json:"document_type"`
	DocumentDigits      *string `json:"document_digits"`
	LegalRepresentative *string `json:"legal_representative"`
	RegisteredAt        *string `json:"registered_at"`
	ChangedAt           *string `json:"changed_at"`
}

// Contact é uma entidade com os papéis dela neste objeto.
type Contact struct {
	Entity
	Roles []string `json:"roles"`
	Via   *string  `json:"via"`
}

// NetworkBrief é um bloco na lista de um ASN.
type NetworkBrief struct {
	Prefix string   `json:"prefix"`
	Type   *string  `json:"type"`
	Status []string `json:"status"`
}

// ASNResponse é GET /asn/{asn}.
type ASNResponse struct {
	ASN          int64          `json:"asn"`
	Handle       *string        `json:"handle"`
	Name         *string        `json:"name"`
	Type         *string        `json:"type"`
	Country      *string        `json:"country"`
	RegisteredAt *string        `json:"registered_at"`
	ChangedAt    *string        `json:"changed_at"`
	Registrant   *Entity        `json:"registrant"`
	Contacts     []Contact      `json:"contacts"`
	Networks     []NetworkBrief `json:"networks"`
	Scan         Scan           `json:"scan"`
}

// Nameserver é um servidor de uma delegação reversa.
type Nameserver = store.Nameserver

// Reverse é uma delegação reversa.
type Reverse struct {
	Prefix       *string      `json:"prefix"`
	StartAddress string       `json:"start_address"`
	EndAddress   string       `json:"end_address"`
	Nameservers  []Nameserver `json:"nameservers"`
	Signed       *bool        `json:"signed"`
	RegisteredAt *string      `json:"registered_at"`
	ChangedAt    *string      `json:"changed_at"`
}

// NetworkResponse é GET /ip/{ip} e GET /prefix/{ip}/{len}.
type NetworkResponse struct {
	Query        string    `json:"query"`
	Prefix       string    `json:"prefix"`
	StartAddress string    `json:"start_address"`
	EndAddress   string    `json:"end_address"`
	IPVersion    int       `json:"ip_version"`
	Handle       *string   `json:"handle"`
	Name         *string   `json:"name"`
	Type         *string   `json:"type"`
	Country      *string   `json:"country"`
	Status       []string  `json:"status"`
	ParentHandle *string   `json:"parent_handle"`
	ASN          *int64    `json:"asn"`
	RegisteredAt *string   `json:"registered_at"`
	ChangedAt    *string   `json:"changed_at"`
	Registrant   *Entity   `json:"registrant"`
	Contacts     []Contact `json:"contacts"`
	Reverse      []Reverse `json:"reverse"`
	Scan         Scan      `json:"scan"`
}

// DocumentASN é um ASN de que o documento é titular.
type DocumentASN struct {
	ASN  int64           `json:"asn"`
	Name *string         `json:"name"`
	Scan DocumentASNScan `json:"scan"`
}

// DocumentASNScan é a data do dado do ASN.
type DocumentASNScan struct {
	CheckedAt *string `json:"checked_at"`
}

// DocumentNetwork é um bloco de que o documento é titular.
type DocumentNetwork struct {
	Prefix string   `json:"prefix"`
	ASN    *int64   `json:"asn"`
	Type   *string  `json:"type"`
	Status []string `json:"status"`
}

// DocumentResponse é GET /document/{document}.
type DocumentResponse struct {
	DocumentDigits string            `json:"document_digits"`
	DocumentType   string            `json:"document_type"`
	Entities       []Entity          `json:"entities"`
	ASNs           []DocumentASN     `json:"asns"`
	Networks       []DocumentNetwork `json:"networks"`
}

// ContactObject é um objeto em que o contato aparece.
type ContactObject struct {
	Kind   string   `json:"kind"`
	ASN    *int64   `json:"asn"`
	Prefix *string  `json:"prefix"`
	Roles  []string `json:"roles"`
	Via    *string  `json:"via"`
}

// ContactResponse é GET /contact/{handle}.
type ContactResponse struct {
	Entity  Entity          `json:"entity"`
	Objects []ContactObject `json:"objects"`
}

// HistoryItem é uma versão da resposta crua (sem egress_ip nem server_time).
type HistoryItem struct {
	UUID         string  `json:"uuid"`
	Result       string  `json:"result"`
	HTTPStatus   int16   `json:"http_status"`
	SHA256       string  `json:"sha256"`
	Bytes        int     `json:"bytes"`
	FetchedAt    string  `json:"fetched_at"`
	LastSeenAt   string  `json:"last_seen_at"`
	SeenCount    int     `json:"seen_count"`
	Processed    bool    `json:"processed"`
	ProcessError *string `json:"process_error"`
}

// HistoryResponse é GET /asn/{asn}/history e /prefix/{ip}/{len}/history.
type HistoryResponse struct {
	Target    string        `json:"target"`
	Kind      string        `json:"kind"`
	Current   *string       `json:"current"`
	Responses []HistoryItem `json:"responses"`
}

// NotFoundResponse é GET /response/{uuid} de uma resposta 404.
type NotFoundResponse struct {
	Result     string `json:"result"`
	HTTPStatus int16  `json:"http_status"`
}

// RefreshResponse é o 202 do POST .../refresh.
type RefreshResponse struct {
	Kind      string  `json:"kind"`
	Target    string  `json:"target"`
	Queued    bool    `json:"queued"`
	Reason    *string `json:"reason"`
	CheckedAt *string `json:"checked_at"`
}

// MetaResponse é GET /meta.
type MetaResponse struct {
	App         string           `json:"app"`
	Version     string           `json:"version"`
	Targets     MetaTargets      `json:"targets"`
	Responses   MetaResponses    `json:"responses"`
	Data        MetaData         `json:"data"`
	Config      MetaConfig       `json:"config"`
	Derivations []MetaDerivation `json:"derivations"`
	Workers     []MetaWorker     `json:"workers"`
	Collector   *MetaCollector   `json:"collector"`
	Refresh     MetaRefresh      `json:"refresh"`
}

// MetaTargets resume registrobr_rdapnum_target.
type MetaTargets struct {
	Total        int     `json:"total"`
	ASN          int     `json:"asn"`
	Prefix       int     `json:"prefix"`
	Active       int     `json:"active"`
	Static       int     `json:"static"`
	NeverScanned int     `json:"never_scanned"`
	Overdue      int     `json:"overdue"`
	Rescan       int     `json:"rescan"`
	Scanning     int     `json:"scanning"`
	Waiting      int     `json:"waiting"`
	OldestScanAt *string `json:"oldest_scan_at"`
	Complete     int     `json:"complete"`
	Truncated    int     `json:"truncated"`
	NotFound     int     `json:"not_found"`
	Error        int     `json:"error"`
}

// MetaResponses resume registrobr_rdapnum_response.
type MetaResponses struct {
	Total         int64 `json:"total"`
	Complete      int64 `json:"complete"`
	Truncated     int64 `json:"truncated"`
	NotFound      int64 `json:"not_found"`
	Pending       int64 `json:"pending"`
	ProcessErrors int64 `json:"process_errors"`
}

// MetaData conta as tabelas processadas.
type MetaData struct {
	Autnum  int64 `json:"autnum"`
	Network int64 `json:"network"`
	Entity  int64 `json:"entity"`
	Reverse int64 `json:"reverse"`
}

// MetaConfig é registrobr_rdapnum_config (null = cada coletor usa a reserva
// do ambiente).
type MetaConfig struct {
	RateLimit    *int    `json:"rate_limit"`
	ScanInterval *string `json:"scan_interval"`
}

// MetaDerivation é a última derivação aplicada.
type MetaDerivation struct {
	Source          string `json:"source"`
	Version         string `json:"version"`
	SourceVersion   string `json:"source_version"`
	SourceUpdatedAt string `json:"source_updated_at"`
	CreatedAt       string `json:"created_at"`
	ASNs            *int   `json:"asns"`
	Prefixes        *int   `json:"prefixes"`
	Inserted        *int   `json:"inserted"`
	NotInSource     *int   `json:"not_in_source"`
}

// MetaWorker é uma instância do coletor, sem o IP de saída.
type MetaWorker struct {
	Host        string  `json:"host"`
	Version     string  `json:"version"`
	StartedAt   string  `json:"started_at"`
	SeenAt      string  `json:"seen_at"`
	Alive       bool    `json:"alive"`
	RateLimit   int     `json:"rate_limit"`
	Proxy       bool    `json:"proxy"`
	Requests    int64   `json:"requests"`
	Complete    int64   `json:"complete"`
	Truncated   int64   `json:"truncated"`
	NotFound    int64   `json:"not_found"`
	Errors      int64   `json:"errors"`
	Refreshes   int64   `json:"refreshes"`
	EgressIPs   int     `json:"egress_ips"`
	PausedUntil *string `json:"paused_until"`
}

// MetaCollector é a linha do coletor em jobs.
type MetaCollector struct {
	App          string  `json:"app"`
	LastSyncAt   *string `json:"last_sync_at"`
	LastCheckAt  *string `json:"last_check_at"`
	Consolidated bool    `json:"consolidated"`
}

// MetaRefresh é o estado da fila de atualização.
type MetaRefresh struct {
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
	Upstream    string   `json:"upstream"`
	DerivedFrom []string `json:"derived_from"`
}

// --- conversões -------------------------------------------------------------

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

func scanInfo(t store.Target) Scan {
	return Scan{
		Kind: t.Kind, Target: t.Name(), Source: t.Source, Active: t.Active,
		CheckedAt: tsPtr(t.LastScanAt), AttemptedAt: tsPtr(t.LastAttemptAt),
		LastResult: strPtr(store.ResultName(t.LastResult)), RetryAt: tsPtr(t.RetryAt), Response: t.ResponseUUID,
	}
}

func entityInfo(e store.Entity) Entity {
	return Entity{
		Handle: e.Handle, Kind: e.Kind, Name: e.Name, Email: e.Email, Country: e.Country,
		Document: e.Document, DocumentType: e.DocumentType, DocumentDigits: e.DocumentDigits,
		LegalRepresentative: e.LegalRepresentative, RegisteredAt: tsPtr(e.RegisteredAt), ChangedAt: tsPtr(e.ChangedAt),
	}
}

func entityPtr(e *store.Entity) *Entity {
	if e == nil {
		return nil
	}
	v := entityInfo(*e)
	return &v
}

// contacts agrupa as ligações em um item por (handle, via), com os papéis em
// ordem alfabética, ordenados por via (null primeiro) e handle. O papel
// registrant fica de fora (o titular sai em registrant).
func contacts(links []store.Link) []Contact {
	type key struct{ handle, via string }
	idx := map[key]int{}
	out := []Contact{}
	for _, l := range links {
		if l.Role == "registrant" {
			continue
		}
		k := key{l.Entity.Handle, deref(l.Via)}
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, Contact{Entity: entityInfo(l.Entity), Roles: []string{}, Via: l.Via})
		}
		if !slices.Contains(out[i].Roles, l.Role) {
			out[i].Roles = append(out[i].Roles, l.Role)
		}
	}
	for i := range out {
		slices.Sort(out[i].Roles)
	}
	slices.SortStableFunc(out, func(a, b Contact) int {
		if c := cmpVia(a.Via, b.Via); c != 0 {
			return c
		}
		return cmp.Compare(a.Handle, b.Handle)
	})
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// cmpVia ordena null primeiro, depois em ordem de texto.
func cmpVia(a, b *string) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	}
	return cmp.Compare(*a, *b)
}

func asnResponse(asn int64, r *store.ASNRow) ASNResponse {
	resp := ASNResponse{ASN: asn, Contacts: []Contact{}, Networks: []NetworkBrief{}, Scan: scanInfo(r.Target)}
	a := r.Autnum
	if a == nil {
		return resp
	}
	resp.Handle, resp.Name, resp.Type, resp.Country = a.Handle, a.Name, a.Type, a.Country
	resp.RegisteredAt, resp.ChangedAt = tsPtr(a.RegisteredAt), tsPtr(a.ChangedAt)
	resp.Registrant = entityPtr(r.Registrant)
	resp.Contacts = contacts(r.Links)
	for _, n := range r.Networks {
		resp.Networks = append(resp.Networks, NetworkBrief{Prefix: n.Prefix.String(), Type: n.Type, Status: orEmpty(n.Status)})
	}
	return resp
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// lastAddr é o último endereço do prefixo.
func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Masked().Addr().AsSlice()
	for i := p.Bits(); i < len(b)*8; i++ {
		b[i/8] |= 0x80 >> (i % 8)
	}
	a, _ := netip.AddrFromSlice(b)
	return a
}

func networkResponse(query string, r *store.NetworkRow) NetworkResponse {
	n := r.Network
	version := 4
	if n.Prefix.Addr().Is6() {
		version = 6
	}
	resp := NetworkResponse{
		Query: query, Prefix: n.Prefix.String(), StartAddress: n.Prefix.Masked().Addr().String(),
		EndAddress: lastAddr(n.Prefix).String(), IPVersion: version,
		Handle: n.Handle, Name: n.Name, Type: n.Type, Country: n.Country, Status: orEmpty(n.Status),
		ParentHandle: n.ParentHandle, ASN: n.ASN, RegisteredAt: tsPtr(n.RegisteredAt), ChangedAt: tsPtr(n.ChangedAt),
		Registrant: entityPtr(r.Registrant), Contacts: contacts(r.Links), Reverse: []Reverse{}, Scan: scanInfo(r.Target),
	}
	for _, rv := range r.Reverses {
		var prefix *string
		if rv.Prefix != nil {
			s := rv.Prefix.String()
			prefix = &s
		}
		ns := rv.Nameservers
		if ns == nil {
			ns = []Nameserver{}
		}
		for i := range ns {
			ns[i].Status = orEmpty(ns[i].Status)
		}
		resp.Reverse = append(resp.Reverse, Reverse{
			Prefix: prefix, StartAddress: rv.Start, EndAddress: rv.End, Nameservers: ns, Signed: rv.Signed,
			RegisteredAt: tsPtr(rv.RegisteredAt), ChangedAt: tsPtr(rv.ChangedAt),
		})
	}
	return resp
}

func documentResponse(digits string, r *store.DocumentRow) DocumentResponse {
	kind := "cnpj"
	if len(digits) == 11 {
		kind = "cpf"
	}
	resp := DocumentResponse{DocumentDigits: digits, DocumentType: kind, Entities: []Entity{},
		ASNs: []DocumentASN{}, Networks: []DocumentNetwork{}}
	for _, e := range r.Entities {
		resp.Entities = append(resp.Entities, entityInfo(e))
	}
	for _, a := range r.ASNs {
		resp.ASNs = append(resp.ASNs, DocumentASN{ASN: a.ASN, Name: a.Name, Scan: DocumentASNScan{CheckedAt: tsPtr(a.LastScanAt)}})
	}
	for _, n := range r.Networks {
		resp.Networks = append(resp.Networks, DocumentNetwork{Prefix: n.Prefix.String(), ASN: n.ASN, Type: n.Type, Status: orEmpty(n.Status)})
	}
	return resp
}

// contactResponse agrupa as ligações em um item por (objeto, via), com os
// papéis em ordem alfabética, na ordem em que o banco as devolveu (tipo,
// objeto, via com null primeiro).
func contactResponse(r *store.ContactRow) ContactResponse {
	resp := ContactResponse{Entity: entityInfo(r.Entity), Objects: []ContactObject{}}
	idx := map[string]int{}
	for _, o := range r.Objects {
		obj := ContactObject{Kind: o.Kind, ASN: o.ASN, Roles: []string{}, Via: o.Via}
		id := o.Kind + "|"
		if o.ASN != nil {
			id += strconv.FormatInt(*o.ASN, 10)
		}
		if o.Prefix != nil {
			s := o.Prefix.String()
			obj.Prefix = &s
			id += s
		}
		id += "|" + deref(o.Via)
		if o.Via == nil {
			id += "\x00"
		}
		i, ok := idx[id]
		if !ok {
			i = len(resp.Objects)
			idx[id] = i
			resp.Objects = append(resp.Objects, obj)
		}
		if !slices.Contains(resp.Objects[i].Roles, o.Role) {
			resp.Objects[i].Roles = append(resp.Objects[i].Roles, o.Role)
		}
	}
	for i := range resp.Objects {
		slices.Sort(resp.Objects[i].Roles)
	}
	return resp
}

func historyResponse(t *store.Target, rows []store.ResponseMeta) HistoryResponse {
	resp := HistoryResponse{Target: t.Name(), Kind: t.Kind, Current: t.ResponseUUID, Responses: []HistoryItem{}}
	for _, r := range rows {
		res := r.Result
		resp.Responses = append(resp.Responses, HistoryItem{
			UUID: r.UUID, Result: store.ResultName(&res), HTTPStatus: r.HTTPStatus, SHA256: r.SHA256, Bytes: r.Bytes,
			FetchedAt: ts(r.FetchedAt), LastSeenAt: ts(r.LastSeenAt), SeenCount: r.SeenCount,
			Processed: r.Processed, ProcessError: r.ProcessError,
		})
	}
	return resp
}
