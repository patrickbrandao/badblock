package httpapi

import (
	"time"

	"github.com/patrickbrandao/badblock/apps/anatel/pst/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/api/internal/store"
)

// DatasetInfo acompanha toda resposta de dados.
type DatasetInfo struct {
	Version   string `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// ProviderResponse é GET /provider/{cnpj}: a prestadora e todos os seus
// serviços.
type ProviderResponse struct {
	Document     string      `json:"document"`
	Name         string      `json:"name"`
	TradeName    *string     `json:"trade_name"`
	Address      Address     `json:"address"`
	Phone        *string     `json:"phone"`
	Email        *string     `json:"email"`
	FirstSeen    string      `json:"first_seen"`
	UpdatedAt    string      `json:"updated_at"`
	ServiceCodes []string    `json:"service_codes"`
	Services     []Service   `json:"services"`
	Dataset      DatasetInfo `json:"dataset"`
}

// Address é o endereço da sede da prestadora.
type Address struct {
	Street       *string `json:"street"`
	Number       *string `json:"number"`
	Complement   *string `json:"complement"`
	District     *string `json:"district"`
	PostalCode   *string `json:"postal_code"`
	CityIBGECode *int    `json:"city_ibge_code"`
	City         *string `json:"city"`
	State        *string `json:"state"`
}

// Service é um serviço notificado pela prestadora, com a outorga.
type Service struct {
	ServiceCode         string  `json:"service_code"`
	ServiceName         string  `json:"service_name"`
	ServiceGroup        string  `json:"service_group"`
	NotificationFistel  string  `json:"notification_fistel"`
	NotificationProcess *string `json:"notification_process"`
	NotifiedOn          *string `json:"notified_on"`
	EntityType          string  `json:"entity_type"`
	GrantType           string  `json:"grant_type"`
	GrantFistel         *string `json:"grant_fistel"`
	GrantProcess        *string `json:"grant_process"`
	GrantedOn           *string `json:"granted_on"`
}

// ServicesResponse é GET /services: o catálogo de serviços.
type ServicesResponse struct {
	Count    int              `json:"count"`
	Services []ServiceSummary `json:"services"`
	Dataset  DatasetInfo      `json:"dataset"`
}

// ServiceSummary é um código de serviço no catálogo.
type ServiceSummary struct {
	ServiceCode  string `json:"service_code"`
	ServiceName  string `json:"service_name"`
	ServiceGroup string `json:"service_group"`
	Providers    int    `json:"providers"`
	Services     int    `json:"services"`
}

// ServiceResponse é GET /service/{code}: as prestadoras de um serviço.
type ServiceResponse struct {
	ServiceCode  string          `json:"service_code"`
	ServiceName  string          `json:"service_name"`
	ServiceGroup string          `json:"service_group"`
	State        *string         `json:"state"`
	Count        int             `json:"count"`
	Providers    []ProviderBrief `json:"providers"`
	Dataset      DatasetInfo     `json:"dataset"`
}

// SearchResponse é GET /search.
type SearchResponse struct {
	Query     string          `json:"query"`
	Limit     int             `json:"limit"`
	Count     int             `json:"count"`
	Truncated bool            `json:"truncated"`
	Providers []ProviderBrief `json:"providers"`
	Dataset   DatasetInfo     `json:"dataset"`
}

// ProviderBrief é uma prestadora numa lista.
type ProviderBrief struct {
	Document  string  `json:"document"`
	Name      string  `json:"name"`
	TradeName *string `json:"trade_name"`
	City      *string `json:"city"`
	State     *string `json:"state"`
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
	Version       string  `json:"version"`
	UpdatedAt     string  `json:"updated_at"`
	Source        string  `json:"source"`
	SHA256        string  `json:"sha256"`
	CSVSHA256     *string `json:"csv_sha256"`
	CSVModifiedAt *string `json:"csv_modified_at"`
	Providers     *int    `json:"providers"`
	Services      *int    `json:"services"`
}

// MetaCollector é a linha do collector-anatel-pst em jobs.
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

// datePtr formata uma coluna date como AAAA-MM-DD (o pgx a lê em UTC).
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

func providerBriefs(list []store.ProviderBrief) []ProviderBrief {
	out := make([]ProviderBrief, 0, len(list))
	for _, p := range list {
		out = append(out, ProviderBrief(p))
	}
	return out
}
