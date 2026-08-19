# Import / Export Engine

## Task 031 repository slice

Task 031 implements the first provider-neutral CSV/JSON pipeline for canonical Products:

`released object -> integrity check -> parse -> mapping -> validation -> dedupe -> preview -> commit -> result report`

The input boundary is deliberately non-bypassable: the engine accepts only `uploads.ReleasedObjectRef` from the Task-088 access gate. It has no API for a raw object key, quarantine key, client filename, URL or arbitrary `io.Reader`. Before parsing, it reads the bounded released object and verifies its exact size and SHA-256; immediately before commit it re-opens and re-verifies that same release reference, so mutated storage cannot become catalog writes.

### Formats and limits

The repository slice supports CSV and a JSON array of flat scalar objects. CSV headers and JSON source property names are mapped to canonical fields. Default limits are 32 MiB per import, 10,000 rows, 64 columns/properties and 100 returned validation errors. Larger/other formats remain later extensions.

### Mapping

Mappings are reusable/versioned values. A mapping has a stable ID, monotonically explicit version, input format, and canonical-target-to-source-field bindings. Product imports require `product_id`, `code`, and `title`; `description` is optional. The engine computes a deterministic SHA-256 mapping fingerprint and binds it into both preview and commit reports.

### Preview and commit

Preview validates canonical Product IDs/text rules, missing/invalid cells, duplicate product IDs and duplicate codes within the file. Commit is fail-closed when any preview row is invalid. `PreparedImport` has no exported fields, so callers cannot manufacture a successful preview token.

Commit creates canonical draft Products through the existing Catalog port, preserving Catalog's transactional event semantics. Replaying the same prepared import is idempotent for already-created identical Products: exact matches are reported as `unchanged`; conflicting existing data is a bounded `commit_failed` row error. The released source digest is re-verified before writes.

### Export

Task 031 also provides deterministic canonical Product encoding in CSV or JSON. This is a platform encoder only; provider-specific feeds, XLSX/XML/YML, SFTP/HTTPS/S3 delivery, durable mapping persistence, incremental imports, bidirectional remote export, approval thresholds and AI mapping suggestions remain future tasks/extensions.

### Security boundary with Task 088

Stage `088a` deliberately could not create a usable `ReleasedObjectRef`. Stage `088b` now supplies MIME/archive/parser/malware evidence and authorized release. Task `031` therefore accepts only a current security-evidenced reference and revalidates it immediately before every released-object read; a re-scan revokes stale references before preview or commit can continue.
