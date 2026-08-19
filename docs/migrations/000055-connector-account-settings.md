# Connector account settings migration

Migration 000055 extends the provider-neutral connector-account family check
with `crm` and creates the stable synthetic organization/workspace used only by
the Community development realm.

The synthetic identifiers are not a production tenant fallback. Production
scope continues to come from reviewed OIDC claims and explicit identity
mapping. Connector credentials remain encrypted behind the secrets provider;
this migration stores no credential material.

The migration is expand-only and keeps existing connector-account rows and
readers compatible.
