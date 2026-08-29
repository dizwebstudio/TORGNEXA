import test from "node:test";
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import {shouldInvalidateRealtimeEvent} from "../.repository-test/app/realtime-events.js";

test("realtime invalidates queries only for explicit change events", () => {
  assert.equal(shouldInvalidateRealtimeEvent("invalidate"), true);
  assert.equal(shouldInvalidateRealtimeEvent("heartbeat"), false);
  assert.equal(shouldInvalidateRealtimeEvent("ready"), false);
  assert.equal(shouldInvalidateRealtimeEvent(undefined), false);
});

test("realtime coalesces an event burst before invalidating queries", () => {
  const hook = readFileSync(new URL("../src/app/useRealtime.ts", import.meta.url), "utf8");
  assert.match(hook, /invalidationTimer/);
  assert.match(hook, /setTimeout\(\(\)=>\{invalidationTimer=0/);
  assert.match(hook, /},150\)/);
  assert.doesNotMatch(hook, /await cache\.invalidateQueries/);
});
