#!/usr/bin/env python3
"""Validate redacted evidence for one non-production external connector."""

from __future__ import annotations

import argparse
import datetime as dt
import re
from typing import Any

from golden_path_linkage import validate_flow
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
    "environment",
    "target",
    "repository",
    "release_commit",
    "connector_id",
    "account_ref",
    "qualified_at",
    "checks",
    "rollback",
}
V2_TOP_LEVEL = TOP_LEVEL | {"flow", "observations"}
SAFE_LABEL = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$")
SAFE_ID = re.compile(r"^[a-z][a-z0-9-]{0,63}$")
CHECK_ID = re.compile(r"^[a-z][a-z0-9_.-]{0,63}$")
REQUIRED_CHECKS = {
    "carrier": {"health", "rate", "label", "shipment_handoff", "return_inspection"},
    "payment": {"health", "refund", "settlement", "reconciliation"},
    "fiscal": {"health", "marking_edo", "refund_fiscalization", "close"},
    "marking": {"health", "product_read", "status_read", "reconciliation"},
    "edo": {"health", "document_send", "document_status", "reconciliation"},
}
V2_REQUIRED_CHECKS = {
    **REQUIRED_CHECKS,
    "fiscal": {"health", "fiscalization", "refund_fiscalization", "close"},
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
    """Validate an external connector evidence document for ``kind``."""
    if kind not in REQUIRED_CHECKS:
        raise ValueError(f"unsupported connector evidence kind: {kind}")
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
    if schema_version == 2:
        if "flow" not in data:
            fail("v2 connector evidence must contain flow linkage")
        validate_flow(data["flow"])
        if kind in {"marking", "edo"}:
            observations = data.get("observations")
            expected_observations = {
                "marking": {"product_status", "marking_status", "reconciliation"},
                "edo": {"send_status", "document_status", "reconciliation"},
            }[kind]
            if not isinstance(observations, dict) or set(observations) != expected_observations:
                fail(f"v2 {kind} observations have an invalid shape")
            expected_values = {
                "product_status": "matched",
                "marking_status": "observed",
                "send_status": "accepted",
                "document_status": "delivered",
                "reconciliation": "matched",
            }
            for field in expected_observations:
                if observations[field] != expected_values[field]:
                    fail(f"v2 {kind} observation {field} is not confirmed")
    if data["status"] != "PASS":
        fail("status must be PASS")
    if data["scope"] != "connector":
        fail("scope must be connector")
    if data["environment"] != "non-production":
        fail("environment must be non-production")
    if data["target"] != "dedicated-non-production":
        fail("target must be dedicated-non-production")
    require_repository(string_field(data["repository"], "repository"))
    require_sha40(string_field(data["release_commit"], "release_commit"))
    string_field(data["connector_id"], "connector_id", SAFE_ID)
    string_field(data["account_ref"], "account_ref", SAFE_LABEL)
    utc_timestamp(data["qualified_at"], "qualified_at")

    checks = data["checks"]
    if not isinstance(checks, list) or not checks:
        fail("checks must be a non-empty array")
    seen: set[str] = set()
    for index, check in enumerate(checks):
        if not isinstance(check, dict):
            fail(f"checks[{index}] must be an object")
        check_id = string_field(check.get("id"), f"checks[{index}].id", CHECK_ID)
        if check_id in seen:
            fail(f"duplicate check: {check_id}")
        seen.add(check_id)
        if check.get("status") != "PASS":
            fail(f"check {check_id} is not PASS")
        string_field(check.get("evidence_ref"), f"checks[{index}].evidence_ref", SAFE_LABEL)
    required_checks = V2_REQUIRED_CHECKS if schema_version == 2 else REQUIRED_CHECKS
    missing_checks = required_checks[kind] - seen
    if missing_checks:
        fail(f"missing required {kind} checks: {', '.join(sorted(missing_checks))}")

    rollback = data["rollback"]
    if not isinstance(rollback, dict) or rollback.get("verified") is not True:
        fail("rollback.verified must be true")
    string_field(rollback.get("evidence_ref"), "rollback.evidence_ref", SAFE_LABEL)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True, help="redacted connector evidence JSON")
    parser.add_argument("--kind", required=True, choices=sorted(REQUIRED_CHECKS))
    args = parser.parse_args()
    try:
        validate(read_json(args.input), args.kind)
    except (QualificationError, OSError) as exc:
        parser.error(str(exc))
    print(f"{args.kind} connector evidence: PASS")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
