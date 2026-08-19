# CBR FX conformance plan

The canonical Task-064 suite must pass all thirteen admission checks using synthetic XML fixtures. The no-auth provider uses only a synthetic test secret in the reusable sandbox fixture because the generic fixture validates secret isolation; the real manifest remains `auth:none` and the production connector receives no secret.

Semantic tests additionally cover exact historical lookup, nominal normalization, no implicit inversion, bounded XML size, source binding and host conversion from `FXRateObservation` to immutable `RateFact`.
