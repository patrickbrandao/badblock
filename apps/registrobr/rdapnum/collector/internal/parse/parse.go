// Package parse transforma uma resposta RDAP completa do registro.br nas
// linhas das tabelas processadas (dados-processados.md), pelas regras de
// fonte.md#regras-do-parser. Só lê: quem grava é o store.
//
// Subir Version numa versão nova do app refaz todo o dado processado a partir
// das respostas cruas, sem consultar o registro.br (decisão #38).
package parse

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Version é o PARSER_VERSION gravado em registrobr_rdapnum_response.parser_version.
const Version = 1

// ErrRefused marca uma resposta que o processamento recusa (process_error).
var ErrRefused = errors.New("resposta recusada")

// Entity é uma linha de registrobr_rdapnum_entity.
type Entity struct {
	Handle              string
	Kind                *string
	Name                *string
	Email               *string
	Country             *string
	Document            *string
	DocumentType        *string
	LegalRepresentative *string
	RegisteredAt        *time.Time
	ChangedAt           *time.Time
}

// Link é o papel de uma entidade no objeto (Via = handle do titular dentro
// do qual o contato veio; vazio = contato do próprio objeto).
type Link struct {
	Handle string
	Role   string
	Via    string
}

// Autnum é a linha de registrobr_rdapnum_autnum.
type Autnum struct {
	ASN          int64
	Handle       *string
	Name         *string
	Type         *string
	Country      *string
	RegisteredAt *time.Time
	ChangedAt    *time.Time
}

// Network é a linha de registrobr_rdapnum_network.
type Network struct {
	Prefix       netip.Prefix
	Handle       *string
	Name         *string
	Type         *string
	Country      *string
	ParentHandle *string
	Status       []string
	ASN          *int64
	RegisteredAt *time.Time
	ChangedAt    *time.Time
}

// Nameserver é um item de registrobr_rdapnum_reverse.nameservers.
type Nameserver struct {
	Name      string   `json:"name"`
	Status    []string `json:"status"`
	CheckedAt *string  `json:"checked_at"`
	LastOKAt  *string  `json:"last_ok_at"`
}

// Reverse é uma linha de registrobr_rdapnum_reverse.
type Reverse struct {
	Start        netip.Addr
	End          netip.Addr
	Prefix       *netip.Prefix // nil quando a faixa não é um CIDR exato
	Nameservers  []Nameserver  // em ordem de nome
	Signed       *bool
	RegisteredAt *time.Time
	ChangedAt    *time.Time
}

// Parsed é uma resposta completa lida.
type Parsed struct {
	Class      string // autnum ou ip network
	Autnum     *Autnum
	Network    *Network
	Entities   []Entity // uma por handle, em ordem de handle
	Links      []Link   // sem repetição, em ordem
	Registrant string   // handle do titular (vazio = sem titular)
	Reverses   []Reverse
}

// --- formato da resposta ---------------------------------------------------

type event struct {
	Action string   `json:"eventAction"`
	Date   string   `json:"eventDate"`
	Status []string `json:"status"`
}

type entityJSON struct {
	Handle              string          `json:"handle"`
	VCard               json.RawMessage `json:"vcardArray"`
	Roles               []string        `json:"roles"`
	PublicIDs           []publicID      `json:"publicIds"`
	LegalRepresentative *string         `json:"legalRepresentative"`
	Events              []event         `json:"events"`
	Entities            []entityJSON    `json:"entities"`
}

type publicID struct {
	Type       string `json:"type"`
	Identifier string `json:"identifier"`
}

type cidr0 struct {
	Length   json.RawMessage `json:"length"`
	V4Prefix string          `json:"v4prefix"`
	V6Prefix string          `json:"v6prefix"`
}

type reverseJSON struct {
	Start       string `json:"startAddress"`
	End         string `json:"endAddress"`
	Nameservers []struct {
		LDHName string  `json:"ldhName"`
		Events  []event `json:"events"`
	} `json:"nameservers"`
	Events    []event `json:"events"`
	SecureDNS *struct {
		DelegationSigned *bool `json:"delegationSigned"`
	} `json:"secureDNS"`
}

type responseJSON struct {
	ObjectClassName string        `json:"objectClassName"`
	Handle          *string       `json:"handle"`
	StartAutnum     *json.Number  `json:"startAutnum"`
	Name            *string       `json:"name"`
	Type            *string       `json:"type"`
	Country         *string       `json:"country"`
	StartAddress    string        `json:"startAddress"`
	EndAddress      string        `json:"endAddress"`
	ParentHandle    *string       `json:"parentHandle"`
	Status          []string      `json:"status"`
	NicbrAutnum     *json.Number  `json:"nicbr_autnum"`
	Cidr0           []cidr0       `json:"cidr0_cidrs"`
	Events          []event       `json:"events"`
	Entities        []entityJSON  `json:"entities"`
	Reverses        []reverseJSON `json:"nicbr_reverseDelegations"`
}

// Parse lê uma resposta completa. Erro = a resposta é recusada (wrap de
// ErrRefused) e o dado processado anterior continua.
func Parse(body []byte) (*Parsed, error) {
	var r responseJSON
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	if err := dec.Decode(&r); err != nil {
		return nil, refuse("JSON ilegível: %v", err)
	}
	p := &Parsed{Class: r.ObjectClassName}
	reg, chg := dates(r.Events)
	switch r.ObjectClassName {
	case "autnum":
		if r.StartAutnum == nil {
			return nil, refuse("startAutnum ausente")
		}
		asn, err := strconv.ParseInt(r.StartAutnum.String(), 10, 64)
		if err != nil || asn < 0 || asn > 4294967295 {
			return nil, refuse("startAutnum inválido: %s", r.StartAutnum.String())
		}
		p.Autnum = &Autnum{ASN: asn, Handle: text(r.Handle), Name: text(r.Name), Type: text(r.Type),
			Country: text(r.Country), RegisteredAt: reg, ChangedAt: chg}
	case "ip network":
		prefix, err := networkPrefix(r)
		if err != nil {
			return nil, err
		}
		n := &Network{Prefix: prefix, Handle: text(r.Handle), Name: text(r.Name), Type: text(r.Type),
			Country: text(r.Country), ParentHandle: text(r.ParentHandle), Status: cleanList(r.Status),
			RegisteredAt: reg, ChangedAt: chg}
		if r.NicbrAutnum != nil {
			asn, err := strconv.ParseInt(r.NicbrAutnum.String(), 10, 64)
			if err != nil || asn < 0 || asn > 4294967295 {
				return nil, refuse("nicbr_autnum inválido: %s", r.NicbrAutnum.String())
			}
			n.ASN = &asn
		}
		p.Network = n
		for _, rv := range r.Reverses {
			rev, err := reverse(rv)
			if err != nil {
				return nil, err
			}
			p.Reverses = append(p.Reverses, rev)
		}
	default:
		return nil, refuse("objectClassName inesperado: %q", r.ObjectClassName)
	}

	ents := map[string]*Entity{}
	links := map[Link]bool{}
	var walk func(list []entityJSON, via string) error
	walk = func(list []entityJSON, via string) error {
		for _, e := range list {
			h := strings.TrimSpace(e.Handle)
			if h == "" {
				continue // sem handle não há chave; fica na resposta crua
			}
			ent := entity(e)
			if cur, ok := ents[h]; ok {
				merge(cur, ent)
			} else {
				ents[h] = &ent
			}
			for _, role := range e.Roles {
				role = strings.TrimSpace(role)
				if role == "" {
					continue
				}
				links[Link{Handle: h, Role: role, Via: via}] = true
				if via == "" && role == "registrant" && p.Registrant == "" {
					p.Registrant = h
				}
			}
			if err := walk(e.Entities, h); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(r.Entities, ""); err != nil {
		return nil, err
	}
	for _, e := range ents {
		p.Entities = append(p.Entities, *e)
	}
	sort.Slice(p.Entities, func(i, j int) bool { return p.Entities[i].Handle < p.Entities[j].Handle })
	for l := range links {
		p.Links = append(p.Links, l)
	}
	sort.Slice(p.Links, func(i, j int) bool {
		a, b := p.Links[i], p.Links[j]
		if a.Handle != b.Handle {
			return a.Handle < b.Handle
		}
		if a.Role != b.Role {
			return a.Role < b.Role
		}
		return a.Via < b.Via
	})
	return p, nil
}

func refuse(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrRefused, fmt.Sprintf(format, args...))
}

// Reason é o texto que vai para process_error (sem o prefixo de ErrRefused).
func Reason(err error) string {
	return strings.TrimPrefix(err.Error(), ErrRefused.Error()+": ")
}

// text tira os espaços das pontas; vazio vira nil.
func text(s *string) *string {
	if s == nil {
		return nil
	}
	return str(*s)
}

func str(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func cleanList(in []string) []string {
	out := []string{}
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// dates lê registration e last changed de events (ausente ou inválida = nil).
func dates(evs []event) (reg, chg *time.Time) {
	for _, e := range evs {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(e.Date))
		if err != nil {
			continue
		}
		switch e.Action {
		case "registration":
			if reg == nil {
				reg = &t
			}
		case "last changed":
			if chg == nil {
				chg = &t
			}
		}
	}
	return reg, chg
}

// entity lê uma entidade (sem as aninhadas).
func entity(e entityJSON) Entity {
	ent := Entity{Handle: strings.TrimSpace(e.Handle), LegalRepresentative: text(e.LegalRepresentative)}
	ent.RegisteredAt, ent.ChangedAt = dates(e.Events)
	if len(e.PublicIDs) > 0 {
		ent.Document = str(e.PublicIDs[0].Identifier)
		if t := str(e.PublicIDs[0].Type); t != nil {
			low := strings.ToLower(*t)
			ent.DocumentType = &low
		}
	}
	if len(e.VCard) > 0 && string(e.VCard) != "null" {
		vcard(e.VCard, &ent)
	}
	return ent
}

// vcard lê kind, fn, o primeiro email e o país do adr de um jCard (RFC 7095).
// Propriedades fora do formato são ignoradas.
func vcard(raw json.RawMessage, ent *Entity) {
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil || len(arr) < 2 {
		return
	}
	var props [][]json.RawMessage
	if json.Unmarshal(arr[1], &props) != nil {
		return
	}
	for _, prop := range props {
		if len(prop) < 4 {
			continue
		}
		var name string
		if json.Unmarshal(prop[0], &name) != nil {
			continue
		}
		switch strings.ToLower(name) {
		case "kind", "fn", "email":
			var v string
			if json.Unmarshal(prop[3], &v) != nil {
				continue
			}
			switch strings.ToLower(name) {
			case "kind":
				if ent.Kind == nil {
					ent.Kind = str(v)
				}
			case "fn":
				if ent.Name == nil {
					ent.Name = str(v)
				}
			case "email":
				if ent.Email == nil {
					if s := str(v); s != nil {
						low := strings.ToLower(*s)
						ent.Email = &low
					}
				}
			}
		case "adr":
			var parts []json.RawMessage
			if json.Unmarshal(prop[3], &parts) != nil || len(parts) < 7 {
				continue
			}
			var country string
			if json.Unmarshal(parts[6], &country) != nil {
				continue
			}
			if ent.Country == nil {
				ent.Country = str(country)
			}
		}
	}
}

// merge completa cur com o que falta e veio em other (a mesma entidade em
// outro lugar da resposta).
func merge(cur *Entity, other Entity) {
	fill := func(dst **string, src *string) {
		if *dst == nil {
			*dst = src
		}
	}
	fill(&cur.Kind, other.Kind)
	fill(&cur.Name, other.Name)
	fill(&cur.Email, other.Email)
	fill(&cur.Country, other.Country)
	fill(&cur.Document, other.Document)
	fill(&cur.DocumentType, other.DocumentType)
	fill(&cur.LegalRepresentative, other.LegalRepresentative)
	if cur.RegisteredAt == nil {
		cur.RegisteredAt = other.RegisteredAt
	}
	if cur.ChangedAt == nil {
		cur.ChangedAt = other.ChangedAt
	}
}

// networkPrefix acha o bloco: o primeiro cidr0_cidrs; sem ele, o handle;
// sem os dois, a faixa startAddress–endAddress, que precisa ser um CIDR.
func networkPrefix(r responseJSON) (netip.Prefix, error) {
	if len(r.Cidr0) > 0 {
		c := r.Cidr0[0]
		addr := c.V4Prefix
		if addr == "" {
			addr = c.V6Prefix
		}
		var n int
		var s string
		if json.Unmarshal(c.Length, &s) == nil {
			n, _ = strconv.Atoi(s)
		} else {
			_ = json.Unmarshal(c.Length, &n)
		}
		if p, err := netip.ParsePrefix(strings.TrimSpace(addr) + "/" + strconv.Itoa(n)); err == nil && n > 0 {
			return p.Masked(), nil
		}
	}
	if r.Handle != nil {
		if p, err := netip.ParsePrefix(strings.TrimSpace(*r.Handle)); err == nil {
			return p.Masked(), nil
		}
	}
	start, err1 := netip.ParseAddr(strings.TrimSpace(r.StartAddress))
	end, err2 := netip.ParseAddr(strings.TrimSpace(r.EndAddress))
	if err1 != nil || err2 != nil {
		return netip.Prefix{}, refuse("bloco sem prefixo legível")
	}
	p, ok := RangePrefix(start, end)
	if !ok {
		return netip.Prefix{}, refuse("faixa não é CIDR: %s-%s", start, end)
	}
	return p, nil
}

func reverse(rv reverseJSON) (Reverse, error) {
	start, err1 := netip.ParseAddr(strings.TrimSpace(rv.Start))
	end, err2 := netip.ParseAddr(strings.TrimSpace(rv.End))
	if err1 != nil || err2 != nil {
		return Reverse{}, refuse("delegação reversa ilegível: %s-%s", rv.Start, rv.End)
	}
	rev := Reverse{Start: start.Unmap(), End: end.Unmap(), Nameservers: []Nameserver{}}
	if p, ok := RangePrefix(rev.Start, rev.End); ok {
		rev.Prefix = &p
	}
	rev.RegisteredAt, rev.ChangedAt = dates(rv.Events)
	if rv.SecureDNS != nil {
		rev.Signed = rv.SecureDNS.DelegationSigned
	}
	for _, ns := range rv.Nameservers {
		name := strings.TrimSpace(ns.LDHName)
		if name == "" {
			continue
		}
		n := Nameserver{Name: name, Status: []string{}}
		for _, e := range ns.Events {
			switch e.Action {
			case "delegation check":
				if n.CheckedAt == nil {
					n.CheckedAt = str(e.Date)
					n.Status = cleanList(e.Status)
				}
			case "last correct delegation check":
				if n.LastOKAt == nil {
					n.LastOKAt = str(e.Date)
				}
			}
		}
		rev.Nameservers = append(rev.Nameservers, n)
	}
	sort.SliceStable(rev.Nameservers, func(i, j int) bool { return rev.Nameservers[i].Name < rev.Nameservers[j].Name })
	return rev, nil
}

// RangePrefix devolve o CIDR exato que vai de start a end, se houver.
func RangePrefix(start, end netip.Addr) (netip.Prefix, bool) {
	start, end = start.Unmap(), end.Unmap()
	if start.BitLen() != end.BitLen() || end.Less(start) {
		return netip.Prefix{}, false
	}
	for bits := 0; bits <= start.BitLen(); bits++ {
		p := netip.PrefixFrom(start, bits).Masked()
		if p.Addr() != start {
			continue
		}
		if last(p) == end {
			return p, true
		}
	}
	return netip.Prefix{}, false
}

// last é o último endereço de um prefixo.
func last(p netip.Prefix) netip.Addr {
	b := p.Addr().AsSlice()
	for i := range b {
		bit := i * 8
		switch {
		case bit >= p.Bits():
			b[i] = 0xff
		case bit+8 > p.Bits():
			b[i] |= byte(0xff >> (p.Bits() - bit))
		}
	}
	a, _ := netip.AddrFromSlice(b)
	return a
}
