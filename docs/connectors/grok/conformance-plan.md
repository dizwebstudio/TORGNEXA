# Grok conformance plan

The canonical Task-064 suite uses a synthetic transport and does not perform
network I/O. Package tests cover the xAI host, Bearer header, Chat Completions
request shape and response normalization. Production use requires a tenant-
owned xAI API key and a successful account health check.
