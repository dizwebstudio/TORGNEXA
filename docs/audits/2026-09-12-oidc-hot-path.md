# Закрытие 234.6 — OIDC/session/membership hot path

Дата проверки: 2026-09-12. Решение: [ADR-0195](../../adr/0195-local-oidc-authentication-hot-path.md).

## Реализованная граница

API локально проверяет RS256 JWT по issuer-bound JWKS и требует одновременно
`aud=torgnexa-api` и `azp=torgnexa-web`. Проверяются срок действия,
not-before, issued-at и subject. JWKS rotation, singleflight, refetch floor и
bounded stale-if-error позволяют продолжать проверку уже известных ключей при
кратком отказе Keycloak, не принимая unsigned payload.

UserInfo вызывается только для bounded profile hydration и подтверждения email
приглашения. Warm запрос не обращается к IdP. Membership имеет односекундный
positive cache и передаётся из tenant resolver в authorizer через typed request
context. SSE reauthorization обходит cache. Session status проверяется в
PostgreSQL всегда, а неизменный `last_seen_at` пишется не чаще раза в минуту.

Label-free snapshot считает provider/cache/session/membership calls, результаты
авторизации, writes/throttles, распределение IdP/DB calls на запрос и latency
p50/p95/p99 до начала business handler/SSE lifetime. Identity, tenant, URL,
route, token, email и raw error labels не используются.

## Проверенные сценарии

- валидная подпись, подмена подписи, unsigned JWT и ошибки
  `iss/aud/azp/exp/nbf/sub`;
- rotation с новым ключом и с заменой ключа при том же `kid`, refetch throttle,
  warm/cold Keycloak outage и bounded stale key;
- malformed или несовпадающий по subject UserInfo не отменяет локально
  подтверждённую JWT-аутентификацию, но не даёт `VerifiedEmail` и не позволяет
  принять приглашение;
- 100 одновременных authenticated запросов: один JWKS fetch, один UserInfo,
  один membership DB lookup, сто session checks и одна initial timestamp write;
- отдельный forced-RLS профиль на 32 параллельных запросах подтверждает те же
  call-count invariants на реальных session/membership repositories;
- revoked session возвращает 401 при полностью прогретых cache;
- forced-RLS PostgreSQL подтверждает throttle `last_seen_at`, последующую
  запись после минуты и запрет после revoke;
- SSE напрямую перепроверяет disabled membership и ошибки session/membership
  stores, не ожидая истечения cache.

## Результаты

- `go test ./...` и `go vet ./...` — PASS.
- целевые OIDC/API tests с `-race` — PASS.
- `./scripts/check-audit-postgres.sh` — PASS с forced RLS и `-race`.
- repository `gosec` policy — PASS: 0 High/Critical findings.
- contracts, architecture, migrations, supply-chain policy, Community
  deployment и package index — PASS.
