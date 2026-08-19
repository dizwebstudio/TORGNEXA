import test from "node:test";
import assert from "node:assert/strict";
import {isSettingsTabID, settingsTabs} from "../.repository-test/features/settings/settings-tabs.js";

test("notification preferences have a dedicated settings tab", () => {
  assert.deepEqual(settingsTabs, [
    {id: "general", label: "Основные"},
    {id: "identity", label: "Провайдеры входа"},
    {id: "notifications", label: "Каналы и важность"},
  ]);
  assert.equal(isSettingsTabID("notifications"), true);
  assert.equal(isSettingsTabID("security"), false);
});
