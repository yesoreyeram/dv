# 0003. Common data-frame model

- Status: Accepted
- Date: 2026-11-07
- Requirements: FR-010, NFR-PERF-003, NFR-I18N-002

## Context

Panels must render data from any source identically, so every source normalises into one
in-memory model.

## Decision

- A `Frame` is **columnar**: `Name`, ordered `Fields`, and `Meta` (source, executed query,
  notices/warnings, row-limit-hit flag).
- A `Field` has `Name`, `Type`, a typed value vector, nullable flags and display `Config`
  (unit, decimals, display name).
- Types: `time` (UTC, ns precision), `number` (float64; int64 kept losslessly where the
  source provides it), `string`, `bool`, `json` (opaque, for nested values).
- Invariant: all fields of a frame have the same length; constructors enforce it.
- Wire format: JSON `{schema:{name,fields:[{name,type,config}]}, data:{values:[[…],…]}}`
  (column-major) for the browser; Arrow IPC may be added later for large results.
- Times transported as RFC 3339 / epoch ms in UTC; formatting is the client's job.
- Limits (rows, bytes) are enforced while building frames, not after.

## Consequences

- Columnar layout maps directly to chart series and is cache friendly.
- Mixed-type columns (common in JSON/CSV) are coerced by type inference with explicit
  per-field overrides; un-coercible values become null plus a frame notice.

## Implementation notes (2026-11-07, FR-010)

- Package `internal/frame`. Values are stored per field as `[]any` holding the Go type for
  the field type (`time.Time`, `float64`/`int64`, `string`, `bool`, `json.RawMessage`) or
  `nil` for null. Generic typed vectors were considered; `[]any` keeps the API small while
  types are enforced on every append. Revisit with Arrow if profiling demands it.
- Wire times are **UTC epoch milliseconds** (integers). Sub-millisecond precision is not
  transported; this matches browser `Date` precision.
- `int64` values are encoded exactly; JavaScript clients lose precision beyond ±2^53.
- `NaN`/`±Inf` are stored as null because JSON cannot represent them.
- `Builder` limits: `MaxRows` and `MaxBytes` (estimated: 8 B per number/time/bool, length
  of strings/JSON + 16 B overhead). Hitting a limit returns `frame.ErrLimitReached`, sets
  `Meta.Truncated` and appends a `warning` notice; the rows already added are kept.
