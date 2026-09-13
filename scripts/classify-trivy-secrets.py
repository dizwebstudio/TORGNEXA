#!/usr/bin/env python3
"""Classify exact synthetic Trivy fixtures before sensitive fields are redacted."""

import argparse
import hashlib
import json
import os
import sys


def classify(report, policy):
    fixtures = policy.get("fixtures")
    if policy.get("version") != 1 or not isinstance(fixtures, list):
        raise ValueError("invalid synthetic secret fixture policy")
    indexed = {}
    for fixture in fixtures:
        if not isinstance(fixture, dict) or set(fixture) != {"id", "target", "rule_id", "match_sha256"}:
            raise ValueError("invalid synthetic secret fixture entry")
        key = (fixture["target"], fixture["rule_id"], fixture["match_sha256"])
        if (
            not isinstance(fixture["id"], str)
            or not fixture["id"]
            or not isinstance(fixture["target"], str)
            or not fixture["target"]
            or not isinstance(fixture["rule_id"], str)
            or not fixture["rule_id"]
            or not isinstance(fixture["match_sha256"], str)
            or len(fixture["match_sha256"]) != 64
            or any(character not in "0123456789abcdef" for character in fixture["match_sha256"])
            or key in indexed
        ):
            raise ValueError("synthetic secret fixtures must have unique exact fingerprints")
        indexed[key] = fixture["id"]
    if report.get("SchemaVersion") != 2 or not isinstance(report.get("Results"), list):
        raise ValueError("invalid Trivy secret report")
    observed = set()
    for result in report["Results"]:
        if not isinstance(result, dict):
            raise ValueError("invalid Trivy result")
        target = result.get("Target", "")
        secrets = result.get("Secrets", [])
        if secrets is None:
            secrets = []
        if not isinstance(secrets, list):
            raise ValueError("invalid Trivy secret collection")
        for secret in secrets:
            if not isinstance(secret, dict):
                raise ValueError("invalid Trivy secret finding")
            match = secret.get("Match")
            rule_id = secret.get("RuleID", "")
            digest = hashlib.sha256(match.encode("utf-8")).hexdigest() if isinstance(match, str) else ""
            fixture_id = indexed.get((target, rule_id, digest))
            if fixture_id:
                if fixture_id in observed:
                    raise ValueError("synthetic secret fixture matched more than once")
                observed.add(fixture_id)
                secret["Classification"] = "synthetic_fixture"
                secret["FixtureID"] = fixture_id
            else:
                secret["Classification"] = "credential_candidate"
    missing = sorted(set(indexed.values()).difference(observed))
    if missing:
        raise ValueError("registered synthetic secret fixture was not observed")
    return report


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", required=True)
    parser.add_argument("--policy", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    try:
        with open(args.input, "r", encoding="utf-8") as handle:
            report = json.load(handle)
        with open(args.policy, "r", encoding="utf-8") as handle:
            policy = json.load(handle)
        descriptor = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            json.dump(classify(report, policy), handle, sort_keys=True)
            handle.write("\n")
    except (OSError, ValueError, json.JSONDecodeError) as error:
        print(f"classify-trivy-secrets: {error}", file=sys.stderr)
        raise SystemExit(1)


if __name__ == "__main__":
    main()
