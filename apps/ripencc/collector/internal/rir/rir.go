// Package rir concentra tudo o que é específico do RIR deste coletor.
//
// Os coletores dos cinco RIRs (afrinic, apnic, arin, lacnic, ripencc) são o
// mesmo código, porque todos publicam o formato "delegated-extended". Este é
// um clone do collector-lacnic (o modelo): o nome foi trocado por sed e,
// depois, só este arquivo foi conferido à mão com a URL padrão e o mínimo de
// registros medidos no arquivo real do RIPE NCC. O resto do código não
// conhece o RIR.
package rir

const (
	// Source é o nome da fonte no BadBlock: prefixo das tabelas (ripencc_*),
	// pasta database/postgres/ripencc/ e caminho da API (/ripencc/).
	Source = "ripencc"

	// App é o nome do app, gravado em jobs.app e usado no log e no User-Agent.
	App = "collector-" + Source

	// Registry é o valor esperado no campo registry do cabeçalho e de cada
	// registro do arquivo. Um arquivo de outro registry é recusado.
	Registry = "ripencc"

	// Title é o nome do RIR nos textos (--help, descrições).
	Title = "RIPE NCC"

	// DefaultSourceURL é o arquivo delegated-extended do RIR; o hash publicado
	// fica na mesma URL com ".md5".
	DefaultSourceURL = "https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest"

	// DefaultMinRecords é o mínimo de registros aceitos (ASN + IPv4 + IPv6):
	// abaixo disso o arquivo é tratado como truncado. O arquivo real tinha
	// 260.793 registros em 2026-09-28; o mínimo deixa ~50% de folga.
	DefaultMinRecords = 130000
)
