BEGIN;
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

CREATE TABLE entitlement_rules (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  feature_key text NOT NULL CHECK(feature_key ~ '^[a-z][a-z0-9._:/-]{0,127}$'),
  enabled boolean NOT NULL,
  source text NOT NULL CHECK(source ~ '^[a-z][a-z0-9._:/-]{0,127}$'),
  version bigint NOT NULL CHECK(version >= 1),
  effective_from timestamptz NOT NULL,
  effective_until timestamptz,
  created_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,id,version),
  CONSTRAINT entitlement_rules_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  UNIQUE(organization_id,workspace_id,feature_key,version),
  CONSTRAINT entitlement_rule_dates CHECK(effective_until IS NULL OR effective_until > effective_from)
);
CREATE INDEX entitlement_rules_resolve_idx ON entitlement_rules(organization_id,workspace_id,feature_key,effective_from,version DESC);

CREATE FUNCTION entitlement_rule_insert_guard() RETURNS trigger LANGUAGE plpgsql AS '
DECLARE expected bigint; stable_id text;
BEGIN
  SELECT COALESCE(max(version),0)+1,min(id) INTO expected,stable_id FROM entitlement_rules WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND feature_key=NEW.feature_key;
  IF NEW.version<>expected THEN RAISE EXCEPTION ''entitlement rule version invalid''; END IF;
  IF stable_id IS NOT NULL AND stable_id<>NEW.id THEN RAISE EXCEPTION ''entitlement rule identity must remain stable''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER entitlement_rule_insert_guard BEFORE INSERT ON entitlement_rules FOR EACH ROW EXECUTE FUNCTION entitlement_rule_insert_guard();
CREATE FUNCTION entitlement_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''entitlement policy evidence is append-only''; END';
CREATE TRIGGER entitlement_rules_append_only BEFORE UPDATE OR DELETE ON entitlement_rules FOR EACH ROW EXECUTE FUNCTION entitlement_append_only();

CREATE TABLE entitlement_quota_policies (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  metric_key text NOT NULL CHECK(metric_key ~ '^[a-z][a-z0-9._:/-]{0,127}$'),
  limit_value bigint NOT NULL CHECK(limit_value >= 0),
  window_kind text NOT NULL CHECK(window_kind IN ('lifetime','calendar_day_utc','calendar_month_utc')),
  source text NOT NULL CHECK(source ~ '^[a-z][a-z0-9._:/-]{0,127}$'),
  version bigint NOT NULL CHECK(version >= 1),
  effective_from timestamptz NOT NULL,
  effective_until timestamptz,
  created_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,id,version),
  CONSTRAINT entitlement_quota_policies_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  UNIQUE(organization_id,workspace_id,metric_key,version),
  CONSTRAINT entitlement_quota_policy_dates CHECK(effective_until IS NULL OR effective_until > effective_from)
);
CREATE INDEX entitlement_quota_policy_resolve_idx ON entitlement_quota_policies(organization_id,workspace_id,metric_key,effective_from,version DESC);
CREATE FUNCTION entitlement_quota_policy_insert_guard() RETURNS trigger LANGUAGE plpgsql AS '
DECLARE expected bigint; stable_id text;
BEGIN
  SELECT COALESCE(max(version),0)+1,min(id) INTO expected,stable_id FROM entitlement_quota_policies WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND metric_key=NEW.metric_key;
  IF NEW.version<>expected THEN RAISE EXCEPTION ''entitlement quota policy version invalid''; END IF;
  IF stable_id IS NOT NULL AND stable_id<>NEW.id THEN RAISE EXCEPTION ''entitlement quota policy identity must remain stable''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER entitlement_quota_policy_insert_guard BEFORE INSERT ON entitlement_quota_policies FOR EACH ROW EXECUTE FUNCTION entitlement_quota_policy_insert_guard();
CREATE TRIGGER entitlement_quota_policies_append_only BEFORE UPDATE OR DELETE ON entitlement_quota_policies FOR EACH ROW EXECUTE FUNCTION entitlement_append_only();

CREATE TABLE entitlement_quota_counters (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  metric_key text NOT NULL CHECK(metric_key ~ '^[a-z][a-z0-9._:/-]{0,127}$'),
  bucket_start timestamptz NOT NULL,
  bucket_end timestamptz NOT NULL,
  used bigint NOT NULL DEFAULT 0 CHECK(used >= 0),
  limit_snapshot bigint NOT NULL CHECK(limit_snapshot >= 0),
  policy_id text NOT NULL,
  policy_version bigint NOT NULL CHECK(policy_version >= 1),
  updated_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,metric_key,bucket_start),
  CONSTRAINT entitlement_quota_counters_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT entitlement_quota_counter_window CHECK(bucket_end > bucket_start),
  CONSTRAINT entitlement_quota_counter_limit CHECK(used <= limit_snapshot)
);
CREATE INDEX entitlement_quota_counter_current_idx ON entitlement_quota_counters(organization_id,workspace_id,metric_key,bucket_end);
CREATE FUNCTION entitlement_quota_counter_guard() RETURNS trigger LANGUAGE plpgsql AS '
BEGIN
  IF TG_OP=''INSERT'' THEN
    IF NEW.used<>0 THEN RAISE EXCEPTION ''quota counter must start at zero''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.metric_key IS DISTINCT FROM OLD.metric_key OR NEW.bucket_start IS DISTINCT FROM OLD.bucket_start THEN RAISE EXCEPTION ''quota counter identity immutable''; END IF;
  IF NEW.used < OLD.used OR NEW.updated_at < OLD.updated_at THEN RAISE EXCEPTION ''quota counter cannot move backwards''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER entitlement_quota_counter_guard BEFORE INSERT OR UPDATE ON entitlement_quota_counters FOR EACH ROW EXECUTE FUNCTION entitlement_quota_counter_guard();

CREATE TABLE entitlement_quota_usage (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  metric_key text NOT NULL CHECK(metric_key ~ '^[a-z][a-z0-9._:/-]{0,127}$'),
  amount bigint NOT NULL CHECK(amount > 0),
  bucket_start timestamptz NOT NULL,
  bucket_end timestamptz NOT NULL,
  correlation_id text NOT NULL CHECK(char_length(correlation_id) BETWEEN 1 AND 256),
  policy_id text NOT NULL,
  policy_version bigint NOT NULL CHECK(policy_version >= 1),
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,id),
  CONSTRAINT entitlement_quota_usage_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT entitlement_quota_usage_policy_fk FOREIGN KEY(organization_id,workspace_id,policy_id,policy_version) REFERENCES entitlement_quota_policies(organization_id,workspace_id,id,version),
  CONSTRAINT entitlement_quota_usage_window CHECK(bucket_end > bucket_start AND occurred_at >= bucket_start AND occurred_at < bucket_end)
);
CREATE INDEX entitlement_quota_usage_metric_idx ON entitlement_quota_usage(organization_id,workspace_id,metric_key,bucket_start,occurred_at,id);
CREATE TRIGGER entitlement_quota_usage_append_only BEFORE UPDATE OR DELETE ON entitlement_quota_usage FOR EACH ROW EXECUTE FUNCTION entitlement_append_only();

ALTER TABLE entitlement_rules ENABLE ROW LEVEL SECURITY; ALTER TABLE entitlement_rules FORCE ROW LEVEL SECURITY;
ALTER TABLE entitlement_quota_policies ENABLE ROW LEVEL SECURITY; ALTER TABLE entitlement_quota_policies FORCE ROW LEVEL SECURITY;
ALTER TABLE entitlement_quota_counters ENABLE ROW LEVEL SECURITY; ALTER TABLE entitlement_quota_counters FORCE ROW LEVEL SECURITY;
ALTER TABLE entitlement_quota_usage ENABLE ROW LEVEL SECURITY; ALTER TABLE entitlement_quota_usage FORCE ROW LEVEL SECURITY;

CREATE POLICY entitlement_rules_select ON entitlement_rules FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY entitlement_rules_insert ON entitlement_rules FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY entitlement_quota_policies_select ON entitlement_quota_policies FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY entitlement_quota_policies_insert ON entitlement_quota_policies FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY entitlement_quota_counters_select ON entitlement_quota_counters FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY entitlement_quota_counters_insert ON entitlement_quota_counters FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY entitlement_quota_counters_update ON entitlement_quota_counters FOR UPDATE USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY entitlement_quota_usage_select ON entitlement_quota_usage FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY entitlement_quota_usage_insert ON entitlement_quota_usage FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

CREATE FUNCTION entitlement_no_delete() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''entitlement evidence cannot be hard-deleted''; END';
CREATE FUNCTION entitlement_no_clear() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''entitlement evidence cannot be cleared''; END';
CREATE TRIGGER entitlement_rules_no_clear BEFORE TRUNCATE ON entitlement_rules EXECUTE FUNCTION entitlement_no_clear();
CREATE TRIGGER entitlement_quota_policies_no_clear BEFORE TRUNCATE ON entitlement_quota_policies EXECUTE FUNCTION entitlement_no_clear();
CREATE TRIGGER entitlement_quota_counters_no_delete BEFORE DELETE ON entitlement_quota_counters FOR EACH ROW EXECUTE FUNCTION entitlement_no_delete();
CREATE TRIGGER entitlement_quota_counters_no_clear BEFORE TRUNCATE ON entitlement_quota_counters EXECUTE FUNCTION entitlement_no_clear();
CREATE TRIGGER entitlement_quota_usage_no_clear BEFORE TRUNCATE ON entitlement_quota_usage EXECUTE FUNCTION entitlement_no_clear();

COMMENT ON TABLE entitlement_rules IS 'Provider-neutral versioned tenant feature entitlements. No subscription plan names are stored here.';
COMMENT ON TABLE entitlement_quota_policies IS 'Provider-neutral versioned tenant quota policies. Cloud billing may synchronize these later but Community runtime does not depend on billing.';
COMMENT ON TABLE entitlement_quota_usage IS 'Append-only idempotent quota usage evidence; counters are derived enforcement state.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
