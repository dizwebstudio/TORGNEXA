BEGIN;
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

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
