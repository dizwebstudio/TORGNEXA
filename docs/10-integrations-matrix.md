# Integration Matrix

| Family | Targets | Initial focus |
|---|---|---|
| Marketplace | Wildberries, Ozon, Yandex Market | products/offers, prices, stock, orders, finance/reports, feedback/ads by capability |
| Marketplace | Megamarket, Magnit Market, AliExpress RU | current capability audit/spec before implementation |
| Classified | Avito | listings, leads/messages, stats where officially permitted |
| Vertical | Auto.ru, CIAN | partner/feed/API audit; Vehicle/Property mapping |
| Social | VK, Telegram, MAX, Instagram, Threads, OK, Rutube, Dzen, YouTube | content/media/comments/analytics by capability |
| ERP | 1C, MoySklad | catalog, stock, orders, shipments, returns, finance mapping |
| Government | Chestny ZNAK, VetIS/Mercury, EGAIS | status/docs/reconciliation; regulated writes phased and approved |
| EDO | Diadoc, Saby | provider SDK, document/status/signing workflow |
| Payments | SBP + reference card/acquirer + plugin acquirers | payment/status/webhook/refund/commission/reconciliation |
| Logistics | reference carrier + carrier plugins/PUDO | rate/create/cancel/track/label/return/capacity/issue |
| Notifications | Email/TG/MAX/Webhook/n8n/SMS providers | transactional delivery/status/fallback with policy |
| Enterprise IAM/SIEM | LDAP/AD, SAML, SCIM, JIT; syslog/webhook/Kafka/OTLP | federation/provisioning and security-event export |
| Developer | n8n, MCP/OpenClaw, REST/webhooks | external automation and agents with scopes |

Each connector starts with a current Connector Spec using `templates/connector-spec.md`, passes `contracts/conformance/connector-conformance.yaml`, and declares unsupported capabilities explicitly. Browser automation/scraping is never the default integration method.
