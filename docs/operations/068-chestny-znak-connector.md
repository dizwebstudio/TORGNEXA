# Task 068 — Честный ЗНАК connector baseline

The baseline is intentionally read/status-first. `chestny-znak` exposes `marking.status.read`, `government.references.read`, and `government.reconciliation.run`; regulated document writes stay disabled until a separately qualified write flow exists.

The official ГИС МТ/True API documentation exposes product lookup by GTIN and code-information/status methods. TORGNEXA therefore treats the remote status as authoritative and stores only SHA-256 code fingerprints in generic reconciliation evidence. Raw Data Matrix/code-identification strings are request-scoped and must not enter logs, audit text, analytics, or generic events.

Official references: `https://docs.crpt.ru/gismt/` and the True API product/code information methods documented there, including `/api/v4/true-api/product/info` and code-information APIs.
