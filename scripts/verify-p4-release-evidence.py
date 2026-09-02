#!/usr/bin/env python3
"""Verify the public P4 decision before a production deployment."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import sys
import urllib.error
import urllib.request
from typing import Any

from p4_common import (
    QualificationError,
    atomic_write_json,
    now_utc,
    reject_secret_shaped_fields,
    require_repository,
    require_semver,
    require_sha40,
)
from p4_root_evidence import REQUIRED as REQUIRED_EVIDENCE

API_VERSION = "2026-03-10"
MAX_ASSET_BYTES = 2 * 1024 * 1024
SHA256 = set("0123456789abcdef")


def _require_string(value: Any, field: str) -> str:
    if not isinstance(value, str) or not value:
        raise QualificationError(f"GitHub release evidence field is invalid: {field}")
    return value


def _request(url: str, token: str, accept: str, limit: int) -> bytes:
    request = urllib.request.Request(
        url,
        headers={
            "Accept": accept,
            "Authorization": f"Bearer {token}",
            "User-Agent": "torgnexa-production-release-gate/1",
            "X-GitHub-Api-Version": API_VERSION,
        },
        method="GET",
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            body = response.read(limit + 1)
    except urllib.error.HTTPError as exc:
        raise QualificationError(f"GitHub release evidence request failed: HTTP {exc.code}") from exc
    except (urllib.error.URLError, TimeoutError, OSError) as exc:
        raise QualificationError("GitHub release evidence request failed") from exc
    if len(body) > limit:
        raise QualificationError("GitHub release evidence asset exceeds the safety limit")
    return body


def _get_json(url: str, token: str) -> Any:
    try:
        return json.loads(_request(url, token, "application/vnd.github+json", MAX_ASSET_BYTES).decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise QualificationError("GitHub release evidence response is not valid JSON") from exc


def _find_asset(release: dict[str, Any]) -> dict[str, Any]:
    assets = release.get("assets")
    if not isinstance(assets, list):
        raise QualificationError("GitHub release assets are missing")
    matches = [asset for asset in assets if isinstance(asset, dict) and asset.get("name") == "p4-go-live.json"]
    if len(matches) != 1:
        raise QualificationError("public GitHub release must contain exactly one p4-go-live.json asset")
    asset = matches[0]
    if asset.get("state") != "uploaded":
        raise QualificationError("p4-go-live.json is not uploaded")
    asset_id = asset.get("id")
    if isinstance(asset_id, bool) or not isinstance(asset_id, int) or asset_id <= 0:
        raise QualificationError("p4-go-live.json asset id is invalid")
    digest = asset.get("digest")
    if not isinstance(digest, str) or not digest.startswith("sha256:"):
        raise QualificationError("p4-go-live.json is missing its GitHub SHA-256 digest")
    digest_value = digest.removeprefix("sha256:")
    if len(digest_value) != 64 or any(char not in SHA256 for char in digest_value):
        raise QualificationError("p4-go-live.json GitHub SHA-256 digest is invalid")
    size = asset.get("size")
    if isinstance(size, bool) or not isinstance(size, int) or size < 0:
        raise QualificationError("p4-go-live.json asset size is invalid")
    return {"id": asset_id, "digest": digest_value, "size": size}


def _validate_root_report(payload: bytes, repository: str, version: str, commit: str) -> None:
    try:
        value = json.loads(payload.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise QualificationError("p4-go-live.json is not valid JSON") from exc
    reject_secret_shaped_fields(value)
    if not isinstance(value, dict):
        raise QualificationError("p4-go-live.json must be a JSON object")
    if value.get("schema_version") != 1 or value.get("status") != "PASS":
        raise QualificationError("p4-go-live.json is not PASS schema v1")
    if value.get("release_version") != version:
        raise QualificationError("p4-go-live.json release version mismatch")
    if value.get("release_commit") != commit:
        raise QualificationError("p4-go-live.json release commit mismatch")
    if value.get("repository") != repository:
        raise QualificationError("p4-go-live.json repository mismatch")
    _require_string(value.get("qualified_at"), "qualified_at")
    evidence = value.get("evidence")
    if not isinstance(evidence, list) or len(evidence) != len(REQUIRED_EVIDENCE):
        raise QualificationError("p4-go-live.json evidence list is incomplete")
    paths: set[str] = set()
    for row in evidence:
        if not isinstance(row, dict) or set(row) != {"path", "sha256"}:
            raise QualificationError("p4-go-live.json evidence row is invalid")
        path = row.get("path")
        digest = row.get("sha256")
        if not isinstance(path, str) or path in paths:
            raise QualificationError("p4-go-live.json evidence paths are invalid")
        if not isinstance(digest, str) or len(digest) != 64 or any(char not in SHA256 for char in digest):
            raise QualificationError("p4-go-live.json subordinate digest is invalid")
        paths.add(path)
    if paths != set(REQUIRED_EVIDENCE):
        raise QualificationError("p4-go-live.json evidence paths are not exact")


def verify_release(
    release: Any,
    payload: bytes,
    repository: str,
    version: str,
    commit: str,
) -> dict[str, Any]:
    """Verify release state, asset bytes and P4 root identity."""
    if not isinstance(release, dict):
        raise QualificationError("GitHub release response is not an object")
    if release.get("tag_name") != f"v{version}":
        raise QualificationError("GitHub release tag mismatch")
    if release.get("draft") is not False or release.get("prerelease") is not False:
        raise QualificationError("production deployment requires a published non-prerelease release")
    published_at = release.get("published_at")
    _require_string(published_at, "published_at")
    release_id = release.get("id")
    if isinstance(release_id, bool) or not isinstance(release_id, int) or release_id <= 0:
        raise QualificationError("GitHub release id is invalid")
    asset = _find_asset(release)
    if len(payload) != asset["size"]:
        raise QualificationError("p4-go-live.json size changed after publication")
    actual_digest = hashlib.sha256(payload).hexdigest()
    if actual_digest != asset["digest"]:
        raise QualificationError("p4-go-live.json digest changed after publication")
    _validate_root_report(payload, repository, version, commit)
    return {
        "schema_version": 1,
        "status": "PASS",
        "verified_at": now_utc(),
        "repository": repository,
        "version": version,
        "commit": commit,
        "release_id": release_id,
        "release_published_at": published_at,
        "release_asset": "p4-go-live.json",
        "asset_id": asset["id"],
        "asset_sha256": actual_digest,
        "asset_size": len(payload),
    }


def verify_public_release(repository: str, version: str, commit: str, token: str) -> dict[str, Any]:
    """Fetch and verify the public GitHub Release for the exact deployment."""
    repository = require_repository(repository)
    version = require_semver(version)
    commit = require_sha40(commit)
    if len(token) < 16 or any(char.isspace() for char in token):
        raise QualificationError("GITHUB_TOKEN is missing or malformed")
    api_root = f"https://api.github.com/repos/{repository}"
    release = _get_json(f"{api_root}/releases/tags/v{version}", token)
    asset = _find_asset(release if isinstance(release, dict) else {})
    payload = _request(f"{api_root}/releases/assets/{asset['id']}", token, "application/octet-stream", MAX_ASSET_BYTES)
    return verify_release(release, payload, repository, version, commit)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    output = os.path.abspath(args.output)
    result = verify_public_release(args.repository, args.version, args.commit, os.environ.get("GITHUB_TOKEN", ""))
    atomic_write_json(output, result)
    print("Public P4 release evidence verification: PASS")


if __name__ == "__main__":
    try:
        main()
    except QualificationError as exc:
        print(f"Public P4 release evidence: {exc}", file=sys.stderr)
        sys.exit(1)
