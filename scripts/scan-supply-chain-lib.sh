#!/usr/bin/env bash

# Shared helpers for the supply-chain scanner and its deterministic tests.

sanitize_json() {
  local raw=$1
  local destination=$2

  [[ -f "$raw" && ! -L "$raw" ]] || return 1
  jq -s 'map(walk(if type == "object" then del(
    .Match, .match, .Secret, .secret, .Code, .code,
    .Snippet, .snippet, .Content, .content
  ) else . end)) | if length == 1 then .[0] else . end' "$raw" >"$destination"
}

normalize_trivy_report() {
  local report=$1
  local normalized

  [[ -f "$report" && ! -L "$report" ]] || return 1
  normalized="$(mktemp "${report}.normalize.XXXXXX")" || return 1
  if ! jq '
    if .SchemaVersion == 2 and ((has("Results") | not) or .Results == null)
    then .Results = []
    else .
    end
  ' "$report" >"$normalized"; then
    rm -f -- "$normalized"
    return 1
  fi
  if ! mv -- "$normalized" "$report"; then
    rm -f -- "$normalized"
    return 1
  fi
}
