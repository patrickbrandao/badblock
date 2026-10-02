// Package rir concentra tudo o que é específico do RIR deste coletor.
//
// Os coletores dos cinco RIRs (afrinic, apnic, arin, lacnic, ripencc) são o
// mesmo código, porque todos publicam o formato "delegated-extended". Este app
// é um clone do collector-lacnic (o modelo): a troca de nome foi feita por sed
// (lacnic → afrinic e LACNIC → AFRINIC) e só este arquivo tem o que é da
// AFRINIC — URL padrão e mínimo de registros medidos no arquivo real. O resto
// do código não conhece o RIR.
package rir

const (
	// Source é o nome da fonte no BadBlock: prefixo das tabelas (afrinic_*),
	// pasta database/postgres/afrinic/ e caminho da API (/afrinic/).
	Source = "afrinic"

	// App é o nome do app, gravado em jobs.app e usado no log e no User-Agent.
	App = "collector-" + Source

	// Registry é o valor esperado no campo registry do cabeçalho e de cada
	// registro do arquivo. Um arquivo de outro registry é recusado.
	Registry = "afrinic"

	// Title é o nome do RIR nos textos (--help, descrições).
	Title = "AFRINIC"

	// DefaultSourceURL é o arquivo delegated-extended do RIR; o hash publicado
	// fica na mesma URL com ".md5".
	DefaultSourceURL = "https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest"

	// DefaultMinRecords é o mínimo de registros aceitos (ASN + IPv4 + IPv6):
	// abaixo disso o arquivo é tratado como truncado. O arquivo real tinha
	// 19.786 registros em 2026-09-28 (o menor dos cinco RIRs); o mínimo deixa
	// ~50% de folga.
	DefaultMinRecords = 10000
)
