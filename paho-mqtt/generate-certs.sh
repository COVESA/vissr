#!/usr/bin/env bash
set -euo pipefail

CERT_DIR="$(cd "$(dirname "$0")" && pwd)/cert"
mkdir -p "$CERT_DIR"

# 1. Generate CA key and self-signed certificate
openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 365 \
  -keyout "$CERT_DIR/ca.key" \
  -out "$CERT_DIR/ca.crt" \
  -subj "/C=US/ST=California/O=vissr-test/CN=vissr-test-CA"

# 2. Generate Server key and CSR
openssl req -newkey rsa:2048 -nodes \
  -keyout "$CERT_DIR/server.key" \
  -out "$CERT_DIR/server.csr" \
  -subj "/C=US/ST=California/O=vissr-test/CN=localhost"

# 3. Sign Server certificate with CA (including SAN and ExtendedKeyUsage)
EXT_FILE="$(mktemp)"
trap 'rm -f "$EXT_FILE"' EXIT
cat > "$EXT_FILE" <<EOF
subjectAltName=DNS:localhost,IP:127.0.0.1
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
EOF

openssl x509 -req -in "$CERT_DIR/server.csr" \
  -CA "$CERT_DIR/ca.crt" -CAkey "$CERT_DIR/ca.key" -CAcreateserial \
  -out "$CERT_DIR/server.crt" -days 365 -sha256 \
  -extfile "$EXT_FILE"

# Ensure the mosquitto user inside the Docker container can read the key and certs
chmod 644 "$CERT_DIR/ca.crt" "$CERT_DIR/server.crt" "$CERT_DIR/server.key"

