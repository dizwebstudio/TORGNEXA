# Roadmap

## MVP / architecture proof
Core catalog/offers/prices/stock/orders, Kafka outbox/inbox, Connector SDK, WB/Ozon, 1C/MoySklad, sync/reconciliation, REST/signed webhooks, n8n skeleton, MCP, audit/approval/secrets/privacy baseline, Compose, connector conformance.

## v1
Yandex Market + priority social (VK/TG/MAX), ClickHouse reporting, settlement ledger, import/export, PIM/MDM, notifications, procurement baseline, logistics/PUDO baseline, Chestny ZNAK read/status/reconciliation.

## v1.5
Remaining marketplace/classified/social connectors, Growth Engine, WMS, customer inbox/claims, EDO, signing/UKEP/MChD, payments, KKT/OFD, VetIS.

## v2 / Enterprise
HA/Kubernetes, advanced plugin isolation/marketplace, HSM/KMS, legal-party master data hardening, Product Compliance, EGAIS, Enterprise IAM (LDAP/AD/SAML/SCIM/JIT), SIEM export, Cloud billing lifecycle, reference acquiring/logistics connectors, upload security, FX provider, SMS and security-edge baseline, advanced regulated writes, full PUDO/WMS operations, DR, developer SDKs, SLO/performance gates, signed supply-chain releases and hardened upgrade framework.

Architecture pillars are frozen in `docs/54-architecture-freeze-v1.md`; roadmap items should extend through SDK/capabilities rather than provider-specific Core changes.
