# Claude conformance plan

The connector uses the canonical Task-064 conformance candidate with a
synthetic transport (`connectors/claude/conformance.go`) and does not perform
real network I/O during the suite. The retained report records all thirteen
SDK admission checks, including sandbox isolation when the Linux runner allows
the namespace probe.

Package tests additionally cover Anthropic's default host, `x-api-key` and
`anthropic-version` headers, the Messages API request shape, the optional host
override and rejection of a response without a text content block.
