# 0005. Observability: slog now, OpenTelemetry seam

- Status: Accepted
- Date: 2026-11-07
- Requirements: NFR-OBS-001…NFR-OBS-004

## Context

We want OpenTelemetry traces and metrics, but the OTel Go SDK is a sizeable dependency tree
and has no value until there are data-source calls worth tracing.

## Decision

- Structured logging with `log/slog`: JSON handler in production, text in development,
  level configurable. One access-log record per request (method, route pattern, status,
  bytes, duration, request ID, trace ID).
- W3C `traceparent` is parsed by middleware; a valid trace ID is placed in the request
  context and added to log records, so logs correlate with upstream traces today.
- All instrumentation goes through `internal/httpx` middleware and context helpers. The
  OTel SDK (OTLP exporter, `otelhttp`-equivalent spans, RED metrics) is added in M7 by
  wrapping the handler chain — no handler changes needed.

## Consequences

- Distributed tracing is partial until M7 (trace IDs propagate into logs, but dv emits no
  spans yet). This is stated in NFR-OBS-003 status.
