# Reconciliation

Shopware remote state is authoritative. TORGNEXA stores correlation/evidence and compares local projections with remote observations before accepting a write outcome; ambiguous transport failures (timeout/unavailable) trigger a re-fetch and reconcile rather than a blind retry, matching the same pattern as every other storefront connector in this repository. The Docker qualification smoke exercises this read-after-write path for product fields, price and stock, with automatic restoration of the synthetic fixture.
