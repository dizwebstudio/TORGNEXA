BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fx_rate_facts (
    id text PRIMARY KEY,
    base_currency text NOT NULL CHECK (base_currency ~ '^[A-Z]{3}$'),
    quote_currency text NOT NULL CHECK (quote_currency ~ '^[A-Z]{3}$' AND quote_currency <> base_currency),
    rate_coefficient bigint NOT NULL CHECK (rate_coefficient > 0),
    rate_scale smallint NOT NULL CHECK (rate_scale BETWEEN 0 AND 9),
    source_id text NOT NULL CHECK (source_id ~ '^[a-z][a-z0-9._-]{0,63}$'),
    source_reference text NOT NULL DEFAULT '',
    rate_type text NOT NULL CHECK (rate_type IN ('official','mid','bid','ask','closing','indicative')),
    observed_at timestamptz NOT NULL,
    effective_at timestamptz NOT NULL,
    schema_version smallint NOT NULL CHECK (schema_version = 1),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (length(source_reference) <= 256)
);
CREATE INDEX fx_rate_facts_lookup_idx ON fx_rate_facts(base_currency,quote_currency,rate_type,source_id,effective_at DESC,observed_at DESC);

CREATE TABLE fx_resolution_evidence (
    id text PRIMARY KEY,
    base_currency text NOT NULL CHECK (base_currency ~ '^[A-Z]{3}$'),
    quote_currency text NOT NULL CHECK (quote_currency ~ '^[A-Z]{3}$' AND quote_currency <> base_currency),
    rate_type text NOT NULL CHECK (rate_type IN ('official','mid','bid','ask','closing','indicative')),
    as_of timestamptz NOT NULL,
    precedence jsonb NOT NULL CHECK (jsonb_typeof(precedence)='array' AND jsonb_array_length(precedence)>0),
    candidate_fact_ids jsonb NOT NULL CHECK (jsonb_typeof(candidate_fact_ids)='array' AND jsonb_array_length(candidate_fact_ids)>0),
    selected_fact_id text NOT NULL REFERENCES fx_rate_facts(id) ON DELETE RESTRICT,
    resolved_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX fx_resolution_lookup_idx ON fx_resolution_evidence(base_currency,quote_currency,rate_type,as_of DESC);

CREATE TABLE fx_conversion_records (
    id text PRIMARY KEY,
    source_currency text NOT NULL CHECK (source_currency ~ '^[A-Z]{3}$'),
    source_minor_units bigint NOT NULL,
    source_minor_unit_scale smallint NOT NULL CHECK (source_minor_unit_scale BETWEEN 0 AND 9),
    target_currency text NOT NULL CHECK (target_currency ~ '^[A-Z]{3}$' AND target_currency <> source_currency),
    target_minor_units bigint NOT NULL,
    target_minor_unit_scale smallint NOT NULL CHECK (target_minor_unit_scale BETWEEN 0 AND 9),
    snapshot jsonb NOT NULL CHECK (jsonb_typeof(snapshot)='object'),
    resolution_evidence_ids jsonb NOT NULL CHECK (jsonb_typeof(resolution_evidence_ids)='array' AND jsonb_array_length(resolution_evidence_ids) BETWEEN 1 AND 2),
    digest text NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$'),
    derived_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX fx_conversion_records_time_idx ON fx_conversion_records(derived_at DESC,id);

CREATE FUNCTION fx_reference_facts_reject_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''FX reference facts and derivation evidence are append-only''; END';
CREATE TRIGGER fx_rate_facts_no_update BEFORE UPDATE OR DELETE ON fx_rate_facts FOR EACH ROW EXECUTE FUNCTION fx_reference_facts_reject_mutation();
CREATE TRIGGER fx_resolution_evidence_no_update BEFORE UPDATE OR DELETE ON fx_resolution_evidence FOR EACH ROW EXECUTE FUNCTION fx_reference_facts_reject_mutation();
CREATE TRIGGER fx_conversion_records_no_update BEFORE UPDATE OR DELETE ON fx_conversion_records FOR EACH ROW EXECUTE FUNCTION fx_reference_facts_reject_mutation();
REVOKE UPDATE, DELETE, TRUNCATE ON fx_rate_facts,fx_resolution_evidence,fx_conversion_records FROM PUBLIC;

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
