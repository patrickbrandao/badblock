#!/bin/sh
# Alternativa ao mkcert: cria uma CA local e um certificado para
# *.badblock.localhost assinado por ela. Nada é instalado no sistema.
#
# Para os navegadores confiarem, importe certs/ca.pem no chaveiro. Para o curl,
# use --cacert certs/ca.pem (o e2e faz isso sozinho).
set -eu

dir="${1:-certs}"
mkdir -p "$dir"
cd "$dir"

if [ ! -f ca.pem ]; then
	openssl req -x509 -new -nodes -newkey rsa:2048 -sha256 -days 825 \
		-keyout ca-key.pem -out ca.pem -subj "/CN=BadBlock Dev CA" >/dev/null 2>&1
fi

cat > server.ext <<'EOF'
basicConstraints=CA:FALSE
keyUsage=digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=DNS:badblock.localhost,DNS:*.badblock.localhost,DNS:localhost,IP:127.0.0.1,IP:::1
EOF

openssl req -new -nodes -newkey rsa:2048 -keyout badblock.localhost-key.pem \
	-out server.csr -subj "/CN=badblock.localhost" >/dev/null 2>&1
openssl x509 -req -in server.csr -CA ca.pem -CAkey ca-key.pem -CAcreateserial \
	-out badblock.localhost.pem -days 825 -sha256 -extfile server.ext >/dev/null 2>&1
rm -f server.csr server.ext ca.srl

echo "Certificado emitido pela CA local em $dir/ca.pem (não instalada no sistema)."
echo "curl: --cacert $dir/ca.pem  |  navegador: importe $dir/ca.pem ou use 'mkcert -install'."
