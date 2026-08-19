import test from "node:test";
import assert from "node:assert/strict";
import {normalizeSession, publicSession, sessionExpired} from "../.repository-test/auth/session-model.js";
import {accountConsoleURL} from "../.repository-test/auth/oidc-urls.js";

test("session normalization deduplicates capabilities and public projection drops token", () => {
  const session = normalizeSession({subject: " user-1 ", displayName: " Operator ", accessToken: " opaque-token ", capabilities: ["orders.read", "products.read", "orders.read"]});
  assert.deepEqual(session.capabilities, ["orders.read", "products.read"]);
  const projected = publicSession(session);
  assert.equal("accessToken" in projected, false);
});

test("expired sessions fail closed", () => {
  const session = normalizeSession({subject: "u", displayName: "U", accessToken: "t", capabilities: [], expiresAt: "2026-08-10T10:00:00Z"});
  assert.equal(sessionExpired(session, Date.parse("2026-08-10T10:00:01Z")), true);
});

test("invalid capability values are rejected", () => {
  assert.throws(() => normalizeSession({subject: "u", displayName: "U", accessToken: "t", capabilities: ["../../admin"]}), /capability/);
});

test("roles are normalized but tokens stay outside the public projection", () => {
  const session = normalizeSession({subject: "u", displayName: "U", accessToken: "secret", capabilities: [], roles: ["admin", "viewer", "admin"]});
  assert.deepEqual(session.roles, ["admin", "viewer"]);
  assert.deepEqual(publicSession(session).roles, ["admin", "viewer"]);
  assert.equal(JSON.stringify(publicSession(session)).includes("secret"), false);
});

test("opaque OIDC subjects and UUID-shaped usernames never become display names", () => {
  const subject = "6cc02148-3568-4540-b5c1-6963e233a2b8";
  const session = normalizeSession({subject, displayName: subject, accessToken: "secret", capabilities: [], roles: ["admin"]});
  assert.equal(session.displayName, "TORGNEXA Administrator");
  assert.equal(session.displayName.includes(subject), false);

  const externalUUID = normalizeSession({subject: "opaque-provider-subject", displayName: "550e8400-e29b-41d4-a716-446655440000", accessToken: "secret", capabilities: [], roles: []});
  assert.equal(externalUUID.displayName, "Пользователь TORGNEXA");
});

test("account management URL is limited to secure issuers or local community development", () => {
  assert.equal(accountConsoleURL("https://id.example/realms/torgnexa"), "https://id.example/realms/torgnexa/account/");
  assert.equal(accountConsoleURL("http://127.0.0.1:8081/realms/torgnexa"), "http://127.0.0.1:8081/realms/torgnexa/account/");
  assert.throws(() => accountConsoleURL("http://id.example/realms/torgnexa"), /not safe/);
  assert.throws(() => accountConsoleURL("https://user:secret@id.example/realms/torgnexa"), /not safe/);
});
