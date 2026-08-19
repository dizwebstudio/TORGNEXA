# Task 069 — Signing, УКЭП and МЧД foundation

Signing is isolated behind `IsolatedSigner`. The host submits an artifact reference and SHA-256 digest, certificate ID, optional МЧД reference, purpose, approval reference and idempotency key. A private key, token PIN, CSP handle bytes, or certificate-container material is never part of the generic request/result/event shape.

Certificate metadata and МЧД authority references are durable; signing evidence is append-only. Signing requires explicit approval. МЧД is represented by registry/authority references and powers, not by copying an uncontrolled XML document into generic events.

FNS describes MЧД as a machine-readable electronic power of attorney signed with a qualified electronic signature; the current FNS service advertises format 5.01. Official references: `https://m4d.nalog.gov.ru/` and FNS guidance on MЧД.
