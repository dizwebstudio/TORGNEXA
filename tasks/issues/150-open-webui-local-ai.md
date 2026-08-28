# Task 150 — Open WebUI local AI gateway evidence

Status: Repository implementation complete

Task 150 records the Open WebUI portion of the local AI provider rollout. It
uses the same tenant-scoped AI settings and governed non-streaming completion
route as Task 149, with the Open WebUI `/api/chat/completions`-compatible base
path and bearer token. Egress is restricted to the reviewed local transport;
automatic gateway or model deployment is intentionally excluded.

Acceptance: Open WebUI is selectable in AI settings and Reports → Ask AI, its
manifest/runtime support/catalog and conformance evidence are synchronized,
and arbitrary public HTTP destinations remain rejected.
