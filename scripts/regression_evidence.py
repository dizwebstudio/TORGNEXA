#!/usr/bin/env python3
"""Build redacted Task 234.10 regression and load evidence."""

import argparse
import json
import os
import re
import sys
from pathlib import Path


REVISION = re.compile(r"^[0-9a-f]{40}$")
METRIC_MARKER = "TORGNEXA_REGRESSION_METRIC "
TASK_TESTS = {
    "234.1": ("TestA01Postgres",),
    "234.2": ("TestA03PostgresPaymentWebhook",),
    "234.3": ("TestA06Postgres", "TestConnectorAuditPostgres"),
    "234.4": ("TestA07Postgres",),
    "234.5": ("TestA08Postgres",),
}
FAILURE_WORDS = (
    "cancel",
    "collision",
    "concurrent",
    "conflict",
    "failure",
    "outage",
    "reauthorization",
    "redelivery",
    "replay",
    "rollback",
    "unavailable",
)
REQUIRED_METRICS = {"oidc_hot_path", "oauth_refresh_pool", "sse_broadcaster"}


def load_json(path):
    with Path(path).open("r", encoding="utf-8") as handle:
        return json.load(handle)


def scanner_versions(path):
    manifest = load_json(path)
    versions = {}
    for item in manifest.get("go_tools", []) + manifest.get("archive_tools", []) + manifest.get("binary_tools", []):
        name = item.get("name")
        version = item.get("version")
        if isinstance(name, str) and isinstance(version, str):
            versions[name] = version
    required = ("gosec", "govulncheck", "trivy", "syft")
    if any(name not in versions for name in required):
        raise ValueError("tool manifest is missing a required scanner version")
    return {name: versions[name] for name in required}


def write_json(path, value):
    destination = Path(path)
    if not destination.parent.is_dir():
        raise ValueError("output parent directory does not exist")
    temporary = destination.with_name(destination.name + ".tmp")
    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL
    descriptor = os.open(temporary, flags, 0o600)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            json.dump(value, handle, indent=2, sort_keys=True)
            handle.write("\n")
        os.replace(temporary, destination)
        os.chmod(destination, 0o600)
    finally:
        if temporary.exists():
            temporary.unlink()


def parse_go_test_events(path):
    outcomes = {}
    metrics = {}
    with Path(path).open("r", encoding="utf-8") as handle:
        for number, raw in enumerate(handle, 1):
            try:
                event = json.loads(raw)
            except json.JSONDecodeError as error:
                raise ValueError(f"malformed go test event at line {number}") from error
            test = event.get("Test")
            action = event.get("Action")
            if isinstance(test, str) and action in {"pass", "fail", "skip"}:
                outcomes[test] = action
            output = event.get("Output")
            if not isinstance(output, str) or METRIC_MARKER not in output:
                continue
            encoded = output.split(METRIC_MARKER, 1)[1].strip()
            try:
                metric = json.loads(encoded)
            except json.JSONDecodeError as error:
                raise ValueError(f"malformed regression metric at line {number}") from error
            name = metric.get("name")
            if not isinstance(name, str) or name in metrics:
                raise ValueError("regression metric names must be unique")
            metrics[name] = metric
    return outcomes, metrics


def task_summary(outcomes):
    summary = {}
    for task, prefixes in TASK_TESTS.items():
        selected = {
            name: status
            for name, status in outcomes.items()
            if "/" not in name and any(name.startswith(prefix) for prefix in prefixes)
        }
        passed = sorted(name for name, status in selected.items() if status == "pass")
        failed = sorted(name for name, status in selected.items() if status != "pass")
        summary[task] = {"passed": passed, "failed_or_skipped": failed}
    return summary


def failure_results(outcomes):
    results = []
    for name, status in sorted(outcomes.items()):
        lowered = name.lower()
        if status == "pass" and any(word in lowered for word in FAILURE_WORDS):
            results.append({"scenario": name, "status": "passed"})
    return results


def build_postgres(args):
    if not REVISION.fullmatch(args.source_revision):
        raise ValueError("source revision must be 40 lowercase hexadecimal characters")
    outcomes, metrics = parse_go_test_events(args.events)
    tasks = task_summary(outcomes)
    missing_tasks = [task for task, result in tasks.items() if not result["passed"] or result["failed_or_skipped"]]
    missing_metrics = sorted(REQUIRED_METRICS.difference(metrics))
    failed_tests = sorted(name for name, status in outcomes.items() if status == "fail")
    result = {
        "schema_version": 1,
        "report": "task_234_postgresql_regression",
        "redacted": True,
        "source_revision": args.source_revision,
        "scanner_versions": scanner_versions(args.tool_versions),
        "status": "PASS" if not missing_tasks and not missing_metrics and not failed_tests else "FAIL",
        "tasks": tasks,
        "metrics": {name: metrics[name] for name in sorted(metrics) if name in REQUIRED_METRICS},
        "injected_failure_results": failure_results(outcomes),
        "diagnostics": {
            "failed_tests": failed_tests,
            "missing_metrics": missing_metrics,
            "missing_task_suites": missing_tasks,
        },
        "redaction": {
            "contains_credentials": False,
            "contains_identity_or_tenant_labels": False,
            "contains_raw_test_output": False,
        },
    }
    write_json(args.output, result)
    return result["status"] == "PASS"


def millis(nanoseconds):
    if not isinstance(nanoseconds, int) or nanoseconds < 0:
        raise ValueError("metric duration must be a non-negative integer")
    return round(nanoseconds / 1_000_000, 3)


def build_complete(args):
    if not REVISION.fullmatch(args.source_revision):
        raise ValueError("source revision must be 40 lowercase hexadecimal characters")
    postgres = load_json(args.postgres)
    runtime = load_json(args.runtime)
    expected_scanners = scanner_versions(args.tool_versions)
    if postgres.get("status") != "PASS" or postgres.get("source_revision") != args.source_revision:
        raise ValueError("PostgreSQL regression evidence did not pass for this revision")
    if postgres.get("scanner_versions") != expected_scanners:
        raise ValueError("PostgreSQL evidence scanner versions do not match the manifest")
    if runtime.get("passed") is not True or runtime.get("profile") != "authenticated_api_and_sse":
        raise ValueError("authenticated runtime load evidence did not pass")
    metrics = postgres.get("metrics", {})
    if set(metrics) != REQUIRED_METRICS:
        raise ValueError("PostgreSQL regression metrics are incomplete")
    oidc = metrics["oidc_hot_path"]
    oauth = metrics["oauth_refresh_pool"]
    sse = metrics["sse_broadcaster"]
    runtime_drills = []
    qualification = Path(args.qualification_dir)
    for path in sorted(qualification.glob("runtime-*.json")):
        item = load_json(path)
        if item.get("status") != "PASS":
            raise ValueError(f"runtime failure drill did not pass: {path.name}")
        runtime_drills.append({"scenario": path.stem, "status": "passed"})
    if len(runtime_drills) < 3:
        raise ValueError("runtime qualification is missing injected restart/outage drills")
    http_load = runtime.get("http", {})
    runtime_sse = runtime.get("sse", {})
    report = {
        "schema_version": 1,
        "report": "task_234_security_performance_regression",
        "redacted": True,
        "source_revision": args.source_revision,
        "status": "PASS",
        "scanner_versions": expected_scanners,
        "latency_ms": {
            "authenticated_request_mix": {
                "p50": http_load.get("p50_ms"),
                "p95": http_load.get("p95_ms"),
                "p99": http_load.get("p99_ms"),
            },
            "authenticated_security_path": {
                "p50": millis(oidc["p50_ns"]),
                "p95": millis(oidc["p95_ns"]),
                "p99": millis(oidc["p99_ns"]),
            },
            "oauth_refresh": {
                "p50": millis(oauth["refresh_p50_ns"]),
                "p95": millis(oauth["refresh_p95_ns"]),
                "p99": millis(oauth["refresh_p99_ns"]),
            },
            "sse_connect": {
                "p50": runtime_sse.get("p50_ms"),
                "p95": runtime_sse.get("p95_ms"),
                "p99": runtime_sse.get("p99_ms"),
            },
        },
        "call_counts": {
            "authenticated_requests": oidc["requests"],
            "database_total": oidc["db_calls_total"],
            "database_max_per_request": oidc["db_calls_max"],
            "identity_provider_total": oidc["idp_calls_total"],
            "identity_provider_max_per_request": oidc["idp_calls_max"],
            "session_last_seen_writes": oidc["session_last_seen_writes"],
            "session_writes_throttled": oidc["session_writes_throttled"],
        },
        "pool_saturation": {
            "oauth_refresh_concurrency_limit": oauth["concurrency_limit"],
            "oauth_refresh_peak_in_flight": oauth["peak_in_flight"],
            "oauth_refresh_admission_waits": oauth["admission_waits"],
            "postgres_peak_saturation_ppm": oauth["peak_saturation_ppm"],
        },
        "load_profiles": {
            "authenticated_http": {
                "requests": http_load.get("requests"),
                "successes": http_load.get("successes"),
                "availability": http_load.get("availability"),
                "concurrency": http_load.get("concurrency"),
                "throughput_ops_per_second": http_load.get("throughput_ops_per_second"),
                "route_count": len(http_load.get("routes", [])),
            },
            "sse": {
                "clients": runtime_sse.get("clients"),
                "successes": runtime_sse.get("successes"),
                "availability": runtime_sse.get("availability"),
                "peak_connected": runtime_sse.get("peak_connected"),
                "integration_clients": sse["clients"],
                "integration_audit_head_queries": sse["audit_head_queries"],
                "integration_client_limit": sse["client_limit"],
            },
        },
        "postgres_regression": postgres["tasks"],
        "injected_failure_results": postgres["injected_failure_results"] + runtime_drills,
        "redaction": {
            "contains_access_tokens": False,
            "contains_credentials": False,
            "contains_identity_or_tenant_labels": False,
            "contains_raw_responses_or_test_output": False,
        },
    }
    if any(report["latency_ms"][profile][percentile] is None for profile in report["latency_ms"] for percentile in ("p50", "p95", "p99")):
        raise ValueError("latency evidence is incomplete")
    write_json(args.output, report)
    return True


def parser():
    value = argparse.ArgumentParser()
    commands = value.add_subparsers(dest="command", required=True)
    postgres = commands.add_parser("postgres")
    postgres.add_argument("--events", required=True)
    postgres.add_argument("--source-revision", required=True)
    postgres.add_argument("--tool-versions", required=True)
    postgres.add_argument("--output", required=True)
    complete = commands.add_parser("complete")
    complete.add_argument("--postgres", required=True)
    complete.add_argument("--runtime", required=True)
    complete.add_argument("--qualification-dir", required=True)
    complete.add_argument("--source-revision", required=True)
    complete.add_argument("--tool-versions", required=True)
    complete.add_argument("--output", required=True)
    return value


def main():
    args = parser().parse_args()
    try:
        passed = build_postgres(args) if args.command == "postgres" else build_complete(args)
    except (KeyError, OSError, TypeError, ValueError, json.JSONDecodeError) as error:
        print(f"regression-evidence: {error}", file=sys.stderr)
        raise SystemExit(1)
    raise SystemExit(0 if passed else 1)


if __name__ == "__main__":
    main()
