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
# TORGNEXA Community environment
# Сгенерировано scripts/init-community-env.sh. Права файла: 0600.
# Подробности: docs/deployment/environment-variables.md
#
# Не публикуйте этот файл и не меняйте секреты при существующих volumes:
# сохранённые PostgreSQL, Keycloak, Garage и ClickHouse используют эти значения.

# Версия и журналирование
TORGNEXA_VERSION=0.1.0-dev
TORGNEXA_LOG_LEVEL=info

# Ключ шифрования секретов интеграций: base64 от 32 случайных байт
TORGNEXA_SECRETS_MASTER_KEY=$(od -An -N 32 -tu1 /dev/urandom | awk '{for(i=1;i<=NF;i++)printf "%c",$i}' | base64 | tr -d '\n')

# PostgreSQL и отдельная прикладная роль
POSTGRES_PASSWORD=$(hex 32)
TORGNEXA_APP_DB_PASSWORD=$(hex 32)

# Keycloak
KEYCLOAK_DB_PASSWORD=$(hex 32)
KEYCLOAK_ADMIN_USERNAME=admin
KEYCLOAK_ADMIN_PASSWORD=$(hex 32)

# S3-совместимое хранилище Garage
GARAGE_RPC_SECRET=$(hex 32)
GARAGE_ADMIN_TOKEN=$(hex 32)
GARAGE_METRICS_TOKEN=$(hex 32)
S3_ACCESS_KEY=GK$(hex 16)
S3_SECRET_KEY=$(hex 32)
S3_BUCKET=torgnexa
TORGNEXA_S3_REQUEST_TIMEOUT=30s

# ClickHouse
CLICKHOUSE_USERNAME=torgnexa
CLICKHOUSE_PASSWORD=$(hex 32)
TORGNEXA_CLICKHOUSE_QUERY_TIMEOUT=5s

# Порты хоста. Все публикации остаются на 127.0.0.1.
POSTGRES_PORT=5432
KAFKA_PORT=9092
VALKEY_PORT=6379
CLICKHOUSE_HTTP_PORT=8123
CLICKHOUSE_NATIVE_PORT=9000
S3_PORT=9002
KEYCLOAK_PORT=8081
TORGNEXA_API_PORT=8080
TORGNEXA_MCP_PORT=8090
TORGNEXA_FRONTEND_PORT=5173

# Пул PostgreSQL
TORGNEXA_DB_MAX_OPEN_CONNS=20
TORGNEXA_DB_MAX_IDLE_CONNS=10
TORGNEXA_DB_CONN_MAX_LIFETIME=30m
TORGNEXA_DB_CONN_MAX_IDLE_TIME=5m
TORGNEXA_DB_CONNECT_TIMEOUT=5s

# Внешние issuer запрещены, пока администратор явно не добавит host через CSV.
TORGNEXA_OIDC_MANAGED_ISSUER_HOSTS=

# Фоновая обработка
TORGNEXA_KAFKA_CONSUMER_GROUP=torgnexa.webhooks.v1
TORGNEXA_WORKER_POLL_INTERVAL=500ms
TORGNEXA_WORKER_DISPATCH_BATCH=32
TORGNEXA_WORKER_LEASE=90s
TORGNEXA_WORKER_RECONCILIATION_ENABLED=true
# Для true сначала предоставьте worker доступный ClamAV и настройте адрес ниже.
TORGNEXA_WORKER_UPLOADS_ENABLED=false
TORGNEXA_CLAMAV_NETWORK=tcp
TORGNEXA_CLAMAV_ADDRESS=127.0.0.1:3310
TORGNEXA_CLAMAV_ENGINE_VERSION=runtime
TORGNEXA_CLAMAV_SIGNATURE_VERSION=runtime
TORGNEXA_CLAMAV_TIMEOUT=30s

# Внешние каналы уведомлений необязательны.
NOTIFICATION_SMTP_ADDRESS=
NOTIFICATION_SMTP_FROM=
NOTIFICATION_SMTP_USERNAME=
NOTIFICATION_SMTP_PASSWORD=
NOTIFICATION_SMTP_SERVER_NAME=
NOTIFICATION_SMTP_IMPLICIT_TLS=false
NOTIFICATION_CHAT_ENDPOINT=
NOTIFICATION_DELIVERY_TIMEOUT=10s
EOF
chmod 600 "$tmp"
mv -f "$tmp" "$out"
trap - EXIT
echo "generated $out with mode 0600"
