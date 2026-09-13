import copy
import hashlib
import importlib.util
import json
import subprocess
import tempfile
import unittest
from pathlib import Path


SPEC = importlib.util.spec_from_file_location("classifier", Path(__file__).with_name("classify-trivy-secrets.py"))
classifier = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(classifier)
POLICY = Path(__file__).parents[1] / "supply-chain" / "trivy-secret-report-policy.jq"


class ClassifyTrivySecretsTest(unittest.TestCase):
    def retained_policy_accepts(self, report):
        with tempfile.NamedTemporaryFile("w", encoding="utf-8") as handle:
            json.dump(report, handle)
            handle.flush()
            return subprocess.run(
                ["jq", "-e", "-f", str(POLICY), handle.name],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                check=False,
            ).returncode == 0

    def test_exact_fixture_is_distinct_from_credential_candidate(self):
        synthetic = "synthetic-value-for-scanner-fixture"
        report = {
            "SchemaVersion": 2,
            "Results": [
                {"Target": "testdata/fixture.txt", "Secrets": [{"RuleID": "synthetic-rule", "Match": synthetic}]},
                {"Target": "config/runtime.env", "Secrets": [{"RuleID": "token-rule", "Match": "credential-candidate"}]},
            ],
        }
        policy = {
            "version": 1,
            "fixtures": [
                {
                    "id": "fixture-1",
                    "target": "testdata/fixture.txt",
                    "rule_id": "synthetic-rule",
                    "match_sha256": hashlib.sha256(synthetic.encode()).hexdigest(),
                }
            ],
        }
        result = classifier.classify(copy.deepcopy(report), policy)
        self.assertEqual(result["Results"][0]["Secrets"][0]["Classification"], "synthetic_fixture")
        self.assertEqual(result["Results"][1]["Secrets"][0]["Classification"], "credential_candidate")

    def test_stale_fixture_policy_fails_closed(self):
        policy = {"version": 1, "fixtures": [{"id": "missing", "target": "fixture", "rule_id": "rule", "match_sha256": "a" * 64}]}
        with self.assertRaises(ValueError):
            classifier.classify({"SchemaVersion": 2, "Results": []}, policy)

    def test_retained_report_policy_handles_root_object_and_fails_candidates(self):
        empty = {"SchemaVersion": 2, "Results": []}
        synthetic = {
            "SchemaVersion": 2,
            "Results": [{"Secrets": [{"Classification": "synthetic_fixture", "FixtureID": "fixture-1"}]}],
        }
        candidate = {
            "SchemaVersion": 2,
            "Results": [{"Secrets": [{"Classification": "credential_candidate"}]}],
        }
        leaked_match = {
            "SchemaVersion": 2,
            "Results": [{"Secrets": [{"Classification": "synthetic_fixture", "FixtureID": "fixture-1", "Match": "redact-me"}]}],
        }
        self.assertTrue(self.retained_policy_accepts(empty))
        self.assertTrue(self.retained_policy_accepts(synthetic))
        self.assertFalse(self.retained_policy_accepts(candidate))
        self.assertFalse(self.retained_policy_accepts(leaked_match))


if __name__ == "__main__":
    unittest.main()
