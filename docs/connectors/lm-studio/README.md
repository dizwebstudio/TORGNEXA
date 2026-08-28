# LM Studio

LM Studio is available as a local OpenAI-compatible AI provider. Start its
local server, load a model, and enter the model identifier shown by LM Studio.

The default address for the production API container is
`http://host.docker.internal:1234/v1`. On Linux Docker Compose this name is
mapped to the host gateway. Loopback addresses are also accepted when the API
process runs directly on the host. TORGNEXA does not install LM Studio or
manage model files.

See [spec.md](spec.md), [capability-audit.md](capability-audit.md), and
[conformance-plan.md](conformance-plan.md).
