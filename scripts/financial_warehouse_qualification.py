#!/usr/bin/env python3
"""Fail-closed gate for credentialed financial and warehouse qualification.

This validator only checks retained redacted evidence. It never calls banks,
marketplaces, carriers, hardware or advertising providers and never accepts
credentials. The shell wrapper runs both provider-neutral repository gates
before invoking it.
"""

from __future__ import annotations

import argparse
import datetime as dt
import pathlib
import re
from typing import Any, Mapping

import external_qualification_evidence
from p4_common import (
    QualificationError,
    read_json,
    reject_secret_shaped_fields,
    require_repository,
    require_sha40,
    sha256_file,
)


TOP_LEVEL = {
    "schema_version",
    "status",
    "scope",
    "environment",
    "target",
    "repository",
    "release_commit",
    "qualified_at",
    "subjects",
    "artifacts",
    "checks",
    "failure_matrix",
    "rollback",
}
V2_TOP_LEVEL = TOP_LEVEL | {"topology"}
SUBJECT_KINDS = ("bank", "acquirer", "marketplace_payout", "fx", "advertising", "fbs", "fbo", "hardware")
OPERATIONAL_KINDS = ("partner_uat", "rollback", "slo_dr", "production_support")
ARTIFACT_KINDS = SUBJECT_KINDS
V2_ARTIFACT_KINDS = SUBJECT_KINDS + OPERATIONAL_KINDS
SAFE_ID = re.compile(r"^[a-z][a-z0-9-]{0,63}$")
SAFE_LABEL = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$")
SHA256 = re.compile(r"^[0-9a-f]{64}$")
REQUIRED_CHECKS = {
    "bank_statement_sync": "bank",
    "acquirer_payment_refund": "acquirer",
    "payout_import": "marketplace_payout",
    "payout_order_match": "marketplace_payout",
    "fx_conversion_snapshot": "fx",
    "advertising_attribution": "advertising",
    "financial_reconciliation": "platform",
    "financial_duplicate_idempotency": "platform",
    "financial_unknown_recovery": "platform",
    "fbs_order_to_handoff": "fbs",
    "fbo_inbound_acceptance": "fbo",
    "warehouse_read_after_write": "platform",
    "warehouse_reconciliation": "platform",
    "warehouse_idempotency": "platform",
    "warehouse_unknown_recovery": "platform",
    "hardware_safe_fallback": "hardware",
    "device_matrix": "hardware",
}
REQUIRED_FAILURES = {
    "duplicate_statement_or_payout",
    "duplicate_scan_or_print",
    "unknown_remote_commit",
    "stale_fx_or_currency_mismatch",
    "unattributed_advertising",
    "cross_tenant",
    "revoked_device",
    "printer_outage",
    "offline_reconnect_conflict",
    "fbo_local_pick_denied",
    "fbs_handoff_timeout",
    "approval_denial",
    "rate_limit",
    "worker_crash_resume",
}
V2_REQUIRED_CHECKS = {
    **REQUIRED_CHECKS,
    "partner_uat": "partner",
    "rollback": "platform",
    "slo": "platform",
    "dr": "platform",
    "production_support": "platform",
    "support_escalation": "platform",
}
V2_REQUIRED_FAILURES = REQUIRED_FAILURES | {
    "uat_rejection",
    "rollback_restore_failure",
    "slo_breach",
    "dr_restore_failure",
    "support_escalation_failure",
}
V2_SUBJECT_KINDS = SUBJECT_KINDS + OPERATIONAL_KINDS
TOPOLOGY_FIELDS = {"topology_ref", "runtime_ref", "region"}


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
    if not isinstance(value, dict) or set(value) != TOPOLOGY_FIELDS:
        fail("topology has an invalid shape")
    for name in sorted(TOPOLOGY_FIELDS):
        string_field(value[name], f"topology.{name}", SAFE_LABEL)


def validate_document(data: Any) -> None:
    """Validate the aggregate financial/warehouse qualification manifest."""
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
        if "topology" not in data:
            fail("v2 evidence must contain target topology")
        validate_topology(data["topology"])
    if data["status"] != "PASS":
        fail("status must be PASS")
    if data["scope"] != "credentialed-financial-warehouse":
        fail("scope must be credentialed-financial-warehouse")
    if data["environment"] != "non-production":
        fail("environment must be non-production")
    if data["target"] != "dedicated-non-production":
        fail("target must be dedicated-non-production")
    require_repository(string_field(data["repository"], "repository"))
    require_sha40(string_field(data["release_commit"], "release_commit"))
    utc_timestamp(data["qualified_at"], "qualified_at")

    subjects = data["subjects"]
    subject_kinds = V2_SUBJECT_KINDS if schema_version == 2 else SUBJECT_KINDS
    if not isinstance(subjects, dict) or set(subjects) != set(subject_kinds):
        fail("subjects must contain all financial, FBS/FBO, hardware and v2 operational subjects")
    for kind in subject_kinds:
        subject = subjects[kind]
        if not isinstance(subject, dict) or set(subject) != {"subject_id", "account_ref", "evidence_ref"}:
            fail(f"subjects.{kind} has an invalid shape")
        string_field(subject["subject_id"], f"subjects.{kind}.subject_id", SAFE_ID)
        string_field(subject["account_ref"], f"subjects.{kind}.account_ref", SAFE_LABEL)
        string_field(subject["evidence_ref"], f"subjects.{kind}.evidence_ref", SAFE_LABEL)

    artifacts = data["artifacts"]
    artifact_kinds = V2_ARTIFACT_KINDS if schema_version == 2 else ARTIFACT_KINDS
    if not isinstance(artifacts, list) or len(artifacts) != len(artifact_kinds):
        fail("artifacts must contain exactly eight v1 or twelve v2 retained evidence artifacts")
    artifact_by_kind: dict[str, dict[str, str]] = {}
    for index, artifact in enumerate(artifacts):
        if not isinstance(artifact, dict) or set(artifact) != {"kind", "evidence_ref", "sha256"}:
            fail(f"artifacts[{index}] has an invalid shape")
        kind = artifact["kind"]
        if kind not in artifact_kinds or kind in artifact_by_kind:
            fail(f"duplicate or unknown artifact kind: {kind}")
        artifact_by_kind[kind] = artifact
        string_field(artifact["evidence_ref"], f"artifacts[{index}].evidence_ref", SAFE_LABEL)
        digest = string_field(artifact["sha256"], f"artifacts[{index}].sha256")
        if SHA256.fullmatch(digest) is None:
            fail(f"artifacts[{index}].sha256 must be a lowercase SHA-256 digest")
    if set(artifact_by_kind) != set(artifact_kinds):
        fail("all financial, FBS/FBO, hardware and v2 operational artifacts are required")
    for kind in subject_kinds:
        if data["subjects"][kind]["evidence_ref"] != artifact_by_kind[kind]["evidence_ref"]:
            fail(f"subjects.{kind}.evidence_ref does not bind its artifact")

    checks = data["checks"]
    required_checks = V2_REQUIRED_CHECKS if schema_version == 2 else REQUIRED_CHECKS
    if not isinstance(checks, list) or len(checks) != len(required_checks):
        fail("checks must contain exactly the financial and warehouse check set")
    seen: set[str] = set()
    for index, check in enumerate(checks):
        if not isinstance(check, dict) or set(check) != {"id", "owner", "status", "evidence_ref"}:
            fail(f"checks[{index}] has an invalid shape")
        check_id = check["id"]
        if check_id not in required_checks or check_id in seen:
            fail(f"duplicate or unknown qualification check: {check_id}")
        seen.add(check_id)
        if check["owner"] != required_checks[check_id]:
            fail(f"check {check_id} has an invalid owner")
        if check["status"] != "PASS":
            fail(f"check {check_id} is not PASS")
        string_field(check["evidence_ref"], f"checks[{index}].evidence_ref", SAFE_LABEL)
    if seen != set(required_checks):
        fail("the financial and warehouse check set is incomplete")

    failure_matrix = data["failure_matrix"]
    required_failures = V2_REQUIRED_FAILURES if schema_version == 2 else REQUIRED_FAILURES
    if not isinstance(failure_matrix, list) or len(failure_matrix) != len(required_failures):
        fail("failure_matrix must contain exactly the required scenarios")
    seen_failures: set[str] = set()
    for index, scenario in enumerate(failure_matrix):
        if not isinstance(scenario, dict) or set(scenario) != {"id", "status", "evidence_ref"}:
            fail(f"failure_matrix[{index}] has an invalid shape")
        scenario_id = scenario["id"]
        if scenario_id not in required_failures or scenario_id in seen_failures:
            fail(f"duplicate or unknown failure scenario: {scenario_id}")
        seen_failures.add(scenario_id)
        if scenario["status"] != "PASS":
            fail(f"failure scenario {scenario_id} is not PASS")
        string_field(scenario["evidence_ref"], f"failure_matrix[{index}].evidence_ref", SAFE_LABEL)
    if seen_failures != required_failures:
        fail("failure_matrix is incomplete")

    rollback = data["rollback"]
    if not isinstance(rollback, dict) or set(rollback) != {"verified", "evidence_ref"}:
        fail("rollback has an invalid shape")
    if rollback["verified"] is not True:
        fail("rollback.verified must be true")
    string_field(rollback["evidence_ref"], "rollback.evidence_ref", SAFE_LABEL)


def _check_identity(data: Mapping[str, Any], expected_commit: str, expected_repository: str | None) -> None:
    if data.get("release_commit") != expected_commit:
        fail("evidence release_commit does not match the checked-out release")
    if expected_repository and data.get("repository") != expected_repository:
        fail("evidence repository does not match the release repository")


def _load_file(path: pathlib.Path, kind: str) -> tuple[Any, str]:
    if not path.is_absolute():
        fail(f"{kind} evidence path must be absolute")
    if path.is_symlink() or not path.is_file():
        fail(f"{kind} evidence path must be a regular non-symlink file")
    data = read_json(path)
    return data, sha256_file(path)


def validate_bundle(
    data: Any,
    evidence_paths: Mapping[str, pathlib.Path],
    expected_commit: str,
    expected_repository: str | None = None,
) -> None:
    """Validate aggregate evidence and every retained external artifact."""
    validate_document(data)
    schema_version = data["schema_version"]
    artifact_kinds = V2_ARTIFACT_KINDS if schema_version == 2 else ARTIFACT_KINDS
    require_sha40(expected_commit)
    if expected_repository:
        require_repository(expected_repository)
    _check_identity(data, expected_commit, expected_repository)
    if set(evidence_paths) != set(artifact_kinds):
        fail("all eight v1 or twelve v2 evidence paths are required")

    artifact_by_kind = {artifact["kind"]: artifact for artifact in data["artifacts"]}
    loaded: dict[str, Any] = {}
    for kind in artifact_kinds:
        artifact_data, digest = _load_file(evidence_paths[kind], kind)
        if digest != artifact_by_kind[kind]["sha256"]:
            fail(f"{kind} evidence SHA-256 does not match the retained manifest")
        loaded[kind] = artifact_data

    for kind in artifact_kinds:
        external_qualification_evidence.validate(loaded[kind], kind)
        _check_identity(loaded[kind], expected_commit, expected_repository)
        if loaded[kind].get("schema_version") != schema_version:
            fail(f"{kind} evidence schema version does not match the aggregate")
        if schema_version == 2 and loaded[kind].get("topology") != data["topology"]:
            fail(f"{kind} evidence topology does not match the aggregate")
        subject = data["subjects"][kind]
        if loaded[kind].get("subject_id") != subject["subject_id"]:
            fail(f"{kind} evidence subject does not match the manifest")
        if loaded[kind].get("account_ref") != subject["account_ref"]:
            fail(f"{kind} evidence account does not match the manifest")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True, help="aggregate financial/warehouse evidence JSON")
    for kind in V2_ARTIFACT_KINDS:
        parser.add_argument(f"--{kind.replace('_', '-')}-evidence")
    parser.add_argument("--expected-release-commit", required=True)
    parser.add_argument("--expected-repository")
    args = parser.parse_args()
    try:
        aggregate = read_json(args.input)
        artifact_kinds = V2_ARTIFACT_KINDS if aggregate.get("schema_version") == 2 else ARTIFACT_KINDS
        paths = {}
        for kind in artifact_kinds:
            value = getattr(args, f"{kind}_evidence")
            if not value:
                parser.error(f"--{kind.replace('_', '-')}-evidence is required for this aggregate")
            paths[kind] = pathlib.Path(value)
        validate_bundle(
            aggregate,
            paths,
            args.expected_release_commit,
            args.expected_repository,
        )
    except (QualificationError, OSError, ValueError) as exc:
        parser.error(str(exc))
    print("Financial and warehouse external qualification evidence: PASS")
    print("Financial feeds, FBS/FBO, hardware topology, partner UAT, rollback, SLO/DR and production support artifacts are bound to this release.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
