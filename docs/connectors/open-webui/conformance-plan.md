# Open WebUI conformance plan

The canonical Task-064 candidate (`connectors/ai/open-webui/conformance.go`) uses
a deterministic fixture transport and performs no call to a real gateway. The
retained report records all thirteen SDK admission checks, including sandbox
isolation. Package tests cover the `/api` base path, bearer token and response
mapping; host tests cover local-only egress.
