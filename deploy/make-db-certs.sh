#!/bin/sh
# Creates a private CA and a server certificate for the database container.
# Production verifies the database's certificate in full (sslmode=verify-full),
# so even a database on the same host needs TLS. The name must be "db": that is
# the host the API connects to, and verify-full checks it.
#
# Usage: sudo sh deploy/make-db-certs.sh
set -eu

dir="$(cd "$(dirname "$0")" && pwd)/certs"
mkdir -p "$dir"

if [ -f "$dir/server.crt" ]; then
	echo "deploy/certs already exists; delete it to issue new certificates." >&2
	exit 1
fi

openssl req -x509 -nodes -newkey rsa:4096 -days 3650 \
	-keyout "$dir/ca.key" -out "$dir/ca.crt" -subj "/CN=cardplay-db-ca" 2>/dev/null

openssl req -nodes -newkey rsa:4096 \
	-keyout "$dir/server.key" -out "$dir/server.csr" -subj "/CN=db" 2>/dev/null

openssl x509 -req -in "$dir/server.csr" -CA "$dir/ca.crt" -CAkey "$dir/ca.key" \
	-CAcreateserial -out "$dir/server.crt" -days 3650 \
	-extfile /dev/stdin <<-EOF 2>/dev/null
		subjectAltName=DNS:db
		basicConstraints=critical,CA:FALSE
		keyUsage=critical,digitalSignature,keyEncipherment
		extendedKeyUsage=serverAuth
	EOF

rm -f "$dir/server.csr" "$dir/ca.srl"

# PostgreSQL refuses a key that others can read, and reads it as the postgres
# user inside the image (uid 999).
chmod 600 "$dir/server.key" "$dir/ca.key"
chmod 644 "$dir/server.crt" "$dir/ca.crt"
if [ "$(id -u)" = "0" ]; then
	chown 999:999 "$dir/server.key" "$dir/server.crt"
else
	echo "Not running as root: run 'sudo chown 999:999 deploy/certs/server.key deploy/certs/server.crt' before starting." >&2
fi

echo "Wrote $dir: ca.crt (trusted by the API), server.crt and server.key (used by the database)."
echo "Keep ca.key private; it is only needed to issue a replacement certificate."
