import unittest

from marketplace_live_smoke_evidence import validate_document


class MarketplaceLiveSmokeEvidenceTest(unittest.TestCase):
    def test_pass_document_is_accepted(self):
        validate_document(
            {
                "schema_version": 1,
                "status": "PASS",
                "scope": "qualification",
                "environment": "non-production",
                "target": "dedicated-non-production",
                "repository": "owner/repository",
                "release_commit": "0123456789abcdef0123456789abcdef01234567",
                "connector_id": "ozon",
                "account_ref": "sandbox-account",
                "qualified_at": "2026-09-03T00:00:00Z",
                "credential_mode": "env_only_secret_accessor",
                "taxonomy": {
                    "status": "PASS",
                    "fingerprint": "a" * 64,
                    "source": "official-adapter-taxonomy",
                },
                "checks": [
                    {"id": "health", "status": "PASS", "evidence_ref": "marketplace-live-smoke/health"},
                    {"id": "cleanup", "status": "PASS", "evidence_ref": "marketplace-live-smoke/cleanup"},
                ],
                "write": {"attempted": True, "read_after_write": True, "restored": True},
            }
        )

    def test_sensitive_field_is_rejected(self):
        document = {
            "schema_version": 1,
            "status": "FAIL",
            "scope": "read",
            "environment": "non-production",
            "target": "dedicated-non-production",
            "repository": "owner/repository",
            "release_commit": "0123456789abcdef0123456789abcdef01234567",
            "connector_id": "wildberries",
            "account_ref": "sandbox-account",
            "qualified_at": "2026-09-03T00:00:00Z",
            "credential_mode": "env_only_secret_accessor",
            "taxonomy": {"status": "NOT_RUN", "fingerprint": "", "source": ""},
            "checks": [],
            "write": {"attempted": False, "read_after_write": False, "restored": True},
            "failure": {"check_id": "configuration", "error_code": "credential_missing"},
            "api_key": "should-not-be-retained",
        }
        with self.assertRaises(ValueError):
            validate_document(document)


if __name__ == "__main__":
    unittest.main()
