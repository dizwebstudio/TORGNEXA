#!/usr/bin/env python3
"""Validate redacted evidence for one non-production external connector."""

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
SAFE_LABEL = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$")
SAFE_ID = re.compile(r"^[a-z][a-z0-9-]{0,63}$")
CHECK_ID = re.compile(r"^[a-z][a-z0-9_.-]{0,63}$")
REQUIRED_CHECKS = {
    "carrier": {"health", "rate", "label", "shipment_handoff", "return_inspection"},
    "payment": {"health", "refund", "settlement", "reconciliation"},
    "fiscal": {"health", "marking_edo", "refund_fiscalization", "close"},
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
    missing_checks = REQUIRED_CHECKS[kind] - seen
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
