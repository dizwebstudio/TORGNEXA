#!/usr/bin/env sh
set -eu
umask 077
repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)"
out="$repo_root/.env"
hex() {
  bytes=$1
  od -An -N "$bytes" -tx1 /dev/urandom | tr -d ' \n'
}
[ ! -L "$out" ] || { echo '.env must not be a symlink' >&2; exit 1; }
if [ -e "$out" ] && [ "${1:-}" != "--force" ]; then
  [ -f "$out" ] || { echo '.env must be a regular file' >&2; exit 1; }
  if grep -Eq '^TORGNEXA_APP_DB_PASSWORD=[0-9a-f]{64}$' "$out"; then
    echo '.env already contains TORGNEXA_APP_DB_PASSWORD; existing volume credentials were preserved'
    exit 0
  fi
  if grep -q '^TORGNEXA_APP_DB_PASSWORD=' "$out"; then
    echo '.env contains an invalid TORGNEXA_APP_DB_PASSWORD; repair it without rotating existing values' >&2
    exit 1
  fi
  tmp="$repo_root/.env.tmp.$$"
  trap 'rm -f "$tmp"' EXIT
  cp "$out" "$tmp"
  printf '\nTORGNEXA_APP_DB_PASSWORD=%s\n' "$(hex 32)" >> "$tmp"
  chmod 600 "$tmp"
  mv -f "$tmp" "$out"
  trap - EXIT
  echo 'upgraded existing .env with TORGNEXA_APP_DB_PASSWORD; existing values were preserved'
  exit 0
fi
if [ -e "$out" ] && [ "${1:-}" = "--force" ]; then
  echo 'WARNING: rotating .env while persistent volumes exist can break stored credentials.' >&2
fi
tmp="$repo_root/.env.tmp.$$"
trap 'rm -f "$tmp"' EXIT
cat > "$tmp" <<EOF
TORGNEXA_VERSION=0.1.0-dev
TORGNEXA_LOG_LEVEL=info
TORGNEXA_SECRETS_MASTER_KEY=$(od -An -N 32 -tu1 /dev/urandom | awk '{for(i=1;i<=NF;i++)printf "%c",$i}' | base64 | tr -d '\n')
POSTGRES_PASSWORD=$(hex 32)
TORGNEXA_APP_DB_PASSWORD=$(hex 32)
KEYCLOAK_DB_PASSWORD=$(hex 32)
KEYCLOAK_ADMIN_USERNAME=admin
KEYCLOAK_ADMIN_PASSWORD=$(hex 32)
GARAGE_RPC_SECRET=$(hex 32)
GARAGE_ADMIN_TOKEN=$(hex 32)
GARAGE_METRICS_TOKEN=$(hex 32)
S3_ACCESS_KEY=GK$(hex 16)
S3_SECRET_KEY=$(hex 32)
S3_BUCKET=torgnexa
CLICKHOUSE_USERNAME=torgnexa
CLICKHOUSE_PASSWORD=$(hex 32)
POSTGRES_PORT=5432
KAFKA_PORT=9092
VALKEY_PORT=6379
CLICKHOUSE_HTTP_PORT=8123
CLICKHOUSE_NATIVE_PORT=9000
S3_PORT=9002
KEYCLOAK_PORT=8081
TORGNEXA_API_PORT=8080
TORGNEXA_MCP_PORT=8090
EOF
chmod 600 "$tmp"
mv -f "$tmp" "$out"
trap - EXIT
echo "generated $out with mode 0600"
