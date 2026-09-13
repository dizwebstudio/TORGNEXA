import copy
import hashlib
import importlib.util
import unittest
from pathlib import Path


SPEC = importlib.util.spec_from_file_location("classifier", Path(__file__).with_name("classify-trivy-secrets.py"))
classifier = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(classifier)


class ClassifyTrivySecretsTest(unittest.TestCase):
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


if __name__ == "__main__":
    unittest.main()
