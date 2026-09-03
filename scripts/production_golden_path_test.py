#!/usr/bin/env python3
import copy
import hashlib
import json
import tempfile
import unittest
from pathlib import Path

from production_golden_path import (
    ARTIFACT_KINDS,
    REQUIRED_CHECKS,
    REQUIRED_FAILURES,
    validate_bundle,
    validate_document,
)


COMMIT = "0123456789abcdef0123456789abcdef01234567"


def flow() -> dict:
    document = {
        "flow_ref": "golden-path/flow-01",
        "order_ref": "order/flow-01",
        "reservation_ref": "reservation/flow-01",
        "shipment_ref": "shipment/flow-01",
        "return_ref": "return/flow-01",
        "refund_ref": "refund/flow-01",
        "settlement_ref": "settlement/flow-01",
        "marking_ref": "marking/flow-01",
        "edo_ref": "edo/flow-01",
    }
    return document


def connector_document(kind: str) -> dict:
    required = {
        "carrier": {"health", "rate", "label", "shipment_handoff", "return_inspection"},
        "payment": {"health", "refund", "settlement", "reconciliation"},
        "fiscal": {"health", "fiscalization", "refund_fiscalization", "close"},
        "marking": {"health", "product_read", "status_read", "reconciliation"},
        "edo": {"health", "document_send", "document_status", "reconciliation"},
    }[kind]
    connector_id = {"marking": "chestny-znak", "edo": "diadoc"}.get(kind, f"{kind}-sandbox")
    document = {
        "schema_version": 2,
        "status": "PASS",
        "scope": "connector",
        "environment": "non-production",
        "target": "dedicated-non-production",
        "repository": "dizwebstudio/TORGNEXA",
        "release_commit": COMMIT,
        "connector_id": connector_id,
        "account_ref": f"{kind}-account-01",
        "qualified_at": "2026-09-03T00:00:00Z",
        "flow": flow(),
        "checks": [
            {"id": check_id, "status": "PASS", "evidence_ref": f"{kind}/{check_id}"}
            for check_id in sorted(required)
        ],
        "rollback": {"verified": True, "evidence_ref": f"{kind}/rollback"},
    }
    if kind == "marking":
        document["observations"] = {
            "product_status": "matched",
            "marking_status": "observed",
            "reconciliation": "matched",
        }
    elif kind == "edo":
        document["observations"] = {
            "send_status": "accepted",
            "document_status": "delivered",
            "reconciliation": "matched",
        }
    return document


def marketplace_remote_document() -> dict:
    return {
        "schema_version": 2,
        "status": "PASS",
        "environment": "non-production",
        "repository": "dizwebstudio/TORGNEXA",
        "release_commit": COMMIT,
        "connector_id": "ozon",
        "account_ref": "marketplace-account-01",
        "qualified_at": "2026-09-03T00:00:00Z",
        "flow": flow(),
        "taxonomy": {
            "version": "sandbox-2026-01",
            "fingerprint": "a" * 64,
            "mapping_ref": "mapping-01",
            "source_ref": "official-sandbox-schema",
        },
        "capabilities": {
            "products.read": {"status": "qualified", "evidence_ref": "products-read"},
            "products.write": {"status": "qualified", "evidence_ref": "products-write"},
            "orders.read": {"status": "qualified", "evidence_ref": "orders-read"},
            "inventory.write": {"status": "qualified", "evidence_ref": "inventory-write"},
            "orders.status.write": {"status": "qualified", "evidence_ref": "orders-status-write"},
        },
        "checks": [
            {"id": check_id, "status": "PASS", "evidence_ref": f"marketplace/{check_id}"}
            for check_id in sorted(
                {
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
            )
        ],
        "rollback": {"verified": True, "evidence_ref": "marketplace/rollback"},
    }


def marketplace_live_document() -> dict:
    return {
        "schema_version": 2,
        "status": "PASS",
        "scope": "qualification",
        "environment": "non-production",
        "target": "dedicated-non-production",
        "repository": "dizwebstudio/TORGNEXA",
        "release_commit": COMMIT,
        "connector_id": "ozon",
        "account_ref": "marketplace-account-01",
        "qualified_at": "2026-09-03T00:00:00Z",
        "credential_mode": "env_only_secret_accessor",
        "flow": flow(),
        "taxonomy": {"status": "PASS", "fingerprint": "a" * 64, "source": "official"},
        "checks": [
            {"id": check_id, "status": "PASS", "evidence_ref": f"marketplace-live-smoke/{check_id}"}
            for check_id in sorted({
                "health", "products_read", "inventory_locations_read", "inventory_read",
                "orders_read", "taxonomy_read", "inventory_write", "read_after_write", "cleanup",
            })
        ],
        "write": {"attempted": True, "read_after_write": True, "restored": True},
    }


def marketplace_compensation_document() -> dict:
    checks = [
        {"id": check_id, "status": "PASS", "evidence_ref": f"marketplace-compensation/{check_id}"}
        for check_id in sorted({
            "health", "return_authorize", "return_receive", "compensation",
            "compensation_read_after_write", "reconciliation",
        })
    ]
    return {
        "schema_version": 2,
        "status": "PASS",
        "scope": "qualification",
        "environment": "non-production",
        "target": "dedicated-non-production",
        "repository": "dizwebstudio/TORGNEXA",
        "release_commit": COMMIT,
        "connector_id": "ozon",
        "account_ref": "marketplace-account-01",
        "qualified_at": "2026-09-03T00:00:00Z",
        "flow": flow(),
        "checks": checks,
        "observations": {
            "return_status": "received",
            "refund_status": "accepted",
            "compensation_status": "accepted",
            "settlement_status": "matched",
            "read_after_write": True,
            "idempotent_replay": True,
            "reconciliation": True,
        },
        "rollback": {"verified": True, "evidence_ref": "marketplace-compensation/rollback"},
    }


def aggregate() -> dict:
    artifacts = [
        {"kind": kind, "evidence_ref": f"evidence/{kind}", "sha256": "0" * 64}
        for kind in ARTIFACT_KINDS
    ]
    checks = [
        {"id": check_id, "owner": owner, "status": "PASS", "evidence_ref": f"path/{check_id}"}
        for check_id, owner in sorted(REQUIRED_CHECKS.items())
    ]
    failures = [
        {"id": scenario_id, "status": "PASS", "evidence_ref": f"failure/{scenario_id}"}
        for scenario_id in sorted(REQUIRED_FAILURES)
    ]
    return {
        "schema_version": 2,
        "status": "PASS",
        "scope": "credentialed-full",
        "environment": "non-production",
        "target": "dedicated-non-production",
        "repository": "dizwebstudio/TORGNEXA",
        "release_commit": COMMIT,
        "qualified_at": "2026-09-03T00:00:00Z",
        "flow": {**flow(), "links": [
            {"id": link_id, "from": endpoints[0], "to": endpoints[1], "evidence_ref": f"link/{link_id}"}
            for link_id, endpoints in {
                "order_to_reservation": ("order", "reservation"),
                "reservation_to_shipment": ("reservation", "shipment"),
                "shipment_to_return": ("shipment", "return"),
                "return_to_refund": ("return", "refund"),
                "refund_to_settlement": ("refund", "settlement"),
                "settlement_to_reconciliation": ("settlement", "reconciliation"),
                "order_to_marking": ("order", "marking"),
                "refund_to_fiscal": ("refund", "fiscal"),
                "settlement_to_edo": ("settlement", "edo"),
            }.items()
        ]},
        "connectors": {
            "marketplace": {
                "connector_id": "ozon",
                "account_ref": "marketplace-account-01",
                "evidence_ref": "evidence/marketplace-remote",
            },
            "carrier": {
                "connector_id": "carrier-sandbox",
                "account_ref": "carrier-account-01",
                "evidence_ref": "evidence/carrier",
            },
            "payment": {
                "connector_id": "payment-sandbox",
                "account_ref": "payment-account-01",
                "evidence_ref": "evidence/payment",
            },
                "fiscal": {
                    "connector_id": "fiscal-sandbox",
                    "account_ref": "fiscal-account-01",
                    "evidence_ref": "evidence/fiscal",
                },
                "marking": {
                    "connector_id": "chestny-znak",
                    "account_ref": "marking-account-01",
                    "evidence_ref": "evidence/chestny-znak",
                },
                "edo": {
                    "connector_id": "diadoc",
                    "account_ref": "edo-account-01",
                    "evidence_ref": "evidence/edo",
                },
        },
        "artifacts": artifacts,
        "checks": checks,
        "failure_matrix": failures,
        "rollback": {"verified": True, "evidence_ref": "evidence/rollback"},
    }


class ProductionGoldenPathTest(unittest.TestCase):
    def test_aggregate_requires_the_complete_check_sets(self):
        validate_document(aggregate())

    def test_missing_failure_scenario_is_rejected(self):
        document = aggregate()
        document["failure_matrix"] = document["failure_matrix"][:-1]
        with self.assertRaises(Exception):
            validate_document(document)

    def test_sensitive_field_is_rejected(self):
        document = aggregate()
        document["credentials"] = "must-not-be-retained"
        with self.assertRaises(Exception):
            validate_document(document)

    def test_flow_link_mismatch_is_rejected(self):
        document = aggregate()
        document["flow"]["links"][0]["to"] = "shipment"
        with self.assertRaises(Exception):
            validate_document(document)

    def test_bundle_binds_hashes_and_connector_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            documents = {
                "marketplace-remote": marketplace_remote_document(),
                "marketplace-live-smoke": marketplace_live_document(),
                "marketplace-compensation": marketplace_compensation_document(),
                "carrier": connector_document("carrier"),
                "payment": connector_document("payment"),
                "fiscal": connector_document("fiscal"),
                "chestny-znak": connector_document("marking"),
                "edo": connector_document("edo"),
            }
            document = aggregate()
            for kind, value in documents.items():
                path = root / f"{kind}.json"
                path.write_text(json.dumps(value), encoding="utf-8")
                digest = hashlib.sha256(path.read_bytes()).hexdigest()
                next(item for item in document["artifacts"] if item["kind"] == kind)["sha256"] = digest
            validate_bundle(
                document,
                {kind: root / f"{kind}.json" for kind in ARTIFACT_KINDS},
                COMMIT,
                "dizwebstudio/TORGNEXA",
            )

    def test_bundle_rejects_an_artifact_from_another_flow(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            documents = {
                "marketplace-remote": marketplace_remote_document(),
                "marketplace-live-smoke": marketplace_live_document(),
                "marketplace-compensation": marketplace_compensation_document(),
                "carrier": connector_document("carrier"),
                "payment": connector_document("payment"),
                "fiscal": connector_document("fiscal"),
                "chestny-znak": connector_document("marking"),
                "edo": connector_document("edo"),
            }
            documents["edo"]["flow"]["flow_ref"] = "golden-path/other-flow"
            document = aggregate()
            for kind, value in documents.items():
                path = root / f"{kind}.json"
                path.write_text(json.dumps(value), encoding="utf-8")
                digest = hashlib.sha256(path.read_bytes()).hexdigest()
                next(item for item in document["artifacts"] if item["kind"] == kind)["sha256"] = digest
            with self.assertRaises(Exception):
                validate_bundle(
                    document,
                    {kind: root / f"{kind}.json" for kind in ARTIFACT_KINDS},
                    COMMIT,
                    "dizwebstudio/TORGNEXA",
                )

    def test_bundle_rejects_sandbox_remote_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            documents = {
                "marketplace-remote": marketplace_remote_document(),
                "marketplace-live-smoke": marketplace_live_document(),
                "marketplace-compensation": marketplace_compensation_document(),
                "carrier": connector_document("carrier"),
                "payment": connector_document("payment"),
                "fiscal": connector_document("fiscal"),
                "chestny-znak": connector_document("marking"),
                "edo": connector_document("edo"),
            }
            documents["marketplace-remote"]["environment"] = "sandbox"
            document = aggregate()
            for kind, value in documents.items():
                path = root / f"{kind}.json"
                path.write_text(json.dumps(value), encoding="utf-8")
                digest = hashlib.sha256(path.read_bytes()).hexdigest()
                next(item for item in document["artifacts"] if item["kind"] == kind)["sha256"] = digest
            with self.assertRaises(Exception):
                validate_bundle(
                    document,
                    {kind: root / f"{kind}.json" for kind in ARTIFACT_KINDS},
                    COMMIT,
                    "dizwebstudio/TORGNEXA",
                )


if __name__ == "__main__":
    unittest.main()
