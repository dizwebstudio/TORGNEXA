#!/usr/bin/env python3
"""Validate the redacted evidence emitted by marketplace live smoke."""

from __future__ import annotations

import argparse
import json
import re
from pathlib import Path
from typing import Any


_SHA40 = re.compile(r"^[0-9a-f]{40}$")
_FINGERPRINT = re.compile(r"^[0-9a-f]{64}$")
_EVIDENCE_REF = re.compile(r"^marketplace-live-smoke/[a-z_]+$")
_SAFE_LABEL = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$")
_FORBIDDEN_KEYS = {
    "authorization",
    "access_token",
    "api_key",
    "client_secret",
    "private_key",
    "password",
    "raw_body",
    "raw_payload",
    "remote_id",
    "product_id",
    "variant_id",
    "quantity",
    "token",
}
_CHECKS = {
    "health",
    "products_read",
    "inventory_locations_read",
    "inventory_read",
    "orders_read",
    "taxonomy_read",
    "inventory_write",
    "read_after_write",
    "cleanup",
}


def _require(condition: bool, message: str) -> None:
    if not condition:
        raise ValueError(message)


def _walk_forbidden_keys(value: Any, path: str = "evidence") -> None:
    if isinstance(value, dict):
        for key, child in value.items():
            _require(key.lower() not in _FORBIDDEN_KEYS, f"forbidden field at {path}.{key}")
            _walk_forbidden_keys(child, f"{path}.{key}")
    elif isinstance(value, list):
        for index, child in enumerate(value):
            _walk_forbidden_keys(child, f"{path}[{index}]")


def validate_document(document: Any) -> None:
    _require(isinstance(document, dict), "evidence root must be an object")
    _walk_forbidden_keys(document)
    required = {
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
        "credential_mode",
        "taxonomy",
        "checks",
        "write",
    }
    _require(required <= document.keys(), "evidence is missing required fields")
    _require(document["schema_version"] == 1, "schema_version must be 1")
    _require(document["status"] in {"PASS", "FAIL"}, "status must be PASS or FAIL")
    _require(document["scope"] in {"read", "qualification"}, "scope is invalid")
    _require(document["environment"] == "non-production", "environment is not non-production")
    _require(document["target"] == "dedicated-non-production", "target is not dedicated non-production")
    _require(isinstance(document["repository"], str) and document["repository"], "repository is required")
    _require(isinstance(document["release_commit"], str) and _SHA40.fullmatch(document["release_commit"]), "release_commit is invalid")
    _require(document["connector_id"] in {"wildberries", "ozon"}, "connector_id is invalid")
    _require(isinstance(document["account_ref"], str) and _SAFE_LABEL.fullmatch(document["account_ref"]), "account_ref is invalid")
    _require(isinstance(document["qualified_at"], str) and document["qualified_at"].endswith("Z"), "qualified_at must be UTC")
    _require(document["credential_mode"] == "env_only_secret_accessor", "credential mode is invalid")

    taxonomy = document["taxonomy"]
    _require(isinstance(taxonomy, dict), "taxonomy must be an object")
    _require(taxonomy.get("status") in {"PASS", "NOT_RUN"}, "taxonomy status is invalid")
    fingerprint = taxonomy.get("fingerprint")
    _require(isinstance(fingerprint, str), "taxonomy fingerprint must be a string")
    if taxonomy["status"] == "PASS":
        _require(_FINGERPRINT.fullmatch(fingerprint) is not None, "taxonomy fingerprint is invalid")
    else:
        _require(fingerprint == "", "unrun taxonomy must not have a fingerprint")
    _require(isinstance(taxonomy.get("source"), str), "taxonomy source must be a string")

    checks = document["checks"]
    _require(isinstance(checks, list), "checks must be an array")
    seen: set[str] = set()
    for check in checks:
        _require(isinstance(check, dict), "check must be an object")
        check_id = check.get("id")
        _require(check_id in _CHECKS, f"unknown check: {check_id}")
        _require(check_id not in seen, f"duplicate check: {check_id}")
        seen.add(check_id)
        _require(check.get("status") == "PASS", f"check {check_id} is not PASS")
        _require(isinstance(check.get("evidence_ref"), str) and _EVIDENCE_REF.fullmatch(check["evidence_ref"]), f"check {check_id} evidence_ref is invalid")

    write = document["write"]
    _require(isinstance(write, dict), "write must be an object")
    _require(all(isinstance(write.get(key), bool) for key in ("attempted", "read_after_write", "restored")), "write flags must be boolean")
    if document["status"] == "PASS":
        _require("failure" not in document, "PASS evidence cannot contain failure")
        _require(write["restored"], "PASS evidence must confirm restoration")
    else:
        failure = document.get("failure")
        _require(isinstance(failure, dict) and failure.get("check_id") and failure.get("error_code"), "FAIL evidence must identify the failure")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("path", type=Path)
    args = parser.parse_args()
    try:
        validate_document(json.loads(args.path.read_text(encoding="utf-8")))
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"marketplace live smoke evidence: invalid: {exc}")
        return 1
    print("marketplace live smoke evidence: valid redacted document")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
