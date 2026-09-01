#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
evidence="${TORGNEXA_MARKETPLACE_EVIDENCE_FILE:-}"
scope="${TORGNEXA_MARKETPLACE_EVIDENCE_SCOPE:-listing}"

if [[ -z "$evidence" ]]; then
  echo "marketplace remote evidence: set TORGNEXA_MARKETPLACE_EVIDENCE_FILE to an absolute redacted JSON path" >&2
  exit 1
fi
if [[ "$evidence" != /* ]]; then
  echo "marketplace remote evidence: evidence path must be absolute" >&2
  exit 1
fi
if [[ "$scope" != listing && "$scope" != full ]]; then
  echo "marketplace remote evidence: TORGNEXA_MARKETPLACE_EVIDENCE_SCOPE must be listing or full" >&2
  exit 1
fi

PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$root/scripts" \
  python3 "$root/scripts/marketplace_remote_evidence.py" \
  --input "$evidence" --scope "$scope"
