#!/usr/bin/env python3
from __future__ import annotations
import importlib.util
import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
MOD = ROOT / "scripts" / "dzen-content-transformer.py"
spec = importlib.util.spec_from_file_location("dzen_transformer", MOD)
assert spec and spec.loader
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

FIXTURES = ROOT / "docs" / "connectors" / "dzen" / "fixtures"
for name in ("post", "article", "video"):
    payload = json.loads((FIXTURES / f"{name}.json").read_text())
    payload.setdefault("publication_id", "01900000-0000-7000-8000-000000000047")
    package = module.transform(payload)
    assert package.type == name

video = json.loads((FIXTURES / "video.json").read_text())
video["publication_id"] = "01900000-0000-7000-8000-000000000047"
video["target"] = "article"
try:
    module.transform(video)
    raise AssertionError("article must reject video")
except module.TransformError:
    pass

article = json.loads((FIXTURES / "article.json").read_text())
article["publication_id"] = "01900000-0000-7000-8000-000000000047"
package = module.transform(article)
try:
    module.publish(package)
    raise AssertionError("live publish must fail closed")
except RuntimeError as exc:
    assert "unavailable" in str(exc)

print("dzen transformer fixtures: 3/3 PASS; fail-closed publish: PASS")
