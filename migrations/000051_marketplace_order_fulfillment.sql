BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 224: make label acquisition an explicit durable checkpoint between
-- warehouse packing and shipment dispatch. This only broadens the accepted
-- stage vocabulary; canonical order, inventory and logistics records remain
-- owned by their existing bounded contexts.
ALTER TABLE marketplace_operation_flows
  DROP CONSTRAINT marketplace_operation_flow_state_chk;
ALTER TABLE marketplace_operation_flows
  ADD CONSTRAINT marketplace_operation_flow_state_chk CHECK (
    stage IN ('account','product','publication','pricing','inventory','order','reservation','pick_pack','label','shipment','return','settlement','profitability','reconciliation','complete') AND
    state IN ('pending','unknown','blocked','complete') AND
    ((stage='complete' AND state='complete') OR stage<>'complete') AND
    updated_at >= created_at
  );
ALTER TABLE marketplace_operation_commands
  DROP CONSTRAINT marketplace_operation_command_state_chk;
ALTER TABLE marketplace_operation_commands
  ADD CONSTRAINT marketplace_operation_command_state_chk CHECK (
    stage IN ('account','product','publication','pricing','inventory','order','reservation','pick_pack','label','shipment','return','settlement','profitability','reconciliation','complete') AND
    outcome IN ('succeeded','rejected','unknown')
  );

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
