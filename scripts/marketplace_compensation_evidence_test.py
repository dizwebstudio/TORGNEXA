import copy
import unittest

from marketplace_compensation_evidence import QualificationError, validate


def sample() -> dict:
    return {
        "schema_version": 2,
        "status": "PASS",
        "scope": "qualification",
        "environment": "non-production",
        "target": "dedicated-non-production",
        "repository": "owner/repository",
        "release_commit": "0123456789abcdef0123456789abcdef01234567",
        "connector_id": "ozon",
        "account_ref": "marketplace-account",
        "qualified_at": "2026-09-03T00:00:00Z",
        "flow": {
            "flow_ref": "golden-path/flow-01",
            "order_ref": "order/flow-01",
            "reservation_ref": "reservation/flow-01",
            "shipment_ref": "shipment/flow-01",
            "return_ref": "return/flow-01",
            "refund_ref": "refund/flow-01",
            "settlement_ref": "settlement/flow-01",
            "marking_ref": "marking/flow-01",
            "edo_ref": "edo/flow-01",
        },
        "checks": [
            {"id": check_id, "status": "PASS", "evidence_ref": f"marketplace-compensation/{check_id}"}
            for check_id in sorted({
                "health", "return_authorize", "return_receive", "compensation",
                "compensation_read_after_write", "reconciliation",
            })
        ],
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


class MarketplaceCompensationEvidenceTest(unittest.TestCase):
    def test_complete_observation_is_accepted(self):
        validate(sample())

    def test_unmatched_settlement_is_rejected(self):
        document = copy.deepcopy(sample())
        document["observations"]["settlement_status"] = "pending"
        with self.assertRaises(QualificationError):
            validate(document)

    def test_remote_identifier_field_is_rejected(self):
        document = copy.deepcopy(sample())
        document["remote_id"] = "must-not-be-retained"
        with self.assertRaises(QualificationError):
            validate(document)


if __name__ == "__main__":
    unittest.main()
