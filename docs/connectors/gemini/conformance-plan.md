# Google Gemini conformance plan

The canonical Task-064 suite uses a synthetic transport and does not perform
network I/O. Package tests cover the Gemini endpoint path, API-key header,
response normalization and model fallback. Production use still requires a
tenant-owned API key and a successful account health check.
