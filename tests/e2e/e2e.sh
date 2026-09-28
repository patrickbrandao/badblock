#!/bin/sh
# Teste ponta a ponta do BadBlock: cliente → Traefik (HTTPS) → registry-api →
# Valkey/Postgres, com os dados carregados pelo registry-sync.
#
# As verificações valem tanto para as fixtures (CI) quanto para os dados reais
# (ambiente local): usam recursos presentes nos dois (TMSoft, Google, 10/8).
#
#   tests/e2e/e2e.sh
#
# Variáveis:
#   API_HOST          host público da API (padrão api.badblock.localhost)
#   CONNECT_TO        IP onde o Traefik escuta (padrão 127.0.0.1)
#   CACERT            CA do certificado local (padrão infra/traefik/certs/ca.pem;
#                     vazio usa o repositório do sistema, ex.: mkcert -install)
#   CHECK_RATE_LIMIT  1 para testar o 429 das listas (consome a cota do IP)
set -eu

API_HOST="${API_HOST:-api.badblock.localhost}"
CONNECT_TO="${CONNECT_TO:-127.0.0.1}"
CACERT="${CACERT-infra/traefik/certs/ca.pem}"
CHECK_RATE_LIMIT="${CHECK_RATE_LIMIT:-1}"
BASE="https://$API_HOST"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
PASS=0
FAIL=0

# curl com resolução fixa (não depende de *.localhost resolver no sistema).
c() {
	if [ -n "$CACERT" ]; then
		curl -sS --max-time 20 --cacert "$CACERT" \
			--resolve "$API_HOST:443:$CONNECT_TO" --resolve "$API_HOST:80:$CONNECT_TO" "$@"
	else
		curl -sS --max-time 20 --resolve "$API_HOST:443:$CONNECT_TO" --resolve "$API_HOST:80:$CONNECT_TO" "$@"
	fi
}

ok() {
	PASS=$((PASS + 1))
	echo "  ok    $1"
}

ko() {
	FAIL=$((FAIL + 1))
	echo "  FALHA $1" >&2
}

# check "descrição" "valor obtido" "valor esperado"
check() {
	if [ "$2" = "$3" ]; then ok "$1"; else ko "$1: obtido [$2], esperado [$3]"; fi
}

# jget arquivo expressão-python (d = JSON do arquivo)
jget() {
	python3 -c "import json,sys; d=json.load(open(sys.argv[1])); v=$2; print('null' if v is None else (str(v).lower() if isinstance(v, bool) else v))" "$1"
}

# get caminho [args do curl...] → corpo em $TMP/body, headers em $TMP/headers, status em $STATUS
get() {
	path="$1"
	shift
	STATUS=$(c -D "$TMP/headers" -o "$TMP/body" -w '%{http_code}' "$@" "$BASE$path")
}

header() {
	grep -i "^$1:" "$TMP/headers" | head -n 1 | cut -d: -f2- | tr -d '\r' | sed 's/^ *//'
}

echo "== BadBlock e2e em $BASE (via $CONNECT_TO)"

echo "-- saúde"
# Espera o Traefik e a API responderem (até 3 minutos).
i=0
until [ "$(c -o /dev/null -w '%{http_code}' "$BASE/ping" 2>/dev/null || true)" = "200" ]; do
	i=$((i + 1))
	[ "$i" -gt 90 ] && break
	sleep 2
done
get /ping
check "GET /ping" "$(cat "$TMP/body")" "pong"
# Espera a primeira sincronização (até 5 minutos).
i=0
while :; do
	get /status
	version=$(jget "$TMP/body" "d.get('dataset_version', 0)" 2>/dev/null || echo 0)
	[ "$version" -gt 0 ] 2>/dev/null && break
	i=$((i + 1))
	[ "$i" -gt 60 ] && break
	sleep 5
done
check "dataset carregado" "$([ "$version" -gt 0 ] 2>/dev/null && echo sim || echo não)" "sim"
check "status online" "$(jget "$TMP/body" "d['status']")" "online"
check "postgres online" "$(jget "$TMP/body" "d['checks']['postgres']")" "online"
check "cache online" "$(jget "$TMP/body" "d['checks']['cache']")" "online"

echo "-- HTTPS e redirecionamento"
redirect=$(c -o /dev/null -w '%{http_code} %{redirect_url}' "http://$API_HOST/ping")
check "HTTP redireciona para HTTPS" "$redirect" "301 https://$API_HOST/ping"

echo "-- lookup de IP"
get /v1/ip/45.171.60.1
check "status 200" "$STATUS" "200"
check "prefixo" "$(jget "$TMP/body" "d['prefix']['cidr']")" "45.171.60.0/22"
check "RIR" "$(jget "$TMP/body" "d['prefix']['rir']")" "LACNIC"
check "titular" "$(jget "$TMP/body" "d['holder']['id']")" "lacnic:258500"
check "CNPJ do NIC.br" "$(jget "$TMP/body" "d['holder']['document']")" "08.030.063/0001-00"
check "ASN pelo NIC.br" "$(jget "$TMP/body" "[a['asn'] for a in d['asns'] if a['link']=='nicbr'][0]")" "61613"
check "cadeia começa na IANA" "$(jget "$TMP/body" "d['chain'][0]['level']")" "iana"
check "não é bogon" "$(jget "$TMP/body" "d['flags']['bogon']")" "false"
etag=$(header ETag)

get /v1/ip/45.171.60.77
check "mesmo /24 vem do cache" "$(header X-Cache)" "HIT"
check "resposta do cache traz o IP pedido" "$(jget "$TMP/body" "d['ip']")" "45.171.60.77"
get /v1/ip/45.171.60.1 -H "If-None-Match: $etag"
check "ETag → 304" "$STATUS" "304"

get /v1/ip/10.1.2.3
check "10/8 é bogon" "$(jget "$TMP/body" "d['flags']['bogon']")" "true"
check "10/8 special" "$(jget "$TMP/body" "d['flags']['special']")" "Private-Use"
get /v1/ip/8.8.8.8
check "8.8.8.8 é do Google" "$(jget "$TMP/body" "d['holder']['name']")" "Google LLC"
get /v1/ip/2804:5964::1
check "IPv6 da TMSoft" "$(jget "$TMP/body" "d['prefix']['cidr']")" "2804:5964::/32"
get /v1/ip/999.1.1.1
check "IP inválido → 400" "$STATUS" "400"

echo "-- IP real (X-Forwarded-For / X-Real-IP forjados pelo cliente)"
get /v1/ip -H "X-Forwarded-For: 1.2.3.4" -H "X-Real-IP: 5.6.7.8"
seen=$(jget "$TMP/body" "d['ip']")
if [ "$seen" != "1.2.3.4" ] && [ "$seen" != "5.6.7.8" ] && [ -n "$seen" ]; then
	ok "headers forjados ignorados (API viu $seen)"
else
	ko "headers forjados aceitos: API viu [$seen]"
fi
check "origem do IP" "$(header X-Client-IP-Source)" "x-forwarded-for"
check "/v1/ip não é cacheável" "$(header Cache-Control)" "private, no-store"

echo "-- ASN"
get /v1/asn/AS61613
check "nome do AS61613" "$(jget "$TMP/body" "d['name']")" "TMSoft Solucoes em Informatica Ltda"
check "prefixos do AS61613" "$(jget "$TMP/body" "sorted(p['cidr'] for p in d['prefixes'] if p['link']=='nicbr')[0]")" "200.192.152.0/22"
get /v1/asn/8075
check "ASN estrangeiro mantém o nome" "$(jget "$TMP/body" "d['name']")" "Microsoft Corporation"
get /v1/asn/64512
check "ASN privado é bogon" "$(jget "$TMP/body" "d['flags']['bogon']")" "true"
get /v1/asn/abc
check "ASN inválido → 400" "$STATUS" "400"

echo "-- rota legado (POC)"
get /asn/61613
check "legado: formato" "$(jget "$TMP/body" "'|'.join(str(d[k]) for k in ['asn','name','cid','country','rir','status','score'])")" \
	"61613|TMSoft Solucoes em Informatica Ltda|08.030.063/0001-00|BR|LACNIC|ALLOCATED|0"
get /asn/4199999999
check "legado: 404" "$STATUS" "404"
check "legado: corpo {}" "$(tr -d '\n' <"$TMP/body")" "{}"

echo "-- prefixo, titular, histórico e fontes"
get /v1/prefix/45.171.60.0/22
check "prefixo exato" "$(jget "$TMP/body" "d['match']")" "exact"
get /v1/holder/lacnic/258500
check "titular" "$(jget "$TMP/body" "d['name']")" "TMSoft Solucoes em Informatica Ltda"
get /v1/asn/61613/history
check "histórico responde" "$STATUS" "200"
get /v1/meta/sources
check "17 fontes" "$(jget "$TMP/body" "len(d['sources'])")" "17"
get /openapi.yaml
check "OpenAPI publicado" "$STATUS" "200"

echo "-- listas"
get "/v1/country/BR/prefixes?format=txt"
if grep -qx "45.171.60.0/22" "$TMP/body"; then ok "lista BR em txt contém 45.171.60.0/22"; else ko "lista BR em txt sem 45.171.60.0/22"; fi
check "lista em text/plain" "$(header Content-Type)" "text/plain; charset=utf-8"

if [ "$CHECK_RATE_LIMIT" = "1" ]; then
	echo "-- rate limit das listas"
	limited=0
	n=0
	while [ "$n" -lt 15 ]; do
		code=$(c -o /dev/null -w '%{http_code}' "$BASE/v1/country/BR/asns?limit=1")
		[ "$code" = "429" ] && limited=1
		n=$((n + 1))
	done
	check "listas devolvem 429 acima da rajada" "$limited" "1"
fi

echo "== $PASS ok, $FAIL falhas"
[ "$FAIL" -eq 0 ]
