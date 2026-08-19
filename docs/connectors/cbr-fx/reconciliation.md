# CBR FX reconciliation

The host persists every accepted observation before selection. Resolution evidence records the requested pair/type/as-of, complete configured source precedence, all persisted candidate fact IDs, the selected fact ID and resolution time.

Cache entries contain only persisted fact IDs. A cache hit is reloaded from immutable storage and rechecked against freshness. Missing or stale facts never fall back to an unrecorded process-local value.
