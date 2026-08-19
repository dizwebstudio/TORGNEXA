# Import / Export contracts

Task 031 defines the provider-neutral CSV/JSON product import skeleton.

The engine consumes only `uploads.ReleasedObjectRef`. Mapping IDs and versions are explicit; the SHA-256 of the released object and the mapping fingerprint are bound into preview/result reports. `Commit` accepts only an opaque prepared preview produced by the engine and re-verifies the released bytes before any catalog mutation.

The first vertical slice targets canonical Products (`product_id`, `code`, `title`, optional `description`). Provider columns and marketplace identifiers remain outside Core and are expressed only as mapping source field names.
