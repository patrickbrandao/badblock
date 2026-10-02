// Package rir concentra tudo o que é específico do RIR desta API.
//
// As APIs dos cinco RIRs são o mesmo código, porque os coletores gravam o
// mesmo modelo de tabelas (formato "delegated-extended"). Esta é um clone da
// api-lacnic (o modelo), feito por troca de nomes (lacnic → apnic, LACNIC →
// APNIC, Lacnic → Apnic), com Title e SourceURL conferidos aqui. O resto do
// código não conhece o RIR: nomes de app, caminho de base, chave de cache e
// textos saem daqui.
package rir

const (
	// Source é o nome da fonte no BadBlock: prefixo das tabelas (apnic_*),
	// pasta database/postgres/apnic/ e caminho de base da API (/apnic).
	Source = "apnic"

	// App é o nome deste app: log, application_name no Postgres, cabeçalho
	// Server, cliente do Valkey e prefixo das chaves de cache.
	App = "api-" + Source

	// Collector é o app que grava as tabelas lidas aqui (linha de jobs.app).
	Collector = "collector-" + Source

	// Title é o nome do RIR nos textos (--help, mensagens de erro, índice).
	Title = "APNIC"

	// SourceURL é o arquivo delegated-extended do RIR, citado no índice. A
	// meta mostra a URL gravada pelo collector na última carga. Na APNIC o
	// caminho não tem /pub, diferente dos outros RIRs.
	SourceURL = "https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest"

	// OpaqueIDChangesDaily diz que o RIR gera um opaque-id novo para cada
	// titular a cada arquivo diário (no RIPE NCC, um UUID novo todo dia): o
	// opaque_id só vale dentro do dataset atual, e o 404 de /holder explica
	// isso e aponta /ip e /asn. Nos RIRs de opaque-id estável, false.
	OpaqueIDChangesDaily = false
)

// BasePath é o caminho de base padrão da API (/apnic).
const BasePath = "/" + Source
