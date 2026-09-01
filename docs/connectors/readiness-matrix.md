# Connector readiness matrix

Generated from all repository manifests, runtime support and redacted conformance reports.
The matrix is descriptive evidence; credentials and provider payloads are never copied here.

## Summary

| Status | Count |
|---|---:|
| `health_only` | 16 |
| `manifest_only` | 2 |
| `partially_supported` | 11 |
| `read_only` | 14 |
| `ready` | 18 |

## All connectors

| Connector | Family | Status | Owner | Priority | Decision | Next action |
|---|---|---|---|---|---|---|
| `aliexpress-ru` | marketplace | `ready` | commerce-integrations | P0 | `qualify` | Подтвердить каждую заявленную capability credentialed sandbox/live evidence |
| `auto-ru` | classified | `health_only` | channel-operations | P2 | `deepen` | Выбрать минимальный business read-срез и провести qualification wave |
| `avito` | classified | `health_only` | channel-operations | P2 | `deepen` | Выбрать минимальный business read-срез и провести qualification wave |
| `bitrix` | storefront | `ready` | commerce-integrations | P0 | `qualify` | Подтвердить каждую заявленную capability credentialed sandbox/live evidence |
| `bitrix24` | crm | `partially_supported` | channel-operations | P2 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `cbr-fx` | fx | `read_only` | finance | P1 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `cdek` | logistics | `partially_supported` | logistics | P1 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `chestny-znak` | government | `health_only` | compliance | P1 | `keep_health_only` | Оставить health-only и документировать специализированное назначение |
| `cian` | classified | `health_only` | channel-operations | P2 | `deepen` | Выбрать минимальный business read-срез и провести qualification wave |
| `claude` | ai | `read_only` | ai-platform | P3 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `cs-cart` | storefront | `ready` | commerce-integrations | P0 | `qualify` | Подтвердить каждую заявленную capability credentialed sandbox/live evidence |
| `deepseek` | ai | `read_only` | ai-platform | P3 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `dellin` | logistics | `partially_supported` | logistics | P1 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `diadoc` | edo | `health_only` | compliance | P1 | `keep_health_only` | Оставить health-only и документировать специализированное назначение |
| `dolyami` | payment | `health_only` | finance | P1 | `keep_health_only` | Оставить health-only и документировать специализированное назначение |
| `egais` | government | `health_only` | compliance | P1 | `keep_health_only` | Оставить health-only и документировать специализированное назначение |
| `fivepost` | logistics | `partially_supported` | logistics | P1 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `gemini` | ai | `read_only` | ai-platform | P3 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `gigachat` | ai | `read_only` | ai-platform | P3 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `grok` | ai | `read_only` | ai-platform | P3 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `instagram` | social | `health_only` | channel-operations | P2 | `keep_health_only` | Оставить health-only и документировать специализированное назначение |
| `kimi` | ai | `read_only` | ai-platform | P3 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `lamoda` | marketplace | `health_only` | commerce-integrations | P0 | `deepen` | Выбрать минимальный business read-срез и провести qualification wave |
| `lm-studio` | ai | `read_only` | ai-platform | P3 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `magento` | storefront | `ready` | commerce-integrations | P0 | `qualify` | Подтвердить каждую заявленную capability credentialed sandbox/live evidence |
| `magnit-market` | marketplace | `ready` | commerce-integrations | P0 | `qualify` | Подтвердить каждую заявленную capability credentialed sandbox/live evidence |
| `max-messenger` | social | `partially_supported` | channel-operations | P2 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `medusa` | storefront | `ready` | commerce-integrations | P0 | `qualify` | Подтвердить каждую заявленную capability credentialed sandbox/live evidence |
| `megamarket` | marketplace | `ready` | commerce-integrations | P0 | `qualify` | Подтвердить каждую заявленную capability credentialed sandbox/live evidence |
| `moysklad` | erp | `ready` | commerce-integrations | P0 | `qualify` | Подтвердить каждую заявленную capability credentialed sandbox/live evidence |
| `mvideo` | marketplace | `health_only` | commerce-integrations | P0 | `deepen` | Выбрать минимальный business read-срез и провести qualification wave |
| `odnoklassniki` | social | `health_only` | channel-operations | P2 | `keep_health_only` | Оставить health-only и документировать специализированное назначение |
| `ollama` | ai | `read_only` | ai-platform | P3 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `onec` | erp | `ready` | commerce-integrations | P0 | `qualify` | Подтвердить каждую заявленную capability credentialed sandbox/live evidence |
| `open-webui` | ai | `read_only` | ai-platform | P3 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `openai-compatible` | ai | `read_only` | ai-platform | P3 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `opencart` | storefront | `ready` | commerce-integrations | P0 | `qualify` | Подтвердить каждую заявленную capability credentialed sandbox/live evidence |
| `ozon` | marketplace | `ready` | commerce-integrations | P0 | `qualify` | Подтвердить каждую заявленную capability credentialed sandbox/live evidence |
| `ozon-delivery` | logistics | `manifest_only` | logistics | P1 | `specialized_surface` | Принять решение по runtime-поверхности или вывести из каталога |
| `ozon-pay` | payment | `manifest_only` | finance | P1 | `specialized_surface` | Принять решение по runtime-поверхности или вывести из каталога |
| `pek` | logistics | `partially_supported` | logistics | P1 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `pochta-russia` | logistics | `partially_supported` | logistics | P1 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `prestashop` | storefront | `ready` | commerce-integrations | P0 | `qualify` | Подтвердить каждую заявленную capability credentialed sandbox/live evidence |
| `qwen` | ai | `read_only` | ai-platform | P3 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `robokassa` | payment | `partially_supported` | finance | P1 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `rutube` | social | `health_only` | channel-operations | P2 | `keep_health_only` | Оставить health-only и документировать специализированное назначение |
| `saby-edo` | edo | `health_only` | compliance | P1 | `keep_health_only` | Оставить health-only и документировать специализированное назначение |
| `saleor` | storefront | `ready` | commerce-integrations | P0 | `qualify` | Подтвердить каждую заявленную capability credentialed sandbox/live evidence |
| `sbp` | payment | `partially_supported` | finance | P1 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `shopify` | storefront | `ready` | commerce-integrations | P0 | `qualify` | Подтвердить каждую заявленную capability credentialed sandbox/live evidence |
| `shopware` | storefront | `ready` | commerce-integrations | P0 | `qualify` | Подтвердить каждую заявленную capability credentialed sandbox/live evidence |
| `telegram` | social | `partially_supported` | channel-operations | P2 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `threads` | social | `health_only` | channel-operations | P2 | `keep_health_only` | Оставить health-only и документировать специализированное назначение |
| `vetis-mercury` | government | `health_only` | compliance | P1 | `keep_health_only` | Оставить health-only и документировать специализированное назначение |
| `vk` | social | `read_only` | channel-operations | P2 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `wildberries` | marketplace | `ready` | commerce-integrations | P0 | `qualify` | Подтвердить каждую заявленную capability credentialed sandbox/live evidence |
| `woocommerce` | storefront | `ready` | commerce-integrations | P0 | `qualify` | Подтвердить каждую заявленную capability credentialed sandbox/live evidence |
| `yandex-market` | marketplace | `ready` | commerce-integrations | P0 | `qualify` | Подтвердить каждую заявленную capability credentialed sandbox/live evidence |
| `yandexgpt` | ai | `read_only` | ai-platform | P3 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `yookassa` | payment | `partially_supported` | finance | P1 | `deepen` | Закрыть недостающие capability tests и read-after-write evidence |
| `youtube` | social | `health_only` | channel-operations | P2 | `keep_health_only` | Оставить health-only и документировать специализированное назначение |

`ready` means repository runtime support is present, not that a provider write is production-qualified.
`qualified` is intentionally absent until retained credentialed evidence exists for the exact capability.
