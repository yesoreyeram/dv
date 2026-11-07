# 0002. Data-source and format plugin model

- Status: Accepted
- Date: 2026-11-07
- Requirements: FR-010…FR-062, NFR-AVAIL-003, NFR-SEC-004/005/006

## Context

dv connects to SQL databases, HTTP APIs and file/object stores, and decodes many formats.
These concerns are orthogonal: a CSV can come from a local file, S3, or an HTTP endpoint.

## Decision

Two plugin kinds, both compiled in and registered at start-up (no dynamic `.so` loading —
Go plugins are fragile and widen the attack surface):

```go
// Source fetches data. Implementations: postgres, mysql, sqlite, rest, graphql, file, s3.
type Source interface {
    Meta() SourceMeta                                     // id, name, capabilities, config schema
    Validate(ctx context.Context, cfg SourceConfig) error // static + connectivity check
    Query(ctx context.Context, q Query) (*frame.Result, error)
    Health(ctx context.Context) error
}

// Format decodes a byte stream into frames. Implementations: csv, tsv, json, ndjson,
// parquet, xml, yaml, excel.
type Format interface {
    Meta() FormatMeta // id, media types, extensions
    Decode(ctx context.Context, r io.Reader, opts DecodeOptions) ([]*frame.Frame, error)
}
```

- Byte-oriented sources (file, S3, REST) delegate decoding to a `Format`.
- Every call takes a `context.Context`; the host applies timeouts, row/byte limits and
  cancellation uniformly.
- Configuration is split into `JSONData` (non-secret, returned to clients) and
  `SecureJSONData` (secret, encrypted at rest, write-only — see ADR-0004).
- Plugins never open network connections themselves: they receive a host-provided
  `Dialer`/`http.Client` that enforces the SSRF policy, and a host-provided `os.Root` for
  file access. This keeps security controls in one place.
- A registry exposes plugin metadata to the UI so editors are generated from schemas.

## Consequences

- Adding a source = implement the interface + register + BDD scenarios.
- Out-of-process plugins (gRPC) can be added later behind the same interface.

## Implementation notes (2026-11-07, FR-015)

- Package `internal/plugin`. `Health` takes the instance `SourceConfig`
  (`Health(ctx, cfg)`), because plugin types are stateless and health is per instance.
- The registry is frozen after start-up (`Freeze`); the `plugins` readiness check fails
  until then, and later `Register*` calls return `ErrFrozen`.
- `SecureJSONData` is the `SecureValues` type: JSON shows only `{key: true}`
  (`secureJsonFields`), `fmt` and `slog` print `[REDACTED]`.
- The config schema in metadata is deferred to B-038; the host-provided `os.Root` and
  HTTP client arrive with the first plugin that needs them (B-037).
