# Migration 000025 — upload security PostgreSQL 18 compatibility

The upload security evidence trigger used `jsonb_object_length()`, which is
not available in the PostgreSQL 18 runtime used by TORGNEXA. This migration
replaces only the trigger body and counts object keys with
`jsonb_object_keys()`, preserving the exact two-field evidence contract.
