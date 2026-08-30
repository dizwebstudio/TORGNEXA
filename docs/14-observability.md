# Observability

OpenTelemetry for correlation. Track HTTP latency/errors, connector remote latency/rate-limits, Kafka lag, outbox age, scheduler lag, sync latency, reconciliation drift, publication failures, approvals, signing/EDO failures and reporting freshness.

Structured logs carry request/correlation/event IDs and redact secrets. Dashboards cover API, Kafka/outbox, connectors, sync, social, compliance and analytics freshness.

## Workflow automation (Task 163)

The workflow control plane exposes the same correlation/causation context as
the event platform. Instrument the scheduler, workflow event consumer and
worker with these stable dimensions (never with definition/config/payload):

- `workflow.trigger.lag` — `occurred_at` to durable run creation;
- `workflow.runs.queued`, `workflow.runs.running` and
  `workflow.runs.waiting` — gauges by tenant and status;
- `workflow.step.duration` and `workflow.retry.age` — histograms by action,
  outcome and machine error code;
- `workflow.approval.wait_age` — age of runs waiting on Task-017;
- `workflow.failure.total`, `workflow.dlq.total` and
  `workflow.fanout.total` — counters for recovery and saturation alerts;
- `workflow.quota.active`, `workflow.quota.runs_per_minute` and
  `workflow.quota.concurrent` — tenant-local quota gauges.

The v1 guardrails are 100 active workflows per workspace, 120 new runs per
minute, eight concurrently claimed runs, 64 nodes/128 edges per definition and
eight attempts per step. Alert separately on `waiting_approval`,
`waiting_retry`, `retry_exhausted`, `adapter_unavailable` and permanent action
failures; combining them into one error rate hides the operator action needed.
The `/workflow-runs/{id}/steps` and `/evidence` APIs are the bounded source for
operator timelines. They expose IDs, timestamps, digests and machine codes
only, so dashboards and logs must not add raw event or provider responses.

Process logs use stable `event` and safe `error_code` attributes. Raw process errors, panic values, HTTP diagnostics, headers and arbitrary structured payloads are not log contracts: complex `slog.Any` values are denied by default, while explicitly grouped/scalar attributes pass key-aware credential redaction. Successful liveness probes are not access-logged at info level.
