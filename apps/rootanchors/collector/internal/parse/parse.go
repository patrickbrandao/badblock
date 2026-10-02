// Package parse lê o root-anchors.xml da IANA (âncoras de confiança DNSSEC da
// zona raiz) e valida cada chave antes de ela chegar ao banco.
//
// O arquivo é pequeno (um TrustAnchor com poucos KeyDigest) e crítico: não há
// "linha descartada". Qualquer regra que falhe recusa o arquivo inteiro. As
// regras estão em specs/fontes/rootanchors/fonte.md.
package parse

import (
	"bytes"
	"crypto/sha1" // DigestType 1 do DS é SHA-1 (RFC 4034): só conferimos o que a IANA publica
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"hash"
	"io"
	"strconv"
	"strings"
	"time"
)

// MaxWarnings é quantos avisos o parser guarda (o total é contado à parte).
const MaxWarnings = 50

// Key é um elemento KeyDigest já validado.
type Key struct {
	ID         string     // atributo id (chave natural)
	KeyTag     int        // 0 a 65535
	Algorithm  int        // 0 a 255
	DigestType int        // 1, 2 ou 4
	Digest     string     // hex maiúsculo, com o tamanho do DigestType
	PublicKey  string     // base64 sem espaços; "" = o KeyDigest não traz PublicKey
	Flags      *int       // nil junto com PublicKey vazio
	ValidFrom  time.Time  // UTC
	ValidUntil *time.Time // nil = sem data de fim; UTC
}

// Dataset é o arquivo inteiro.
type Dataset struct {
	AnchorID string // atributo id do TrustAnchor
	Source   string // atributo source do TrustAnchor
	Zone     string // sempre "."
	Keys     []Key  // na ordem do arquivo

	Warnings     []string // os MaxWarnings primeiros
	warningCount int
}

// WarningCount é o total de avisos, inclusive os que não foram guardados.
func (d *Dataset) WarningCount() int { return d.warningCount }

func (d *Dataset) warn(format string, args ...any) {
	d.warningCount++
	if len(d.Warnings) < MaxWarnings {
		d.Warnings = append(d.Warnings, fmt.Sprintf(format, args...))
	}
}

// Tamanho em dígitos hexadecimais de cada DigestType aceito.
var digestHexLen = map[int]int{1: 40, 2: 64, 4: 96}

func newDigest(t int) hash.Hash {
	switch t {
	case 1:
		return sha1.New()
	case 2:
		return sha256.New()
	default:
		return sha512.New384()
	}
}

// Bit Zone Key do campo flags do DNSKEY (RFC 4034, 2.1.1).
const flagZoneKey = 0x0100

type xmlAny struct {
	XMLName xml.Name
}

type xmlAnchor struct {
	XMLName xml.Name   `xml:"TrustAnchor"`
	ID      string     `xml:"id,attr"`
	Source  string     `xml:"source,attr"`
	Zone    []string   `xml:"Zone"`
	Keys    []xmlKey   `xml:"KeyDigest"`
	Attrs   []xml.Attr `xml:",any,attr"`
	Extra   []xmlAny   `xml:",any"`
}

type xmlKey struct {
	ID         *string    `xml:"id,attr"`
	ValidFrom  *string    `xml:"validFrom,attr"`
	ValidUntil *string    `xml:"validUntil,attr"`
	KeyTag     []string   `xml:"KeyTag"`
	Algorithm  []string   `xml:"Algorithm"`
	DigestType []string   `xml:"DigestType"`
	Digest     []string   `xml:"Digest"`
	PublicKey  []string   `xml:"PublicKey"`
	Flags      []string   `xml:"Flags"`
	Attrs      []xml.Attr `xml:",any,attr"`
	Extra      []xmlAny   `xml:",any"`
}

// Parse lê o arquivo inteiro. Devolve erro (e nenhum dataset) quando o XML
// não é um TrustAnchor da raiz bem formado ou quando alguma chave não passa
// nas regras.
func Parse(r io.Reader) (*Dataset, error) {
	dec := xml.NewDecoder(r)
	var doc xmlAnchor
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("XML: arquivo vazio")
		}
		return nil, fmt.Errorf("XML: %w", err)
	}
	if err := trailing(dec); err != nil {
		return nil, err
	}

	ds := &Dataset{AnchorID: strings.TrimSpace(doc.ID), Source: strings.TrimSpace(doc.Source)}
	if ds.AnchorID == "" {
		return nil, errors.New("sem o atributo id no TrustAnchor")
	}
	switch len(doc.Zone) {
	case 0:
		return nil, errors.New("sem o elemento Zone no TrustAnchor")
	case 1:
	default:
		return nil, fmt.Errorf("%d elementos Zone no TrustAnchor", len(doc.Zone))
	}
	if ds.Zone = strings.TrimSpace(doc.Zone[0]); ds.Zone != "." {
		return nil, fmt.Errorf("zona %q: só a raiz (\".\") é aceita", ds.Zone)
	}
	for _, a := range doc.Attrs {
		ds.warn("TrustAnchor: atributo desconhecido %s ignorado", a.Name.Local)
	}
	for _, e := range doc.Extra {
		ds.warn("TrustAnchor: elemento desconhecido <%s> ignorado", e.XMLName.Local)
	}
	if len(doc.Keys) == 0 {
		return nil, errors.New("nenhum KeyDigest no arquivo")
	}

	seen := map[string]bool{}
	for i, xk := range doc.Keys {
		k, err := key(xk)
		label := fmt.Sprintf("KeyDigest %d", i+1)
		if xk.ID != nil && strings.TrimSpace(*xk.ID) != "" {
			label = "KeyDigest " + strings.TrimSpace(*xk.ID)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", label, err)
		}
		if seen[k.ID] {
			return nil, fmt.Errorf("%s: id repetido", label)
		}
		seen[k.ID] = true
		for _, a := range xk.Attrs {
			ds.warn("%s: atributo desconhecido %s ignorado", label, a.Name.Local)
		}
		for _, e := range xk.Extra {
			ds.warn("%s: elemento desconhecido <%s> ignorado", label, e.XMLName.Local)
		}
		ds.Keys = append(ds.Keys, *k)
	}
	return ds, nil
}

// trailing recusa qualquer elemento ou texto depois de </TrustAnchor>
// (comentários, instruções de processamento e espaços são aceitos).
func trailing(dec *xml.Decoder) error {
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			return fmt.Errorf("XML: elemento <%s> depois de </TrustAnchor>", t.Name.Local)
		case xml.CharData:
			if len(bytes.TrimSpace(t)) > 0 {
				return errors.New("XML: texto depois de </TrustAnchor>")
			}
		}
	}
}

// one exige exatamente uma ocorrência de um elemento obrigatório.
func one(name string, v []string) (string, error) {
	switch len(v) {
	case 0:
		return "", fmt.Errorf("sem o elemento %s", name)
	case 1:
		return strings.TrimSpace(v[0]), nil
	default:
		return "", fmt.Errorf("elemento %s repetido", name)
	}
}

// optional aceita zero ou uma ocorrência.
func optional(name string, v []string) (string, bool, error) {
	if len(v) == 0 {
		return "", false, nil
	}
	s, err := one(name, v)
	return s, true, err
}

func uintField(name, s string, bits int) (int, error) {
	n, err := strconv.ParseUint(s, 10, bits)
	if err != nil {
		return 0, fmt.Errorf("%s inválido: %q", name, s)
	}
	return int(n), nil
}

func timeAttr(name, s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, fmt.Errorf("%s inválido: %q", name, s)
	}
	return t.UTC(), nil
}

func stripSpace(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r':
			return -1
		}
		return r
	}, s)
}

func key(xk xmlKey) (*Key, error) {
	k := &Key{}
	if xk.ID == nil || strings.TrimSpace(*xk.ID) == "" {
		return nil, errors.New("sem o atributo id")
	}
	k.ID = strings.TrimSpace(*xk.ID)

	if xk.ValidFrom == nil {
		return nil, errors.New("sem o atributo validFrom")
	}
	var err error
	if k.ValidFrom, err = timeAttr("validFrom", *xk.ValidFrom); err != nil {
		return nil, err
	}
	if xk.ValidUntil != nil {
		until, err := timeAttr("validUntil", *xk.ValidUntil)
		if err != nil {
			return nil, err
		}
		if until.Before(k.ValidFrom) {
			return nil, fmt.Errorf("validUntil %s anterior a validFrom %s",
				until.Format(time.RFC3339), k.ValidFrom.Format(time.RFC3339))
		}
		k.ValidUntil = &until
	}

	s, err := one("KeyTag", xk.KeyTag)
	if err != nil {
		return nil, err
	}
	if k.KeyTag, err = uintField("KeyTag", s, 16); err != nil {
		return nil, err
	}
	if s, err = one("Algorithm", xk.Algorithm); err != nil {
		return nil, err
	}
	if k.Algorithm, err = uintField("Algorithm", s, 8); err != nil {
		return nil, err
	}
	if s, err = one("DigestType", xk.DigestType); err != nil {
		return nil, err
	}
	if k.DigestType, err = uintField("DigestType", s, 8); err != nil {
		return nil, err
	}
	want, ok := digestHexLen[k.DigestType]
	if !ok {
		return nil, fmt.Errorf("tipo de digest %d não suportado (DigestType aceitos: 1, 2, 4)", k.DigestType)
	}
	if s, err = one("Digest", xk.Digest); err != nil {
		return nil, err
	}
	k.Digest = strings.ToUpper(stripSpace(s))
	if _, err := hex.DecodeString(k.Digest); err != nil || k.Digest == "" {
		return nil, fmt.Errorf("digest não é hexadecimal: %q", s)
	}
	if len(k.Digest) != want {
		return nil, fmt.Errorf("digest com %d dígitos hex; DigestType %d pede %d", len(k.Digest), k.DigestType, want)
	}

	pub, hasPub, err := optional("PublicKey", xk.PublicKey)
	if err != nil {
		return nil, err
	}
	flags, hasFlags, err := optional("Flags", xk.Flags)
	if err != nil {
		return nil, err
	}
	switch {
	case hasPub && !hasFlags:
		return nil, errors.New("elemento PublicKey sem Flags")
	case hasFlags && !hasPub:
		return nil, errors.New("elemento Flags sem PublicKey")
	case !hasPub:
		return k, nil
	}

	f, err := uintField("Flags", flags, 16)
	if err != nil {
		return nil, err
	}
	if f&flagZoneKey == 0 {
		return nil, fmt.Errorf("flags %d sem o bit Zone Key (256)", f)
	}
	k.Flags = &f
	k.PublicKey = stripSpace(pub)
	raw, err := base64.StdEncoding.DecodeString(k.PublicKey)
	if err != nil || len(raw) == 0 {
		return nil, errors.New("PublicKey: não é base64 válido")
	}

	rdata := DNSKEYRData(f, k.Algorithm, raw)
	if tag := KeyTag(rdata); tag != k.KeyTag {
		return nil, fmt.Errorf("key tag %d não bate com o calculado da PublicKey (%d)", k.KeyTag, tag)
	}
	if d := DSDigest(k.DigestType, rdata); d != k.Digest {
		return nil, fmt.Errorf("digest não bate com o calculado da PublicKey (%s)", d)
	}
	return k, nil
}

// DNSKEYRData monta o RDATA do DNSKEY: flags (2 bytes) | protocolo 3 (1) |
// algoritmo (1) | chave pública (RFC 4034, 2.1).
func DNSKEYRData(flags, algorithm int, publicKey []byte) []byte {
	rdata := make([]byte, 4, 4+len(publicKey))
	binary.BigEndian.PutUint16(rdata, uint16(flags))
	rdata[2] = 3
	rdata[3] = byte(algorithm)
	return append(rdata, publicKey...)
}

// KeyTag calcula o key tag do RDATA de um DNSKEY (RFC 4034, apêndice B;
// o algoritmo 1, RSA/MD5, usa a regra própria do apêndice B.1).
func KeyTag(rdata []byte) int {
	if len(rdata) > 4 && rdata[3] == 1 {
		if len(rdata) < 7 {
			return 0
		}
		return int(rdata[len(rdata)-3])<<8 | int(rdata[len(rdata)-2])
	}
	var ac uint32
	for i, b := range rdata {
		if i&1 == 1 {
			ac += uint32(b)
		} else {
			ac += uint32(b) << 8
		}
	}
	ac += ac >> 16 & 0xFFFF
	return int(ac & 0xFFFF)
}

// DSDigest calcula o digest do registro DS da raiz (RFC 4034, 5.1.4): o hash
// do nome do dono em wire format — a raiz é um único byte 0x00 — seguido do
// RDATA do DNSKEY. Devolve hex maiúsculo.
func DSDigest(digestType int, rdata []byte) string {
	h := newDigest(digestType)
	h.Write([]byte{0x00})
	h.Write(rdata)
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil)))
}
