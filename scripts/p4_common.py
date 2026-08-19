#!/usr/bin/env python3
"""Shared fail-closed helpers for P4 go-live evidence."""
from __future__ import annotations

import datetime as dt
import hashlib
import json
import os
import pathlib
import re
import tempfile
from typing import Any

SEMVER = re.compile(r"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$")
SHA40 = re.compile(r"^[0-9a-f]{40}$")
REPOSITORY = re.compile(r"^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9][A-Za-z0-9._-]{0,99}$")
SECRET_KEY = re.compile(r"(?i)(password|passwd|secret|token|api[_-]?key|private[_-]?key|authorization|credential|cookie|session)")

class QualificationError(RuntimeError):
    pass

def now_utc() -> str:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace('+00:00','Z')

def read_json(path: os.PathLike[str] | str) -> Any:
    p = pathlib.Path(path)
    if not p.is_file() or p.is_symlink():
        raise QualificationError(f"unsafe or missing JSON: {p}")
    try:
        return json.loads(p.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise QualificationError(f"invalid JSON {p}: {exc}") from exc

def atomic_write_json(path: os.PathLike[str] | str, value: Any) -> None:
    p = pathlib.Path(path)
    p.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp = tempfile.mkstemp(prefix=p.name + ".", dir=str(p.parent))
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump(value, f, indent=2, sort_keys=True)
            f.write("\n")
            f.flush(); os.fsync(f.fileno())
        os.chmod(tmp, 0o600)
        os.replace(tmp, p)
    finally:
        if os.path.exists(tmp): os.unlink(tmp)

def sha256_file(path: os.PathLike[str] | str) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for block in iter(lambda: f.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()

def reject_secret_shaped_fields(value: Any, where: str = "$", allow: set[str] | None = None) -> None:
    allow = allow or set()
    if isinstance(value, dict):
        for key, child in value.items():
            if key not in allow and SECRET_KEY.search(str(key)):
                raise QualificationError(f"secret-shaped field is forbidden in retained evidence: {where}.{key}")
            reject_secret_shaped_fields(child, f"{where}.{key}", allow)
    elif isinstance(value, list):
        for i, child in enumerate(value):
            reject_secret_shaped_fields(child, f"{where}[{i}]", allow)

def require_semver(value: str) -> str:
    if not SEMVER.fullmatch(value):
        raise QualificationError("release_version must be canonical SemVer without v prefix")
    return value

def require_sha40(value: str) -> str:
    if not SHA40.fullmatch(value):
        raise QualificationError("release_commit must be lowercase 40-hex SHA")
    return value

def require_repository(value: str) -> str:
    if not REPOSITORY.fullmatch(value):
        raise QualificationError("repository must be OWNER/NAME")
    return value
