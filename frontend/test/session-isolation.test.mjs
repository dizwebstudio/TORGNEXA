import test from "node:test";
import assert from "node:assert/strict";
import {SessionController} from "../.repository-test/auth/session-controller.js";
import {normalizeSession, publicSession} from "../.repository-test/auth/session-model.js";

const session = (subject, overrides = {}) => ({subject, displayName: `Synthetic ${subject}`, accessToken: `token-${subject}`, cacheScope: "issuer/tenant/workspace/login", capabilities: ["orders.read"], roles: ["viewer"], ...overrides});
const deferred = () => { let resolve; const promise = new Promise(r => {resolve = r;}); return {promise, resolve}; };

function fixture() {
  let current = session("A");
  let listener;
  const adapter = {getSession: async () => current, login: async () => {}, logout: async () => {}, subscribe: fn => {listener = fn; return () => {listener = null;};}};
  const controller = new SessionController(adapter);
  return {adapter, controller, set: next => {current = next;}, notify: () => listener?.()};
}

test("logout retires the old lifetime before awaiting the adapter", async () => {
  const {adapter, controller} = fixture();
  await controller.refresh();
  const previous = controller.getSnapshot().lifetime;
  const slowLogout = deferred();
  adapter.logout = () => slowLogout.promise;
  const pending = controller.logout();
  assert.equal(controller.getSnapshot().session, null);
  assert.equal(previous.signal.aborted, true);
  slowLogout.resolve(); await pending;
});

test("latest identity wins over a late refresh from the previous user", async () => {
  const {adapter, controller} = fixture();
  await controller.refresh();
  const old = deferred();
  adapter.getSession = () => old.promise;
  const pending = controller.refresh();
  await controller.logout();
  adapter.getSession = async () => session("B");
  await controller.refresh();
  const lifetime = controller.getSnapshot().lifetime;
  old.resolve(session("A"));
  assert.equal(await pending, null);
  assert.equal(controller.getSnapshot().session.subject, "B");
  assert.equal(controller.getSnapshot().lifetime, lifetime);
});

test("subject, scope, permissions and roles each retire cached state", async () => {
  for (const next of [session("B"),session("A", {cacheScope:"issuer/tenant/another-workspace/login"}),session("A", {capabilities:[]}),session("A", {roles:["operator"]})]) {
    const {controller, set} = fixture(); await controller.refresh();
    const before = controller.getSnapshot().lifetime;
    set(next); await controller.refresh();
    assert.equal(before.signal.aborted, true);
    assert.notEqual(controller.getSnapshot().lifetime, before);
  }
});

test("same scoped session token renewal preserves cache, opaque adapter replacement does not", async () => {
  const {controller,set} = fixture(); await controller.refresh();
  const before = controller.getSnapshot().lifetime;
  set(session("A",{accessToken:"renewed"})); await controller.refresh();
  assert.equal(controller.getSnapshot().lifetime,before);
  set(session("A",{cacheScope:undefined}));await controller.refresh();
  const opaque=controller.getSnapshot().lifetime;
  set(session("A",{accessToken:"another-token",cacheScope:undefined}));await controller.refresh();
  assert.equal(opaque.signal.aborted,true);
  assert.notEqual(controller.getSnapshot().lifetime,opaque);
});

test("expired, missing, malformed and failed sessions discard old data lifetime", async () => {
  for (const invalid of [null,session("A",{expiresAt:"2020-01-01T00:00:00Z"}),session("A",{subject:""}),new Error("unavailable")]) {
    const {controller,adapter}=fixture();await controller.refresh();
    const before=controller.getSnapshot().lifetime;
    adapter.getSession=async()=>{if(invalid instanceof Error)throw invalid;return invalid;};
    await controller.refresh();
    assert.equal(controller.getSnapshot().session,null);
    assert.equal(before.signal.aborted,true);
  }
});

test("adding or removing scope metadata retires cache even with the same token", async () => {
  const {controller, set} = fixture();
  await controller.refresh();
  for (const cacheScope of [undefined, "issuer/tenant/workspace/login"]) {
    const before = controller.getSnapshot().lifetime;
    set(session("A", {cacheScope}));
    await controller.refresh();
    assert.equal(before.signal.aborted, true);
    assert.notEqual(controller.getSnapshot().lifetime, before);
  }
});

test("an expiry timer queued before renewal cannot expire the renewed session", async () => {
  const {controller, set} = fixture();
  set(session("A", {expiresAt: new Date(Date.now() + 10_000).toISOString()}));
  await controller.refresh();
  const lifetime = controller.getSnapshot().lifetime;
  set(session("A", {accessToken: "renewed", expiresAt: new Date(Date.now() + 300_000).toISOString()}));
  await controller.refresh();
  controller.expire(lifetime);
  assert.equal(controller.getSnapshot().lifetime, lifetime);
  assert.equal(lifetime.signal.aborted, false);
  assert.equal(controller.getSnapshot().session.accessToken, "renewed");
});

test("adapter change notification hides the old identity while lookup is pending", async () => {
  const {adapter,controller,notify}=fixture();
  const stop=controller.start();await controller.refresh();
  const before=controller.getSnapshot().lifetime;
  const next=deferred();adapter.getSession=()=>next.promise;
  notify();
  assert.equal(before.signal.aborted,true);
  assert.equal(controller.getSnapshot().session,null);
  next.resolve(session("B"));await next.promise;
  assert.equal(controller.getSnapshot().session.subject,"B");stop();
});

test("an old expiry or failed account-management operation cannot disturb a newer session", async () => {
  const {adapter,controller,set}=fixture();await controller.refresh();
  const before=controller.getSnapshot().lifetime;
  const pending=deferred();adapter.manageAccount=()=>pending.promise.then(()=>{throw new Error("old failure");});
  const management=controller.manageAccount();
  set(session("B"));await controller.refresh();
  controller.expire(before);pending.resolve();await management;
  assert.equal(controller.getSnapshot().session.subject,"B");
  assert.equal(controller.getSnapshot().error,null);
});

test("cache partition is bounded, memory-only metadata excluded from public session", () => {
  const value=normalizeSession(session("A"));
  assert.equal(value.cacheScope,"issuer/tenant/workspace/login");
  assert.equal("cacheScope" in publicSession(value),false);
  assert.throws(()=>normalizeSession(session("A",{cacheScope:"x".repeat(2049)})));
});
