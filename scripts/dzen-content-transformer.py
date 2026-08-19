#!/usr/bin/env python3
"""Fail-closed Task-047 Dzen content transformer.

This tool mirrors the bounded Task-020 SocialPublishRequest fields used by the
fixture audit. It performs no network access and admits no Dzen credential or
endpoint. Live publication remains unavailable until a qualified official API
contract is admitted through the Connector SDK.
"""
from __future__ import annotations

import json
import re
import sys
from dataclasses import dataclass
from typing import Any

PUBLICATION_RE = re.compile(r"^[0-9a-fA-F-]{16,64}$")
UPLOAD_RE = re.compile(r"^upl_[A-Za-z0-9_-]{16,128}$")
VALID_KINDS = {"text", "media", "video"}
VALID_TARGETS = {"post", "article", "video"}
VALID_MEDIA = {"image", "video"}

class TransformError(ValueError):
    pass

@dataclass(frozen=True)
class Package:
    publication_id: str
    type: str
    text: str
    media: tuple[dict[str, str], ...]

    def as_dict(self) -> dict[str, Any]:
        return {
            "publication_id": self.publication_id,
            "type": self.type,
            "text": self.text,
            "media": [dict(item) for item in self.media],
        }

def _plain(value: Any, *, maximum: int) -> str:
    if not isinstance(value, str) or len(value) > maximum or value.strip() != value:
        raise TransformError("invalid string")
    if any(ord(ch) < 0x20 and ch not in "\n\t" for ch in value):
        raise TransformError("invalid control character")
    return value

def transform(payload: dict[str, Any]) -> Package:
    if not isinstance(payload, dict):
        raise TransformError("object required")
    publication_id = _plain(payload.get("publication_id", ""), maximum=80)
    kind = _plain(payload.get("kind", ""), maximum=16)
    target = _plain(payload.get("target", ""), maximum=16)
    text = _plain(payload.get("text", ""), maximum=50000)
    if not PUBLICATION_RE.fullmatch(publication_id) or kind not in VALID_KINDS or target not in VALID_TARGETS:
        raise TransformError("invalid identity/kind/target")
    if payload.get("buttons") not in (None, []):
        raise TransformError("buttons are not qualified")
    raw_media = payload.get("media", [])
    if not isinstance(raw_media, list) or len(raw_media) > 20:
        raise TransformError("invalid media list")
    media: list[dict[str, str]] = []
    seen: set[str] = set()
    for item in raw_media:
        if not isinstance(item, dict):
            raise TransformError("invalid media item")
        upload_id = _plain(item.get("upload_id", ""), maximum=160)
        media_kind = _plain(item.get("kind", ""), maximum=16)
        alt_text = _plain(item.get("alt_text", ""), maximum=2000)
        if not UPLOAD_RE.fullmatch(upload_id) or media_kind not in VALID_MEDIA or upload_id in seen:
            raise TransformError("invalid media reference")
        seen.add(upload_id)
        media.append({"upload_id": upload_id, "kind": media_kind, "alt_text": alt_text})
    if kind == "text" and media:
        raise TransformError("text kind cannot contain media")
    if kind == "media" and (not media or any(x["kind"] != "image" for x in media)):
        raise TransformError("media kind requires image references")
    if kind == "video" and (len(media) != 1 or media[0]["kind"] != "video"):
        raise TransformError("video kind requires one video")
    if target == "post":
        if kind == "video" or (not text and not media):
            raise TransformError("invalid post package")
    elif target == "article":
        if not text or kind == "video" or any(x["kind"] != "image" for x in media):
            raise TransformError("invalid article package")
    elif target == "video":
        if kind != "video":
            raise TransformError("invalid video package")
    return Package(publication_id, target, text, tuple(media))

def publish(_: Package) -> None:
    raise RuntimeError("dzen: live publishing unavailable; no qualified official public API contract in Task-047 audit")

def main() -> int:
    try:
        payload = json.load(sys.stdin)
        package = transform(payload)
        json.dump(package.as_dict(), sys.stdout, ensure_ascii=False, separators=(",", ":"))
        sys.stdout.write("\n")
        return 0
    except (TransformError, json.JSONDecodeError) as exc:
        print(f"dzen-transform: {exc}", file=sys.stderr)
        return 2

if __name__ == "__main__":
    raise SystemExit(main())
