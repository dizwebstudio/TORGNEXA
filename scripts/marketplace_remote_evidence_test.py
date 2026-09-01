#!/usr/bin/env python3
import copy
import unittest

from marketplace_remote_evidence import QualificationError, validate


def sample() -> dict:
    checks = [
        {"id": check_id, "status": "PASS", "evidence_ref": check_id}
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
            }
        )
    ]
    return {
        "schema_version": 1,
        "status": "PASS",
        "environment": "non-production",
        "repository": "dizwebstudio/TORGNEXA",
        "release_commit": "0123456789abcdef0123456789abcdef01234567",
        "connector_id": "ozon",
        "api_version": "2026-01",
        "account_ref": "sandbox-account-01",
        "qualified_at": "2026-09-01T00:00:00Z",
        "taxonomy": {
            "version": "sandbox-2026-01",
            "fingerprint": "a" * 64,
            "mapping_ref": "mapping-01",
            "source_ref": "official-sandbox-schema",
        },
        "capabilities": {
            "products.read": {"status": "qualified", "evidence_ref": "products-read"},
            "products.write": {"status": "qualified", "evidence_ref": "products-write"},
        },
        "checks": checks,
        "rollback": {"verified": True, "evidence_ref": "rollback-01"},
    }


class MarketplaceRemoteEvidenceTest(unittest.TestCase):
    def test_listing_evidence_is_valid(self):
        validate(sample(), "listing")

    def test_secret_shaped_field_is_rejected(self):
        evidence = sample()
        evidence["token"] = "redacted"
        with self.assertRaises(QualificationError):
            validate(evidence, "listing")

    def test_full_scope_requires_operational_checks(self):
        with self.assertRaises(QualificationError):
            validate(sample(), "full")

    def test_failed_check_is_rejected(self):
        evidence = copy.deepcopy(sample())
        evidence["checks"][0]["status"] = "FAIL"
        with self.assertRaises(QualificationError):
            validate(evidence, "listing")


if __name__ == "__main__":
    unittest.main()
