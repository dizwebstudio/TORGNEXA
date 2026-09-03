#!/usr/bin/env python3
"""Validate one redacted credentialed finance/WMS/hardware evidence artifact."""

from __future__ import annotations

import argparse
import datetime as dt
import re
from typing import Any

from p4_common import (
    QualificationError,
    read_json,
    reject_secret_shaped_fields,
    require_repository,
    require_sha40,
)


TOP_LEVEL = {
    "schema_version",
    "status",
    "scope",
    "subject_kind",
    "environment",
    "target",
    "repository",
    "release_commit",
    "subject_id",
    "account_ref",
    "qualified_at",
    "version",
    "scopes",
    "checks",
    "rollback",
}
V2_TOP_LEVEL = TOP_LEVEL | {"topology", "observations"}
SAFE_ID = re.compile(r"^[a-z][a-z0-9-]{0,63}$")
CHECK_ID = re.compile(r"^[a-z][a-z0-9_.-]{0,63}$")
SCOPE_ID = re.compile(r"^[a-z][a-z0-9_.-]{0,63}$")
SAFE_LABEL = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$")
REQUIRED_CHECKS = {
    "bank": {"health", "account_discovery", "statement_sync", "duplicate_sync", "unknown_transfer", "reconciliation"},
    "acquirer": {"health", "payment_settlement", "refund", "bank_match", "unknown_outcome", "reconciliation"},
    "marketplace_payout": {"health", "payout_import", "order_match", "duplicate_payout", "reversal", "reconciliation"},
    "fx": {"health", "rate_read", "stale_rate", "conversion_snapshot", "currency_guard", "reconciliation"},
    "advertising": {"health", "spend_import", "attribution", "unattributed", "duplicate_import", "reconciliation"},
    "fbs": {"health", "order_read", "inventory_read", "fulfillment_mode", "label", "tracking", "handoff", "read_after_write", "reconciliation"},
    "fbo": {"health", "inbound", "acceptance", "order_visibility", "tracking", "return", "read_after_write", "reconciliation"},
    "hardware": {"discovery", "pairing", "health", "timeout", "retry", "safe_fallback", "scan", "camera", "scale", "print"},
}
REQUIRED_SCOPES = {
    "bank": {"accounts.read", "statements.read"},
    "acquirer": {"payments.read", "refunds.read", "settlements.read"},
    "marketplace_payout": {"orders.read", "payouts.read", "settlements.read"},
    "fx": {"rates.read"},
    "advertising": {"campaigns.read", "spend.read"},
    "fbs": {"orders.read", "inventory.read", "fulfillment.write", "labels.write", "shipments.write", "tracking.read", "handoff.write"},
    "fbo": {"inbound.write", "acceptance.read", "orders.read", "tracking.read", "returns.read"},
    "hardware": {"scanner", "camera", "scale", "printer"},
}
OPERATIONAL_KINDS = {"partner_uat", "rollback", "slo_dr", "production_support"}
V2_REQUIRED_CHECKS = {
    **REQUIRED_CHECKS,
    "partner_uat": {"plan_accepted", "happy_path", "failure_recovery", "tenant_isolation", "operator_handoff", "sign_off"},
    "rollback": {"release_rollback", "database_restore", "connector_disable", "replay_after_rollback", "data_integrity", "approval"},
    "slo_dr": {"latency_slo", "error_budget", "backup", "restore", "rto", "rpo", "dr_rehearsal", "alerting"},
    "production_support": {"on_call", "runbook", "incident_escalation", "support_queue", "observability", "release_handoff"},
}
V2_REQUIRED_SCOPES = {
    **REQUIRED_SCOPES,
    "partner_uat": {"uat.plan.read", "uat.execute", "uat.acceptance.record"},
    "rollback": {"release.rollback", "database.restore", "connector.disable"},
    "slo_dr": {"metrics.read", "backup.restore", "dr.rehearse"},
    "production_support": {"oncall.read", "runbooks.read", "incidents.write", "status.read"},
}
V2_SCOPE_BY_KIND = {
    "partner_uat": "partner-uat",
    "rollback": "rollback",
    "slo_dr": "slo-dr",
    "production_support": "production-support",
}
V2_SUBJECT_KIND_BY_KIND = {
    "partner_uat": "partner",
    "rollback": "platform",
    "slo_dr": "platform",
    "production_support": "platform",
}
V2_OBSERVATION_FIELDS = {
    "bank": {"source_status", "read_after_write", "reconciliation"},
    "acquirer": {"source_status", "read_after_write", "reconciliation"},
    "marketplace_payout": {"source_status", "read_after_write", "reconciliation"},
    "fx": {"source_status", "read_after_write", "reconciliation"},
    "advertising": {"source_status", "read_after_write", "reconciliation"},
    "fbs": {"fulfillment_mode", "handoff_status", "read_after_write", "reconciliation"},
    "fbo": {"fulfillment_mode", "acceptance_status", "read_after_write", "reconciliation"},
    "hardware": {"profile_status", "topology_match", "scan_status", "camera_status", "scale_status", "print_status", "safe_fallback", "reconciliation"},
    "partner_uat": {"partner_ref", "decision", "scenario_count", "critical_findings", "signed_off"},
    "rollback": {"rollback_status", "restore_status", "data_integrity", "replay_safe"},
    "slo_dr": {"slo_status", "error_budget", "rto_status", "rpo_status", "backup_restore", "dr_rehearsal", "alerting"},
    "production_support": {"support_status", "on_call", "runbook", "escalation", "handoff"},
}
V2_OBSERVATION_VALUES = {
    "bank": {"source_status": "observed", "read_after_write": True, "reconciliation": "matched"},
    "acquirer": {"source_status": "observed", "read_after_write": True, "reconciliation": "matched"},
    "marketplace_payout": {"source_status": "observed", "read_after_write": True, "reconciliation": "matched"},
    "fx": {"source_status": "observed", "read_after_write": True, "reconciliation": "matched"},
    "advertising": {"source_status": "observed", "read_after_write": True, "reconciliation": "matched"},
    "fbs": {"fulfillment_mode": "fbs", "handoff_status": "accepted", "read_after_write": True, "reconciliation": "matched"},
    "fbo": {"fulfillment_mode": "fbo", "acceptance_status": "accepted", "read_after_write": True, "reconciliation": "matched"},
    "hardware": {"profile_status": "matched", "topology_match": True, "scan_status": "observed", "camera_status": "observed", "scale_status": "observed", "print_status": "accepted", "safe_fallback": True, "reconciliation": "matched"},
    "rollback": {"rollback_status": "verified", "restore_status": "verified", "data_integrity": "preserved", "replay_safe": True},
    "slo_dr": {"slo_status": "met", "error_budget": "within", "rto_status": "met", "rpo_status": "met", "backup_restore": "verified", "dr_rehearsal": "passed", "alerting": "verified"},
    "production_support": {"support_status": "ready", "on_call": "confirmed", "runbook": "verified", "escalation": "tested", "handoff": "accepted"},
}


def fail(message: str) -> None:
    raise QualificationError(message)


def string_field(value: Any, name: str, pattern: re.Pattern[str] | None = None) -> str:
    if not isinstance(value, str) or not value.strip():
        fail(f"{name} must be a non-empty string")
    if pattern is not None and pattern.fullmatch(value) is None:
        fail(f"{name} has an unsafe format")
    return value


def utc_timestamp(value: Any, name: str) -> str:
    timestamp = string_field(value, name)
    if not timestamp.endswith("Z"):
        fail(f"{name} must be UTC")
    try:
        dt.datetime.fromisoformat(timestamp[:-1] + "+00:00")
    except ValueError as exc:
        fail(f"{name} must be an ISO-8601 timestamp: {exc}")
    return timestamp


def validate_topology(value: Any) -> None:
    if not isinstance(value, dict) or set(value) != {"topology_ref", "runtime_ref", "region"}:
        fail("topology has an invalid shape")
    for name in ("topology_ref", "runtime_ref", "region"):
        string_field(value[name], f"topology.{name}", SAFE_LABEL)


def validate_observations(value: Any, kind: str) -> None:
    expected = V2_OBSERVATION_FIELDS[kind]
    if not isinstance(value, dict) or set(value) != expected:
        fail(f"v2 {kind} observations have an invalid shape")
    for name, expected_value in V2_OBSERVATION_VALUES.get(kind, {}).items():
        if value[name] != expected_value:
            fail(f"v2 {kind} observation {name} is not confirmed")
    if kind == "partner_uat":
        string_field(value["partner_ref"], "observations.partner_ref", SAFE_LABEL)
        if value["decision"] != "accepted" or value["signed_off"] is not True:
            fail("partner UAT must be accepted and signed off")
        if not isinstance(value["scenario_count"], int) or value["scenario_count"] < 1:
            fail("partner UAT scenario_count must be a positive integer")
        if value["critical_findings"] != 0:
            fail("partner UAT must have no critical findings")


def validate(data: Any, kind: str) -> None:
    """Validate an evidence artifact and require the checks for ``kind``."""
    if kind not in V2_REQUIRED_CHECKS:
        raise ValueError(f"unsupported evidence kind: {kind}")
    if not isinstance(data, dict):
        fail("evidence root must be an object")
    reject_secret_shaped_fields(data)
    schema_version = data.get("schema_version")
    allowed_top_level = V2_TOP_LEVEL if schema_version == 2 else TOP_LEVEL
    unknown = set(data) - allowed_top_level
    if unknown:
        fail(f"unsupported top-level fields: {', '.join(sorted(unknown))}")
    missing = TOP_LEVEL - set(data)
    if missing:
        fail(f"missing top-level fields: {', '.join(sorted(missing))}")
    if schema_version not in {1, 2}:
        fail("schema_version must be 1 or 2")
    if kind in OPERATIONAL_KINDS and schema_version != 2:
        fail(f"{kind} evidence must use schema v2")
    if schema_version == 2:
        missing_v2 = {"topology", "observations"} - set(data)
        if missing_v2:
            fail(f"v2 evidence is missing: {', '.join(sorted(missing_v2))}")
        validate_topology(data["topology"])
        validate_observations(data["observations"], kind)
    if data["status"] != "PASS":
        fail("status must be PASS")
    expected_scope = V2_SCOPE_BY_KIND.get(kind, "hardware" if kind == "hardware" else ("warehouse" if kind in {"fbs", "fbo"} else "financial"))
    if data["scope"] != expected_scope:
        fail(f"scope must be {expected_scope}")
    expected_subject_kind = V2_SUBJECT_KIND_BY_KIND.get(kind, "hardware" if kind == "hardware" else "connector")
    if data["subject_kind"] != expected_subject_kind:
        fail(f"subject_kind must be {expected_subject_kind}")
    if data["environment"] != "non-production":
        fail("environment must be non-production")
    if data["target"] != "dedicated-non-production":
        fail("target must be dedicated-non-production")
    require_repository(string_field(data["repository"], "repository"))
    require_sha40(string_field(data["release_commit"], "release_commit"))
    string_field(data["subject_id"], "subject_id", SAFE_ID)
    string_field(data["account_ref"], "account_ref", SAFE_LABEL)
    utc_timestamp(data["qualified_at"], "qualified_at")
    string_field(data["version"], "version", SAFE_LABEL)

    scopes = data["scopes"]
    if not isinstance(scopes, list) or not scopes:
        fail("scopes must be a non-empty array")
    normalized_scopes: set[str] = set()
    for index, scope in enumerate(scopes):
        value = string_field(scope, f"scopes[{index}]", SCOPE_ID)
        if value in normalized_scopes:
            fail(f"duplicate scope: {value}")
        normalized_scopes.add(value)
    required_scopes = V2_REQUIRED_SCOPES if schema_version == 2 else REQUIRED_SCOPES
    missing_scopes = required_scopes[kind] - normalized_scopes
    if missing_scopes:
        fail(f"missing required {kind} scopes: {', '.join(sorted(missing_scopes))}")

    checks = data["checks"]
    if not isinstance(checks, list) or not checks:
        fail("checks must be a non-empty array")
    seen: set[str] = set()
    for index, check in enumerate(checks):
        if not isinstance(check, dict) or set(check) != {"id", "status", "evidence_ref"}:
            fail(f"checks[{index}] has an invalid shape")
        check_id = string_field(check["id"], f"checks[{index}].id", CHECK_ID)
        if check_id in seen:
            fail(f"duplicate check: {check_id}")
        seen.add(check_id)
        if check["status"] != "PASS":
            fail(f"check {check_id} is not PASS")
        string_field(check["evidence_ref"], f"checks[{index}].evidence_ref", SAFE_LABEL)
    required_checks = V2_REQUIRED_CHECKS if schema_version == 2 else REQUIRED_CHECKS
    missing_checks = required_checks[kind] - seen
    if missing_checks:
        fail(f"missing required {kind} checks: {', '.join(sorted(missing_checks))}")

    rollback = data["rollback"]
    if not isinstance(rollback, dict) or set(rollback) != {"verified", "evidence_ref"}:
        fail("rollback has an invalid shape")
    if rollback["verified"] is not True:
        fail("rollback.verified must be true")
    string_field(rollback["evidence_ref"], "rollback.evidence_ref", SAFE_LABEL)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True)
    parser.add_argument("--kind", required=True, choices=sorted(V2_REQUIRED_CHECKS))
    args = parser.parse_args()
    try:
        validate(read_json(args.input), args.kind)
    except (QualificationError, OSError) as exc:
        parser.error(str(exc))
    print(f"{args.kind} external qualification evidence: PASS")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
