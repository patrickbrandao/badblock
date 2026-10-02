package httpapi

import (
	"strconv"
	"time"

	"github.com/patrickbrandao/badblock/apps/rootanchors/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/rootanchors/api/internal/store"
)

// DatasetInfo acompanha toda resposta de dados.
type DatasetInfo struct {
	Version   string `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// TrustAnchor é o elemento TrustAnchor do arquivo aplicado (rootanchors_run).
type TrustAnchor struct {
	ID     string  `json:"id"`
	Source *string `json:"source"`
	Zone   string  `json:"zone"`
}

// Key é uma chave (um KeyDigest) nas respostas de dados.
//
// Não há campo "ativa": ele depende do relógio, e o corpo fica guardado no
// Valkey e no cache HTTP enquanto a versão dos dados não muda. O cliente
// calcula com valid_from e valid_until (ver specs/fontes/rootanchors/api.md).
type Key struct {
	KeyID      string  `json:"key_id"`
	KeyTag     int     `json:"key_tag"`
	Algorithm  int     `json:"algorithm"`
	DigestType int     `json:"digest_type"`
	Digest     string  `json:"digest"`
	DS         string  `json:"ds"`
	PublicKey  *string `json:"public_key"`
	Flags      *int    `json:"flags"`
	DNSKEY     *string `json:"dnskey"`
	ValidFrom  string  `json:"valid_from"`
	ValidUntil *string `json:"valid_until"`
	FirstSeen  string  `json:"first_seen"`
	UpdatedAt  string  `json:"updated_at"`
}

// KeysResponse é GET /keys.
type KeysResponse struct {
	TrustAnchor TrustAnchor `json:"trust_anchor"`
	Count       int         `json:"count"`
	Keys        []Key       `json:"keys"`
	Dataset     DatasetInfo `json:"dataset"`
}

// KeyTagResponse é GET /key/{key_tag}.
type KeyTagResponse struct {
	KeyTag      int         `json:"key_tag"`
	TrustAnchor TrustAnchor `json:"trust_anchor"`
	Count       int         `json:"count"`
	Keys        []Key       `json:"keys"`
	Dataset     DatasetInfo `json:"dataset"`
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
	Version     string      `json:"version"`
	UpdatedAt   string      `json:"updated_at"`
	Source      string      `json:"source"`
	SHA256      string      `json:"sha256"`
	Keys        int         `json:"keys"`
	TrustAnchor TrustAnchor `json:"trust_anchor"`
}

// MetaCollector é a linha do collector-rootanchors em jobs.
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

func trustAnchor(d *store.Dataset) TrustAnchor {
	return TrustAnchor{ID: d.AnchorID, Source: d.AnchorSource, Zone: d.Zone}
}

// dnskeyProtocol é o campo protocolo do DNSKEY, sempre 3 (RFC 4034, 2.1.2).
const dnskeyProtocol = 3

// keyOf monta a chave da resposta, com o DS e o DNSKEY em texto de arquivo de
// zona (owner, classe IN, tipo e RDATA separados por um espaço, sem TTL):
//
//	. IN DS 20326 8 2 E06D44B80B8F1D39A95C0B0D7C65D08458E880409BBC683457104237C7F8EC8D
//	. IN DNSKEY 257 3 8 AwEAAaz/tAm8yTn4Mfeh...
//
// owner é a zona do TrustAnchor (sempre "."). dnskey é null quando a chave não
// traz PublicKey.
func keyOf(owner string, k store.Key) Key {
	out := Key{
		KeyID:      k.KeyID,
		KeyTag:     k.KeyTag,
		Algorithm:  k.Algorithm,
		DigestType: k.DigestType,
		Digest:     k.Digest,
		DS: owner + " IN DS " + strconv.Itoa(k.KeyTag) + " " + strconv.Itoa(k.Algorithm) + " " +
			strconv.Itoa(k.DigestType) + " " + k.Digest,
		PublicKey:  k.PublicKey,
		Flags:      k.Flags,
		ValidFrom:  ts(k.ValidFrom),
		ValidUntil: tsPtr(k.ValidUntil),
		FirstSeen:  ts(k.CreatedAt),
		UpdatedAt:  ts(k.UpdatedAt),
	}
	if k.PublicKey != nil && k.Flags != nil {
		s := owner + " IN DNSKEY " + strconv.Itoa(*k.Flags) + " " + strconv.Itoa(dnskeyProtocol) + " " +
			strconv.Itoa(k.Algorithm) + " " + *k.PublicKey
		out.DNSKEY = &s
	}
	return out
}

func keysOf(owner string, list []store.Key) []Key {
	out := make([]Key, 0, len(list))
	for _, k := range list {
		out = append(out, keyOf(owner, k))
	}
	return out
}
