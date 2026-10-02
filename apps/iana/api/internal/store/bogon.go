package store

// Bogon diz se o IP ou prefixo consultado é bogon segundo os registros da
// IANA: endereço que não deve aparecer como origem ou destino na Internet
// pública. A regra (specs/fontes/iana/api.md, "Regra de bogon") é avaliada
// em ordem:
//
//  1. Special-purpose registries: entre os blocos especiais em vigor
//     (termination_date NULL) que contêm a consulta, o mais específico decide
//     quando tem globally_reachable preenchido: false = bogon, true = não
//     bogon. É o que a IANA manda nas notas de rodapé ("unless allowed by a
//     more specific allocation"): 192.0.0.9/32 (true) vale dentro de
//     192.0.0.0/24 (false). Se o mais específico tem globally_reachable NULL
//     (N/A: TEREDO 2001::/32, 6to4 2002::/16), a IANA não decide ali e vale a
//     regra 2. Blocos encerrados (192.88.99.0/24, 2001:10::/28) não contam.
//  2. Alocação da IANA: bogon se nenhum bloco de iana_prefix_block que cruza a
//     consulta tem status diferente de RESERVED — isto é, a consulta está toda
//     em blocos RESERVED (0/8, 10/8, 127/8, multicast 224–239, 240–255,
//     3ffe::/16...) ou fora de qualquer bloco (IPv6 que a IANA não entregou a
//     ninguém: multicast ff00::/8, 2000::/16, 4000::/3...).
func (l *PrefixLookup) Bogon() bool {
	for _, s := range l.Special { // do mais específico para o menos
		if s.TerminationDate != nil {
			continue
		}
		if s.GloballyReachable != nil {
			return !*s.GloballyReachable
		}
		break
	}
	return !l.Unreserved
}
