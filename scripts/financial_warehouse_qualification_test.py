#!/usr/bin/env python3
import copy
import hashlib
import json
import tempfile
import unittest
from pathlib import Path

from financial_warehouse_qualification import (
    ARTIFACT_KINDS,
    REQUIRED_CHECKS,
    REQUIRED_FAILURES,
    SUBJECT_KINDS,
    validate_bundle,
    validate_document,
)
from external_qualification_evidence import REQUIRED_CHECKS as EXTERNAL_CHECKS
from external_qualification_evidence import REQUIRED_SCOPES
from p4_common import QualificationError


COMMIT = "0123456789abcdef0123456789abcdef01234567"
REPOSITORY = "dizwebstudio/TORGNEXA"


def subject_id(kind: str) -> str:
    return f"{kind.replace('_', '-')}-sandbox"


def account_ref(kind: str) -> str:
    return f"{kind.replace('_', '-')}-account-01"


def external_document(kind: str) -> dict:
    return {
        "schema_version": 1,
        "status": "PASS",
        "scope": "hardware" if kind == "hardware" else ("warehouse" if kind in {"fbs", "fbo"} else "financial"),
        "subject_kind": "hardware" if kind == "hardware" else "connector",
        "environment": "non-production",
        "target": "dedicated-non-production",
        "repository": REPOSITORY,
        "release_commit": COMMIT,
        "subject_id": subject_id(kind),
        "account_ref": account_ref(kind),
        "qualified_at": "2026-09-03T00:00:00Z",
        "version": "sandbox-2026-01",
        "scopes": sorted(REQUIRED_SCOPES[kind]),
        "checks": [
            {"id": check_id, "status": "PASS", "evidence_ref": f"{kind}/{check_id}"}
            for check_id in sorted(EXTERNAL_CHECKS[kind])
        ],
        "rollback": {"verified": True, "evidence_ref": f"{kind}/rollback"},
    }


def aggregate() -> dict:
    return {
        "schema_version": 1,
        "status": "PASS",
        "scope": "credentialed-financial-warehouse",
        "environment": "non-production",
        "target": "dedicated-non-production",
        "repository": REPOSITORY,
        "release_commit": COMMIT,
        "qualified_at": "2026-09-03T00:00:00Z",
        "subjects": {
            kind: {
                "subject_id": subject_id(kind),
                "account_ref": account_ref(kind),
                "evidence_ref": f"evidence/{kind}",
            }
            for kind in SUBJECT_KINDS
        },
        "artifacts": [
            {"kind": kind, "evidence_ref": f"evidence/{kind}", "sha256": "0" * 64}
            for kind in ARTIFACT_KINDS
        ],
        "checks": [
            {"id": check_id, "owner": owner, "status": "PASS", "evidence_ref": f"path/{check_id}"}
            for check_id, owner in sorted(REQUIRED_CHECKS.items())
        ],
        "failure_matrix": [
            {"id": scenario_id, "status": "PASS", "evidence_ref": f"failure/{scenario_id}"}
            for scenario_id in sorted(REQUIRED_FAILURES)
        ],
        "rollback": {"verified": True, "evidence_ref": "evidence/rollback"},
    }


class FinancialWarehouseQualificationTest(unittest.TestCase):
    def test_aggregate_requires_all_subjects_and_checks(self):
        validate_document(aggregate())

    def test_missing_financial_check_is_rejected(self):
        document = aggregate()
        document["checks"] = [item for item in document["checks"] if item["id"] != "fx_conversion_snapshot"]
        with self.assertRaises(QualificationError):
            validate_document(document)

    def test_secret_shaped_field_is_rejected(self):
        document = aggregate()
        document["api_key"] = "must-not-be-retained"
        with self.assertRaises(QualificationError):
            validate_document(document)

    def test_bundle_binds_hashes_and_subject_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            document = aggregate()
            paths = {}
            for kind in ARTIFACT_KINDS:
                path = root / f"{kind}.json"
                path.write_text(json.dumps(external_document(kind), sort_keys=True), encoding="utf-8")
                digest = hashlib.sha256(path.read_bytes()).hexdigest()
                next(item for item in document["artifacts"] if item["kind"] == kind)["sha256"] = digest
                paths[kind] = path
            validate_bundle(document, paths, COMMIT, REPOSITORY)

    def test_bundle_rejects_tampered_artifact(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            document = aggregate()
            paths = {}
            for kind in ARTIFACT_KINDS:
                path = root / f"{kind}.json"
                path.write_text(json.dumps(external_document(kind), sort_keys=True), encoding="utf-8")
                next(item for item in document["artifacts"] if item["kind"] == kind)["sha256"] = hashlib.sha256(path.read_bytes()).hexdigest()
                paths[kind] = path
            tampered = copy.deepcopy(external_document("fx"))
            tampered["checks"] = tampered["checks"][:-1]
            paths["fx"].write_text(json.dumps(tampered, sort_keys=True), encoding="utf-8")
            with self.assertRaises(QualificationError):
                validate_bundle(document, paths, COMMIT, REPOSITORY)


if __name__ == "__main__":
    unittest.main()
