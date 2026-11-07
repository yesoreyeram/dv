# Functional requirements

Every requirement has a stable ID (`FR-xxx`), a MoSCoW priority, a target milestone and
acceptance criteria. IDs are never reused. Gherkin scenarios in `features/` reference the
IDs they cover with tags (e.g. `@FR-001`). A PR must list the IDs it implements.

Status values: `Planned`, `In progress`, `Done` (implemented, tested, merged).

| Priority | Meaning |
| --- | --- |
| **M**ust | Required for the milestone to ship |
| **S**hould | High value; ship unless blocked |
| **C**ould | Nice to have |
| **W**on't (now) | Explicitly out of scope for the stated milestone |

## Summary

| ID | Title | Priority | Milestone | Status |
| --- | --- | --- | --- | --- |
| FR-001 | Liveness endpoint | M | M0/M1 | In progress |
| FR-002 | Readiness endpoint | M | M0/M1 | In progress |
| FR-003 | Build/version information endpoint | M | M0/M1 | In progress |
| FR-004 | Machine-readable API description (OpenAPI 3.1) | M | M0/M1 | In progress |
| FR-005 | Application shell (navigation, layout, theming) | M | M0/M1 | In progress |
| FR-006 | System status view in the UI | S | M0/M1 | In progress |
| FR-007 | API reference view in the UI | C | M0/M1 | In progress |
| FR-010 | Common data-frame model | M | M2 | In progress |
| FR-015 | Plugin registry and plugin catalogue | M | M2 | In progress |
| FR-011 | CSV / TSV file source | M | M2 | In progress |
| FR-012 | JSON file source | M | M2 | Planned |
| FR-013 | Table panel | M | M2 | Planned |
| FR-014 | Time-series line chart panel | M | M2 | Planned |
| FR-020 | Local user accounts & sessions | M | M3 | Planned |
| FR-021 | Role-based access control (Viewer / Editor / Admin) | M | M3 | Planned |
| FR-022 | Encrypted data-source secrets | M | M3 | Planned |
| FR-030 | PostgreSQL source | M | M4 | Planned |
| FR-031 | MySQL source | M | M4 | Planned |
| FR-032 | SQLite source | S | M4 | Planned |
| FR-033 | Safe SQL query editor | M | M4 | Planned |
| FR-040 | REST/JSON API source | M | M5 | Planned |
| FR-041 | GraphQL API source | S | M5 | Planned |
| FR-050 | Dashboards: create, save, load | M | M6 | Planned |
| FR-051 | Dashboard variables | S | M6 | Planned |
| FR-052 | Dashboard time range | M | M6 | Planned |
| FR-060 | NDJSON, XML, YAML formats | S | M7 | Planned |
| FR-061 | Parquet and Excel formats | S | M7 | Planned |
| FR-062 | Object-storage source (S3-compatible) | S | M7 | Planned |
| FR-063 | Query result caching | S | M7 | Planned |
| FR-064 | Audit log viewer | S | M7 | Planned |

## Details

### FR-001 — Liveness endpoint (M)
The server exposes `GET /healthz` that reports whether the process is alive, without
touching dependencies.
- AC1: returns `200` with JSON `{"status":"ok"}` while the process is serving.
- AC2: requires no authentication and leaks no version, host or dependency details.
- AC3: response carries the standard security headers (NFR-SEC-007).

### FR-002 — Readiness endpoint (M)
`GET /readyz` reports whether the server can accept traffic, aggregating registered
readiness checks (database, plugin registry, … as they are added).
- AC1: returns `200` `{"status":"ready","checks":{…}}` when all checks pass.
- AC2: returns `503` `{"status":"not_ready",…}` when any check fails or during shutdown.
- AC3: each check is time-boxed (default 2 s) and honours request cancellation.
- AC4: check failure details are reduced to `pass`/`fail`; error text is logged server-side, never returned.

### FR-003 — Build/version information (M)
`GET /api/v1/version` returns `{"version","commit","buildDate","goVersion"}` injected at build time.
- AC1: defaults to `version: "dev"` when not injected.
- AC2: JSON content type, `Cache-Control: no-store`.

### FR-004 — OpenAPI description (M)
`GET /api/openapi.json` serves an OpenAPI 3.1 document describing every public endpoint.
- AC1: the document is valid JSON, `openapi` starts with `3.1`.
- AC2: a unit test fails if a registered route is missing from the document.

### FR-005 — Application shell (M)
A React single-page application provides the product frame.
- AC1: persistent navigation with Home, Explore, Dashboards, Data sources, API reference.
- AC2: active route is indicated visually and via `aria-current="page"`.
- AC3: "Skip to main content" link is the first focusable element.
- AC4: light/dark theme toggle; initial theme follows `prefers-color-scheme`; choice persists per browser.
- AC5: unknown routes render a "Page not found" view with a link home.
- AC6: features not yet built show an explicit "planned for milestone Mx" empty state — never fake data.

### FR-006 — System status view (S)
The Home page shows server liveness/readiness and build version, fetched via TanStack Query.
- AC1: shows "Operational" when `/readyz` returns 200, "Degraded" otherwise.
- AC2: shows the server version and commit.
- AC3: while loading shows an accessible busy state; on network error shows an alert with retry.

### FR-007 — API reference view (C)
The UI lists endpoints from `/api/openapi.json` (method, path, summary).
- AC1: every path/method in the spec is rendered.
- AC2: rendering uses text only (no HTML injection from the spec).

### FR-010 — Common data-frame model (M)
Every source normalises its result into `internal/frame` (ADR-0003) so panels render data
from any source identically.
- AC1: a frame is columnar: a name, ordered fields and metadata; each field has a name, a
  type (`time`, `number`, `string`, `bool`, `json`), nullable values and display config
  (unit, decimals, display name).
- AC2: all fields of a frame have the same length; constructing or appending a row that
  would break this is rejected with an error, never a panic.
- AC3: a value of the wrong type for its field is rejected with an error naming the field.
  `NaN` and `±Inf` numbers are stored as null (JSON cannot represent them).
- AC4: frames encode to the column-major JSON wire format
  `{"schema":{"name","meta","fields":[{"name","type","config"}]},"data":{"values":[[…]]}}`;
  times are UTC epoch milliseconds, nulls are JSON `null`. Decoding the wire format
  validates types and lengths and round-trips losslessly (at millisecond precision).
- AC5: a frame builder enforces configurable row and byte limits *while* rows are added
  (NFR-PERF-003); hitting a limit stops the build, sets `meta.truncated` and adds a notice.
- AC6: field names must be non-empty, at most 256 bytes and unique within a frame.
- AC7: the TypeScript client decodes the wire format into typed columns (times as epoch
  ms numbers) and rejects malformed payloads.

### FR-015 — Plugin registry and catalogue (M)
Data-source and format plugins (ADR-0002) are compiled in and registered at start-up.
- AC1: `Source` and `Format` interfaces exist as specified in ADR-0002; every method takes
  a `context.Context`.
- AC2: plugin IDs match `^[a-z][a-z0-9-]{1,62}$`; duplicate or invalid IDs are rejected at
  registration.
- AC3: the registry is frozen once start-up completes; later registration fails. The
  `plugins` readiness check fails until the registry is frozen.
- AC4: `GET /api/v1/plugins` returns `{"sources":[…],"formats":[…]}` sorted by ID with only
  public metadata (id, name, description, capabilities / media types, extensions).
- AC5: secret source settings (`SecureJSONData`) are write-only: JSON encoding exposes only
  which keys are set, and `slog` output shows `[REDACTED]` (NFR-SEC-002).
- AC6: the Data sources page lists the registered plugin types from the API, and shows an
  explicit empty state when none are registered (no fake data, FR-005 AC6).

### FR-011 — CSV / TSV file source (M)
Operators expose directories of files to dv; editors query CSV/TSV files in them as frames.
- AC1: operators configure named file roots (`fileRoots` in the config file or
  `DV_FILE_ROOTS=name=/abs/dir,…`). Root names match `^[a-z][a-z0-9-]{0,62}$`; paths must be
  absolute, existing directories. Invalid roots fail start-up (NFR-SEC-009). Each root is
  opened once with `os.Root` (NFR-SEC-006).
- AC2: a `file` source instance references one root by name in `jsonData.root`; unknown
  roots or unknown `jsonData` keys fail `Validate`. `Health` fails when the root is no
  longer accessible, and the `file-roots` readiness check covers every configured root.
- AC3: query paths are relative, `/`-separated and at most 1024 bytes. Empty paths, absolute
  paths, `..` segments, backslashes, NUL and control characters are rejected before any file
  system access. Symlinks that resolve outside the root are rejected by `os.Root`.
- AC4: only regular files are read. The size is checked on the opened handle (no
  check-then-open race) against `fileMaxBytes` (default 64 MiB, `DV_FILE_MAX_BYTES`) and
  enforced again while reading, so a file that grows is still capped.
- AC5: the format is given explicitly (`format`) or chosen by file extension (`.csv` → csv,
  `.tsv`/`.tab` → tsv, case-insensitive). Unknown formats or extensions are rejected.
- AC6: decoding: header row on by default (blank names become `Column N`, duplicates get a
  ` (2)`, ` (3)` … suffix); configurable delimiter and comment character (one printable ASCII character or tab);
  a UTF-8 BOM is stripped; rows with a different number of columns fail with the line
  number; at most 1024 columns, enforced while bytes stream in (before a record is
  allocated). CSV follows RFC 4180 quoting; TSV has no quoting (quotes
  are literal data, per IANA `text/tab-separated-values`). Input after the row-limit
  overflow record is never parsed, so it cannot fail a truncated result.
- AC7: types are inferred per column from non-empty cells: `bool` (`true`/`false`, any
  case), `number` (`int64` when integral, else `float64`; NaN/Inf stay strings), `time`
  (RFC 3339, `YYYY-MM-DD`, `YYYY-MM-DD HH:MM:SS`, interpreted as UTC), else `string`. Empty
  cells are null. `fieldTypes` overrides inference by field name; values that do not parse
  under an override become null and the frame gets one warning notice per field with the
  count.
- AC8: host row/byte limits (NFR-PERF-003) apply; truncation sets `meta.truncated` with a
  notice. Decoding honours context cancellation.
- AC9: errors never reveal the operator's absolute root path; they use the relative path
  and a stable error kind (invalid path, not found, not a regular file, too large,
  unsupported format) so the query API (B-014) can map them to HTTP statuses.
- AC10: frames are named after the file's base name; `meta.source` is `file` and
  `meta.executedQuery` is `<root>:<path>`.

### FR-012 … FR-064
Defined at milestone planning time; acceptance criteria are written before implementation
(see `docs/BACKLOG.md`). Draft intent:
- **FR-012** JSON files through the same `file` source and the FR-011 path/size controls; type inference with explicit overrides.
- **FR-013/014** Panels render frames; line chart needs one time field and ≥1 numeric field.
- **FR-020/021** Argon2id-hashed passwords, server-side sessions, CSRF protection; RBAC enforced server-side on every endpoint.
- **FR-022** Secrets encrypted with AES-256-GCM envelope encryption; write-only through the API.
- **FR-030–033** Parameterized queries only; per-source read-only credentials recommended; statement timeouts and row limits.
- **FR-040/041** Outbound requests pass the SSRF guard (NFR-SEC-005).
- **FR-050–052** Dashboards stored as versioned JSON documents with schema validation.
