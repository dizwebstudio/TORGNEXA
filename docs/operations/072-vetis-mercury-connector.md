# Task 072 — VetIS/Mercury connector

`vetis-mercury` implements read document/status, approval-gated regulated writes, inventory reads and reconciliation through the Government Connector family. No write request is accepted without an approval reference. Remote Mercury state is authoritative.

The adapter deliberately uses an injected typed transport rather than hard-coding SOAP credentials/endpoints into business modules. Official Vetis.API documentation publishes the Mercury G2B integration profile and XSD namespaces; production transport must bind to the organization-approved Vetis.API account and current schema version.

Official reference: `https://help.vetrf.ru/wiki/MercuryG2B:Services:v3.0` and `https://help.vetrf.ru/wiki/Компонент_Ветис.API`.
