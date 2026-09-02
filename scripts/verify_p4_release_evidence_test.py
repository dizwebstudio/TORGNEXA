#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import importlib.util
import json
import pathlib
import sys
import unittest

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

from p4_common import QualificationError
from p4_root_evidence import REQUIRED

_SPEC = importlib.util.spec_from_file_location(
    "verify_p4_release_evidence",
    HERE / "verify-p4-release-evidence.py",
)
assert _SPEC and _SPEC.loader
_MODULE = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(_MODULE)
verify_release = _MODULE.verify_release


class PublicP4ReleaseEvidenceTests(unittest.TestCase):
    repository = "example/torgnexa"
    version = "1.2.3"
    commit = "a" * 40

    def _payload(self) -> bytes:
        value = {
            "schema_version": 1,
            "status": "PASS",
            "qualified_at": "2026-09-02T00:00:00Z",
            "release_version": self.version,
            "release_commit": self.commit,
            "repository": self.repository,
            "evidence": [{"path": path, "sha256": "b" * 64} for path in REQUIRED],
        }
        return (json.dumps(value, sort_keys=True) + "\n").encode("utf-8")

    def _release(self, payload: bytes, **overrides):
        asset = {
            "id": 22,
            "name": "p4-go-live.json",
            "state": "uploaded",
            "digest": f"sha256:{hashlib.sha256(payload).hexdigest()}",
            "size": len(payload),
        }
        release = {
            "id": 11,
            "tag_name": f"v{self.version}",
            "draft": False,
            "prerelease": False,
            "published_at": "2026-09-02T00:01:00Z",
            "assets": [asset],
        }
        release.update(overrides)
        return release

    def test_accepts_exact_published_release_and_asset(self):
        payload = self._payload()
        result = verify_release(self._release(payload), payload, self.repository, self.version, self.commit)
        self.assertEqual(result["status"], "PASS")
        self.assertEqual(result["asset_size"], len(payload))

    def test_rejects_draft_release(self):
        payload = self._payload()
        with self.assertRaises(QualificationError):
            verify_release(self._release(payload, draft=True), payload, self.repository, self.version, self.commit)

    def test_rejects_changed_asset_bytes(self):
        payload = self._payload()
        with self.assertRaises(QualificationError):
            verify_release(self._release(payload), payload + b"changed", self.repository, self.version, self.commit)

    def test_rejects_root_identity_mismatch(self):
        payload = self._payload().replace(self.commit.encode(), ("c" * 40).encode(), 1)
        release = self._release(payload)
        with self.assertRaises(QualificationError):
            verify_release(release, payload, self.repository, self.version, self.commit)

    def test_rejects_secret_shaped_retained_field(self):
        payload = self._payload().replace(b'"status": "PASS"', b'"access_token": "forbidden", "status": "PASS"', 1)
        release = self._release(payload)
        with self.assertRaises(QualificationError):
            verify_release(release, payload, self.repository, self.version, self.commit)


if __name__ == "__main__":
    unittest.main()
