package cache

import (
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Prefix é o início de todas as chaves. A versão do dataset entra na chave:
// quando o registry-sync publica uma versão nova, as chaves antigas deixam de
// ser lidas e expiram pelo TTL, sem varredura nem invalidação explícita.
func Prefix(version int64) string {
	return "bb:v1:" + strconv.FormatInt(version, 10) + ":"
}

// IPKey devolve a chave do lookup de um IP.
//
// Todos os IPs de um mesmo /24 (IPv4) ou /48 (IPv6) têm a mesma resposta,
// porque nenhuma delegação é menor que isso, exceto nos blocos listados em
// exceptions (special-purpose e as poucas delegações menores). Nesses, a
// chave é por IP.
func IPKey(version int64, ip netip.Addr, exceptions map[netip.Prefix]struct{}) string {
	bucket := Bucket(ip)
	if _, fragmented := exceptions[bucket]; fragmented {
		return Prefix(version) + "ip:" + ip.String()
	}
	return Prefix(version) + "ipb:" + bucket.String()
}

// Bucket devolve o /24 (IPv4) ou /48 (IPv6) que contém o IP.
func Bucket(ip netip.Addr) netip.Prefix {
	bits := 48
	if ip.Is4() {
		bits = 24
	}
	return netip.PrefixFrom(ip, bits).Masked()
}

// RequestKey devolve a chave de uma resposta completa: o caminho mais a query
// em ordem canônica, para que ?a=1&b=2 e ?b=2&a=1 compartilhem a entrada.
func RequestKey(version int64, path string, query url.Values) string {
	var b strings.Builder
	b.WriteString(Prefix(version))
	b.WriteString("req:")
	b.WriteString(path)
	if len(query) > 0 {
		keys := make([]string, 0, len(query))
		for k := range query {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('?')
		for i, k := range keys {
			vals := append([]string(nil), query[k]...)
			sort.Strings(vals)
			for j, v := range vals {
				if i > 0 || j > 0 {
					b.WriteByte('&')
				}
				b.WriteString(url.QueryEscape(k))
				b.WriteByte('=')
				b.WriteString(url.QueryEscape(v))
			}
		}
	}
	return b.String()
}
