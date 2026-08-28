# Open WebUI

Open WebUI is available as a local AI gateway. Configure a running Open WebUI
instance and select a model that the gateway exposes.

The default Docker Compose address is `http://open-webui:3000/api`; the adapter
calls `/chat/completions` below that base path. Supply an Open WebUI API token
in the credential field. TORGNEXA does not deploy Open WebUI or its backing
model provider.

See [spec.md](spec.md), [capability-audit.md](capability-audit.md), and
[conformance-plan.md](conformance-plan.md).
