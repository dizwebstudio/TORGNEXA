import json
import tempfile
import unittest
from pathlib import Path

import regression_evidence


REVISION = "a" * 40


class RegressionEvidenceTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.tools = self.root / "tools.json"
        self.tools.write_text(
            json.dumps(
                {
                    "go_tools": [{"name": "gosec", "version": "v1"}, {"name": "govulncheck", "version": "v2"}],
                    "archive_tools": [{"name": "trivy", "version": "v3"}, {"name": "syft", "version": "v4"}],
                    "binary_tools": [],
                }
            ),
            encoding="utf-8",
        )

    def tearDown(self):
        self.temp.cleanup()

    def write_events(self, fail=False):
        events = self.root / "events.jsonl"
        rows = []
        for test in (
            "TestA01PostgresLastAdministratorConcurrencyAndReplay",
            "TestA03PostgresPaymentWebhookRollbackAndRedelivery",
            "TestA06PostgresMemberAuditRollbackAndReplay",
            "TestA07PostgresSignedWebhookTopicBindingBeforeInbox",
            "TestA08PostgresRefreshBoundsConcurrentAccountsAndReportsPoolSaturation",
        ):
            rows.append({"Action": "fail" if fail and test.startswith("TestA01") else "pass", "Test": test})
        metrics = (
            {"name": "oidc_hot_path", "requests": 32, "authorized": 32, "db_calls_total": 33, "db_calls_max": 2, "idp_calls_total": 2, "idp_calls_max": 2, "session_last_seen_writes": 1, "session_writes_throttled": 31, "p50_ns": 1, "p95_ns": 2, "p99_ns": 3},
            {"name": "oauth_refresh_pool", "operations": 10, "concurrency_limit": 8, "peak_in_flight": 8, "admission_waits": 2, "peak_saturation_ppm": 666666, "refresh_p50_ns": 4, "refresh_p95_ns": 5, "refresh_p99_ns": 6},
            {"name": "sse_broadcaster", "clients": 32, "active_tenants": 1, "audit_head_queries": 2, "deliveries_queued": 32, "peak_clients": 32, "client_limit": 64},
        )
        rows.extend({"Action": "output", "Output": "    fixture: " + regression_evidence.METRIC_MARKER + json.dumps(metric) + "\n"} for metric in metrics)
        events.write_text("".join(json.dumps(row) + "\n" for row in rows), encoding="utf-8")
        return events

    def postgres_args(self, events, output):
        return type("Args", (), {"events": str(events), "source_revision": REVISION, "tool_versions": str(self.tools), "output": str(output)})()

    def test_builds_redacted_postgres_and_complete_report(self):
        postgres = self.root / "postgres.json"
        self.assertTrue(regression_evidence.build_postgres(self.postgres_args(self.write_events(), postgres)))
        runtime = self.root / "runtime.json"
        runtime.write_text(json.dumps({"profile": "authenticated_api_and_sse", "passed": True, "http": {"requests": 100, "successes": 100, "availability": 1, "concurrency": 8, "throughput_ops_per_second": 200, "p50_ms": 1, "p95_ms": 2, "p99_ms": 3, "routes": [{"path": "/one"}, {"path": "/two"}]}, "sse": {"clients": 16, "successes": 16, "availability": 1, "peak_connected": 16, "p50_ms": 4, "p95_ms": 5, "p99_ms": 6}}), encoding="utf-8")
        for name in ("runtime-initial", "runtime-after-worker-restart", "runtime-after-postgres-restart"):
            (self.root / f"{name}.json").write_text('{"status":"PASS"}', encoding="utf-8")
        output = self.root / "complete.json"
        args = type("Args", (), {"postgres": str(postgres), "runtime": str(runtime), "qualification_dir": str(self.root), "source_revision": REVISION, "tool_versions": str(self.tools), "output": str(output)})()
        self.assertTrue(regression_evidence.build_complete(args))
        report = json.loads(output.read_text(encoding="utf-8"))
        self.assertEqual(report["status"], "PASS")
        self.assertEqual(report["call_counts"]["identity_provider_total"], 2)
        self.assertNotIn("synthetic.jwt.value", output.read_text(encoding="utf-8").lower())

    def test_failed_required_suite_fails_gate(self):
        output = self.root / "failed.json"
        self.assertFalse(regression_evidence.build_postgres(self.postgres_args(self.write_events(fail=True), output)))
        self.assertEqual(json.loads(output.read_text(encoding="utf-8"))["status"], "FAIL")


if __name__ == "__main__":
    unittest.main()
