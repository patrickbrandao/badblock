// Package rir concentra tudo o que é específico do RIR deste coletor.
//
// Os coletores dos cinco RIRs (afrinic, apnic, arin, lacnic, ripencc) são o
// mesmo código, porque todos publicam o formato "delegated-extended". Um clone
// troca o nome por sed (lacnic → <rir> e LACNIC → <RIR>) e depois confere, só
// neste arquivo, a URL padrão e o mínimo de registros medidos no arquivo real
// do RIR. O resto do código não conhece o RIR.
package rir

const (
	// Source é o nome da fonte no BadBlock: prefixo das tabelas (lacnic_*),
	// pasta database/postgres/lacnic/ e caminho da API (/lacnic/).
	Source = "lacnic"

	// App é o nome do app, gravado em jobs.app e usado no log e no User-Agent.
	App = "collector-" + Source

	// Registry é o valor esperado no campo registry do cabeçalho e de cada
	// registro do arquivo. Um arquivo de outro registry é recusado.
	Registry = "lacnic"

	// Title é o nome do RIR nos textos (--help, descrições).
	Title = "LACNIC"

	// DefaultSourceURL é o arquivo delegated-extended do RIR; o hash publicado
	// fica na mesma URL com ".md5".
	DefaultSourceURL = "https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest"

	// DefaultMinRecords é o mínimo de registros aceitos (ASN + IPv4 + IPv6):
	// abaixo disso o arquivo é tratado como truncado. O arquivo real tinha
	// 97.301 registros em 2026-09-28; o mínimo deixa ~50% de folga.
	DefaultMinRecords = 50000
)
