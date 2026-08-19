#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
LEGACY = ROOT / "migrations_legacy_pre_v1"
ACTIVE = ROOT / "migrations"

GROUPS = [
    (4, 8, "security_eventing", "expand"),
    (9, 17, "commerce_core", "expand"),
    (18, 27, "operations_foundation", "expand"),
    (28, 40, "commerce_extensions", "expand"),
    (41, 54, "regulated_integrations", "expand"),
    (55, 63, "control_plane", "expand"),
    (64, 64, "legacy_contract", "contract"),
    (65, 74, "runtime_operations", "expand"),
]

HISTORY_SQL = """INSERT INTO migration_history (\n  version, name, file_name, phase, risk, checksum_sha256,\n  application_version, execution_id, duration_ms\n) VALUES (\n  current_setting('torgnexa.migration_version')::integer,\n  current_setting('torgnexa.migration_name'),\n  current_setting('torgnexa.migration_file'),\n  current_setting('torgnexa.migration_phase'),\n  current_setting('torgnexa.migration_risk'),\n  current_setting('torgnexa.migration_checksum'),\n  current_setting('torgnexa.application_version'),\n  current_setting('torgnexa.migration_execution_id'),\n  current_setting('torgnexa.migration_duration_ms')::bigint\n);\n"""


def sha(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def legacy_catalog() -> dict:
    return json.loads((LEGACY / "catalog.json").read_text())


def strip_payload(version: int, text: str) -> str:
    stripped = text.strip()
    if not stripped.startswith("BEGIN;") or not stripped.endswith("COMMIT;"):
        raise ValueError(f"legacy migration {version} does not have one transaction")
    body = stripped[len("BEGIN;"):]
    body = body[: body.rfind("COMMIT;")]
    if version >= 4:
        marker = body.rfind("INSERT INTO migration_history")
        if marker < 0:
            raise ValueError(f"legacy migration {version} has no history insert")
        body = body[:marker]
    return body.strip("\n") + "\n"


def baseline_file(version: int, start: int, end: int, name: str, legacy_digest: str) -> bytes:
    pieces = [
        "BEGIN;\n",
        f"\n-- TORGNEXA pre-v1 baseline component {version:06d}: {name}.\n",
        f"-- Squashed, statement-order-preserving source range: legacy {start:06d}..{end:06d}.\n",
        "-- Do not edit by hand; regenerate with scripts/generate-pre-v1-baseline.py.\n",
        "\n-- BASELINE_SOURCE_BEGIN\n",
    ]
    for old in range(start, end + 1):
        source = LEGACY / next(m["file"] for m in legacy_catalog()["migrations"] if m["version"] == old)
        pieces.append(f"\n-- SOURCE {source.name}\n")
        pieces.append(strip_payload(old, source.read_text()))
    pieces.append("-- BASELINE_SOURCE_END\n\n")
    if version == 4:
        pieces.extend([
            "CREATE TABLE IF NOT EXISTS migration_baseline_evidence (\n",
            "  baseline_id text PRIMARY KEY,\n",
            "  source_head_version integer NOT NULL CHECK (source_head_version > 0),\n",
            "  source_catalog_sha256 text NOT NULL CHECK (source_catalog_sha256 ~ '^[0-9a-f]{64}$'),\n",
            "  source_history_rows integer NOT NULL CHECK (source_history_rows >= 0),\n",
            "  mode text NOT NULL CHECK (mode IN ('fresh_baseline','legacy_rebaseline')),\n",
            "  stamped_at timestamptz NOT NULL DEFAULT clock_timestamp()\n",
            ");\n",
            "REVOKE ALL ON migration_baseline_evidence FROM PUBLIC;\n",
            "INSERT INTO migration_baseline_evidence(baseline_id,source_head_version,source_catalog_sha256,source_history_rows,mode)\n",
            f"VALUES ('pre_v1_v1',74,'{legacy_digest}',0,'fresh_baseline')\n",
            "ON CONFLICT (baseline_id) DO NOTHING;\n\n",
        ])
    pieces.append(HISTORY_SQL)
    pieces.append("\nCOMMIT;\n")
    return "".join(pieces).encode()


def build() -> tuple[dict, dict]:
    legacy = legacy_catalog()
    by_version = {m["version"]: m for m in legacy["migrations"]}
    if sorted(by_version) != list(range(1, 75)):
        raise ValueError("legacy catalog must contain exactly versions 1..74")
    legacy_sql = sorted(LEGACY.glob("*.sql"))
    if len(legacy_sql) != 74:
        raise ValueError(f"legacy SQL inventory must contain 74 files, got {len(legacy_sql)}")
    for migration in legacy["migrations"]:
        path = LEGACY / migration["file"]
        if path.is_symlink() or not path.is_file() or sha(path.read_bytes()) != migration["sha256"]:
            raise ValueError(f"legacy checksum drift/unsafe path: {migration['file']}")
    legacy_catalog_bytes = (LEGACY / "catalog.json").read_bytes()
    legacy_catalog_digest = sha(legacy_catalog_bytes)

    # Active v1 baseline deliberately retains the three framework-adoption files
    # byte-for-byte so the existing bootstrap runner remains crash-safe.
    active_entries = []
    for old_version in (1, 2, 3):
        old = by_version[old_version]
        data = (LEGACY / old["file"]).read_bytes()
        (ACTIVE / old["file"]).write_bytes(data)
        entry = dict(old)
        entry["sha256"] = sha(data)
        active_entries.append(entry)

    manifest_groups = []
    next_version = 4
    keep = {by_version[v]["file"] for v in (1, 2, 3)}
    for start, end, name, phase in GROUPS:
        filename = f"{next_version:06d}_{name}.sql"
        data = baseline_file(next_version, start, end, name, legacy_catalog_digest)
        (ACTIVE / filename).write_bytes(data)
        keep.add(filename)
        legacy_sources = []
        for v in range(start, end + 1):
            m = by_version[v]
            legacy_sources.append({"version": v, "name": m["name"], "file": m["file"], "sha256": m["sha256"]})
        contract_preconditions = []
        compatibility = {"old_readers": True, "old_writers": True, "new_binary_on_old_schema": True, "contract_preconditions": []}
        if phase == "contract":
            compatibility = dict(by_version[64]["compatibility"])
            contract_preconditions = compatibility["contract_preconditions"]
        entry = {
            "version": next_version,
            "name": name,
            "file": filename,
            "phase": phase,
            "risk": "high",
            "transaction": "embedded",
            "policy": "v1",
            "history_mode": "atomic",
            "requires_backup": True,
            "dependencies": [next_version - 1],
            "compatibility": compatibility,
            "backfill": None,
            "sha256": sha(data),
        }
        active_entries.append(entry)
        manifest_groups.append({
            "baseline_version": next_version,
            "baseline_name": name,
            "baseline_file": filename,
            "phase": phase,
            "legacy_start": start,
            "legacy_end": end,
            "legacy_sources": legacy_sources,
            "baseline_sha256": sha(data),
            "contract_preconditions": contract_preconditions,
        })
        next_version += 1

    # Remove old active SQL files not part of the compact baseline.
    for path in ACTIVE.glob("*.sql"):
        if path.name not in keep:
            path.unlink()

    catalog = {"schema_version": 1, "migrations": active_entries}
    catalog_bytes = (json.dumps(catalog, indent=2) + "\n").encode()
    (ACTIVE / "catalog.json").write_bytes(catalog_bytes)

    manifest = {
        "schema_version": 1,
        "baseline_id": "pre_v1_v1",
        "baseline_migration_count": len(active_entries),
        "legacy_head_version": 74,
        "legacy_migration_count": 74,
        "legacy_catalog_sha256": legacy_catalog_digest,
        "active_catalog_sha256": sha(catalog_bytes),
        "strategy": "statement_order_preserving_squash",
        "groups": manifest_groups,
    }
    (ACTIVE / "baseline-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    return catalog, manifest


def verify() -> None:
    # build() only ever knows about the squashed 000001..000011 baseline: it
    # is a one-time compaction utility for the legacy 74-file history, not a
    # generator for the active migration set as a whole. Ordinary new
    # migrations (version > 11) are added by hand after the baseline and are
    # intentionally outside its scope, so this only verifies that files
    # 000001..000011 and their catalog.json entries still exactly match the
    # deterministic legacy-derived output; never run this script without
    # --check against a migrations/ directory that has post-baseline files —
    # the mutating (non --check) path deletes anything not in the baseline.
    global ACTIVE
    real_active = ACTIVE
    real_catalog = json.loads((real_active / "catalog.json").read_bytes())
    tracked = lambda p: p.is_file() and (p.suffix == ".sql" or p.name == "baseline-manifest.json")
    actual = {path.name: path.read_bytes() for path in real_active.iterdir() if tracked(path)}
    with tempfile.TemporaryDirectory(prefix="torgnexa-baseline-") as temp_dir:
        ACTIVE = Path(temp_dir)
        try:
            catalog, manifest = build()
            expected = {path.name: path.read_bytes() for path in ACTIVE.iterdir() if tracked(path)}
        finally:
            ACTIVE = real_active
    baseline_files = {m["file"] for m in catalog["migrations"]} | {"baseline-manifest.json"}
    actual_baseline = {name: data for name, data in actual.items() if name in baseline_files}
    changed = sorted(name for name in (set(actual_baseline) | set(expected)) if actual_baseline.get(name) != expected.get(name))
    if changed:
        raise SystemExit("baseline drift: regenerate and commit: " + ", ".join(changed))
    baseline_count = len(catalog["migrations"])
    real_migrations = sorted(real_catalog["migrations"], key=lambda m: m["version"])
    real_head = real_migrations[:baseline_count]
    if len(real_migrations) < baseline_count or [m["sha256"] for m in real_head] != [m["sha256"] for m in catalog["migrations"]]:
        raise SystemExit("baseline drift: migrations/catalog.json entries 1.." + str(baseline_count) + " no longer match the deterministic squash")
    if baseline_count != 11 or manifest["legacy_head_version"] != 74:
        raise SystemExit("baseline metadata invariant failed")
    print(f"TORGNEXA pre-v1 baseline: PASS — 11 active migrations equivalent to legacy head 000074 ({len(real_migrations)} active migrations total)")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    if args.check:
        verify()
    else:
        catalog, _ = build()
        print(f"generated {len(catalog['migrations'])} active baseline migrations")


if __name__ == "__main__":
    main()
