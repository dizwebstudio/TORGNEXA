#!/bin/sh
set -eu
: "${PGHOST:?}" "${PGUSER:?}" "${PGPASSWORD:?}" "${KEYCLOAK_DB_PASSWORD:?}"
export PGPASSWORD
until pg_isready -h "$PGHOST" -p "${PGPORT:-5432}" -U "$PGUSER" >/dev/null 2>&1; do sleep 2; done
if [ "$(psql -h "$PGHOST" -U "$PGUSER" -d postgres -Atqc "SELECT count(*) FROM pg_roles WHERE rolname='keycloak'")" = 0 ]; then
  psql -h "$PGHOST" -U "$PGUSER" -d postgres --set=kcpass="$KEYCLOAK_DB_PASSWORD" <<'SQL'
CREATE ROLE keycloak LOGIN PASSWORD :'kcpass' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT;
SQL
else
  psql -h "$PGHOST" -U "$PGUSER" -d postgres --set=kcpass="$KEYCLOAK_DB_PASSWORD" <<'SQL'
ALTER ROLE keycloak PASSWORD :'kcpass';
SQL
fi
if [ "$(psql -h "$PGHOST" -U "$PGUSER" -d postgres -Atqc "SELECT count(*) FROM pg_database WHERE datname='keycloak'")" = 0 ]; then
  psql -h "$PGHOST" -U "$PGUSER" -d postgres -c 'CREATE DATABASE keycloak OWNER keycloak;'
fi
