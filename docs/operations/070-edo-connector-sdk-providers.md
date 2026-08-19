# Task 070 — EDO Connector SDK + Diadoc/Saby

Connector SDK v1 gains additive `EDODocumentReader`, `EDODocumentSender`, and `EDOSignWorkflowRequester` interfaces. The frozen root `Connector`/`Runtime` interfaces are unchanged.

`diadoc` and `saby-edo` adapters consume already signed artifact/signature references; private keys remain in Task 069. Local send state begins as submitted/sent, while later remote provider status is authoritative. Both providers pass Task-064 conformance.

Official references: Diadoc API document/status documentation at `https://developer.kontur.ru/doc/diadoc-api/`; Saby EDO API at `https://saby.ru/help/integration/api/edo`.
