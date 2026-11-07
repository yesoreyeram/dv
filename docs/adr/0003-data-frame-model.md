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
