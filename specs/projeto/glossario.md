# Glossário

| Termo | Significado |
|---|---|
| **fonte** | Arquivo (ou conjunto de arquivos) público de um registro da internet, tratado por um par de apps. O nome da fonte (`cgibr`, `lacnic`...) é um token sem hífen que batiza apps, tabelas, caminhos e specs; num site com vários conjuntos, são dois tokens (`anatel/pst`) |
| **site / conjunto** | Site que publica vários conjuntos de dados independentes (ex.: `anatel`) e cada um deles (ex.: `pst`); cada conjunto é uma fonte de dois níveis, `<site>/<conjunto>` ([estrutura.md](estrutura.md#fonte-de-dois-níveis-siteconjunto)) |
| **app** | Um processo com pasta, módulo, imagem e deploy próprios: `collector-<fonte>` ou `api-<fonte>` |
| **coletor** (collector) | App que importa a fonte para as tabelas `<fonte>_*`; sem API web |
| **API** | App HTTP que só lê as tabelas de uma fonte e responde abaixo de `/<fonte>/` |
| **dataset** | O conteúdo lógico de uma fonte num momento (um arquivo, ou os 10 arquivos da IANA juntos) |
| **verificação** | Uma passada do coletor: checagens de mudança e, se mudou, download, validação e aplicação |
| **aplicação** | Gravar um dataset novo nas tabelas, numa transação só |
| **execução** (run) | Linha em `<fonte>_run`: uma verificação que baixou um dataset novo, aplicado (`status = 1`) ou recusado (`status = 0`) |
| **versão do dataset** | `uuid` da última execução aplicada; vai nas chaves de cache e no ETag da API |
| **recusa** | Dataset baixado e não aplicado (hash, parser, mínimo, trava, banco); fica registrado com `status = 0` |
| **trava de remoção** | Recusa de um dataset que removeria mais de `REMOVAL_THRESHOLD` das linhas atuais (proteção contra arquivo truncado) |
| **`--force`** | Aplicação sem as checagens de mudança e sem a trava de remoção |
| **`jobs`** | Tabela central: uma linha por coletor, contrato com a consolidação |
| **consolidação** | Fase 2 (futura): copia os dados de todas as fontes para tabelas centrais e grava `consolidated = 1` |
| **fase 1 / fase 2** | Atual (um par por fonte) / futura (tabelas centrais consolidadas) |
| **RIR** | Registro Regional da Internet: AFRINIC, APNIC, ARIN, LACNIC, RIPE NCC. Cada um é uma fonte |
| **família RIR / modelo** | Os cinco pares de RIR são o mesmo código, clonado do lacnic; a spec do modelo está em `specs/fontes/rir/` |
| **clone** | App copiado do modelo; o que é dele fica em `internal/rir/rir.go` |
| **delegated-extended** | Formato de arquivo dos cinco RIRs com as delegações de ASNs e blocos (`registry\|cc\|type\|start\|value\|date\|status\|opaque-id`) |
| **opaque-id / titular** (holder) | Identificador do titular dentro de um RIR; liga ASNs e blocos da mesma organização |
| **registro não-CIDR** | Registro IPv4 cuja quantidade de endereços não forma um CIDR; vira vários blocos |
| **NIC.br / registro.br** | Registro nacional brasileiro (fonte `cgibr`) |
| **Anatel** | Agência Nacional de Telecomunicações; publica dados abertos de outorga e licenciamento (site `anatel`) |
| **PST** | Prestadoras de Serviços de Telecomunicações: o cadastro da Anatel de quem tem outorga ou é dispensado dela, com os serviços notificados (fonte `anatel/pst`) |
| **Fistel** | Número de 11 dígitos do Fundo de Fiscalização das Telecomunicações que a Anatel dá a cada outorga e a cada notificação de serviço |
| **SCM** | Serviço de Comunicação Multimídia (código 045 da Anatel): a licença de quem vende acesso à internet (banda larga fixa) |
| **special-purpose / bogon** | Endereços e ASNs de uso especial (IANA) / endereço que não deveria aparecer na internet pública |
| **bootstrap RDAP** | Arquivos da IANA que dizem qual servidor RDAP responde por cada faixa |
| **servidores raiz / root hints** | Os 13 servidores (`a` a `m.root-servers.net`) que respondem pela zona raiz do DNS; o `named.root` lista seus endereços (fonte `roothints`) |
| **zona raiz** (root zone) | A zona `.` do DNS, com a delegação de cada TLD (NS, glue A/AAAA, DS); o `root.zone` é ela inteira (fonte `rootzone`) |
| **serial** | Número de versão da zona no registro SOA (`AAAAMMDDNN`); o `named.root` cita o serial da zona com que foi gerado |
| **âncora de confiança** (trust anchor) | Resumo (DS) da chave KSK da raiz com que um resolvedor valida o DNSSEC; o `root-anchors.xml` publica as KSKs com validade (fonte `rootanchors`) |
| **recorte** (fixture) | Trecho real da fonte guardado em `testdata/` para os testes |
| **cache-aside** | A API procura no Valkey; se não achar, consulta o Postgres e guarda |
| **fail-open** | Falha do cache vira "não achei": a API segue respondendo pelo banco |
| **disjuntor** | Depois de 5 falhas seguidas do Valkey, a API deixa de consultá-lo por 5 s |
| **singleflight** | Consultas simultâneas à mesma chave viram uma só |
| **caminho de base** | `/<fonte>`: a API é dona de tudo abaixo dele |
| **v1 fixa** | `/<fonte>/v1/...`: responde como a v1 mesmo depois de uma v2 |
| **manifesto** | O `openapi.yaml` (OpenAPI 3.1) de uma API, derivado da spec |
| **sub-agente** | Agente de IA dedicado a um app (`.claude/agents/<app>.md`) |
| **sessão principal** | O agente (ou a pessoa) que integra: cuida das specs de projeto, plataforma, padrões e processos e dos arquivos da raiz |
