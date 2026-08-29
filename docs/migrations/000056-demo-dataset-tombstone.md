# Demo dataset tombstone migration

Migration 000056 adds tenant-scoped `demo_dataset_tombstones` records. Removing
the Community demo dataset records a tombstone instead of rewriting immutable
order history; report/search queries use it to exclude the synthetic dataset.
The dataset currently contains 24 catalog products, each with a synthetic
description and a bundled demo image, plus the original five demo orders.

The table uses forced PostgreSQL RLS with organization and workspace scope.
Tombstones contain no customer PII and are retained with the tenant operational
metadata they govern.
