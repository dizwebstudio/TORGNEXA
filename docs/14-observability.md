# Observability

OpenTelemetry for correlation. Track HTTP latency/errors, connector remote latency/rate-limits, Kafka lag, outbox age, scheduler lag, sync latency, reconciliation drift, publication failures, approvals, signing/EDO failures and reporting freshness.

Structured logs carry request/correlation/event IDs and redact secrets. Dashboards cover API, Kafka/outbox, connectors, sync, social, compliance and analytics freshness.

Process logs use stable `event` and safe `error_code` attributes. Raw process errors, panic values, HTTP diagnostics, headers and arbitrary structured payloads are not log contracts: complex `slog.Any` values are denied by default, while explicitly grouped/scalar attributes pass key-aware credential redaction. Successful liveness probes are not access-logged at info level.
