# Task 149 — Ollama, LM Studio and Open WebUI local AI providers

Status: Repository implementation complete

## Problem

The AI provider settings accepted only hosted services. Teams running models on
their own workstation or VPS could not use Ollama, LM Studio, or an Open WebUI
gateway from the reports assistant.

## Scope

- add `ollama`, `lm-studio`, and `open-webui` as separate AI-provider cards;
- send the existing governed `ai.completion.generate` request through the
  OpenAI-compatible `/chat/completions` contract;
- keep local egress host-mediated, pinned to a private/loopback address, with a
  fixed hostname allowlist, no proxy use, no redirects and bounded bodies;
- allow only approved local HTTP base URLs while retaining HTTPS-only rules for
  hosted providers;
- update the AI settings form, reports provider selector, manifests, generated
  catalogs, migration, contracts, architecture evidence and conformance docs.

## Explicit exclusions

This task does not deploy model servers, download models, expose arbitrary LAN
discovery, enable streaming/tool calls, or add product/order synchronization.
The small production Compose profile remains model-service agnostic; operators
connect an already-running local service.

## Defaults

| Provider | Default base URL | Health/model hint |
|---|---|---|
| Ollama | `http://ollama:11434/v1` | `llama3.2` |
| LM Studio | `http://host.docker.internal:1234/v1` | `local-model` |
| Open WebUI | `http://open-webui:3000/api` | `local-model` |

The API key field is still required by the generic secret contract. Ollama and
LM Studio commonly accept any non-empty placeholder when authentication is
disabled; Open WebUI expects its bearer token.

## Acceptance criteria

- all three cards are visible in Settings → Integrations and the providers are
  selectable in Settings → AI providers and Reports → Ask AI;
- a tenant-scoped account can complete an approved prompt against each local
  endpoint without credentials or prompt material being logged or persisted;
- external HTTPS provider routing is unchanged and arbitrary HTTP/public/local
  hostnames are rejected;
- migration 000021, OpenAPI, runtime support, manifests and generated catalogs
  agree;
- connector, transport, API, frontend, contract and migration checks pass.
