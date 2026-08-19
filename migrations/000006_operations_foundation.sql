BEGIN;

-- TORGNEXA pre-v1 baseline component 000006: operations_foundation.
-- Squashed, statement-order-preserving source range: legacy 000018..000027.
-- Do not edit by hand; regenerate with scripts/generate-pre-v1-baseline.py.

-- BASELINE_SOURCE_BEGIN

-- SOURCE 000018_entitlements.sql
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

-- SOURCE 000019_search_provider.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Search remains a derived read capability over PostgreSQL system-of-record rows.
-- Immutable helpers keep the query expression identical to the GIN indexes and
-- avoid adding provider-specific or independently mutable search documents.
CREATE FUNCTION search_product_vector(code text, title text, description text) RETURNS tsvector
LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS 'SELECT setweight(to_tsvector(''simple''::regconfig,COALESCE(code,'''')),''A'') || setweight(to_tsvector(''simple''::regconfig,COALESCE(title,'''')),''A'') || setweight(to_tsvector(''simple''::regconfig,COALESCE(description,'''')),''B'')';

CREATE FUNCTION search_offer_vector(sku text, gtin text) RETURNS tsvector
LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS 'SELECT setweight(to_tsvector(''simple''::regconfig,COALESCE(sku,'''')),''A'') || setweight(to_tsvector(''simple''::regconfig,COALESCE(gtin,'''')),''A'')';

CREATE FUNCTION search_order_vector(order_number text) RETURNS tsvector
LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS 'SELECT setweight(to_tsvector(''simple''::regconfig,COALESCE(order_number,'''')),''A'')';

CREATE FUNCTION search_order_item_vector(sku_snapshot text) RETURNS tsvector
LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS 'SELECT setweight(to_tsvector(''simple''::regconfig,COALESCE(sku_snapshot,'''')),''A'')';

CREATE INDEX products_search_fts_idx ON products USING GIN(search_product_vector(code,title,description));
CREATE INDEX offers_search_fts_idx ON offers USING GIN(search_offer_vector(sku,gtin));
CREATE INDEX orders_search_fts_idx ON orders USING GIN(search_order_vector(order_number));
CREATE INDEX order_items_search_fts_idx ON order_items USING GIN(search_order_item_vector(sku_snapshot));

-- Prefix/exact ranking is intentionally bounded by tenant predicates in the
-- repository. These indexes support the identifier/title prefix paths without
-- introducing pg_trgm or another extension dependency.
CREATE INDEX products_tenant_code_lower_prefix_idx ON products(organization_id,workspace_id,(lower(code)) text_pattern_ops);
CREATE INDEX products_tenant_title_lower_prefix_idx ON products(organization_id,workspace_id,(lower(title)) text_pattern_ops);
CREATE INDEX offers_tenant_sku_lower_prefix_idx ON offers(organization_id,workspace_id,(lower(sku)) text_pattern_ops);
CREATE INDEX orders_tenant_number_lower_prefix_idx ON orders(organization_id,workspace_id,(lower(order_number)) text_pattern_ops);
CREATE INDEX order_items_tenant_sku_lower_prefix_idx ON order_items(organization_id,workspace_id,(lower(sku_snapshot)) text_pattern_ops);

COMMENT ON FUNCTION search_product_vector(text,text,text) IS 'Derived PostgreSQL FTS projection for canonical Product fields; source rows remain authoritative and tenant RLS remains mandatory.';
COMMENT ON FUNCTION search_offer_vector(text,text) IS 'Derived PostgreSQL FTS projection for canonical Offer SKU/GTIN fields.';
COMMENT ON FUNCTION search_order_vector(text) IS 'Derived PostgreSQL FTS projection for canonical Order number.';
COMMENT ON FUNCTION search_order_item_vector(text) IS 'Derived PostgreSQL FTS projection for immutable OrderItem SKU snapshot.';

-- SOURCE 000020_durable_webhooks.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE webhook_subscriptions (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  endpoint_url text NOT NULL,
  event_types jsonb NOT NULL,
  status text NOT NULL DEFAULT 'active',
  signing_secret_reference text NOT NULL,
  previous_signing_secret_reference text,
  previous_valid_until timestamptz,
  consecutive_failures integer NOT NULL DEFAULT 0,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT webhook_subscriptions_pkey PRIMARY KEY (id),
  CONSTRAINT webhook_subscriptions_tenant_key UNIQUE (id, organization_id, workspace_id),
  CONSTRAINT webhook_subscriptions_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT webhook_subscriptions_secret_fk FOREIGN KEY (signing_secret_reference, organization_id, workspace_id)
    REFERENCES secret_references (reference, organization_id, workspace_id) ON DELETE RESTRICT,
  CONSTRAINT webhook_subscriptions_previous_secret_fk FOREIGN KEY (previous_signing_secret_reference, organization_id, workspace_id)
    REFERENCES secret_references (reference, organization_id, workspace_id) ON DELETE RESTRICT,
  CONSTRAINT webhook_subscriptions_id_chk CHECK (id = btrim(id) AND char_length(id) BETWEEN 1 AND 128 AND id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'),
  CONSTRAINT webhook_subscriptions_endpoint_chk CHECK (endpoint_url = btrim(endpoint_url) AND char_length(endpoint_url) BETWEEN 1 AND 2048 AND endpoint_url ~ '^https://'),
  CONSTRAINT webhook_subscriptions_events_chk CHECK (jsonb_typeof(event_types)='array' AND jsonb_array_length(event_types) BETWEEN 1 AND 128),
  CONSTRAINT webhook_subscriptions_status_chk CHECK (status IN ('active','disabled')),
  CONSTRAINT webhook_subscriptions_failure_chk CHECK (consecutive_failures >= 0),
  CONSTRAINT webhook_subscriptions_rotation_chk CHECK ((previous_signing_secret_reference IS NULL) = (previous_valid_until IS NULL)),
  CONSTRAINT webhook_subscriptions_time_chk CHECK (updated_at >= created_at),
  CONSTRAINT webhook_subscriptions_version_chk CHECK (version >= 1)
);

CREATE TABLE webhook_deliveries (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  subscription_id text NOT NULL,
  event_id text NOT NULL,
  event_type text NOT NULL,
  endpoint_url text NOT NULL,
  signing_secret_reference text NOT NULL,
  body jsonb NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  attempt integer NOT NULL DEFAULT 0,
  available_at timestamptz NOT NULL,
  lease_token text,
  lease_expires_at timestamptz,
  replay_of text,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  succeeded_at timestamptz,
  dlq_at timestamptz,
  last_http_status integer,
  last_error_code text,
  CONSTRAINT webhook_deliveries_pkey PRIMARY KEY (id),
  CONSTRAINT webhook_deliveries_tenant_key UNIQUE (id, organization_id, workspace_id),
  CONSTRAINT webhook_deliveries_subscription_fk FOREIGN KEY (subscription_id, organization_id, workspace_id)
    REFERENCES webhook_subscriptions (id, organization_id, workspace_id) ON DELETE RESTRICT,
  CONSTRAINT webhook_deliveries_secret_fk FOREIGN KEY (signing_secret_reference, organization_id, workspace_id)
    REFERENCES secret_references (reference, organization_id, workspace_id) ON DELETE RESTRICT,
  CONSTRAINT webhook_deliveries_replay_fk FOREIGN KEY (replay_of, organization_id, workspace_id)
    REFERENCES webhook_deliveries (id, organization_id, workspace_id) ON DELETE RESTRICT,
  CONSTRAINT webhook_deliveries_body_chk CHECK (jsonb_typeof(body) = 'object' AND octet_length(body::text) <= 1081344),
  CONSTRAINT webhook_deliveries_status_chk CHECK (status IN ('pending','inflight','succeeded','dlq')),
  CONSTRAINT webhook_deliveries_attempt_chk CHECK (attempt >= 0 AND attempt <= 32),
  CONSTRAINT webhook_deliveries_lease_chk CHECK ((lease_token IS NULL) = (lease_expires_at IS NULL)),
  CONSTRAINT webhook_deliveries_terminal_chk CHECK (
    (status='succeeded' AND succeeded_at IS NOT NULL AND dlq_at IS NULL) OR
    (status='dlq' AND dlq_at IS NOT NULL AND succeeded_at IS NULL) OR
    (status IN ('pending','inflight') AND succeeded_at IS NULL AND dlq_at IS NULL)
  ),
  CONSTRAINT webhook_deliveries_error_code_chk CHECK (last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  CONSTRAINT webhook_deliveries_http_status_chk CHECK (last_http_status IS NULL OR last_http_status BETWEEN 100 AND 599),
  CONSTRAINT webhook_deliveries_time_chk CHECK (updated_at >= created_at AND available_at >= created_at)
);
CREATE UNIQUE INDEX webhook_deliveries_initial_event_uniq
  ON webhook_deliveries (organization_id, workspace_id, subscription_id, event_id)
  WHERE replay_of IS NULL;
CREATE INDEX webhook_deliveries_claim_idx
  ON webhook_deliveries (organization_id, workspace_id, available_at, created_at, id)
  WHERE status IN ('pending','inflight');
CREATE INDEX webhook_deliveries_history_idx
  ON webhook_deliveries (organization_id, workspace_id, subscription_id, created_at DESC, id DESC);

CREATE TABLE webhook_delivery_attempts (
  delivery_id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  attempt integer NOT NULL,
  outcome text NOT NULL,
  http_status integer,
  duration_ms bigint NOT NULL,
  error_code text,
  completed_at timestamptz NOT NULL,
  CONSTRAINT webhook_delivery_attempts_pkey PRIMARY KEY (delivery_id, attempt),
  CONSTRAINT webhook_delivery_attempts_delivery_fk FOREIGN KEY (delivery_id, organization_id, workspace_id)
    REFERENCES webhook_deliveries (id, organization_id, workspace_id) ON DELETE RESTRICT,
  CONSTRAINT webhook_delivery_attempts_attempt_chk CHECK (attempt BETWEEN 1 AND 32),
  CONSTRAINT webhook_delivery_attempts_outcome_chk CHECK (outcome IN ('succeeded','retry','dlq')),
  CONSTRAINT webhook_delivery_attempts_status_chk CHECK (http_status IS NULL OR http_status BETWEEN 100 AND 599),
  CONSTRAINT webhook_delivery_attempts_duration_chk CHECK (duration_ms >= 0 AND duration_ms <= 3600000),
  CONSTRAINT webhook_delivery_attempts_error_chk CHECK (error_code IS NULL OR error_code ~ '^[a-z][a-z0-9_]{0,63}$')
);
CREATE INDEX webhook_delivery_attempts_tenant_history_idx
  ON webhook_delivery_attempts (organization_id, workspace_id, delivery_id, attempt DESC);

ALTER TABLE webhook_subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_subscriptions FORCE ROW LEVEL SECURITY;
ALTER TABLE webhook_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_deliveries FORCE ROW LEVEL SECURITY;
ALTER TABLE webhook_delivery_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_delivery_attempts FORCE ROW LEVEL SECURITY;

CREATE POLICY webhook_subscriptions_tenant_all ON webhook_subscriptions FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY webhook_deliveries_tenant_all ON webhook_deliveries FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY webhook_delivery_attempts_tenant_select ON webhook_delivery_attempts FOR SELECT
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY webhook_delivery_attempts_tenant_insert ON webhook_delivery_attempts FOR INSERT
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE DELETE, TRUNCATE ON webhook_subscriptions, webhook_deliveries, webhook_delivery_attempts FROM PUBLIC;
REVOKE UPDATE ON webhook_delivery_attempts FROM PUBLIC;

CREATE FUNCTION webhook_deliveries_guard_update() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF NEW.id<>OLD.id OR NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id
     OR NEW.subscription_id<>OLD.subscription_id OR NEW.event_id<>OLD.event_id OR NEW.event_type<>OLD.event_type
     OR NEW.endpoint_url<>OLD.endpoint_url OR NEW.signing_secret_reference<>OLD.signing_secret_reference
     OR NEW.body<>OLD.body OR NEW.replay_of IS DISTINCT FROM OLD.replay_of OR NEW.created_at<>OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''webhook delivery identity and request snapshot are immutable'';
  END IF;
  IF NEW.attempt < OLD.attempt THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''webhook delivery attempt cannot decrease'';
  END IF;
  IF OLD.status IN (''succeeded'',''dlq'') AND NEW IS DISTINCT FROM OLD THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''terminal webhook delivery is immutable'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER webhook_deliveries_update_guard BEFORE UPDATE ON webhook_deliveries
  FOR EACH ROW EXECUTE FUNCTION webhook_deliveries_guard_update();

CREATE FUNCTION webhook_attempts_reject_mutation() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''webhook delivery attempts are immutable'';
  RETURN NULL;
END';
CREATE TRIGGER webhook_attempts_no_update_delete BEFORE UPDATE OR DELETE ON webhook_delivery_attempts
  FOR EACH ROW EXECUTE FUNCTION webhook_attempts_reject_mutation();
CREATE TRIGGER webhook_attempts_no_clear BEFORE TRUNCATE ON webhook_delivery_attempts
  FOR EACH STATEMENT EXECUTE FUNCTION webhook_attempts_reject_mutation();

COMMENT ON TABLE webhook_deliveries IS 'Durable immutable webhook request snapshots with mutable operational delivery state; at-least-once attempts are recorded separately.';
COMMENT ON COLUMN webhook_deliveries.body IS 'Canonical webhook envelope only. Never stores response bodies, Authorization headers, plaintext signing material or arbitrary remote error text.';
COMMENT ON TABLE webhook_delivery_attempts IS 'Immutable bounded delivery history containing only outcome/status/duration/machine error code; remote response bodies and raw errors are forbidden.';

-- SOURCE 000021_notifications.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE notifications (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  recipient_id text NOT NULL,
  dedupe_key text NOT NULL,
  severity text NOT NULL,
  title text NOT NULL,
  body text NOT NULL DEFAULT '',
  entity_type text,
  entity_id text,
  source_event_id text,
  source_event_type text,
  occurrence_count integer NOT NULL DEFAULT 1,
  first_occurred_at timestamptz NOT NULL,
  last_occurred_at timestamptz NOT NULL,
  read_at timestamptz,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT notifications_pkey PRIMARY KEY (id),
  CONSTRAINT notifications_tenant_key UNIQUE (id, organization_id, workspace_id),
  CONSTRAINT notifications_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT notifications_dedupe_uniq UNIQUE (organization_id, workspace_id, recipient_id, dedupe_key),
  CONSTRAINT notifications_id_chk CHECK (id = btrim(id) AND char_length(id) BETWEEN 1 AND 128 AND id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'),
  CONSTRAINT notifications_recipient_chk CHECK (recipient_id = btrim(recipient_id) AND char_length(recipient_id) BETWEEN 1 AND 128 AND recipient_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'),
  CONSTRAINT notifications_dedupe_chk CHECK (dedupe_key = btrim(dedupe_key) AND char_length(dedupe_key) BETWEEN 1 AND 200 AND dedupe_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'),
  CONSTRAINT notifications_severity_chk CHECK (severity IN ('info','warning','critical')),
  CONSTRAINT notifications_title_chk CHECK (title=btrim(title) AND char_length(title) BETWEEN 1 AND 200),
  CONSTRAINT notifications_body_chk CHECK (body=btrim(body) AND char_length(body) <= 4000),
  CONSTRAINT notifications_entity_pair_chk CHECK ((entity_type IS NULL)=(entity_id IS NULL)),
  CONSTRAINT notifications_source_pair_chk CHECK ((source_event_id IS NULL)=(source_event_type IS NULL)),
  CONSTRAINT notifications_entity_type_chk CHECK (entity_type IS NULL OR (entity_type=btrim(entity_type) AND char_length(entity_type) BETWEEN 1 AND 128 AND entity_type ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$')),
  CONSTRAINT notifications_entity_id_chk CHECK (entity_id IS NULL OR (entity_id=btrim(entity_id) AND char_length(entity_id) BETWEEN 1 AND 128 AND entity_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$')),
  CONSTRAINT notifications_source_event_id_chk CHECK (source_event_id IS NULL OR (source_event_id=btrim(source_event_id) AND char_length(source_event_id) BETWEEN 1 AND 128 AND source_event_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$')),
  CONSTRAINT notifications_source_event_type_chk CHECK (source_event_type IS NULL OR (char_length(source_event_type) BETWEEN 8 AND 255 AND source_event_type ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*\.v[1-9][0-9]{0,2}$')),
  CONSTRAINT notifications_occurrence_chk CHECK (occurrence_count >= 1),
  CONSTRAINT notifications_time_chk CHECK (last_occurred_at >= first_occurred_at AND updated_at >= created_at AND (read_at IS NULL OR read_at >= created_at))
);
CREATE INDEX notifications_inbox_idx ON notifications(organization_id,workspace_id,recipient_id,last_occurred_at DESC,id DESC);
CREATE INDEX notifications_unread_idx ON notifications(organization_id,workspace_id,recipient_id,last_occurred_at DESC) WHERE read_at IS NULL;

CREATE TABLE notification_preferences (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  recipient_id text NOT NULL,
  channel text NOT NULL,
  enabled boolean NOT NULL,
  min_severity text NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  updated_at timestamptz NOT NULL,
  CONSTRAINT notification_preferences_pkey PRIMARY KEY (organization_id,workspace_id,recipient_id,channel),
  CONSTRAINT notification_preferences_workspace_fk FOREIGN KEY (organization_id,workspace_id)
    REFERENCES workspaces (organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT notification_preferences_recipient_chk CHECK (recipient_id=btrim(recipient_id) AND char_length(recipient_id) BETWEEN 1 AND 128 AND recipient_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'),
  CONSTRAINT notification_preferences_channel_chk CHECK (channel IN ('web_ui','webhook')),
  CONSTRAINT notification_preferences_severity_chk CHECK (min_severity IN ('info','warning','critical')),
  CONSTRAINT notification_preferences_version_chk CHECK (version >= 1)
);

CREATE TABLE notification_deliveries (
  notification_id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  channel text NOT NULL,
  status text NOT NULL,
  error_code text,
  occurrence integer NOT NULL,
  attempt integer NOT NULL,
  attempted_at timestamptz NOT NULL,
  CONSTRAINT notification_deliveries_pkey PRIMARY KEY (notification_id,channel,occurrence,attempt),
  CONSTRAINT notification_deliveries_notification_fk FOREIGN KEY (notification_id,organization_id,workspace_id)
    REFERENCES notifications(id,organization_id,workspace_id) ON DELETE RESTRICT,
  CONSTRAINT notification_deliveries_channel_chk CHECK (channel IN ('web_ui','webhook')),
  CONSTRAINT notification_deliveries_status_chk CHECK (status IN ('succeeded','suppressed','failed')),
  CONSTRAINT notification_deliveries_error_chk CHECK ((status='failed' AND error_code ~ '^[a-z][a-z0-9_]{0,63}$') OR (status<>'failed' AND error_code IS NULL)),
  CONSTRAINT notification_deliveries_occurrence_chk CHECK (occurrence >= 1),
  CONSTRAINT notification_deliveries_attempt_chk CHECK (attempt BETWEEN 1 AND 64)
);
CREATE INDEX notification_deliveries_history_idx ON notification_deliveries(organization_id,workspace_id,notification_id,occurrence,channel,attempt);

CREATE FUNCTION notifications_guard_update() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF NEW.id<>OLD.id OR NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.recipient_id<>OLD.recipient_id OR NEW.dedupe_key<>OLD.dedupe_key OR NEW.first_occurred_at<>OLD.first_occurred_at OR NEW.created_at<>OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''notification identity is immutable'';
  END IF;
  IF NEW.occurrence_count<OLD.occurrence_count THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''notification occurrence count cannot decrease'';
  END IF;
  IF NEW.last_occurred_at<OLD.last_occurred_at OR NEW.updated_at<OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''notification timestamps cannot move backwards'';
  END IF;
  IF (CASE OLD.severity WHEN ''info'' THEN 1 WHEN ''warning'' THEN 2 WHEN ''critical'' THEN 3 ELSE 99 END) > (CASE NEW.severity WHEN ''info'' THEN 1 WHEN ''warning'' THEN 2 WHEN ''critical'' THEN 3 ELSE 0 END) THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''notification severity cannot decrease'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER notifications_guard_update BEFORE UPDATE ON notifications
  FOR EACH ROW EXECUTE FUNCTION notifications_guard_update();

ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications FORCE ROW LEVEL SECURITY;
ALTER TABLE notification_preferences ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_preferences FORCE ROW LEVEL SECURITY;
ALTER TABLE notification_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_deliveries FORCE ROW LEVEL SECURITY;

CREATE POLICY notifications_tenant_all ON notifications FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY notification_preferences_tenant_all ON notification_preferences FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY notification_deliveries_tenant_select ON notification_deliveries FOR SELECT
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY notification_deliveries_tenant_insert ON notification_deliveries FOR INSERT
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE DELETE, TRUNCATE ON notifications, notification_preferences, notification_deliveries FROM PUBLIC;
REVOKE UPDATE ON notification_deliveries FROM PUBLIC;

CREATE FUNCTION notification_deliveries_reject_mutation() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''notification delivery history is immutable'';
  RETURN NULL;
END';
CREATE TRIGGER notification_deliveries_no_update_delete BEFORE UPDATE OR DELETE ON notification_deliveries
  FOR EACH ROW EXECUTE FUNCTION notification_deliveries_reject_mutation();
CREATE TRIGGER notification_deliveries_no_clear BEFORE TRUNCATE ON notification_deliveries
  FOR EACH STATEMENT EXECUTE FUNCTION notification_deliveries_reject_mutation();

COMMENT ON TABLE notifications IS 'Tenant-scoped canonical notification inbox. Dedupe key collapses repeated conditions while occurrence_count and monotonic severity preserve material changes.';
COMMENT ON TABLE notification_preferences IS 'Per-recipient channel enablement and minimum severity. External webhook delivery is opt-in by default in application policy.';
COMMENT ON TABLE notification_deliveries IS 'Immutable per-occurrence channel attempt history; retries append attempts and remote bodies, headers, tokens and raw provider errors are forbidden.';

-- SOURCE 000022_upload_quarantine_foundation.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE uploads (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  original_filename text NOT NULL,
  declared_media_type text,
  declared_size_bytes bigint NOT NULL,
  state text NOT NULL,
  quarantine_object_key text,
  released_object_key text,
  content_size_bytes bigint,
  content_sha256 text,
  security_evidence_id text,
  version bigint NOT NULL DEFAULT 1,
  received_at timestamptz NOT NULL,
  quarantined_at timestamptz,
  released_at timestamptz,
  updated_at timestamptz NOT NULL,
  CONSTRAINT uploads_pkey PRIMARY KEY (id),
  CONSTRAINT uploads_tenant_key UNIQUE (id, organization_id, workspace_id),
  CONSTRAINT uploads_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT uploads_id_chk CHECK (id ~ '^upl_[0-9a-f]{32}$'),
  CONSTRAINT uploads_filename_chk CHECK (original_filename=btrim(original_filename) AND char_length(original_filename) BETWEEN 1 AND 512),
  CONSTRAINT uploads_media_type_chk CHECK (declared_media_type IS NULL OR (char_length(declared_media_type) BETWEEN 3 AND 255 AND declared_media_type ~ '^[A-Za-z0-9][A-Za-z0-9!#$&^_.+/-]{0,126}/[A-Za-z0-9][A-Za-z0-9!#$&^_.+-]{0,126}$')),
  CONSTRAINT uploads_declared_size_chk CHECK (declared_size_bytes BETWEEN 0 AND 10737418240),
  CONSTRAINT uploads_content_size_chk CHECK (content_size_bytes IS NULL OR content_size_bytes BETWEEN 0 AND 10737418240),
  CONSTRAINT uploads_sha256_chk CHECK (content_sha256 IS NULL OR content_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT uploads_evidence_chk CHECK (security_evidence_id IS NULL OR (security_evidence_id=btrim(security_evidence_id) AND char_length(security_evidence_id) BETWEEN 1 AND 128 AND security_evidence_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$')),
  CONSTRAINT uploads_state_chk CHECK (state IN ('received','quarantined','validated','scanning','clean','rejected','released')),
  CONSTRAINT uploads_version_chk CHECK (version >= 1),
  CONSTRAINT uploads_time_chk CHECK (updated_at >= received_at AND (quarantined_at IS NULL OR quarantined_at >= received_at) AND (released_at IS NULL OR (quarantined_at IS NOT NULL AND released_at >= quarantined_at))),
  CONSTRAINT uploads_quarantine_key_chk CHECK (quarantine_object_key IS NULL OR quarantine_object_key = 'quarantine/'||organization_id||'/'||workspace_id||'/'||id||'/object'),
  CONSTRAINT uploads_release_key_chk CHECK (released_object_key IS NULL OR released_object_key = 'released/'||organization_id||'/'||workspace_id||'/'||id||'/object'),
  CONSTRAINT uploads_lifecycle_shape_chk CHECK (
    (state='received' AND quarantine_object_key IS NULL AND released_object_key IS NULL AND content_size_bytes IS NULL AND content_sha256 IS NULL AND security_evidence_id IS NULL AND quarantined_at IS NULL AND released_at IS NULL)
    OR
    (state IN ('quarantined','validated','scanning') AND quarantine_object_key IS NOT NULL AND released_object_key IS NULL AND content_size_bytes IS NOT NULL AND content_sha256 IS NOT NULL AND security_evidence_id IS NULL AND quarantined_at IS NOT NULL AND released_at IS NULL)
    OR
    (state IN ('clean','rejected') AND quarantine_object_key IS NOT NULL AND released_object_key IS NULL AND content_size_bytes IS NOT NULL AND content_sha256 IS NOT NULL AND security_evidence_id IS NOT NULL AND quarantined_at IS NOT NULL AND released_at IS NULL)
    OR
    (state='released' AND quarantine_object_key IS NOT NULL AND released_object_key IS NOT NULL AND content_size_bytes IS NOT NULL AND content_sha256 IS NOT NULL AND security_evidence_id IS NOT NULL AND quarantined_at IS NOT NULL AND released_at IS NOT NULL)
  )
);

CREATE INDEX uploads_tenant_state_idx ON uploads(organization_id, workspace_id, state, updated_at DESC, id DESC);

ALTER TABLE uploads ENABLE ROW LEVEL SECURITY;
ALTER TABLE uploads FORCE ROW LEVEL SECURITY;
CREATE POLICY uploads_tenant_all ON uploads FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- Task 088a is intentionally fail-closed. This trigger permits only the
-- foundation transition. Task 088b must replace it when security evidence,
-- scanner/parser/archive controls and release authorization exist.
CREATE FUNCTION uploads_foundation_guard_update() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF NEW.id<>OLD.id OR NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.original_filename<>OLD.original_filename OR NEW.declared_media_type IS DISTINCT FROM OLD.declared_media_type OR NEW.declared_size_bytes<>OLD.declared_size_bytes OR NEW.received_at<>OLD.received_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''upload identity and client metadata are immutable'';
  END IF;
  IF NOT (OLD.state=''received'' AND NEW.state=''quarantined'') THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''upload security pipeline incomplete: only received to quarantined is allowed before task 088b'';
  END IF;
  IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''upload version/time must advance exactly once'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER uploads_foundation_guard_update BEFORE UPDATE ON uploads
  FOR EACH ROW EXECUTE FUNCTION uploads_foundation_guard_update();

REVOKE DELETE, TRUNCATE ON uploads FROM PUBLIC;
CREATE FUNCTION uploads_reject_delete() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''upload records are retained through the security pipeline'';
  RETURN NULL;
END';
CREATE TRIGGER uploads_no_delete BEFORE DELETE ON uploads FOR EACH ROW EXECUTE FUNCTION uploads_reject_delete();
CREATE TRIGGER uploads_no_clear BEFORE TRUNCATE ON uploads FOR EACH STATEMENT EXECUTE FUNCTION uploads_reject_delete();

COMMENT ON TABLE uploads IS 'Tenant-scoped upload quarantine state. Task 088a permits only RECEIVED->QUARANTINED; downstream access requires RELEASED plus security evidence introduced by Task 088b.';
COMMENT ON COLUMN uploads.original_filename IS 'Untrusted display metadata only. It must never be interpreted as an object-storage or filesystem path.';
COMMENT ON COLUMN uploads.security_evidence_id IS 'Reserved for immutable Task-088b validation/scan evidence. Foundation writes NULL only.';

-- SOURCE 000023_upload_security_pipeline.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE upload_security_evidence (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  upload_id text NOT NULL,
  attempt bigint NOT NULL,
  policy_version text NOT NULL,
  content_sha256 text NOT NULL,
  content_size_bytes bigint NOT NULL,
  detected_media_type text NOT NULL,
  extension text NOT NULL,
  decision text NOT NULL,
  reason_code text NOT NULL,
  checks jsonb NOT NULL,
  scanner_name text NOT NULL,
  scanner_engine_version text NOT NULL,
  scanner_signature_version text NOT NULL,
  scanner_status text NOT NULL,
  threat_code text,
  rescan_of text,
  created_at timestamptz NOT NULL,
  CONSTRAINT upload_security_evidence_pkey PRIMARY KEY (id),
  CONSTRAINT upload_security_evidence_tenant_attempt UNIQUE (organization_id, workspace_id, upload_id, attempt),
  CONSTRAINT upload_security_evidence_tenant_identity UNIQUE (id, organization_id, workspace_id, upload_id),
  CONSTRAINT upload_security_evidence_upload_fk FOREIGN KEY (upload_id, organization_id, workspace_id)
    REFERENCES uploads (id, organization_id, workspace_id) ON DELETE RESTRICT,
  CONSTRAINT upload_security_evidence_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT upload_security_evidence_id_chk CHECK (id ~ '^uev_[0-9a-f]{32}$'),
  CONSTRAINT upload_security_evidence_attempt_chk CHECK (attempt >= 1),
  CONSTRAINT upload_security_evidence_policy_chk CHECK (policy_version ~ '^[a-z][a-z0-9._-]{0,127}$'),
  CONSTRAINT upload_security_evidence_sha_chk CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT upload_security_evidence_size_chk CHECK (content_size_bytes BETWEEN 0 AND 10737418240),
  CONSTRAINT upload_security_evidence_media_chk CHECK (char_length(detected_media_type) BETWEEN 3 AND 255 AND detected_media_type ~ '^[A-Za-z0-9][A-Za-z0-9!#$&^_.+/-]{0,126}/[A-Za-z0-9][A-Za-z0-9!#$&^_.+-]{0,126}$'),
  CONSTRAINT upload_security_evidence_extension_chk CHECK (extension='none' OR extension ~ '^\.[a-z0-9]{1,15}$'),
  CONSTRAINT upload_security_evidence_decision_chk CHECK (decision IN ('clean','rejected','error')),
  CONSTRAINT upload_security_evidence_reason_chk CHECK (reason_code ~ '^[a-z0-9][a-z0-9._-]{0,95}$'),
  CONSTRAINT upload_security_evidence_checks_chk CHECK (jsonb_typeof(checks)='array' AND jsonb_array_length(checks) BETWEEN 1 AND 32),
  CONSTRAINT upload_security_evidence_scanner_chk CHECK (
    scanner_name ~ '^[a-z0-9][a-z0-9._-]{0,127}$'
    AND char_length(scanner_engine_version) BETWEEN 1 AND 128
    AND char_length(scanner_signature_version) BETWEEN 1 AND 128
    AND scanner_status IN ('clean','infected','error','not_run')
  ),
  CONSTRAINT upload_security_evidence_threat_chk CHECK (
    (scanner_status='infected' AND threat_code ~ '^[a-z0-9][a-z0-9._-]{0,127}$')
    OR (scanner_status<>'infected' AND threat_code IS NULL)
  ),
  CONSTRAINT upload_security_evidence_decision_scanner_chk CHECK (
    (decision='clean' AND scanner_status='clean')
    OR (decision='error' AND scanner_status='error')
    OR (decision='rejected' AND scanner_status IN ('infected','error','not_run'))
  ),
  CONSTRAINT upload_security_evidence_rescan_chk CHECK (rescan_of IS NULL OR rescan_of ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$')
);

CREATE INDEX upload_security_evidence_history_idx
  ON upload_security_evidence(organization_id, workspace_id, upload_id, attempt DESC);

ALTER TABLE upload_security_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE upload_security_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY upload_security_evidence_tenant_all ON upload_security_evidence FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

ALTER TABLE uploads ADD CONSTRAINT uploads_security_evidence_fk
  FOREIGN KEY (security_evidence_id, organization_id, workspace_id, id)
  REFERENCES upload_security_evidence (id, organization_id, workspace_id, upload_id)
  DEFERRABLE INITIALLY IMMEDIATE;

DROP TRIGGER uploads_foundation_guard_update ON uploads;
DROP FUNCTION uploads_foundation_guard_update();

CREATE FUNCTION uploads_security_guard_update() RETURNS trigger
LANGUAGE plpgsql
AS 'DECLARE
  evidence_decision text;
BEGIN
  IF NEW.id<>OLD.id OR NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.original_filename<>OLD.original_filename OR NEW.declared_media_type IS DISTINCT FROM OLD.declared_media_type OR NEW.declared_size_bytes<>OLD.declared_size_bytes OR NEW.received_at<>OLD.received_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''upload identity and client metadata are immutable'';
  END IF;

  IF OLD.state<>''received'' AND (
    NEW.quarantine_object_key IS DISTINCT FROM OLD.quarantine_object_key
    OR NEW.content_size_bytes IS DISTINCT FROM OLD.content_size_bytes
    OR NEW.content_sha256 IS DISTINCT FROM OLD.content_sha256
    OR NEW.quarantined_at IS DISTINCT FROM OLD.quarantined_at
  ) THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''quarantined upload identity and content metadata are immutable'';
  END IF;

  IF NOT (
    (OLD.state=''received'' AND NEW.state=''quarantined'')
    OR (OLD.state=''quarantined'' AND NEW.state IN (''validated'',''rejected''))
    OR (OLD.state=''validated'' AND NEW.state=''scanning'')
    OR (OLD.state=''scanning'' AND NEW.state IN (''clean'',''rejected''))
    OR (OLD.state=''clean'' AND NEW.state=''released'')
    OR (OLD.state IN (''clean'',''rejected'',''released'') AND NEW.state=''quarantined'')
  ) THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''invalid upload security state transition'';
  END IF;

  IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''upload version/time must advance exactly once'';
  END IF;

  IF NEW.state IN (''clean'',''rejected'',''released'') THEN
    SELECT decision INTO evidence_decision
      FROM upload_security_evidence
      WHERE id=NEW.security_evidence_id
        AND organization_id=NEW.organization_id
        AND workspace_id=NEW.workspace_id
        AND upload_id=NEW.id;
    IF evidence_decision IS NULL THEN
      RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''upload terminal state requires same-tenant immutable security evidence'';
    END IF;
    IF NEW.state=''clean'' AND evidence_decision<>''clean'' THEN
      RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''clean upload requires clean security evidence'';
    END IF;
    IF NEW.state=''rejected'' AND evidence_decision<>''rejected'' THEN
      RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''rejected upload requires rejected security evidence'';
    END IF;
    IF NEW.state=''released'' AND (evidence_decision<>''clean'' OR NEW.security_evidence_id IS DISTINCT FROM OLD.security_evidence_id) THEN
      RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''release must preserve the clean security evidence'';
    END IF;
  END IF;

  IF OLD.state IN (''clean'',''rejected'',''released'') AND NEW.state=''quarantined'' THEN
    IF NEW.security_evidence_id IS NOT NULL OR NEW.released_object_key IS NOT NULL OR NEW.released_at IS NOT NULL THEN
      RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''rescan must revoke released capability before scanning'';
    END IF;
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER uploads_security_guard_update BEFORE UPDATE ON uploads
  FOR EACH ROW EXECUTE FUNCTION uploads_security_guard_update();

CREATE FUNCTION upload_security_evidence_guard_insert() RETURNS trigger
LANGUAGE plpgsql
AS 'DECLARE
  item jsonb;
BEGIN
  FOR item IN SELECT value FROM jsonb_array_elements(NEW.checks) LOOP
    IF jsonb_typeof(item)<>''object''
      OR jsonb_object_length(item)<>2
      OR NOT (item ? ''code'')
      OR NOT (item ? ''outcome'')
      OR jsonb_typeof(item->''code'')<>''string''
      OR jsonb_typeof(item->''outcome'')<>''string''
      OR (item->>''code'') !~ ''^[a-z0-9][a-z0-9._-]{0,127}$''
      OR (item->>''outcome'') NOT IN (''pass'',''fail'') THEN
      RAISE EXCEPTION USING ERRCODE=''23514'', MESSAGE=''invalid upload security check evidence'';
    END IF;
  END LOOP;
  RETURN NEW;
END';
CREATE TRIGGER upload_security_evidence_validate_insert BEFORE INSERT ON upload_security_evidence
  FOR EACH ROW EXECUTE FUNCTION upload_security_evidence_guard_insert();

REVOKE UPDATE, DELETE, TRUNCATE ON upload_security_evidence FROM PUBLIC;
CREATE FUNCTION upload_security_evidence_reject_mutation() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''upload security evidence is immutable'';
  RETURN NULL;
END';
CREATE TRIGGER upload_security_evidence_no_update BEFORE UPDATE ON upload_security_evidence FOR EACH ROW EXECUTE FUNCTION upload_security_evidence_reject_mutation();
CREATE TRIGGER upload_security_evidence_no_delete BEFORE DELETE ON upload_security_evidence FOR EACH ROW EXECUTE FUNCTION upload_security_evidence_reject_mutation();
CREATE TRIGGER upload_security_evidence_no_clear BEFORE TRUNCATE ON upload_security_evidence FOR EACH STATEMENT EXECUTE FUNCTION upload_security_evidence_reject_mutation();

COMMENT ON TABLE upload_security_evidence IS 'Append-only tenant-scoped MIME/archive/parser/malware evidence for Task 088. Raw file content and client credentials are forbidden.';
COMMENT ON COLUMN upload_security_evidence.checks IS 'Bounded machine-code pass/fail checks only; never raw file content or scanner logs.';
COMMENT ON COLUMN uploads.security_evidence_id IS 'Current immutable security decision evidence. Cleared before every re-scan and required for CLEAN/REJECTED/RELEASED.';

-- SOURCE 000024_sync_engine.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE UNIQUE INDEX connector_accounts_sync_tenant_identity_uq
  ON connector_accounts (id, organization_id, workspace_id);

CREATE TABLE sync_policies (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  entity_type text NOT NULL,
  direction text NOT NULL,
  source_of_truth text NOT NULL,
  enabled boolean NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT sync_policies_pkey PRIMARY KEY (id),
  CONSTRAINT sync_policies_tenant_identity UNIQUE (id, organization_id, workspace_id),
  CONSTRAINT sync_policies_account_fk FOREIGN KEY (connector_account_id, organization_id, workspace_id)
    REFERENCES connector_accounts (id, organization_id, workspace_id),
  CONSTRAINT sync_policies_entity_type_chk CHECK (entity_type ~ '^[a-z][a-z0-9._-]{0,63}$'),
  CONSTRAINT sync_policies_direction_chk CHECK (direction IN ('inbound','outbound','bidirectional')),
  CONSTRAINT sync_policies_source_truth_chk CHECK (source_of_truth IN ('local','remote','manual')),
  CONSTRAINT sync_policies_version_chk CHECK (version >= 1),
  CONSTRAINT sync_policies_time_chk CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX sync_policies_account_entity_uq
  ON sync_policies (organization_id, workspace_id, connector_account_id, entity_type);

CREATE TABLE sync_checkpoints (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_id text NOT NULL,
  cursor text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1,
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT sync_checkpoints_pkey PRIMARY KEY (organization_id, workspace_id, policy_id),
  CONSTRAINT sync_checkpoints_policy_fk FOREIGN KEY (policy_id, organization_id, workspace_id)
    REFERENCES sync_policies (id, organization_id, workspace_id),
  CONSTRAINT sync_checkpoints_version_chk CHECK (version >= 1),
  CONSTRAINT sync_checkpoints_cursor_chk CHECK (length(cursor) <= 1024 AND cursor !~ '[[:cntrl:]]')
);

CREATE TABLE sync_entity_states (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_id text NOT NULL,
  local_entity_id text NOT NULL,
  remote_id text NOT NULL,
  last_local_version bigint NOT NULL,
  last_remote_revision text NOT NULL,
  last_synced_fingerprint text NOT NULL,
  last_local_event_id text NOT NULL,
  last_remote_change_id text,
  version bigint NOT NULL DEFAULT 1,
  updated_at timestamptz NOT NULL,
  CONSTRAINT sync_entity_states_pkey PRIMARY KEY (organization_id, workspace_id, policy_id, local_entity_id),
  CONSTRAINT sync_entity_states_remote_uq UNIQUE (organization_id, workspace_id, policy_id, remote_id),
  CONSTRAINT sync_entity_states_policy_fk FOREIGN KEY (policy_id, organization_id, workspace_id)
    REFERENCES sync_policies (id, organization_id, workspace_id),
  CONSTRAINT sync_entity_states_local_version_chk CHECK (last_local_version >= 1),
  CONSTRAINT sync_entity_states_fingerprint_chk CHECK (last_synced_fingerprint ~ '^[0-9a-f]{64}$'),
  CONSTRAINT sync_entity_states_revision_chk CHECK (length(last_remote_revision) BETWEEN 1 AND 256 AND last_remote_revision !~ '[[:cntrl:]]'),
  CONSTRAINT sync_entity_states_remote_id_chk CHECK (length(remote_id) BETWEEN 1 AND 512 AND remote_id !~ '[[:cntrl:]]'),
  CONSTRAINT sync_entity_states_version_chk CHECK (version >= 1)
);

CREATE TABLE sync_local_receipts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_id text NOT NULL,
  change_id text NOT NULL,
  fingerprint text NOT NULL,
  outcome text NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT sync_local_receipts_pkey PRIMARY KEY (organization_id, workspace_id, policy_id, change_id),
  CONSTRAINT sync_local_receipts_policy_fk FOREIGN KEY (policy_id, organization_id, workspace_id)
    REFERENCES sync_policies (id, organization_id, workspace_id),
  CONSTRAINT sync_local_receipts_fingerprint_chk CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
  CONSTRAINT sync_local_receipts_outcome_chk CHECK (outcome IN ('applied','duplicate','loop_suppressed','stale_suppressed','conflict_local_wins'))
);

CREATE TABLE sync_remote_receipts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_id text NOT NULL,
  change_id text NOT NULL,
  fingerprint text NOT NULL,
  outcome text NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT sync_remote_receipts_pkey PRIMARY KEY (organization_id, workspace_id, policy_id, change_id),
  CONSTRAINT sync_remote_receipts_policy_fk FOREIGN KEY (policy_id, organization_id, workspace_id)
    REFERENCES sync_policies (id, organization_id, workspace_id),
  CONSTRAINT sync_remote_receipts_fingerprint_chk CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
  CONSTRAINT sync_remote_receipts_outcome_chk CHECK (outcome IN ('applied','duplicate','loop_suppressed','stale_suppressed','conflict_local_wins'))
);

CREATE INDEX sync_entity_states_remote_revision_idx
  ON sync_entity_states (organization_id, workspace_id, policy_id, last_remote_revision, local_entity_id);
CREATE INDEX sync_local_receipts_created_idx
  ON sync_local_receipts (organization_id, workspace_id, policy_id, created_at, change_id);
CREATE INDEX sync_remote_receipts_created_idx
  ON sync_remote_receipts (organization_id, workspace_id, policy_id, created_at, change_id);

ALTER TABLE sync_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE sync_policies FORCE ROW LEVEL SECURITY;
ALTER TABLE sync_checkpoints ENABLE ROW LEVEL SECURITY;
ALTER TABLE sync_checkpoints FORCE ROW LEVEL SECURITY;
ALTER TABLE sync_entity_states ENABLE ROW LEVEL SECURITY;
ALTER TABLE sync_entity_states FORCE ROW LEVEL SECURITY;
ALTER TABLE sync_local_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE sync_local_receipts FORCE ROW LEVEL SECURITY;
ALTER TABLE sync_remote_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE sync_remote_receipts FORCE ROW LEVEL SECURITY;

CREATE POLICY sync_policies_tenant_all ON sync_policies
  USING (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true))
  WITH CHECK (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true));
CREATE POLICY sync_checkpoints_tenant_all ON sync_checkpoints
  USING (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true))
  WITH CHECK (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true));
CREATE POLICY sync_entity_states_tenant_all ON sync_entity_states
  USING (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true))
  WITH CHECK (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true));
CREATE POLICY sync_local_receipts_tenant_all ON sync_local_receipts
  USING (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true))
  WITH CHECK (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true));
CREATE POLICY sync_remote_receipts_tenant_all ON sync_remote_receipts
  USING (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true))
  WITH CHECK (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true));

CREATE FUNCTION sync_policy_guard() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''sync policy must start at version 1'';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.connector_account_id IS DISTINCT FROM OLD.connector_account_id
     OR NEW.entity_type IS DISTINCT FROM OLD.entity_type OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''sync policy identity is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''sync policy version transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER sync_policy_guard_insert BEFORE INSERT ON sync_policies FOR EACH ROW EXECUTE FUNCTION sync_policy_guard();
CREATE TRIGGER sync_policy_guard_update BEFORE UPDATE ON sync_policies FOR EACH ROW EXECUTE FUNCTION sync_policy_guard();

CREATE FUNCTION sync_state_guard() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''sync entity state must start at version 1'';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.policy_id IS DISTINCT FROM OLD.policy_id OR NEW.local_entity_id IS DISTINCT FROM OLD.local_entity_id
     OR NEW.remote_id IS DISTINCT FROM OLD.remote_id THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''sync entity identity is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at OR NEW.last_local_version < OLD.last_local_version THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''sync entity state progression is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER sync_state_guard_insert BEFORE INSERT ON sync_entity_states FOR EACH ROW EXECUTE FUNCTION sync_state_guard();
CREATE TRIGGER sync_state_guard_update BEFORE UPDATE ON sync_entity_states FOR EACH ROW EXECUTE FUNCTION sync_state_guard();

CREATE FUNCTION sync_receipt_reject_mutation() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''sync receipt history is immutable'';
  RETURN NULL;
END';
CREATE TRIGGER sync_local_receipts_no_update BEFORE UPDATE OR DELETE ON sync_local_receipts FOR EACH ROW EXECUTE FUNCTION sync_receipt_reject_mutation();
CREATE TRIGGER sync_remote_receipts_no_update BEFORE UPDATE OR DELETE ON sync_remote_receipts FOR EACH ROW EXECUTE FUNCTION sync_receipt_reject_mutation();
CREATE TRIGGER sync_local_receipts_no_clear BEFORE TRUNCATE ON sync_local_receipts FOR EACH STATEMENT EXECUTE FUNCTION sync_receipt_reject_mutation();
CREATE TRIGGER sync_remote_receipts_no_clear BEFORE TRUNCATE ON sync_remote_receipts FOR EACH STATEMENT EXECUTE FUNCTION sync_receipt_reject_mutation();

REVOKE DELETE, TRUNCATE ON sync_policies, sync_checkpoints, sync_entity_states FROM PUBLIC;
REVOKE UPDATE, DELETE, TRUNCATE ON sync_local_receipts, sync_remote_receipts FROM PUBLIC;

COMMENT ON TABLE sync_policies IS 'Task-013 provider-neutral direction and source-of-truth policy; provider names are forbidden from sync semantics.';
COMMENT ON TABLE sync_checkpoints IS 'Task-013 durable remote pull cursor; advanced only after a complete page is resolved.';
COMMENT ON TABLE sync_entity_states IS 'Task-013 last synchronized local/remote versions and canonical payload fingerprint for conflict and loop prevention.';
COMMENT ON TABLE sync_local_receipts IS 'Task-013 append-only outbound event replay receipts.';
COMMENT ON TABLE sync_remote_receipts IS 'Task-013 append-only inbound remote-change replay receipts.';

-- SOURCE 000025_reconciliation.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE reconciliation_runs (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_id text NOT NULL,
  mode text NOT NULL,
  trigger_ref text,
  status text NOT NULL,
  cursor text NOT NULL DEFAULT '',
  scanned_count bigint NOT NULL DEFAULT 0,
  drift_count bigint NOT NULL DEFAULT 0,
  version bigint NOT NULL DEFAULT 1,
  started_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  completed_at timestamptz,
  CONSTRAINT reconciliation_runs_pkey PRIMARY KEY (id),
  CONSTRAINT reconciliation_runs_tenant_identity UNIQUE (id, organization_id, workspace_id),
  CONSTRAINT reconciliation_runs_policy_fk FOREIGN KEY (policy_id, organization_id, workspace_id)
    REFERENCES sync_policies (id, organization_id, workspace_id),
  CONSTRAINT reconciliation_runs_mode_chk CHECK (mode IN ('incremental','scheduled_full','on_demand')),
  CONSTRAINT reconciliation_runs_status_chk CHECK (status IN ('running','interrupted','completed')),
  CONSTRAINT reconciliation_runs_counts_chk CHECK (scanned_count >= 0 AND drift_count >= 0 AND version >= 1),
  CONSTRAINT reconciliation_runs_cursor_chk CHECK (length(cursor) <= 1024 AND cursor !~ '[[:cntrl:]]'),
  CONSTRAINT reconciliation_runs_trigger_chk CHECK (trigger_ref IS NULL OR (length(trigger_ref) BETWEEN 1 AND 128 AND trigger_ref !~ '[[:cntrl:]]')),
  CONSTRAINT reconciliation_runs_time_chk CHECK (updated_at >= started_at AND (completed_at IS NULL OR completed_at >= started_at)),
  CONSTRAINT reconciliation_runs_completion_chk CHECK ((status = 'completed') = (completed_at IS NOT NULL))
);

CREATE TABLE reconciliation_drifts (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  run_id text NOT NULL,
  policy_id text NOT NULL,
  kind text NOT NULL,
  local_entity_id text,
  remote_id text,
  local_fingerprint text,
  remote_fingerprint text,
  local_status text,
  remote_status text,
  local_version bigint NOT NULL DEFAULT 0,
  remote_revision text,
  mapping_local_count integer NOT NULL DEFAULT 0,
  mapping_remote_count integer NOT NULL DEFAULT 0,
  detected_at timestamptz NOT NULL,
  status text NOT NULL DEFAULT 'open',
  recommended_action text NOT NULL DEFAULT 'none',
  version bigint NOT NULL DEFAULT 1,
  resolved_at timestamptz,
  CONSTRAINT reconciliation_drifts_pkey PRIMARY KEY (id),
  CONSTRAINT reconciliation_drifts_tenant_identity UNIQUE (id, organization_id, workspace_id),
  CONSTRAINT reconciliation_drifts_run_fk FOREIGN KEY (run_id, organization_id, workspace_id)
    REFERENCES reconciliation_runs (id, organization_id, workspace_id),
  CONSTRAINT reconciliation_drifts_policy_fk FOREIGN KEY (policy_id, organization_id, workspace_id)
    REFERENCES sync_policies (id, organization_id, workspace_id),
  CONSTRAINT reconciliation_drifts_kind_chk CHECK (kind IN ('content_drift','missing_mapping','orphan_mapping','duplicate_mapping','status_mismatch','stale_connector')),
  CONSTRAINT reconciliation_drifts_status_chk CHECK (status IN ('open','auto_fixed','notified','approval_pending','ignored')),
  CONSTRAINT reconciliation_drifts_action_chk CHECK (recommended_action IN ('none','auto_fix','notify','approval','ignore')),
  CONSTRAINT reconciliation_drifts_counts_chk CHECK (local_version >= 0 AND mapping_local_count BETWEEN 0 AND 1000 AND mapping_remote_count BETWEEN 0 AND 1000 AND version >= 1),
  CONSTRAINT reconciliation_drifts_local_fp_chk CHECK (local_fingerprint IS NULL OR local_fingerprint ~ '^[0-9a-f]{64}$'),
  CONSTRAINT reconciliation_drifts_remote_fp_chk CHECK (remote_fingerprint IS NULL OR remote_fingerprint ~ '^[0-9a-f]{64}$'),
  CONSTRAINT reconciliation_drifts_remote_revision_chk CHECK (remote_revision IS NULL OR (length(remote_revision) BETWEEN 1 AND 256 AND remote_revision !~ '[[:cntrl:]]')),
  CONSTRAINT reconciliation_drifts_resolution_chk CHECK ((status = 'open') = (resolved_at IS NULL)),
  CONSTRAINT reconciliation_drifts_resolution_time_chk CHECK (resolved_at IS NULL OR resolved_at >= detected_at)
);

CREATE TABLE reconciliation_actions (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  drift_id text NOT NULL,
  action text NOT NULL,
  idempotency_key text NOT NULL,
  result text NOT NULL,
  error_code text,
  created_at timestamptz NOT NULL,
  CONSTRAINT reconciliation_actions_pkey PRIMARY KEY (id),
  CONSTRAINT reconciliation_actions_drift_fk FOREIGN KEY (drift_id, organization_id, workspace_id)
    REFERENCES reconciliation_drifts (id, organization_id, workspace_id),
  CONSTRAINT reconciliation_actions_action_chk CHECK (action IN ('auto_fix','notify','approval','ignore')),
  CONSTRAINT reconciliation_actions_result_chk CHECK (result IN ('succeeded','failed')),
  CONSTRAINT reconciliation_actions_error_chk CHECK ((result = 'succeeded' AND error_code IS NULL) OR (result = 'failed' AND error_code ~ '^[a-z][a-z0-9_]{0,63}$')),
  CONSTRAINT reconciliation_actions_idempotency_chk CHECK (length(idempotency_key) BETWEEN 1 AND 128 AND idempotency_key !~ '[[:cntrl:]]')
);

CREATE INDEX reconciliation_runs_policy_status_idx ON reconciliation_runs (organization_id, workspace_id, policy_id, status, updated_at, id);
CREATE INDEX reconciliation_drifts_run_kind_idx ON reconciliation_drifts (organization_id, workspace_id, run_id, kind, status, detected_at, id);
CREATE INDEX reconciliation_drifts_open_idx ON reconciliation_drifts (organization_id, workspace_id, policy_id, status, detected_at, id) WHERE status = 'open';
CREATE INDEX reconciliation_actions_drift_idx ON reconciliation_actions (organization_id, workspace_id, drift_id, created_at, id);

ALTER TABLE reconciliation_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE reconciliation_runs FORCE ROW LEVEL SECURITY;
ALTER TABLE reconciliation_drifts ENABLE ROW LEVEL SECURITY;
ALTER TABLE reconciliation_drifts FORCE ROW LEVEL SECURITY;
ALTER TABLE reconciliation_actions ENABLE ROW LEVEL SECURITY;
ALTER TABLE reconciliation_actions FORCE ROW LEVEL SECURITY;

CREATE POLICY reconciliation_runs_tenant_all ON reconciliation_runs
  USING (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true))
  WITH CHECK (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true));
CREATE POLICY reconciliation_drifts_tenant_all ON reconciliation_drifts
  USING (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true))
  WITH CHECK (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true));
CREATE POLICY reconciliation_actions_tenant_all ON reconciliation_actions
  USING (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true))
  WITH CHECK (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true));

CREATE FUNCTION reconciliation_run_guard() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 OR NEW.status <> ''running'' OR NEW.scanned_count <> 0 OR NEW.drift_count <> 0 OR NEW.completed_at IS NOT NULL THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''reconciliation run initial state is invalid'';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.policy_id IS DISTINCT FROM OLD.policy_id OR NEW.mode IS DISTINCT FROM OLD.mode OR NEW.trigger_ref IS DISTINCT FROM OLD.trigger_ref OR NEW.started_at IS DISTINCT FROM OLD.started_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''reconciliation run identity is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at OR NEW.scanned_count < OLD.scanned_count OR NEW.drift_count < OLD.drift_count THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''reconciliation run progression is invalid'';
  END IF;
  IF OLD.status = ''completed'' THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''completed reconciliation run is immutable'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER reconciliation_run_guard_insert BEFORE INSERT ON reconciliation_runs FOR EACH ROW EXECUTE FUNCTION reconciliation_run_guard();
CREATE TRIGGER reconciliation_run_guard_update BEFORE UPDATE ON reconciliation_runs FOR EACH ROW EXECUTE FUNCTION reconciliation_run_guard();

CREATE FUNCTION reconciliation_drift_guard() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 OR NEW.status <> ''open'' OR NEW.resolved_at IS NOT NULL THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''reconciliation drift initial state is invalid'';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.run_id IS DISTINCT FROM OLD.run_id OR NEW.policy_id IS DISTINCT FROM OLD.policy_id OR NEW.kind IS DISTINCT FROM OLD.kind
     OR NEW.local_entity_id IS DISTINCT FROM OLD.local_entity_id OR NEW.remote_id IS DISTINCT FROM OLD.remote_id
     OR NEW.local_fingerprint IS DISTINCT FROM OLD.local_fingerprint OR NEW.remote_fingerprint IS DISTINCT FROM OLD.remote_fingerprint
     OR NEW.local_status IS DISTINCT FROM OLD.local_status OR NEW.remote_status IS DISTINCT FROM OLD.remote_status
     OR NEW.local_version IS DISTINCT FROM OLD.local_version OR NEW.remote_revision IS DISTINCT FROM OLD.remote_revision
     OR NEW.mapping_local_count IS DISTINCT FROM OLD.mapping_local_count OR NEW.mapping_remote_count IS DISTINCT FROM OLD.mapping_remote_count
     OR NEW.detected_at IS DISTINCT FROM OLD.detected_at OR NEW.recommended_action IS DISTINCT FROM OLD.recommended_action THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''reconciliation drift evidence is immutable'';
  END IF;
  IF OLD.status <> ''open'' OR NEW.status = ''open'' OR NEW.version <> OLD.version + 1 OR NEW.resolved_at IS NULL THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''reconciliation drift transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER reconciliation_drift_guard_insert BEFORE INSERT ON reconciliation_drifts FOR EACH ROW EXECUTE FUNCTION reconciliation_drift_guard();
CREATE TRIGGER reconciliation_drift_guard_update BEFORE UPDATE ON reconciliation_drifts FOR EACH ROW EXECUTE FUNCTION reconciliation_drift_guard();

CREATE FUNCTION reconciliation_action_reject_mutation() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''reconciliation action history is immutable'';
  RETURN NULL;
END';
CREATE TRIGGER reconciliation_actions_no_update BEFORE UPDATE OR DELETE ON reconciliation_actions FOR EACH ROW EXECUTE FUNCTION reconciliation_action_reject_mutation();
CREATE TRIGGER reconciliation_actions_no_clear BEFORE TRUNCATE ON reconciliation_actions FOR EACH STATEMENT EXECUTE FUNCTION reconciliation_action_reject_mutation();

REVOKE DELETE, TRUNCATE ON reconciliation_runs, reconciliation_drifts FROM PUBLIC;
REVOKE UPDATE, DELETE, TRUNCATE ON reconciliation_actions FROM PUBLIC;

COMMENT ON TABLE reconciliation_runs IS 'Task-014 resumable incremental/scheduled-full/on-demand reconciliation progress; no remote payloads or raw errors.';
COMMENT ON TABLE reconciliation_drifts IS 'Task-014 bounded immutable drift evidence with one-way resolution state transitions.';
COMMENT ON TABLE reconciliation_actions IS 'Task-014 append-only remediation attempt receipts; external effects use deterministic idempotency keys.';

-- SOURCE 000026_ai_agent_governance.sql
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

CREATE TABLE ai_agent_policies (
  id text NOT NULL CHECK(id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'),
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  version bigint NOT NULL CHECK(version >= 1),
  agent_id text NOT NULL CHECK(agent_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'),
  integration_id text NOT NULL CHECK(integration_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'),
  rules jsonb NOT NULL CHECK(jsonb_typeof(rules)='array' AND jsonb_array_length(rules) BETWEEN 1 AND 256),
  effective_from timestamptz NOT NULL,
  effective_until timestamptz,
  changed_by text NOT NULL CHECK(char_length(changed_by) BETWEEN 1 AND 256),
  reason text NOT NULL CHECK(char_length(reason) BETWEEN 1 AND 256),
  created_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,id,version),
  CONSTRAINT ai_agent_policies_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  UNIQUE(organization_id,workspace_id,agent_id,integration_id,version),
  CONSTRAINT ai_agent_policy_dates CHECK(effective_until IS NULL OR effective_until > effective_from)
);
CREATE INDEX ai_agent_policy_resolve_idx ON ai_agent_policies(organization_id,workspace_id,agent_id,integration_id,effective_from,version DESC);

CREATE FUNCTION ai_agent_policy_insert_guard() RETURNS trigger LANGUAGE plpgsql AS '
DECLARE expected bigint; stable_id text;
BEGIN
  SELECT COALESCE(max(version),0)+1,min(id) INTO expected,stable_id FROM ai_agent_policies WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND agent_id=NEW.agent_id AND integration_id=NEW.integration_id;
  IF NEW.version<>expected THEN RAISE EXCEPTION ''ai agent policy version invalid''; END IF;
  IF stable_id IS NOT NULL AND stable_id<>NEW.id THEN RAISE EXCEPTION ''ai agent policy identity must remain stable''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER ai_agent_policy_insert_guard BEFORE INSERT ON ai_agent_policies FOR EACH ROW EXECUTE FUNCTION ai_agent_policy_insert_guard();

CREATE FUNCTION ai_agent_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''ai agent governance evidence is append-only''; END';
CREATE TRIGGER ai_agent_policies_append_only BEFORE UPDATE OR DELETE ON ai_agent_policies FOR EACH ROW EXECUTE FUNCTION ai_agent_append_only();

CREATE TABLE ai_agent_kill_switches (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  scope_kind text NOT NULL CHECK(scope_kind IN ('tenant','agent','integration')),
  subject_id text NOT NULL CHECK(char_length(subject_id) BETWEEN 1 AND 160),
  version bigint NOT NULL CHECK(version >= 1),
  disabled boolean NOT NULL,
  changed_by text NOT NULL CHECK(char_length(changed_by) BETWEEN 1 AND 256),
  reason text NOT NULL DEFAULT '' CHECK(char_length(reason) <= 256),
  changed_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,scope_kind,subject_id,version),
  CONSTRAINT ai_agent_kill_switches_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT ai_agent_kill_subject CHECK((scope_kind='tenant' AND subject_id='*') OR (scope_kind<>'tenant' AND subject_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'))
);
CREATE INDEX ai_agent_kill_resolve_idx ON ai_agent_kill_switches(organization_id,workspace_id,scope_kind,subject_id,version DESC);
CREATE FUNCTION ai_agent_kill_insert_guard() RETURNS trigger LANGUAGE plpgsql AS '
DECLARE expected bigint;
BEGIN
  SELECT COALESCE(max(version),0)+1 INTO expected FROM ai_agent_kill_switches WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND scope_kind=NEW.scope_kind AND subject_id=NEW.subject_id;
  IF NEW.version<>expected THEN RAISE EXCEPTION ''ai agent kill-switch version invalid''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER ai_agent_kill_insert_guard BEFORE INSERT ON ai_agent_kill_switches FOR EACH ROW EXECUTE FUNCTION ai_agent_kill_insert_guard();
CREATE TRIGGER ai_agent_kill_append_only BEFORE UPDATE OR DELETE ON ai_agent_kill_switches FOR EACH ROW EXECUTE FUNCTION ai_agent_append_only();

CREATE TABLE ai_agent_call_counters (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_id text NOT NULL,
  policy_version bigint NOT NULL CHECK(policy_version >= 1),
  agent_id text NOT NULL,
  integration_id text NOT NULL,
  tool text NOT NULL CHECK(tool ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'),
  window_start timestamptz NOT NULL,
  window_end timestamptz NOT NULL,
  used bigint NOT NULL DEFAULT 0 CHECK(used >= 0),
  max_calls_snapshot bigint NOT NULL CHECK(max_calls_snapshot > 0),
  updated_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,policy_id,policy_version,agent_id,integration_id,tool,window_start),
  CONSTRAINT ai_agent_call_counters_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT ai_agent_call_counters_policy_fk FOREIGN KEY(organization_id,workspace_id,policy_id,policy_version) REFERENCES ai_agent_policies(organization_id,workspace_id,id,version),
  CONSTRAINT ai_agent_counter_window CHECK(window_end > window_start),
  CONSTRAINT ai_agent_counter_limit CHECK(used <= max_calls_snapshot)
);
CREATE INDEX ai_agent_call_counter_current_idx ON ai_agent_call_counters(organization_id,workspace_id,agent_id,integration_id,tool,window_end);
CREATE FUNCTION ai_agent_counter_guard() RETURNS trigger LANGUAGE plpgsql AS '
BEGIN
  IF TG_OP=''INSERT'' THEN
    IF NEW.used<>0 THEN RAISE EXCEPTION ''ai agent call counter must start at zero''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.policy_id IS DISTINCT FROM OLD.policy_id OR NEW.policy_version IS DISTINCT FROM OLD.policy_version OR NEW.agent_id IS DISTINCT FROM OLD.agent_id OR NEW.integration_id IS DISTINCT FROM OLD.integration_id OR NEW.tool IS DISTINCT FROM OLD.tool OR NEW.window_start IS DISTINCT FROM OLD.window_start OR NEW.window_end IS DISTINCT FROM OLD.window_end OR NEW.max_calls_snapshot IS DISTINCT FROM OLD.max_calls_snapshot THEN RAISE EXCEPTION ''ai agent call counter identity immutable''; END IF;
  IF NEW.used < OLD.used OR NEW.updated_at < OLD.updated_at THEN RAISE EXCEPTION ''ai agent call counter cannot move backwards''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER ai_agent_counter_guard BEFORE INSERT OR UPDATE ON ai_agent_call_counters FOR EACH ROW EXECUTE FUNCTION ai_agent_counter_guard();

CREATE TABLE ai_agent_call_usage (
  invocation_id text NOT NULL CHECK(invocation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$'),
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_id text NOT NULL,
  policy_version bigint NOT NULL CHECK(policy_version >= 1),
  agent_id text NOT NULL,
  integration_id text NOT NULL,
  tool text NOT NULL,
  window_start timestamptz NOT NULL,
  window_end timestamptz NOT NULL,
  max_calls_snapshot bigint NOT NULL CHECK(max_calls_snapshot > 0),
  allowed boolean NOT NULL,
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,invocation_id),
  CONSTRAINT ai_agent_call_usage_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT ai_agent_call_usage_policy_fk FOREIGN KEY(organization_id,workspace_id,policy_id,policy_version) REFERENCES ai_agent_policies(organization_id,workspace_id,id,version),
  CONSTRAINT ai_agent_call_usage_window CHECK(window_end > window_start AND occurred_at >= window_start AND occurred_at < window_end)
);
CREATE INDEX ai_agent_call_usage_tool_idx ON ai_agent_call_usage(organization_id,workspace_id,agent_id,integration_id,tool,window_start,occurred_at,invocation_id);
CREATE TRIGGER ai_agent_call_usage_append_only BEFORE UPDATE OR DELETE ON ai_agent_call_usage FOR EACH ROW EXECUTE FUNCTION ai_agent_append_only();

ALTER TABLE ai_agent_policies ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_agent_policies FORCE ROW LEVEL SECURITY;
ALTER TABLE ai_agent_kill_switches ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_agent_kill_switches FORCE ROW LEVEL SECURITY;
ALTER TABLE ai_agent_call_counters ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_agent_call_counters FORCE ROW LEVEL SECURITY;
ALTER TABLE ai_agent_call_usage ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_agent_call_usage FORCE ROW LEVEL SECURITY;

CREATE POLICY ai_agent_policies_select ON ai_agent_policies FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ai_agent_policies_insert ON ai_agent_policies FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ai_agent_kill_select ON ai_agent_kill_switches FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ai_agent_kill_insert ON ai_agent_kill_switches FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ai_agent_counters_select ON ai_agent_call_counters FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ai_agent_counters_insert ON ai_agent_call_counters FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ai_agent_counters_update ON ai_agent_call_counters FOR UPDATE USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ai_agent_usage_select ON ai_agent_call_usage FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ai_agent_usage_insert ON ai_agent_call_usage FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

CREATE FUNCTION ai_agent_no_delete() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''ai agent governance evidence cannot be hard-deleted''; END';
CREATE TRIGGER ai_agent_policies_no_clear BEFORE TRUNCATE ON ai_agent_policies FOR EACH STATEMENT EXECUTE FUNCTION ai_agent_no_delete();
CREATE TRIGGER ai_agent_kill_no_clear BEFORE TRUNCATE ON ai_agent_kill_switches FOR EACH STATEMENT EXECUTE FUNCTION ai_agent_no_delete();
CREATE TRIGGER ai_agent_counters_no_delete BEFORE DELETE OR TRUNCATE ON ai_agent_call_counters FOR EACH STATEMENT EXECUTE FUNCTION ai_agent_no_delete();
CREATE TRIGGER ai_agent_usage_no_clear BEFORE TRUNCATE ON ai_agent_call_usage FOR EACH STATEMENT EXECUTE FUNCTION ai_agent_no_delete();

-- SOURCE 000027_social_core.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE FUNCTION social_valid_capabilities(value jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE STRICT
AS 'DECLARE
  item text;
  previous text := '''';
  seen text[] := ARRAY[]::text[];
BEGIN
  IF jsonb_typeof(value) <> ''array'' OR jsonb_array_length(value) < 1 OR jsonb_array_length(value) > 8 THEN
    RETURN false;
  END IF;
  FOR item IN SELECT jsonb_array_elements_text(value) LOOP
    IF item NOT IN (
      ''social.post.text'',''social.post.media'',''social.post.video'',''social.post.edit'',
      ''social.post.delete'',''social.comments.read'',''social.comments.reply'',''social.analytics.read''
    ) THEN RETURN false; END IF;
    IF item = ANY(seen) THEN RETURN false; END IF;
    IF previous <> '''' AND item <= previous THEN RETURN false; END IF;
    seen := array_append(seen,item);
    previous := item;
  END LOOP;
  RETURN true;
END';

CREATE TABLE social_contents (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  title text NOT NULL DEFAULT '',
  body text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'draft',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(id),
  CONSTRAINT social_contents_tenant_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT social_contents_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_contents_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT social_contents_title_chk CHECK(title=btrim(title) AND char_length(title)<=300),
  CONSTRAINT social_contents_body_chk CHECK(body=btrim(body) AND char_length(body)<=50000),
  CONSTRAINT social_contents_nonempty_chk CHECK(title<>'' OR body<>''),
  CONSTRAINT social_contents_status_chk CHECK(status IN ('draft','ready','archived')),
  CONSTRAINT social_contents_version_chk CHECK(version>=1),
  CONSTRAINT social_contents_time_chk CHECK(updated_at>=created_at)
);
CREATE INDEX social_contents_status_idx ON social_contents(organization_id,workspace_id,status,updated_at DESC,id DESC);

CREATE TABLE social_content_variants (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  content_id text NOT NULL,
  format text NOT NULL,
  title text NOT NULL DEFAULT '',
  body text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(id),
  CONSTRAINT social_variants_tenant_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT social_variants_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_variants_content_fk FOREIGN KEY(organization_id,workspace_id,content_id) REFERENCES social_contents(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_variants_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT social_variants_format_chk CHECK(format IN ('text','image','gallery','video','article')),
  CONSTRAINT social_variants_title_chk CHECK(title=btrim(title) AND char_length(title)<=300),
  CONSTRAINT social_variants_body_chk CHECK(body=btrim(body) AND char_length(body)<=50000),
  CONSTRAINT social_variants_version_chk CHECK(version=1)
);
CREATE INDEX social_variants_content_idx ON social_content_variants(organization_id,workspace_id,content_id,created_at DESC,id DESC);

CREATE TABLE social_variant_media_refs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  variant_id text NOT NULL,
  position smallint NOT NULL,
  upload_id text NOT NULL,
  kind text NOT NULL,
  alt_text text NOT NULL DEFAULT '',
  PRIMARY KEY(organization_id,workspace_id,variant_id,position),
  CONSTRAINT social_variant_media_unique UNIQUE(organization_id,workspace_id,variant_id,upload_id),
  CONSTRAINT social_variant_media_variant_fk FOREIGN KEY(organization_id,workspace_id,variant_id) REFERENCES social_content_variants(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_variant_media_upload_fk FOREIGN KEY(upload_id,organization_id,workspace_id) REFERENCES uploads(id,organization_id,workspace_id) ON DELETE RESTRICT,
  CONSTRAINT social_variant_media_position_chk CHECK(position BETWEEN 0 AND 19),
  CONSTRAINT social_variant_media_kind_chk CHECK(kind IN ('image','video')),
  CONSTRAINT social_variant_media_alt_chk CHECK(alt_text=btrim(alt_text) AND char_length(alt_text)<=1000)
);

CREATE TABLE social_channel_accounts (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  display_name text NOT NULL,
  capabilities jsonb NOT NULL,
  status text NOT NULL DEFAULT 'disabled',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(id),
  CONSTRAINT social_channel_accounts_tenant_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT social_channel_accounts_connector_key UNIQUE(organization_id,workspace_id,connector_account_id),
  CONSTRAINT social_channel_accounts_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_channel_accounts_connector_fk FOREIGN KEY(organization_id,workspace_id,connector_account_id) REFERENCES connector_accounts(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_channel_accounts_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT social_channel_accounts_name_chk CHECK(display_name=btrim(display_name) AND char_length(display_name) BETWEEN 1 AND 300),
  CONSTRAINT social_channel_accounts_capabilities_chk CHECK(social_valid_capabilities(capabilities)),
  CONSTRAINT social_channel_accounts_status_chk CHECK(status IN ('disabled','active')),
  CONSTRAINT social_channel_accounts_version_chk CHECK(version>=1),
  CONSTRAINT social_channel_accounts_time_chk CHECK(updated_at>=created_at)
);
CREATE INDEX social_channel_accounts_status_idx ON social_channel_accounts(organization_id,workspace_id,status,updated_at DESC,id DESC);

CREATE TABLE social_publications (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  variant_id text NOT NULL,
  channel_account_id text NOT NULL,
  schedule_mode text NOT NULL,
  scheduled_at timestamptz,
  status text NOT NULL,
  attempt integer NOT NULL DEFAULT 0,
  reason_code text,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  published_at timestamptz,
  PRIMARY KEY(id),
  CONSTRAINT social_publications_tenant_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT social_publications_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_publications_variant_fk FOREIGN KEY(organization_id,workspace_id,variant_id) REFERENCES social_content_variants(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_publications_channel_fk FOREIGN KEY(organization_id,workspace_id,channel_account_id) REFERENCES social_channel_accounts(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_publications_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT social_publications_schedule_chk CHECK((schedule_mode='immediate' AND scheduled_at IS NULL) OR (schedule_mode='at' AND scheduled_at IS NOT NULL)),
  CONSTRAINT social_publications_status_chk CHECK(status IN ('scheduled','ready','publishing','published','failed','cancelled')),
  CONSTRAINT social_publications_attempt_chk CHECK(attempt BETWEEN 0 AND 1000),
  CONSTRAINT social_publications_reason_chk CHECK(reason_code IS NULL OR reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  CONSTRAINT social_publications_failure_shape_chk CHECK((status='failed' AND reason_code IS NOT NULL) OR (status<>'failed' AND reason_code IS NULL)),
  CONSTRAINT social_publications_published_shape_chk CHECK((status='published' AND published_at IS NOT NULL) OR (status<>'published' AND published_at IS NULL)),
  CONSTRAINT social_publications_version_chk CHECK(version>=1),
  CONSTRAINT social_publications_time_chk CHECK(updated_at>=created_at AND (published_at IS NULL OR published_at>=created_at))
);
CREATE INDEX social_publications_due_idx ON social_publications(organization_id,workspace_id,scheduled_at,id) WHERE status='scheduled';
CREATE INDEX social_publications_status_idx ON social_publications(organization_id,workspace_id,status,updated_at DESC,id DESC);

CREATE TABLE social_publication_status_events (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  event_id text NOT NULL,
  publication_id text NOT NULL,
  publication_version bigint NOT NULL,
  status text NOT NULL,
  attempt integer NOT NULL,
  reason_code text,
  correlation_id text NOT NULL,
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,event_id),
  CONSTRAINT social_status_events_publication_fk FOREIGN KEY(organization_id,workspace_id,publication_id) REFERENCES social_publications(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_status_events_event_chk CHECK(event_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  CONSTRAINT social_status_events_version_chk CHECK(publication_version>=1),
  CONSTRAINT social_status_events_status_chk CHECK(status IN ('scheduled','ready','publishing','published','failed','cancelled')),
  CONSTRAINT social_status_events_attempt_chk CHECK(attempt BETWEEN 0 AND 1000),
  CONSTRAINT social_status_events_reason_chk CHECK(reason_code IS NULL OR reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  CONSTRAINT social_status_events_failure_shape_chk CHECK((status='failed' AND reason_code IS NOT NULL) OR (status<>'failed' AND reason_code IS NULL)),
  CONSTRAINT social_status_events_correlation_chk CHECK(correlation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$')
);
CREATE INDEX social_status_events_history_idx ON social_publication_status_events(organization_id,workspace_id,publication_id,publication_version,event_id);

ALTER TABLE social_contents ENABLE ROW LEVEL SECURITY; ALTER TABLE social_contents FORCE ROW LEVEL SECURITY;
ALTER TABLE social_content_variants ENABLE ROW LEVEL SECURITY; ALTER TABLE social_content_variants FORCE ROW LEVEL SECURITY;
ALTER TABLE social_variant_media_refs ENABLE ROW LEVEL SECURITY; ALTER TABLE social_variant_media_refs FORCE ROW LEVEL SECURITY;
ALTER TABLE social_channel_accounts ENABLE ROW LEVEL SECURITY; ALTER TABLE social_channel_accounts FORCE ROW LEVEL SECURITY;
ALTER TABLE social_publications ENABLE ROW LEVEL SECURITY; ALTER TABLE social_publications FORCE ROW LEVEL SECURITY;
ALTER TABLE social_publication_status_events ENABLE ROW LEVEL SECURITY; ALTER TABLE social_publication_status_events FORCE ROW LEVEL SECURITY;

CREATE POLICY social_contents_tenant_all ON social_contents FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY social_variants_tenant_all ON social_content_variants FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY social_variant_media_tenant_all ON social_variant_media_refs FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY social_channel_accounts_tenant_all ON social_channel_accounts FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY social_publications_tenant_all ON social_publications FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY social_status_events_tenant_all ON social_publication_status_events FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

CREATE FUNCTION social_contents_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF TG_OP=''INSERT'' THEN
    IF NEW.status<>''draft'' OR NEW.version<>1 THEN RAISE EXCEPTION ''new social content must start draft at version 1''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN RAISE EXCEPTION ''social content identity is immutable''; END IF;
  IF OLD.status=''archived'' THEN RAISE EXCEPTION ''archived social content is immutable''; END IF;
  IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION ''social content version transition invalid''; END IF;
  IF NEW.status IS DISTINCT FROM OLD.status THEN
    IF NOT ((OLD.status=''draft'' AND NEW.status IN (''ready'',''archived'')) OR (OLD.status=''ready'' AND NEW.status IN (''draft'',''archived''))) THEN RAISE EXCEPTION ''social content lifecycle transition invalid''; END IF;
    IF NEW.title IS DISTINCT FROM OLD.title OR NEW.body IS DISTINCT FROM OLD.body THEN RAISE EXCEPTION ''social content status change cannot mutate body''; END IF;
  ELSEIF OLD.status<>''draft'' AND (NEW.title IS DISTINCT FROM OLD.title OR NEW.body IS DISTINCT FROM OLD.body) THEN
    RAISE EXCEPTION ''social content must be draft before editing'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER social_contents_guard_insert BEFORE INSERT ON social_contents FOR EACH ROW EXECUTE FUNCTION social_contents_guard();
CREATE TRIGGER social_contents_guard_update BEFORE UPDATE ON social_contents FOR EACH ROW EXECUTE FUNCTION social_contents_guard();

CREATE FUNCTION social_variants_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE parent_status text;
BEGIN
  SELECT status INTO parent_status FROM social_contents WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.content_id;
  IF parent_status IS NULL OR parent_status=''archived'' THEN RAISE EXCEPTION ''social variant parent unavailable''; END IF;
  IF NEW.version<>1 THEN RAISE EXCEPTION ''social variants are immutable version-1 snapshots''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER social_variants_guard_insert BEFORE INSERT ON social_content_variants FOR EACH ROW EXECUTE FUNCTION social_variants_guard();

CREATE FUNCTION social_variant_media_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE upload_state text;
BEGIN
  SELECT state INTO upload_state FROM uploads WHERE id=NEW.upload_id AND organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id;
  IF upload_state IS DISTINCT FROM ''released'' THEN RAISE EXCEPTION ''social media requires current released upload''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER social_variant_media_guard_insert BEFORE INSERT ON social_variant_media_refs FOR EACH ROW EXECUTE FUNCTION social_variant_media_guard();

CREATE FUNCTION social_channel_accounts_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE connector_family text; connector_status text;
BEGIN
  SELECT family,status INTO connector_family,connector_status FROM connector_accounts WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.connector_account_id;
  IF connector_family<>''social'' THEN RAISE EXCEPTION ''social channel requires social connector account''; END IF;
  IF TG_OP=''INSERT'' THEN
    IF NEW.status<>''disabled'' OR NEW.version<>1 THEN RAISE EXCEPTION ''new social channel must start disabled at version 1''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.connector_account_id IS DISTINCT FROM OLD.connector_account_id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN RAISE EXCEPTION ''social channel identity is immutable''; END IF;
  IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION ''social channel version transition invalid''; END IF;
  IF NEW.status=''active'' AND connector_status<>''active'' THEN RAISE EXCEPTION ''active social channel requires active connector account''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER social_channel_accounts_guard_insert BEFORE INSERT ON social_channel_accounts FOR EACH ROW EXECUTE FUNCTION social_channel_accounts_guard();
CREATE TRIGGER social_channel_accounts_guard_update BEFORE UPDATE ON social_channel_accounts FOR EACH ROW EXECUTE FUNCTION social_channel_accounts_guard();

CREATE FUNCTION social_publications_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE variant_format text; content_status text; channel_status text; channel_caps jsonb; required_cap text;
BEGIN
  SELECT v.format,c.status INTO variant_format,content_status FROM social_content_variants v JOIN social_contents c ON c.organization_id=v.organization_id AND c.workspace_id=v.workspace_id AND c.id=v.content_id WHERE v.organization_id=NEW.organization_id AND v.workspace_id=NEW.workspace_id AND v.id=NEW.variant_id;
  SELECT status,capabilities INTO channel_status,channel_caps FROM social_channel_accounts WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.channel_account_id;
  IF variant_format IN (''text'',''article'') THEN required_cap:=''social.post.text''; ELSIF variant_format IN (''image'',''gallery'') THEN required_cap:=''social.post.media''; ELSIF variant_format=''video'' THEN required_cap:=''social.post.video''; ELSE RAISE EXCEPTION ''unsupported social variant format''; END IF;
  IF TG_OP=''INSERT'' THEN
    IF content_status<>''ready'' THEN RAISE EXCEPTION ''publication requires ready content''; END IF;
    IF channel_status<>''active'' THEN RAISE EXCEPTION ''publication requires active channel''; END IF;
    IF NOT channel_caps ? required_cap THEN RAISE EXCEPTION ''social publication capability missing''; END IF;
    IF NEW.version<>1 OR NEW.attempt<>0 OR NEW.reason_code IS NOT NULL OR NEW.published_at IS NOT NULL THEN RAISE EXCEPTION ''new social publication state invalid''; END IF;
    IF NEW.schedule_mode=''immediate'' AND NEW.status<>''ready'' THEN RAISE EXCEPTION ''immediate publication must start ready''; END IF;
    IF NEW.schedule_mode=''at'' AND NEW.status<>''scheduled'' THEN RAISE EXCEPTION ''scheduled publication must start scheduled''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.variant_id IS DISTINCT FROM OLD.variant_id OR NEW.channel_account_id IS DISTINCT FROM OLD.channel_account_id OR NEW.schedule_mode IS DISTINCT FROM OLD.schedule_mode OR NEW.scheduled_at IS DISTINCT FROM OLD.scheduled_at OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN RAISE EXCEPTION ''social publication identity and schedule are immutable''; END IF;
  IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION ''social publication version transition invalid''; END IF;
  IF NOT ((OLD.status=''scheduled'' AND NEW.status IN (''ready'',''cancelled'')) OR (OLD.status=''ready'' AND NEW.status IN (''publishing'',''cancelled'')) OR (OLD.status=''publishing'' AND NEW.status IN (''published'',''failed'')) OR (OLD.status=''failed'' AND NEW.status IN (''ready'',''cancelled''))) THEN RAISE EXCEPTION ''social publication lifecycle transition invalid''; END IF;
  IF NEW.status IN (''ready'',''publishing'') THEN
    IF content_status<>''ready'' THEN RAISE EXCEPTION ''publication requires ready content''; END IF;
    IF channel_status<>''active'' THEN RAISE EXCEPTION ''publication requires active channel''; END IF;
    IF NOT channel_caps ? required_cap THEN RAISE EXCEPTION ''social publication capability missing''; END IF;
  END IF;
  IF NEW.status=''publishing'' THEN
    IF NEW.attempt<>OLD.attempt+1 THEN RAISE EXCEPTION ''publishing transition must increment attempt''; END IF;
  ELSIF NEW.attempt<>OLD.attempt THEN RAISE EXCEPTION ''non-publishing transition cannot change attempt''; END IF;
  IF NEW.status=''published'' THEN
    IF NEW.published_at IS NULL THEN RAISE EXCEPTION ''published publication needs timestamp''; END IF;
  ELSIF NEW.published_at IS NOT NULL THEN RAISE EXCEPTION ''only published publication may have published_at''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER social_publications_guard_insert BEFORE INSERT ON social_publications FOR EACH ROW EXECUTE FUNCTION social_publications_guard();
CREATE TRIGGER social_publications_guard_update BEFORE UPDATE ON social_publications FOR EACH ROW EXECUTE FUNCTION social_publications_guard();

CREATE FUNCTION social_status_events_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''social publication status history is append-only''; END';
CREATE TRIGGER social_status_events_no_update BEFORE UPDATE OR DELETE ON social_publication_status_events FOR EACH ROW EXECUTE FUNCTION social_status_events_append_only();

REVOKE UPDATE, DELETE, TRUNCATE ON social_content_variants, social_variant_media_refs, social_publication_status_events FROM PUBLIC;
REVOKE DELETE, TRUNCATE ON social_contents, social_channel_accounts, social_publications FROM PUBLIC;

CREATE FUNCTION social_reject_delete() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''social core history cannot be hard-deleted''; END';
CREATE TRIGGER social_contents_no_delete BEFORE DELETE ON social_contents FOR EACH ROW EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_contents_no_clear BEFORE TRUNCATE ON social_contents FOR EACH STATEMENT EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_variants_no_delete BEFORE DELETE ON social_content_variants FOR EACH ROW EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_variants_no_clear BEFORE TRUNCATE ON social_content_variants FOR EACH STATEMENT EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_variant_media_no_delete BEFORE DELETE ON social_variant_media_refs FOR EACH ROW EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_variant_media_no_clear BEFORE TRUNCATE ON social_variant_media_refs FOR EACH STATEMENT EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_channel_accounts_no_delete BEFORE DELETE ON social_channel_accounts FOR EACH ROW EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_channel_accounts_no_clear BEFORE TRUNCATE ON social_channel_accounts FOR EACH STATEMENT EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_publications_no_delete BEFORE DELETE ON social_publications FOR EACH ROW EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_publications_no_clear BEFORE TRUNCATE ON social_publications FOR EACH STATEMENT EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_status_events_no_clear BEFORE TRUNCATE ON social_publication_status_events FOR EACH STATEMENT EXECUTE FUNCTION social_reject_delete();

COMMENT ON TABLE social_contents IS 'Provider-neutral master social/content records. Remote IDs and provider fields are forbidden.';
COMMENT ON TABLE social_content_variants IS 'Immutable channel-ready content snapshots; new edits create a new variant.';
COMMENT ON TABLE social_variant_media_refs IS 'Only Task-088 UploadIDs are persisted. Raw object keys, signed URLs and filenames are forbidden publication authority.';
COMMENT ON TABLE social_channel_accounts IS 'Trusted projection of one social connector account plus canonical capability snapshot.';
COMMENT ON TABLE social_publications IS 'Canonical TORGNEXA schedule and publication state. Provider-native scheduling is not the source of truth.';
COMMENT ON TABLE social_publication_status_events IS 'Append-only normalized publication status history; raw remote error bodies are forbidden.';
-- BASELINE_SOURCE_END

INSERT INTO migration_history (
  version, name, file_name, phase, risk, checksum_sha256,
  application_version, execution_id, duration_ms
) VALUES (
  current_setting('torgnexa.migration_version')::integer,
  current_setting('torgnexa.migration_name'),
  current_setting('torgnexa.migration_file'),
  current_setting('torgnexa.migration_phase'),
  current_setting('torgnexa.migration_risk'),
  current_setting('torgnexa.migration_checksum'),
  current_setting('torgnexa.application_version'),
  current_setting('torgnexa.migration_execution_id'),
  current_setting('torgnexa.migration_duration_ms')::bigint
);

COMMIT;
