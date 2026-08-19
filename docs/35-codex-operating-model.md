# Codex Operating Model

Codex works task-first from `tasks/issues/` and obeys `AGENTS.md`.

Use repo skills for repeated work: connectors, Kafka events, DB migrations, privacy, growth, WMS, settlements, conformance, release security, SRE/performance, upgrades, developer platform and AI-agent governance.

Before adding a provider, Codex creates/updates a Connector Spec, capability
audit, manifest, and conformance plan, and first verifies that provider
admission is enabled. Before changing protected Core/Platform/process paths,
run `prompts/05-architecture-gap-audit.txt` and add the exact structured review.
A frozen pillar, architecture gate, or mixed provider/Core change also adds a
new/superseding ADR including compatibility, migration/data, security/privacy,
and operational impact.

Do not implement adjacent tasks implicitly. Report missing follow-ups into the backlog/task graph.
