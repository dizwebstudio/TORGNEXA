#!/usr/bin/env python3
"""Validate redacted live marketplace return/compensation evidence."""

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
    "flow",
    "checks",
    "observations",
    "rollback",
}
SAFE_ID = re.compile(r"^[a-z][a-z0-9-]{0,63}$")
SAFE_LABEL = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$")
CHECK_ID = re.compile(r"^[a-z][a-z0-9_.-]{0,63}$")
EVIDENCE_REF = re.compile(r"^marketplace-compensation/[a-z_]+$")
REQUIRED_CHECKS = {
    "health",
    "return_authorize",
    "return_receive",
    "compensation",
    "compensation_read_after_write",
    "reconciliation",
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


def validate(data: Any) -> None:
    """Validate a complete return/refund/compensation observation."""
    if not isinstance(data, dict):
        fail("evidence root must be an object")
    reject_secret_shaped_fields(data)
    unknown = set(data) - TOP_LEVEL
    if unknown:
        fail(f"unsupported top-level fields: {', '.join(sorted(unknown))}")
    missing = TOP_LEVEL - set(data)
    if missing:
        fail(f"missing top-level fields: {', '.join(sorted(missing))}")
    if data["schema_version"] != 2:
        fail("schema_version must be 2")
    if data["status"] != "PASS":
        fail("status must be PASS")
    if data["scope"] != "qualification":
        fail("scope must be qualification")
    if data["environment"] != "non-production":
        fail("environment must be non-production")
    if data["target"] != "dedicated-non-production":
        fail("target must be dedicated-non-production")
    require_repository(string_field(data["repository"], "repository"))
    require_sha40(string_field(data["release_commit"], "release_commit"))
    if data["connector_id"] not in {"wildberries", "ozon", "yandex-market"}:
        fail("connector_id is outside the supported marketplace wave")
    string_field(data["account_ref"], "account_ref", SAFE_LABEL)
    utc_timestamp(data["qualified_at"], "qualified_at")
    validate_flow(data["flow"])

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
        if check_id not in REQUIRED_CHECKS:
            fail(f"unknown compensation check: {check_id}")
        seen.add(check_id)
        if check["status"] != "PASS":
            fail(f"check {check_id} is not PASS")
        string_field(check["evidence_ref"], f"checks[{index}].evidence_ref", EVIDENCE_REF)
    if seen != REQUIRED_CHECKS:
        fail("marketplace compensation check set is incomplete")

    observations = data["observations"]
    expected_observations = {
        "return_status",
        "refund_status",
        "compensation_status",
        "settlement_status",
        "read_after_write",
        "idempotent_replay",
        "reconciliation",
    }
    if not isinstance(observations, dict) or set(observations) != expected_observations:
        fail("observations has an invalid shape")
    if observations["return_status"] != "received":
        fail("return_status must be received")
    if observations["refund_status"] != "accepted":
        fail("refund_status must be accepted")
    if observations["compensation_status"] != "accepted":
        fail("compensation_status must be accepted")
    if observations["settlement_status"] != "matched":
        fail("settlement_status must be matched")
    for field in ("read_after_write", "idempotent_replay", "reconciliation"):
        if observations[field] is not True:
            fail(f"observations.{field} must be true")

    rollback = data["rollback"]
    if not isinstance(rollback, dict) or set(rollback) != {"verified", "evidence_ref"}:
        fail("rollback has an invalid shape")
    if rollback["verified"] is not True:
        fail("rollback.verified must be true")
    string_field(rollback["evidence_ref"], "rollback.evidence_ref", SAFE_LABEL)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True, help="redacted marketplace compensation evidence JSON")
    args = parser.parse_args()
    try:
        validate(read_json(args.input))
    except (QualificationError, OSError) as exc:
        parser.error(str(exc))
    print("Marketplace compensation evidence: PASS")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
