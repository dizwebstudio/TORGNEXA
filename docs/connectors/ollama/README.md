# Ollama

Ollama is available in the AI-provider catalog as a local, OpenAI-compatible
completion endpoint. Configure a running Ollama server and select the exact
model name installed there (for example `llama3.2`).

The default Docker Compose address is `http://ollama:11434/v1`. A loopback
address (`localhost`, `127.0.0.1`, or `::1`) is also accepted for a host-owned
deployment. TORGNEXA does not install Ollama or download models.

See [spec.md](spec.md), [capability-audit.md](capability-audit.md), and
[conformance-plan.md](conformance-plan.md).
