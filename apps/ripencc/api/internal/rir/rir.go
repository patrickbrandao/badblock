// Package rir concentra tudo o que é específico do RIR desta API.
//
// As APIs dos cinco RIRs são o mesmo código, porque os coletores gravam o
// mesmo modelo de tabelas (formato "delegated-extended"). Um clone troca o
// nome por sed (lacnic → <rir>, LACNIC → <RIR>, Lacnic → <Rir>) a partir da
// api-lacnic (o modelo) e depois confere, só neste arquivo, Title, SourceURL
// e OpaqueIDChangesDaily. O resto do código não conhece o RIR: nomes de app,
// caminho de base, chave de cache e textos saem daqui.
package rir

const (
	// Source é o nome da fonte no BadBlock: prefixo das tabelas (ripencc_*),
	// pasta database/postgres/ripencc/ e caminho de base da API (/ripencc).
	Source = "ripencc"

	// App é o nome deste app: log, application_name no Postgres, cabeçalho
	// Server, cliente do Valkey e prefixo das chaves de cache.
	App = "api-" + Source

	// Collector é o app que grava as tabelas lidas aqui (linha de jobs.app).
	Collector = "collector-" + Source

	// Title é o nome do RIR nos textos (--help, mensagens de erro, índice).
	Title = "RIPE NCC"

	// SourceURL é o arquivo delegated-extended do RIR, citado no índice. A
	// meta mostra a URL gravada pelo collector na última carga.
	SourceURL = "https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest"

	// OpaqueIDChangesDaily diz que o RIR gera um opaque-id novo para cada
	// titular a cada arquivo diário (no RIPE NCC, um UUID novo todo dia): o
	// opaque_id só vale dentro do dataset atual, e o 404 de /holder explica
	// isso e aponta /ip e /asn. Nos RIRs de opaque-id estável, false.
	OpaqueIDChangesDaily = true
)

// BasePath é o caminho de base padrão da API (/ripencc).
const BasePath = "/" + Source
