#!/usr/bin/env bash
set -Eeuo pipefail

# Bounded Compose smoke for Task 169. The script sends synthetic questions
# only, never prints response bodies, and does not require an AI provider.
BASE_URL="${TORGNEXA_URL:-http://127.0.0.1:8080}"
TOKEN="${ACCESS_TOKEN:?set ACCESS_TOKEN to a short-lived OIDC token}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

request_id="assistant-smoke-$(date +%s)"
session_file="$TMP_DIR/session.json"
curl --fail-with-body --silent --show-error --retry 2 \
  -X POST "$BASE_URL/api/v1/assistant/sessions" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Idempotency-Key: $request_id" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Compose smoke","locale":"ru-RU"}' >"$session_file"

session_id="$(jq -er '.id' "$session_file")"
questions=(
  'Что требует внимания в интеграциях?'
  'Почему товар не публикуется?'
  'Какие каналы просели?'
  'Что будет с остатком и когда пополнять?'
  'Сформируй план исправления'
)

for index in "${!questions[@]}"; do
  response_file="$TMP_DIR/run-$index.json"
  curl --fail-with-body --silent --show-error --retry 2 \
    -X POST "$BASE_URL/api/v1/assistant/sessions/$session_id/messages" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Idempotency-Key: assistant-smoke-$index-$(date +%s)" \
    -H 'Content-Type: application/json' \
    -d "$(jq -cn --arg question "${questions[$index]}" '{question:$question}')" >"$response_file"
  jq -e '.state and .answer.grounding_state' "$response_file" >/dev/null
  if rg -qi 'bearer|api[_ -]?key|private[_ -]?key|password[[:space:]]*:' "$response_file"; then
    echo "assistant smoke: response contains a credential-shaped value" >&2
    exit 1
  fi
  echo "assistant smoke: question $((index + 1))/5 passed"
done

echo "assistant smoke: PASS (synthetic, provider-neutral baseline)"
