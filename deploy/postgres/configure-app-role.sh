#!/bin/sh
set -eu
umask 077
: "${PGHOST:?PGHOST is required}" "${PGDATABASE:?PGDATABASE is required}" "${PGUSER:?PGUSER is required}" "${PGPASSWORD:?PGPASSWORD is required}" "${TORGNEXA_APP_DB_PASSWORD:?TORGNEXA_APP_DB_PASSWORD is required}"
export PGPASSWORD

psql --no-psqlrc --set ON_ERROR_STOP=1 --host="$PGHOST" --port="${PGPORT:-5432}" --username="$PGUSER" --dbname="$PGDATABASE" --set app_password="$TORGNEXA_APP_DB_PASSWORD" <<'SQL'
SELECT format('CREATE ROLE %I LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOINHERIT PASSWORD %L', 'torgnexa_app', :'app_password')
 WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='torgnexa_app') \gexec

ALTER ROLE torgnexa_app LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOINHERIT PASSWORD :'app_password';
ALTER ROLE torgnexa_app IN DATABASE torgnexa SET row_security = on;
ALTER ROLE torgnexa_app IN DATABASE torgnexa SET statement_timeout = '60s';
ALTER ROLE torgnexa_app IN DATABASE torgnexa SET idle_in_transaction_session_timeout = '30s';

REVOKE CREATE ON SCHEMA public FROM PUBLIC, torgnexa_app;
GRANT CONNECT ON DATABASE torgnexa TO torgnexa_app;
GRANT USAGE ON SCHEMA public TO torgnexa_app;
GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA public TO torgnexa_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO torgnexa_app;
GRANT DELETE ON TABLE demo_dataset_tombstones TO torgnexa_app;
GRANT DELETE ON TABLE catalog_product_images TO torgnexa_app;
GRANT EXECUTE ON FUNCTION claim_connector_sync_jobs(text,text,integer,integer) TO torgnexa_app;

-- Preserve append/evidence semantics even if a trigger is accidentally changed.
REVOKE UPDATE ON TABLE
  audit_records,inbox_receipts,secret_versions,upload_security_evidence,
  sync_local_receipts,sync_remote_receipts,reconciliation_actions,
  webhook_delivery_attempts,notification_deliveries,
  settings_login_events,settings_identity_provider_revisions,
  settings_identity_provider_validations,connector_health_history,
  connector_account_capability_history,fx_rate_facts,fx_resolution_evidence,
  fx_conversion_records,plugin_marketplace_versions,plugin_private_versions,
  plugin_marketplace_consents,plugin_marketplace_revocations,
  plugin_installation_revocations,security_evidence,ai_egress_policy_revisions,
  ai_egress_usage,connector_replay_runs,profitability_scenarios,
  social_publication_receipts
FROM torgnexa_app;
REVOKE DELETE, TRUNCATE ON ALL TABLES IN SCHEMA public FROM torgnexa_app;
GRANT DELETE ON TABLE demo_dataset_tombstones TO torgnexa_app;
GRANT DELETE ON TABLE catalog_product_images TO torgnexa_app;

ALTER DEFAULT PRIVILEGES FOR ROLE torgnexa IN SCHEMA public GRANT SELECT, INSERT, UPDATE ON TABLES TO torgnexa_app;
ALTER DEFAULT PRIVILEGES FOR ROLE torgnexa IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO torgnexa_app;
ALTER DEFAULT PRIVILEGES FOR ROLE torgnexa IN SCHEMA public REVOKE DELETE, TRUNCATE ON TABLES FROM torgnexa_app;
SQL

echo "TORGNEXA application database role configured"
