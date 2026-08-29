#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"
[[ -f .env ]] || { echo 'missing .env; run make community-init first' >&2; exit 1; }

demo_password="${TORGNEXA_DEMO_PASSWORD:-demo-local-only}"
compose=(docker compose --env-file .env)

for attempt in $(seq 1 30); do
  if demo_user_id=$("${compose[@]}" exec -T -e "TORGNEXA_DEMO_PASSWORD=$demo_password" keycloak sh -eu -c '
    kcadm=/opt/keycloak/bin/kcadm.sh
    "$kcadm" config credentials \
      --server http://127.0.0.1:8080 \
      --realm master \
      --user "$KC_BOOTSTRAP_ADMIN_USERNAME" \
      --password "$KC_BOOTSTRAP_ADMIN_PASSWORD" >/dev/null
    user_id=$("$kcadm" get users -r torgnexa -q username=demo --fields id --format csv --noquotes | sed -n "1p" | tr -d "\r")
    if [ -z "$user_id" ]; then
      "$kcadm" create users -r torgnexa \
        -s username=demo \
        -s enabled=true \
        -s email=demo@local.torgnexa \
        -s emailVerified=true \
        -s firstName=Демо \
        -s lastName=Оператор >/dev/null
      user_id=$("$kcadm" get users -r torgnexa -q username=demo --fields id --format csv --noquotes | sed -n "1p" | tr -d "\r")
    fi
    [ -n "$user_id" ]
    "$kcadm" set-password -r torgnexa --userid "$user_id" --new-password "$TORGNEXA_DEMO_PASSWORD"
    "$kcadm" update "users/$user_id" -r torgnexa \
      -s firstName=Демо \
      -s lastName=Оператор \
      -s "attributes.picture=/demo-images/demo-avatar.svg" \
      -s "attributes.birthdate=1988-04-17" \
      -s "attributes.job_title=Старший операционный менеджер" \
      -s "attributes.department=Коммерческие операции" \
      -s "attributes.phone_number=+7 (495) 555-01-42" >/dev/null
    "$kcadm" add-roles -r torgnexa --uusername demo --rolename admin >/dev/null 2>&1 || true
    printf "%s\n" "$user_id"
  '); then
    demo_user_id="${demo_user_id//$'\r'/}"
    demo_user_id="${demo_user_id//$'\n'/}"
    if [[ ! "$demo_user_id" =~ ^[a-zA-Z0-9._:-]{1,255}$ ]]; then
      echo 'Keycloak returned an invalid demo user identifier' >&2
    else
      demo_subject="$(printf '%s\0%s' 'http://127.0.0.1:8081/realms/torgnexa' "$demo_user_id" | sha256sum | cut -d' ' -f1)"
      demo_member_id="dev-${demo_subject:0:26}"
      demo_invitation_key="community-demo:${demo_subject:0:32}"
      if "${compose[@]}" exec -T \
        -e "TORGNEXA_DEMO_SUBJECT=$demo_subject" \
        -e "TORGNEXA_DEMO_MEMBER_ID=$demo_member_id" \
        -e "TORGNEXA_DEMO_INVITATION_KEY=$demo_invitation_key" \
        postgres psql -v ON_ERROR_STOP=1 -U torgnexa -d torgnexa \
        -v demo_subject="$demo_subject" \
        -v demo_member_id="$demo_member_id" \
        -v demo_invitation_key="$demo_invitation_key" \
        -f - < scripts/community-demo-member.sql; then
        echo "Community demo user is ready: demo / $demo_password"
        echo 'Community demo workspace membership is ready.'
        exit 0
      fi
    fi
  fi
  sleep 2
done

echo 'Keycloak did not become ready; demo user was not provisioned' >&2
exit 1
