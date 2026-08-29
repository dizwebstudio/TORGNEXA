export const capabilityLabels: Readonly<Record<string, string>> = {
  "ai.analyze": "Аналитические запросы ИИ",
  "approvals.read": "Просмотр согласований",
  "approvals.write": "Управление согласованиями",
  "audit.read": "Просмотр аудита",
  "cloud.subscription.read": "Просмотр подписки",
  "compliance.read": "Просмотр соответствия",
  "connectors.accounts.read": "Просмотр кабинетов интеграций",
  "connectors.accounts.write": "Управление кабинетами интеграций",
  "connectors.read": "Просмотр интеграций",
  "connectors.replay.run": "Запуск проверки интеграций",
  "counterparties.read": "Просмотр контрагентов",
  "fx.read": "Просмотр курсов валют",
  "notifications.read": "Просмотр уведомлений",
  "lineage.read": "Просмотр происхождения данных",
  "operations.realtime.read": "Просмотр оперативных обновлений",
  "orders.read": "Просмотр заказов",
  "orders.status.write": "Изменение статусов заказов",
  "plugins.read": "Просмотр плагинов",
  "privacy.requests.write": "Управление запросами приватности",
  "products.read": "Просмотр товаров",
  "products.write": "Управление товарами",
  "profitability.scenarios.write": "Запуск сценариев прибыльности",
  "reports.read": "Просмотр отчётов",
  "settings.ai_governance.read": "Просмотр политик ИИ",
  "settings.ai_governance.write": "Управление политиками ИИ",
  "settings.ai_providers.read": "Просмотр провайдеров ИИ",
  "settings.ai_providers.write": "Управление провайдерами ИИ",
  "settings.identity_providers.read": "Просмотр провайдеров входа",
  "settings.identity_providers.write": "Управление провайдерами входа",
  "settings.mcp_accounts.read": "Просмотр MCP-аккаунтов",
  "settings.mcp_accounts.write": "Управление MCP-аккаунтами",
  "settings.members.read": "Просмотр участников workspace",
  "settings.members.write": "Управление участниками workspace",
  "settings.read": "Просмотр настроек",
  "settings.security.read": "Просмотр безопасности",
  "settings.security.evidence.read": "Просмотр свидетельств безопасности",
  "settings.security.posture.read": "Просмотр состояния безопасности",
  "settings.security.write": "Управление безопасностью",
  "settlements.read": "Просмотр расчётов",
  "stock.read": "Просмотр остатков",
  "stock.write": "Управление остатками",
  "sync.read": "Просмотр синхронизации",
  "sync.write": "Управление синхронизацией",
  "webhooks.read": "Просмотр вебхуков",
  "webhooks.write": "Управление вебхуками",
};

export const roleLabels: Readonly<Record<string, string>> = {
  admin: "Администратор",
  manager: "Менеджер",
  operator: "Оператор",
  viewer: "Наблюдатель",
  "default-roles-torgnexa": "Базовые роли TORGNEXA",
  offline_access: "Автономный доступ",
  uma_authorization: "Авторизация доступа",
};

export type CapabilityGroupID = "commerce" | "integrations" | "control" | "workspace" | "other";

export function capabilityGroupFor(value: string): CapabilityGroupID {
  if (/^(products|orders|stock|settlements|fx|reports|profitability|counterparties)(\.|$)/.test(value)) return "commerce";
  if (/^(connectors|sync|webhooks|operations|plugins|cloud)(\.|$)/.test(value)) return "integrations";
  if (/^(approvals|audit|compliance|privacy|notifications|lineage)(\.|$)/.test(value) || value.startsWith("settings.security")) return "control";
  if (value.startsWith("ai.") || /^settings\.(ai_|mcp_|identity_|members|read)/.test(value)) return "workspace";
  return "other";
}

export const syncModeLabels: Readonly<Record<string, string>> = {
  incremental: "Инкрементальная",
  scheduled_full: "Полная сверка",
  on_demand: "По запросу",
};

export const syncActionLabels: Readonly<Record<string, string>> = {
  apply_local: "Применить локальное состояние",
  apply_remote: "Применить состояние кабинета",
  ignore: "Игнорировать",
  manual: "Проверить вручную",
};

export const connectorHealthLabels: Readonly<Record<string, string>> = {
  healthy: "Работает",
  degraded: "Ограничен",
  unavailable: "Недоступен",
  unknown: "Неизвестно",
};

export const workspaceStatusLabels: Readonly<Record<string, string>> = {
  active: "Активно",
  disabled: "Отключено",
  suspended: "Приостановлено",
  archived: "Архивировано",
};

export const bootstrapJobStatusLabels: Readonly<Record<string, string>> = {
  pending: "В очереди",
  running: "Выполняется",
  retry_wait: "Ожидает повтора",
  completed: "Завершена",
  failed: "Ошибка",
};

export function labelFor(value: string | undefined, labels: Readonly<Record<string, string>>): string {
  if (!value) return "—";
  return labels[value] ?? value;
}
