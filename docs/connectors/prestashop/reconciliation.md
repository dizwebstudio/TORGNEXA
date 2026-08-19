# PrestaShop reconciliation

Price, StockAvailable quantity and order-state writes are desired-state operations. The connector reads current state, suppresses duplicates, writes once, then verifies. Transport/unavailable ambiguity triggers one reconciliation read; an unprovable effect returns `write_outcome_unknown`.
