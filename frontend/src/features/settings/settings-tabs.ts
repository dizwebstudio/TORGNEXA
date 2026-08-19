export const settingsTabs = [
  {id: "general", label: "Основные"},
  {id: "identity", label: "Провайдеры входа"},
  {id: "notifications", label: "Каналы и важность"},
  {id: "mcp", label: "MCP-агенты"},
] as const;

export type SettingsTabID = typeof settingsTabs[number]["id"];

export function isSettingsTabID(value: string): value is SettingsTabID {
  return settingsTabs.some((tab) => tab.id === value);
}
