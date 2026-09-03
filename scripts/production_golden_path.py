#!/usr/bin/env python3
"""Fail-closed gate for the complete credentialed production golden path.

The gate validates retained redacted evidence only. It never calls a provider,
never receives credentials and cannot turn a synthetic result into a live one.
The shell wrapper runs the provider-neutral synthetic qualification before
invoking this validator.
"""

from __future__ import annotations

import argparse
import datetime as dt
import pathlib
import re
from typing import Any, Mapping

import connector_golden_path_evidence
import marketplace_compensation_evidence
import marketplace_live_smoke_evidence
import marketplace_remote_evidence
from golden_path_linkage import SAFE_LABEL, same_flow, validate_flow
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
    "flow",
    "connectors",
    "artifacts",
    "checks",
    "failure_matrix",
    "rollback",
}
CONNECTOR_KINDS = ("marketplace", "carrier", "payment", "fiscal", "marking", "edo")
ARTIFACT_KINDS = (
    "marketplace-remote",
    "marketplace-live-smoke",
    "marketplace-compensation",
    "carrier",
    "payment",
    "fiscal",
    "chestny-znak",
    "edo",
)
MARKETPLACE_CONNECTORS = {"wildberries", "ozon", "yandex-market"}
SAFE_ID = re.compile(r"^[a-z][a-z0-9-]{0,63}$")
SHA256 = re.compile(r"^[0-9a-f]{64}$")
REQUIRED_CHECKS = {
    "marketplace_live_smoke": "marketplace",
    "order_import": "marketplace",
    "reservation": "marketplace",
    "marketplace_compensation": "marketplace",
    "pick_pack": "platform",
    "label": "carrier",
    "shipment_handoff": "carrier",
    "return_inspection": "carrier",
    "refund_settlement": "payment",
    "fiscalization": "fiscal",
    "chestny_znak_live": "marking",
    "edo_live": "edo",
    "settlement_reconciliation": "payment",
    "profitability": "platform",
    "reconciliation": "platform",
}
FLOW_LINKS = {
    "order_to_reservation": ("order", "reservation"),
    "reservation_to_shipment": ("reservation", "shipment"),
    "shipment_to_return": ("shipment", "return"),
    "return_to_refund": ("return", "refund"),
    "refund_to_settlement": ("refund", "settlement"),
    "settlement_to_reconciliation": ("settlement", "reconciliation"),
    "order_to_marking": ("order", "marking"),
    "refund_to_fiscal": ("refund", "fiscal"),
    "settlement_to_edo": ("settlement", "edo"),
}
REQUIRED_FAILURES = {
    "duplicate_commands",
    "duplicate_webhooks",
    "out_of_order_events",
    "worker_crash_resume",
    "lease_loss",
    "timeout_after_remote_commit",
    "insufficient_stock",
    "expired_label",
    "over_refund",
    "cross_tenant_access",
    "approval_denial",
    "rate_limit",
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


def validate_document(data: Any) -> None:
    """Validate the aggregate production golden path evidence document."""
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
    if data["scope"] != "credentialed-full":
        fail("scope must be credentialed-full")
    if data["environment"] != "non-production":
        fail("environment must be non-production")
    if data["target"] != "dedicated-non-production":
        fail("target must be dedicated-non-production")
    require_repository(string_field(data["repository"], "repository"))
    require_sha40(string_field(data["release_commit"], "release_commit"))
    utc_timestamp(data["qualified_at"], "qualified_at")

    flow = data["flow"]
    if not isinstance(flow, dict) or "links" not in flow:
        fail("flow must contain shared references and links")
    validate_flow({field: flow.get(field) for field in flow if field != "links"}, "flow")
    expected_flow_fields = {"flow_ref", "order_ref", "reservation_ref", "shipment_ref", "return_ref", "refund_ref", "settlement_ref", "marking_ref", "edo_ref", "links"}
    if set(flow) != expected_flow_fields:
        fail("flow must contain exactly the shared references and links")
    links = flow["links"]
    if not isinstance(links, list) or len(links) != len(FLOW_LINKS):
        fail("flow.links must contain exactly the cross-system linkage set")
    seen_links: set[str] = set()
    for index, link in enumerate(links):
        if not isinstance(link, dict) or set(link) != {"id", "from", "to", "evidence_ref"}:
            fail(f"flow.links[{index}] has an invalid shape")
        link_id = string_field(link["id"], f"flow.links[{index}].id")
        if link_id not in FLOW_LINKS or link_id in seen_links:
            fail(f"duplicate or unknown flow link: {link_id}")
        seen_links.add(link_id)
        expected_from, expected_to = FLOW_LINKS[link_id]
        if link["from"] != expected_from or link["to"] != expected_to:
            fail(f"flow link {link_id} has invalid endpoints")
        string_field(link["evidence_ref"], f"flow.links[{index}].evidence_ref", SAFE_LABEL)
    if seen_links != set(FLOW_LINKS):
        fail("flow.links is incomplete")

    connectors = data["connectors"]
    if not isinstance(connectors, dict) or set(connectors) != set(CONNECTOR_KINDS):
        fail("connectors must contain marketplace, carrier, payment, fiscal, marking and edo")
    for kind in CONNECTOR_KINDS:
        connector = connectors[kind]
        if not isinstance(connector, dict) or set(connector) != {"connector_id", "account_ref", "evidence_ref"}:
            fail(f"connectors.{kind} has an invalid shape")
        connector_id = string_field(connector["connector_id"], f"connectors.{kind}.connector_id", SAFE_ID)
        string_field(connector["account_ref"], f"connectors.{kind}.account_ref", SAFE_LABEL)
        string_field(connector["evidence_ref"], f"connectors.{kind}.evidence_ref", SAFE_LABEL)
        if kind == "marketplace" and connector_id not in MARKETPLACE_CONNECTORS:
            fail("connectors.marketplace.connector_id is outside the supported wave")
        if kind == "marking" and connector_id != "chestny-znak":
            fail("connectors.marking.connector_id must be chestny-znak")
        if kind == "edo" and connector_id not in {"diadoc", "saby-edo"}:
            fail("connectors.edo.connector_id must be diadoc or saby-edo")

    artifacts = data["artifacts"]
    if not isinstance(artifacts, list) or len(artifacts) != len(ARTIFACT_KINDS):
        fail("artifacts must contain exactly eight retained evidence artifacts")
    artifact_by_kind: dict[str, dict[str, str]] = {}
    for index, artifact in enumerate(artifacts):
        if not isinstance(artifact, dict) or set(artifact) != {"kind", "evidence_ref", "sha256"}:
            fail(f"artifacts[{index}] has an invalid shape")
        kind = artifact["kind"]
        if kind not in ARTIFACT_KINDS or kind in artifact_by_kind:
            fail(f"duplicate or unknown artifact kind: {kind}")
        artifact_by_kind[kind] = artifact
        string_field(artifact["evidence_ref"], f"artifacts[{index}].evidence_ref", SAFE_LABEL)
        digest = string_field(artifact["sha256"], f"artifacts[{index}].sha256")
        if SHA256.fullmatch(digest) is None:
            fail(f"artifacts[{index}].sha256 must be a lowercase SHA-256 digest")
    if set(artifact_by_kind) != set(ARTIFACT_KINDS):
        fail("all marketplace, compensation, carrier, payment, fiscal, Chestny ZNAK and EDO artifacts are required")
    for connector_kind, artifact_kind in (
        ("marketplace", "marketplace-remote"),
        ("carrier", "carrier"),
        ("payment", "payment"),
        ("fiscal", "fiscal"),
        ("marking", "chestny-znak"),
        ("edo", "edo"),
    ):
        if connectors[connector_kind]["evidence_ref"] != artifact_by_kind[artifact_kind]["evidence_ref"]:
            fail(f"connectors.{connector_kind}.evidence_ref does not bind its artifact")

    checks = data["checks"]
    if not isinstance(checks, list) or len(checks) != len(REQUIRED_CHECKS):
        fail("checks must contain exactly the full golden path check set")
    seen: set[str] = set()
    for index, check in enumerate(checks):
        if not isinstance(check, dict) or set(check) != {"id", "owner", "status", "evidence_ref"}:
            fail(f"checks[{index}] has an invalid shape")
        check_id = check["id"]
        if check_id not in REQUIRED_CHECKS or check_id in seen:
            fail(f"duplicate or unknown golden path check: {check_id}")
        seen.add(check_id)
        if check["owner"] != REQUIRED_CHECKS[check_id]:
            fail(f"check {check_id} has an invalid owner")
        if check["status"] != "PASS":
            fail(f"check {check_id} is not PASS")
        string_field(check["evidence_ref"], f"checks[{index}].evidence_ref", SAFE_LABEL)
    if seen != set(REQUIRED_CHECKS):
        fail("the full golden path check set is incomplete")

    failure_matrix = data["failure_matrix"]
    if not isinstance(failure_matrix, list) or len(failure_matrix) != len(REQUIRED_FAILURES):
        fail("failure_matrix must contain exactly the required failure scenarios")
    seen_failures: set[str] = set()
    for index, scenario in enumerate(failure_matrix):
        if not isinstance(scenario, dict) or set(scenario) != {"id", "status", "evidence_ref"}:
            fail(f"failure_matrix[{index}] has an invalid shape")
        scenario_id = scenario["id"]
        if scenario_id not in REQUIRED_FAILURES or scenario_id in seen_failures:
            fail(f"duplicate or unknown failure scenario: {scenario_id}")
        seen_failures.add(scenario_id)
        if scenario["status"] != "PASS":
            fail(f"failure scenario {scenario_id} is not PASS")
        string_field(scenario["evidence_ref"], f"failure_matrix[{index}].evidence_ref", SAFE_LABEL)
    if seen_failures != REQUIRED_FAILURES:
        fail("failure_matrix is incomplete")

    rollback = data["rollback"]
    if not isinstance(rollback, dict) or set(rollback) != {"verified", "evidence_ref"}:
        fail("rollback has an invalid shape")
    if rollback["verified"] is not True:
        fail("rollback.verified must be true")
    string_field(rollback["evidence_ref"], "rollback.evidence_ref", SAFE_LABEL)


def _check_release_identity(
    data: Mapping[str, Any], expected_commit: str, expected_repository: str | None
) -> None:
    if data.get("release_commit") != expected_commit:
        fail("evidence release_commit does not match the checked-out release")
    if expected_repository and data.get("repository") != expected_repository:
        fail("evidence repository does not match the release repository")


def _check_file(path: pathlib.Path, kind: str) -> tuple[Any, str]:
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
    """Validate the aggregate and all eight linked evidence artifacts."""
    validate_document(data)
    require_sha40(expected_commit)
    if expected_repository:
        require_repository(expected_repository)
    _check_release_identity(data, expected_commit, expected_repository)
    if set(evidence_paths) != set(ARTIFACT_KINDS):
        fail("all eight evidence paths are required")

    artifact_by_kind = {artifact["kind"]: artifact for artifact in data["artifacts"]}
    loaded: dict[str, Any] = {}
    for kind in ARTIFACT_KINDS:
        artifact_data, digest = _check_file(evidence_paths[kind], kind)
        artifact = artifact_by_kind[kind]
        if digest != artifact["sha256"]:
            fail(f"{kind} evidence SHA-256 does not match the retained manifest")
        loaded[kind] = artifact_data

    remote = loaded["marketplace-remote"]
    marketplace_remote_evidence.validate(remote, "full")
    if remote.get("schema_version") != 2 or remote.get("environment") != "non-production":
        fail("marketplace remote evidence must use linked schema v2")
    live = loaded["marketplace-live-smoke"]
    marketplace_live_smoke_evidence.validate_document(live)
    if live.get("schema_version") != 2 or live.get("status") != "PASS" or live.get("scope") != "qualification":
        fail("marketplace live smoke must be a linked credentialed qualification PASS")
    marketplace_compensation_evidence.validate(loaded["marketplace-compensation"])
    connector_identity = data["connectors"]
    if remote.get("connector_id") != connector_identity["marketplace"]["connector_id"]:
        fail("marketplace remote evidence connector does not match the manifest")
    if live.get("connector_id") != connector_identity["marketplace"]["connector_id"]:
        fail("marketplace live smoke connector does not match the manifest")
    if remote.get("account_ref") != connector_identity["marketplace"]["account_ref"]:
        fail("marketplace remote evidence account does not match the manifest")
    if live.get("account_ref") != connector_identity["marketplace"]["account_ref"]:
        fail("marketplace live smoke account does not match the manifest")
    if loaded["marketplace-compensation"].get("connector_id") != connector_identity["marketplace"]["connector_id"]:
        fail("marketplace compensation connector does not match the manifest")
    if loaded["marketplace-compensation"].get("account_ref") != connector_identity["marketplace"]["account_ref"]:
        fail("marketplace compensation account does not match the manifest")

    for connector_kind, artifact_kind in (
        ("carrier", "carrier"),
        ("payment", "payment"),
        ("fiscal", "fiscal"),
        ("marking", "chestny-znak"),
        ("edo", "edo"),
    ):
        connector_golden_path_evidence.validate(loaded[artifact_kind], connector_kind)
        if loaded[artifact_kind].get("connector_id") != connector_identity[connector_kind]["connector_id"]:
            fail(f"{connector_kind} evidence connector does not match the manifest")
        if loaded[artifact_kind].get("account_ref") != connector_identity[connector_kind]["account_ref"]:
            fail(f"{connector_kind} evidence account does not match the manifest")

    for kind, artifact_kind in (
        ("marketplace-remote", "marketplace-remote"),
        ("marketplace-live-smoke", "marketplace-live-smoke"),
        ("carrier", "carrier"),
        ("payment", "payment"),
        ("fiscal", "fiscal"),
        ("chestny-znak", "chestny-znak"),
        ("edo", "edo"),
    ):
        if artifact_by_kind[kind]["evidence_ref"] == "":
            fail(f"{artifact_kind} evidence_ref must not be empty")

    for kind, artifact_data in loaded.items():
        _check_release_identity(artifact_data, expected_commit, expected_repository)
        if artifact_data.get("schema_version") != 2:
            fail(f"{kind} evidence must use linked schema v2")
        same_flow(
            {field: data["flow"][field] for field in data["flow"] if field != "links"},
            artifact_data.get("flow"),
            f"{kind}.flow",
        )


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True, help="aggregate production golden path evidence JSON")
    parser.add_argument("--marketplace-evidence", required=True, help="retained full marketplace evidence JSON")
    parser.add_argument("--marketplace-live-smoke", required=True, help="retained linked marketplace live smoke JSON")
    parser.add_argument("--marketplace-compensation", required=True, help="retained linked marketplace compensation JSON")
    parser.add_argument("--carrier-evidence", required=True, help="retained carrier evidence JSON")
    parser.add_argument("--payment-evidence", required=True, help="retained payment evidence JSON")
    parser.add_argument("--fiscal-evidence", required=True, help="retained fiscal evidence JSON")
    parser.add_argument("--marking-evidence", required=True, help="retained Chestny ZNAK evidence JSON")
    parser.add_argument("--edo-evidence", required=True, help="retained EDO evidence JSON")
    parser.add_argument("--expected-release-commit", required=True)
    parser.add_argument("--expected-repository")
    args = parser.parse_args()
    paths = {
        "marketplace-remote": pathlib.Path(args.marketplace_evidence),
        "marketplace-live-smoke": pathlib.Path(args.marketplace_live_smoke),
        "marketplace-compensation": pathlib.Path(args.marketplace_compensation),
        "carrier": pathlib.Path(args.carrier_evidence),
        "payment": pathlib.Path(args.payment_evidence),
        "fiscal": pathlib.Path(args.fiscal_evidence),
        "chestny-znak": pathlib.Path(args.marking_evidence),
        "edo": pathlib.Path(args.edo_evidence),
    }
    try:
        validate_bundle(
            read_json(args.input),
            paths,
            args.expected_release_commit,
            args.expected_repository,
        )
    except (QualificationError, OSError, ValueError) as exc:
        parser.error(str(exc))
    print("Production golden path evidence: PASS")
    print("Linked credentialed marketplace, carrier, payment, fiscal, Chestny ZNAK and EDO artifacts are bound to this release.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
