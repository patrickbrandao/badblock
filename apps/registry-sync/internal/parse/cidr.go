package parse

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"net/netip"
)

// IPv4RangeToCIDRs devolve a menor lista de CIDRs que cobre exatamente os
// count endereços a partir de start.
//
// Os arquivos delegated publicam IPv4 como (início, quantidade), e 3.787
// registros (medição de 2026-09-28) não formam um CIDR único: 768 endereços
// viram um /23 e um /24, por exemplo.
func IPv4RangeToCIDRs(start netip.Addr, count uint64) ([]netip.Prefix, error) {
	if !start.Is4() {
		return nil, fmt.Errorf("%s não é IPv4", start)
	}
	if count == 0 {
		return nil, fmt.Errorf("quantidade zero")
	}
	b := start.As4()
	first := uint64(binary.BigEndian.Uint32(b[:]))
	last := first + count - 1
	if last > 0xFFFFFFFF {
		return nil, fmt.Errorf("%s + %d passa do fim do espaço IPv4", start, count)
	}

	var out []netip.Prefix
	for cur := first; cur <= last; {
		// Maior bloco alinhado em cur (bit menos significativo ligado) que
		// ainda cabe até last. Para cur == 0 o alinhamento é o espaço todo.
		size := cur & -cur
		if cur == 0 {
			size = 1 << 32
		}
		for size > last-cur+1 {
			size >>= 1
		}
		var ip [4]byte
		binary.BigEndian.PutUint32(ip[:], uint32(cur))
		out = append(out, netip.PrefixFrom(netip.AddrFrom4(ip), 32-bits.TrailingZeros64(size)))
		cur += size
	}
	return out, nil
}
