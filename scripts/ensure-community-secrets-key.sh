#!/usr/bin/env sh
set -eu
umask 077
repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)"
out="$repo_root/.env"
[ -f "$out" ] && [ ! -L "$out" ] || { echo '.env must be a regular file' >&2; exit 1; }
if grep -Eq '^TORGNEXA_SECRETS_MASTER_KEY=.+$' "$out"; then
  echo 'community secrets master key already configured'
  exit 0
fi
key="$(od -An -N 32 -tu1 /dev/urandom | awk '{for(i=1;i<=NF;i++)printf "%c",$i}' | base64 | tr -d '\n')"
tmp="$repo_root/.env.secrets.$$"
trap 'rm -f "$tmp"' EXIT
awk -v key="$key" '
  BEGIN { written=0 }
  /^TORGNEXA_SECRETS_MASTER_KEY=/ { print "TORGNEXA_SECRETS_MASTER_KEY=" key; written=1; next }
  { print }
  END { if (!written) print "TORGNEXA_SECRETS_MASTER_KEY=" key }
' "$out" > "$tmp"
chmod 600 "$tmp"
mv -f "$tmp" "$out"
trap - EXIT
echo 'configured a new Community secrets master key in .env (value hidden)'
