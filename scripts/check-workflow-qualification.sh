#!/usr/bin/env bash
set -euo pipefail

# Task 163 repository qualification. This is intentionally deterministic and
# safe to run on a small VPS: it exercises the bounded compiler, retry/lease
# state machine and contract fixtures in a disposable Go container, then runs
# the static API/UI checks. Live provider credentials and production capacity
# remain deployment evidence and are never synthesized here.
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$root"

command -v docker >/dev/null 2>&1 || { echo "workflow qualification: Docker is required" >&2; exit 1; }
docker version >/dev/null

evidence="${TORGNEXA_WORKFLOW_EVIDENCE_DIR:-$root/qualification/evidence/workflow-$(date -u +%Y%m%dT%H%M%SZ)}"
mkdir -p "$evidence"

docker run --rm -v "$root":/app -w /app golang:1.26-alpine sh -c '
  set -eu
  export GOTOOLCHAIN=local GOWORK=off
  go test ./internal/core/workflow ./internal/platform/postgres/workflowrepo ./internal/platform/workflowengine ./internal/platform/background
  go test ./internal/app/api ./internal/app/worker
  go vet ./internal/core/workflow ./internal/platform/postgres/workflowrepo ./internal/platform/workflowengine ./internal/app/api ./internal/app/worker
' | tee "$evidence/go.txt"

docker run --rm -v "$root":/app -w /app golang:1.26-alpine sh -c '
  set -eu
  export GOTOOLCHAIN=local GOWORK=off
  go -C tools/contractcheck run -mod=readonly ./cmd/contractcheck --root ../..
' | tee "$evidence/contracts.txt"

if [[ -d frontend/node_modules ]]; then
  npm --prefix frontend run test:logic | tee "$evidence/frontend-logic.txt"
  npm --prefix frontend run test:docs | tee "$evidence/frontend-docs.txt"
else
  echo "frontend node_modules absent; frontend qualification must run in CI/release image" | tee "$evidence/frontend.txt"
fi

python3 - "$evidence" <<'PY'
import json, pathlib, sys, time
out = pathlib.Path(sys.argv[1])
manifest = {
    "task": "163",
    "status": "PASS",
    "measurement_class": "deterministic_repository_qualification",
    "qualified_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    "evidence": sorted(p.name for p in out.iterdir() if p.name != "metadata.json"),
    "limits": {
        "max_nodes": 64,
        "max_edges": 128,
        "max_step_attempts": 8,
        "max_concurrent_runs": 8,
    },
    "external_live_qualification_required": True,
}
(out / "metadata.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")
PY

echo "Task 163 deterministic qualification: PASS"
echo "evidence: $evidence"
