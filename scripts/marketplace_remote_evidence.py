#!/usr/bin/env python3
"""Validate retained, redacted marketplace qualification evidence.

This is a structural gate only. It never calls a provider and cannot turn
synthetic evidence into a live qualification.
"""
from __future__ import annotations

import argparse
import re
from typing import Any

from p4_common import (
    QualificationError,
    read_json,
    reject_secret_shaped_fields,
    require_repository,
    require_sha40,
)


CONNECTORS = {"wildberries", "ozon", "yandex-market"}
ENVIRONMENTS = {"sandbox", "staging", "non-production"}
SHA40 = re.compile(r"^[0-9a-f]{40}$")
SCHEMA_VERSION = 1

LISTING_CHECKS = {
    "taxonomy_read",
    "taxonomy_mapping",
    "batch_apply",
    "rate_limit",
    "idempotency",
    "partial_result",
    "read_after_write",
    "unknown_outcome",
    "reconciliation",
    "rollback",
}
FULL_CHECKS = LISTING_CHECKS | {
    "order_import",
    "reservation",
    "pick_pack",
    "label",
    "shipment_handoff",
    "return_inspection",
    "refund_settlement",
    "marking_edo",
    "p_and_l",
}
TOP_LEVEL = {
    "schema_version",
    "status",
    "environment",
    "repository",
    "release_commit",
    "connector_id",
    "api_version",
    "account_ref",
    "qualified_at",
    "taxonomy",
    "capabilities",
    "checks",
    "rollback",
}


def fail(message: str) -> None:
    raise QualificationError(message)


def string_field(value: Any, name: str) -> str:
    if not isinstance(value, str) or not value.strip():
        fail(f"{name} must be a non-empty string")
    return value


def validate(data: Any, scope: str) -> None:
    if not isinstance(data, dict):
        fail("evidence root must be an object")
    reject_secret_shaped_fields(data)
    unknown = set(data) - TOP_LEVEL
    if unknown:
        fail(f"unsupported top-level fields: {', '.join(sorted(unknown))}")
    required = TOP_LEVEL - {"api_version"}
    missing = required - set(data)
    if missing:
        fail(f"missing top-level fields: {', '.join(sorted(missing))}")
    if data["schema_version"] != SCHEMA_VERSION:
        fail(f"schema_version must be {SCHEMA_VERSION}")
    if data["status"] != "PASS":
        fail("status must be PASS")
    if data["environment"] not in ENVIRONMENTS:
        fail("environment must be sandbox, staging or non-production")
    if data["connector_id"] not in CONNECTORS:
        fail("connector_id is outside the first marketplace qualification wave")
    require_repository(string_field(data["repository"], "repository"))
    for name in ("account_ref", "qualified_at"):
        string_field(data[name], name)
    require_sha40(data["release_commit"])
    if "api_version" in data:
        string_field(data["api_version"], "api_version")

    taxonomy = data["taxonomy"]
    if not isinstance(taxonomy, dict):
        fail("taxonomy must be an object")
    for name in ("version", "fingerprint", "mapping_ref", "source_ref"):
        string_field(taxonomy.get(name), f"taxonomy.{name}")
    if not re.fullmatch(r"[0-9a-f]{64}", taxonomy["fingerprint"]):
        fail("taxonomy.fingerprint must be a SHA-256 digest")

    capabilities = data["capabilities"]
    if not isinstance(capabilities, dict) or not capabilities:
        fail("capabilities must be a non-empty object")
    required_capabilities = {"products.read", "products.write"}
    if scope == "full":
        required_capabilities |= {
            "orders.read",
            "inventory.write",
            "orders.status.write",
        }
    for capability in required_capabilities:
        item = capabilities.get(capability)
        if not isinstance(item, dict) or item.get("status") != "qualified":
            fail(f"capability {capability} must have status qualified")
        string_field(item.get("evidence_ref"), f"capabilities.{capability}.evidence_ref")

    checks = data["checks"]
    if not isinstance(checks, list) or not checks:
        fail("checks must be a non-empty array")
    required_checks = FULL_CHECKS if scope == "full" else LISTING_CHECKS
    seen: set[str] = set()
    for index, check in enumerate(checks):
        if not isinstance(check, dict):
            fail(f"checks[{index}] must be an object")
        check_id = string_field(check.get("id"), f"checks[{index}].id")
        if check_id in seen:
            fail(f"duplicate check: {check_id}")
        seen.add(check_id)
        if check.get("status") != "PASS":
            fail(f"check {check_id} is not PASS")
        string_field(check.get("evidence_ref"), f"checks[{index}].evidence_ref")
    missing_checks = required_checks - seen
    if missing_checks:
        fail(f"missing required checks: {', '.join(sorted(missing_checks))}")

    rollback = data["rollback"]
    if not isinstance(rollback, dict) or rollback.get("verified") is not True:
        fail("rollback.verified must be true")
    string_field(rollback.get("evidence_ref"), "rollback.evidence_ref")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True, help="redacted evidence JSON")
    parser.add_argument("--scope", choices=("listing", "full"), default="listing")
    args = parser.parse_args()
    try:
        validate(read_json(args.input), args.scope)
    except QualificationError as exc:
        parser.error(str(exc))
    print(f"Marketplace {args.scope} evidence structure: PASS")
    print("This gate validates retained redacted evidence only; live qualification remains external.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
