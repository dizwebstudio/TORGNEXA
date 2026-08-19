# ADR 0024: Product Compliance Is a Blocking Policy Domain

## Decision
Model compliance documents/requirements separately from marking. All publication/sale workflows can invoke an explainable compliance policy returning allow/warn/approval/block.

## Consequences
Connector code cannot bypass expired/missing required evidence; registry verification remains pluggable.
