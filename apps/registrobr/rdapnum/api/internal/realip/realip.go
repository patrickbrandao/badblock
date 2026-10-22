// Package realip descobre o IP real do cliente atrás do Traefik.
//
// X-Forwarded-For e X-Real-IP só são considerados quando a conexão vem de um
// proxy confiável (TRUSTED_PROXIES). O X-Forwarded-For é lido da direita para
// a esquerda, pulando os proxies confiáveis: o primeiro endereço não confiável
// é o cliente. Ler da esquerda seria aceitar o que o próprio cliente escreveu.
package realip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

// DefaultTrustedProxies são as faixas privadas e de loopback: a API não
// publica porta na internet, então só o Traefik (e outros containers das
// redes internas) chega até ela.
const DefaultTrustedProxies = "127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7"

// DefaultHeaders é a ordem padrão de leitura.
const DefaultHeaders = "X-Forwarded-For,X-Real-IP"

// Resolver aplica as regras de confiança.
type Resolver struct {
	trusted []netip.Prefix
	headers []string
}

// New cria um Resolver a partir de listas separadas por vírgula.
func New(trustedCSV, headersCSV string) (*Resolver, error) {
	r := &Resolver{}
	for item := range strings.SplitSeq(trustedCSV, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.Contains(item, "/") {
			a, err := netip.ParseAddr(item)
			if err != nil {
				return nil, fmt.Errorf("proxy confiável inválido %q", item)
			}
			item = netip.PrefixFrom(a, a.BitLen()).String()
		}
		p, err := netip.ParsePrefix(item)
		if err != nil {
			return nil, fmt.Errorf("faixa de proxy confiável inválida %q", item)
		}
		r.trusted = append(r.trusted, p.Masked())
	}
	for h := range strings.SplitSeq(headersCSV, ",") {
		h = http.CanonicalHeaderKey(strings.TrimSpace(h))
		switch h {
		case "":
		case "X-Forwarded-For", "X-Real-Ip":
			r.headers = append(r.headers, h)
		default:
			return nil, fmt.Errorf("header de IP real não suportado %q (use X-Forwarded-For e/ou X-Real-IP)", h)
		}
	}
	return r, nil
}

// ClientIP devolve o IP do cliente e de onde ele veio ("peer",
// "x-forwarded-for" ou "x-real-ip").
func (r *Resolver) ClientIP(req *http.Request) (netip.Addr, string) {
	peer, ok := parseHostPort(req.RemoteAddr)
	if !ok {
		return netip.Addr{}, "unknown"
	}
	if !r.isTrusted(peer) {
		return peer, "peer"
	}
	for _, h := range r.headers {
		switch h {
		case "X-Forwarded-For":
			if ip, ok := r.fromForwardedFor(req.Header.Values("X-Forwarded-For")); ok {
				return ip, "x-forwarded-for"
			}
		case "X-Real-Ip":
			if ip, ok := parseIP(req.Header.Get("X-Real-Ip")); ok {
				return ip, "x-real-ip"
			}
		}
	}
	return peer, "peer"
}

// fromForwardedFor percorre a lista da direita para a esquerda. Uma entrada
// inválida interrompe a leitura: dali para a esquerda nada é confiável.
func (r *Resolver) fromForwardedFor(values []string) (netip.Addr, bool) {
	var hops []string
	for _, v := range values {
		for item := range strings.SplitSeq(v, ",") {
			if item = strings.TrimSpace(item); item != "" {
				hops = append(hops, item)
			}
		}
	}
	var leftmost netip.Addr
	for _, hop := range slices.Backward(hops) {
		ip, ok := parseIP(hop)
		if !ok {
			return netip.Addr{}, false
		}
		if !r.isTrusted(ip) {
			return ip, true
		}
		leftmost = ip
	}
	// Todos os saltos são confiáveis: o cliente é interno.
	return leftmost, leftmost.IsValid()
}

func (r *Resolver) isTrusted(ip netip.Addr) bool {
	for _, p := range r.trusted {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func parseIP(v string) (netip.Addr, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return netip.Addr{}, false
	}
	if ip, err := netip.ParseAddr(v); err == nil {
		return normalize(ip)
	}
	return parseHostPort(v)
}

func parseHostPort(v string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(v)
	if err != nil {
		return netip.Addr{}, false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return normalize(ip)
}

func normalize(ip netip.Addr) (netip.Addr, bool) {
	if ip.Zone() != "" {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}
