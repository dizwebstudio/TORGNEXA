#!/usr/bin/env bash
set -euo pipefail

health_url=${1:?health URL is required}
curl --fail --silent --show-error --max-time 10 "$health_url" >/dev/null
