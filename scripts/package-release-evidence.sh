#!/usr/bin/env bash
set -euo pipefail
umask 077
export LC_ALL=C TZ=UTC
for cmd in tar gzip; do command -v "$cmd" >/dev/null || { echo "package release evidence: $cmd is required" >&2; exit 1; }; done
evidence="" output=""
while (($#)); do case "$1" in --evidence-dir) evidence=${2:-}; shift 2;; --output) output=${2:-}; shift 2;; *) echo "usage: $0 --evidence-dir DIR --output FILE" >&2; exit 2;; esac; done
[[ "$evidence" == /* && -d "$evidence" && ! -L "$evidence" && "$output" == /* ]] || { echo "package release evidence: safe absolute paths required" >&2; exit 1; }
[[ -f "$evidence/evidence.json" && ! -L "$evidence/evidence.json" ]] || { echo "package release evidence: evidence.json missing" >&2; exit 1; }
mkdir -p -- "$(dirname -- "$output")"
tmp="$output.tmp.$$"; trap 'rm -f -- "$tmp"' EXIT
tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner -C "$evidence" -cf - . | gzip -n >"$tmp"
chmod 0600 "$tmp"; mv -- "$tmp" "$output"; trap - EXIT
echo "package release evidence: PASS"
