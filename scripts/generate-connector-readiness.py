#!/usr/bin/env python3
"""Build the non-secret connector readiness matrix from repository sources.

The matrix is deliberately derived from manifests, the executable runtime
support catalog and redacted conformance metadata. It never copies auth
secrets, connection-test headers or remote payloads.
"""

from __future__ import annotations

import json
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
MANIFEST_ROOT = ROOT / "connectors"
SUPPORT_PATH = ROOT / "contracts/connectors/builtin-runtime-support-v1.json"
MATRIX_PATH = ROOT / "contracts/connectors/readiness-matrix-v1.json"
GO_PATH = ROOT / "internal/platform/connectors/readiness_matrix_generated.go"
DOC_PATH = ROOT / "docs/connectors/readiness-matrix.md"


def writable(capability: str) -> bool:
    return capability.endswith((
        ".write", ".reply", ".send", ".refund", ".create", ".cancel",
        ".submit", ".archive", ".unarchive", ".restore", ".edit", ".delete",
        ".manage", ".introduce", ".withdraw", ".reserve", ".request",
    ))


def owner_for(family: str) -> tuple[str, str]:
    if family in {"marketplace", "storefront", "erp"}:
        return "commerce-integrations", "P0"
    if family in {"logistics", "pickup"}:
        return "logistics", "P1"
    if family in {"payment", "fx"}:
        return "finance", "P1"
    if family in {"government", "edo"}:
        return "compliance", "P1"
    if family in {"social", "classified", "crm"}:
        return "channel-operations", "P2"
    if family == "ai":
        return "ai-platform", "P3"
    return "platform", "P2"


def decision_for(status: str, family: str) -> tuple[str, str]:
    if status == "health_only":
        if family in {"marketplace", "storefront", "classified"}:
            return "deepen", "Выбрать минимальный business read-срез и провести qualification wave"
        return "keep_health_only", "Оставить health-only и документировать специализированное назначение"
    if status == "manifest_only":
        return "specialized_surface", "Принять решение по runtime-поверхности или вывести из каталога"
    if status == "ready":
        return "qualify", "Подтвердить каждую заявленную capability credentialed sandbox/live evidence"
    return "deepen", "Закрыть недостающие capability tests и read-after-write evidence"


def classify(manifest: dict, support: dict) -> str:
    if support.get("health_only"):
        return "health_only"
    if support.get("stage") == "ready":
        return "ready"
    if support.get("operational_capabilities"):
        operations = support["operational_capabilities"]
        return "partially_supported" if any(writable(value) for value in operations) else "read_only"
    return "manifest_only"


def conformance_timestamp(connector_id: str) -> str:
    report = ROOT / "docs/connectors" / connector_id / "conformance-report.json"
    if not report.exists():
        return ""
    try:
        payload = json.loads(report.read_text())
    except json.JSONDecodeError:
        return ""
    return str(payload.get("completed_at", ""))


def build() -> list[dict]:
    support_payload = json.loads(SUPPORT_PATH.read_text())
    support_by_id = {item["connector_id"]: item for item in support_payload["connectors"]}
    manifests = []
    for path in sorted(MANIFEST_ROOT.glob("**/manifest.json")):
        manifests.append((path, json.loads(path.read_text())))
    if len(manifests) != 61:
        raise SystemExit(f"expected 61 manifests, found {len(manifests)}")

    profiles = []
    for manifest_path, manifest in manifests:
        connector_id = manifest["id"]
        support = support_by_id.get(connector_id)
        if support is None:
            raise SystemExit(f"runtime support missing for {connector_id}")
        status = classify(manifest, support)
        owner, priority = owner_for(manifest["family"])
        decision, next_action = decision_for(status, manifest["family"])
        operations = set(support.get("operational_capabilities", []))
        evidence_at = conformance_timestamp(connector_id)
        live_status = ROOT / "docs/connectors" / connector_id / "live-qualification-status.json"
        live = "not_recorded"
        if live_status.exists():
            try:
                live = str(json.loads(live_status.read_text()).get("status", "not_recorded"))
            except json.JSONDecodeError:
                live = "invalid"
        capabilities = []
        for capability in sorted(manifest["capabilities"]):
            is_write = writable(capability)
            if capability not in operations:
                capability_status = "health_only" if status == "health_only" else "not_available"
            elif status == "ready":
                capability_status = "ready"
            elif is_write:
                capability_status = "partially_supported"
            else:
                capability_status = "read_only"
            scopes = sorted({
                scope
                for auth in manifest.get("auth", [])
                for scope in auth.get("oauth2", {}).get("scopes", [])
            })
            capabilities.append({
                "name": capability,
                "status": capability_status,
                "direction": "write" if is_write else "read",
                "required_scopes": scopes,
                "risk_class": "write_sensitive" if is_write else "read",
                "idempotency": "required" if is_write else "not_applicable",
                "read_after_write": "required" if is_write else "not_applicable",
                "webhook_or_reconciliation": any(token in capability for token in ("webhook", "status", "return", "reconcile")),
                "runtime_evidence": str(SUPPORT_PATH.relative_to(ROOT)) if capability in operations else "not_available",
            })
        blockers = []
        if status in {"ready", "read_only", "partially_supported"}:
            blockers.append("credentialed_runtime_evidence_required")
        if status == "manifest_only":
            blockers.append("no_runtime_operation_surface")
        profiles.append({
            "connector_id": connector_id,
            "display_name": manifest["name"],
            "family": manifest["family"],
            "surface": support.get("surface", "unknown"),
            "status": status,
            "owner": owner,
            "priority": priority,
            "decision": decision,
            "next_action": next_action,
            "official_docs_ref": f"docs/connectors/{connector_id}/spec.md",
            "official_docs_status": "repository_reference_pending_provider_evidence",
            "sandbox_status": "pass" if live == "pass" else "not_recorded",
            "live_qualification_status": live,
            "last_verified_at": evidence_at,
            "conformance_ref": f"docs/connectors/{connector_id}/conformance-report.json",
            "runtime_ref": str(SUPPORT_PATH.relative_to(ROOT)),
            "health_only": status == "health_only",
            "capabilities": capabilities,
            "blockers": blockers,
            "rate_limit": {
                "max_concurrency": manifest["rate_limit"]["max_concurrency"],
                "min_interval_ms": manifest["rate_limit"]["min_interval_ms"],
                "request_timeout_ms": manifest["rate_limit"]["request_timeout_ms"],
                "retry_max_attempts": manifest["rate_limit"]["retry"]["max_attempts"],
            },
        })
    return sorted(profiles, key=lambda profile: profile["connector_id"])


def write_outputs(profiles: list[dict]) -> None:
    payload = {"schema_version": 1, "source": "manifest+runtime-support+redacted-conformance", "profiles": profiles}
    encoded = json.dumps(payload, ensure_ascii=False, indent=2) + "\n"
    MATRIX_PATH.write_text(encoded)
    GO_PATH.write_text("// Code generated by scripts/generate-connector-readiness.py; DO NOT EDIT.\n\npackage connectors\n\nconst generatedReadinessMatrixJSON = `" + encoded.replace("`", "\\u0060") + "`\n")

    counts: dict[str, int] = {}
    for profile in profiles:
        counts[profile["status"]] = counts.get(profile["status"], 0) + 1
    lines = [
        "# Connector readiness matrix",
        "",
        "Generated from all repository manifests, runtime support and redacted conformance reports.",
        "The matrix is descriptive evidence; credentials and provider payloads are never copied here.",
        "",
        "## Summary",
        "",
        "| Status | Count |",
        "|---|---:|",
    ]
    lines.extend(f"| `{status}` | {counts[status]} |" for status in sorted(counts))
    lines.extend([
        "",
        "## All connectors",
        "",
        "| Connector | Family | Status | Owner | Priority | Decision | Next action |",
        "|---|---|---|---|---|---|---|",
    ])
    for profile in profiles:
        action = profile["next_action"].replace("|", "\\|")
        lines.append(f"| `{profile['connector_id']}` | {profile['family']} | `{profile['status']}` | {profile['owner']} | {profile['priority']} | `{profile['decision']}` | {action} |")
    lines.extend([
        "",
        "`ready` means repository runtime support is present, not that a provider write is production-qualified.",
        "`qualified` is intentionally absent until retained credentialed evidence exists for the exact capability.",
    ])
    DOC_PATH.write_text("\n".join(lines) + "\n")


if __name__ == "__main__":
    matrix = build()
    write_outputs(matrix)
    print(f"Generated connector readiness matrix: {len(matrix)} profiles")
