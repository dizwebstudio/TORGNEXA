# Ollama conformance plan

The connector uses the canonical Task-064 conformance candidate with a
synthetic transport (`connectors/ai/ollama/conformance.go`); the suite performs no
network calls to an Ollama daemon. The retained report records all thirteen SDK
admission checks, including sandbox isolation.

Package tests cover the local base URL, bearer header, request path and
completion response shape. Runtime transport tests cover private-address
pinning and rejection of an external hostname.
