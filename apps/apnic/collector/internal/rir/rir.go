// Package rir concentra tudo o que é específico do RIR deste coletor.
//
// Os coletores dos cinco RIRs (afrinic, apnic, arin, lacnic, ripencc) são o
// mesmo código, porque todos publicam o formato "delegated-extended". Este app
// é um clone do collector-lacnic (o modelo): o nome foi trocado por sed
// (lacnic → apnic e LACNIC → APNIC) e só este arquivo guarda a URL padrão e o
// mínimo de registros medidos no arquivo real da APNIC. O resto do código não
// conhece o RIR.
package rir

const (
	// Source é o nome da fonte no BadBlock: prefixo das tabelas (apnic_*),
	// pasta database/postgres/apnic/ e caminho da API (/apnic/).
	Source = "apnic"

	// App é o nome do app, gravado em jobs.app e usado no log e no User-Agent.
	App = "collector-" + Source

	// Registry é o valor esperado no campo registry do cabeçalho e de cada
	// registro do arquivo. Um arquivo de outro registry é recusado.
	Registry = "apnic"

	// Title é o nome do RIR nos textos (--help, descrições).
	Title = "APNIC"

	// DefaultSourceURL é o arquivo delegated-extended do RIR; o hash publicado
	// fica na mesma URL com ".md5". Na APNIC o caminho não tem /pub.
	DefaultSourceURL = "https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest"

	// DefaultMinRecords é o mínimo de registros aceitos (ASN + IPv4 + IPv6):
	// abaixo disso o arquivo é tratado como truncado. O arquivo real tinha
	// 190.268 registros em 2026-09-28 (serial 20260929); o mínimo deixa ~50%
	// de folga.
	DefaultMinRecords = 95000
)
