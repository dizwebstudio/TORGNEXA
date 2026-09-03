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


def validate(data: Any, kind: str) -> None:
    """Validate an evidence artifact and require the checks for ``kind``."""
    if kind not in REQUIRED_CHECKS:
        raise ValueError(f"unsupported evidence kind: {kind}")
    if not isinstance(data, dict):
        fail("evidence root must be an object")
    reject_secret_shaped_fields(data)
    unknown = set(data) - TOP_LEVEL
    if unknown:
        fail(f"unsupported top-level fields: {', '.join(sorted(unknown))}")
    missing = TOP_LEVEL - set(data)
    if missing:
        fail(f"missing top-level fields: {', '.join(sorted(missing))}")
    if data["schema_version"] != 1:
        fail("schema_version must be 1")
    if data["status"] != "PASS":
        fail("status must be PASS")
    expected_scope = "hardware" if kind == "hardware" else ("warehouse" if kind in {"fbs", "fbo"} else "financial")
    if data["scope"] != expected_scope:
        fail(f"scope must be {expected_scope}")
    expected_subject_kind = "hardware" if kind == "hardware" else "connector"
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
    missing_scopes = REQUIRED_SCOPES[kind] - normalized_scopes
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
    missing_checks = REQUIRED_CHECKS[kind] - seen
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
    parser.add_argument("--kind", required=True, choices=sorted(REQUIRED_CHECKS))
    args = parser.parse_args()
    try:
        validate(read_json(args.input), args.kind)
    except (QualificationError, OSError) as exc:
        parser.error(str(exc))
    print(f"{args.kind} external qualification evidence: PASS")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
