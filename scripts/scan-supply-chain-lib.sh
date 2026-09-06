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

run_command_with_retries() {
  local max_attempts=$1
  local retry_delay_seconds=$2
  shift 2
  local attempt status=1

  [[ "$max_attempts" =~ ^[1-9][0-9]*$ ]] || return 2
  [[ "$retry_delay_seconds" =~ ^[0-9]+$ ]] || return 2
  for ((attempt = 1; attempt <= max_attempts; attempt++)); do
    if "$@"; then
      return 0
    else
      status=$?
    fi
    if ((attempt < max_attempts && retry_delay_seconds > 0)); then
      sleep "$((retry_delay_seconds * attempt))"
    fi
  done
  return "$status"
}

run_trivy_json_with_retries() {
  local trivy_binary=$1
  local raw_report=$2
  local stderr_report=$3
  local max_attempts=$4
  local retry_delay_seconds=$5
  shift 5
  local attempt status=1

  [[ "$max_attempts" =~ ^[1-9][0-9]*$ ]] || return 2
  [[ "$retry_delay_seconds" =~ ^[0-9]+$ ]] || return 2
  : >"$stderr_report"
  for ((attempt = 1; attempt <= max_attempts; attempt++)); do
    rm -f -- "$raw_report"
    if "$trivy_binary" "$@" --format json --output "$raw_report" \
      >/dev/null 2>>"$stderr_report"; then
      return 0
    else
      status=$?
    fi
    if ((attempt < max_attempts && retry_delay_seconds > 0)); then
      sleep "$((retry_delay_seconds * attempt))"
    fi
  done
  return "$status"
}

merge_trivy_vulnerability_reports() {
  local os_report=$1
  local language_report=$2
  local image=$3
  local destination=$4

  jq -s --arg image "$image" '
    if length == 2 and all(.[]; .SchemaVersion == 2 and (.Results | type == "array"))
    then
      .[0] as $os |
      .[1] as $language |
      $os |
      .ArtifactName = $image |
      .ArtifactType = "container_image" |
      .Results = ($os.Results + $language.Results)
    else
      error("cannot merge malformed Trivy vulnerability reports")
    end
  ' "$os_report" "$language_report" >"$destination"
}
