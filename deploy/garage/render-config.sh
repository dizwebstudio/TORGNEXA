#!/bin/sh
set -eu
umask 077
: "${GARAGE_RPC_SECRET:?GARAGE_RPC_SECRET is required}" "${GARAGE_ADMIN_TOKEN:?GARAGE_ADMIN_TOKEN is required}" "${GARAGE_METRICS_TOKEN:?GARAGE_METRICS_TOKEN is required}"
for token in "$GARAGE_RPC_SECRET" "$GARAGE_ADMIN_TOKEN" "$GARAGE_METRICS_TOKEN"; do echo "$token" | grep -Eq '^[0-9a-f]{64}$' || { echo 'Garage secrets must be 64 lowercase hex chars' >&2; exit 1; }; done
mkdir -p /config
cat > /config/garage.toml <<EOF
metadata_dir = "/var/lib/garage/meta"
data_dir = "/var/lib/garage/data"
db_engine = "sqlite"
replication_factor = 1
rpc_bind_addr = "0.0.0.0:3901"
rpc_public_addr = "127.0.0.1:3901"
rpc_secret = "$GARAGE_RPC_SECRET"

[s3_api]
s3_region = "garage"
api_bind_addr = "0.0.0.0:3900"
root_domain = ".s3.garage.localhost"

[admin]
api_bind_addr = "0.0.0.0:3903"
admin_token = "$GARAGE_ADMIN_TOKEN"
metrics_token = "$GARAGE_METRICS_TOKEN"
EOF
chmod 600 /config/garage.toml
