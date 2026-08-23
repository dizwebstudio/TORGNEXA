import test from "node:test";
import assert from "node:assert/strict";
import {shouldInvalidateRealtimeEvent} from "../.repository-test/app/realtime-events.js";

test("realtime invalidates queries only for explicit change events", () => {
  assert.equal(shouldInvalidateRealtimeEvent("invalidate"), true);
  assert.equal(shouldInvalidateRealtimeEvent("heartbeat"), false);
  assert.equal(shouldInvalidateRealtimeEvent("ready"), false);
  assert.equal(shouldInvalidateRealtimeEvent(undefined), false);
});
