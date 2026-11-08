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
| FR-016 | Query API (`POST /api/v1/query`) | M | M2 | In progress |
| FR-017 | Provisioned data-source instances | M | M2 | In progress |
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
- AC5: concurrent probes share one evaluation, and a completed result is reused for a short TTL
  (`DV_READINESS_CACHE_TTL`, default 1 s, `0` disables). A probe flood therefore runs each check at
  most once per TTL. Passing and failing results are both reused, so a dependency recovering or
  failing shows up within one TTL. Draining is never cached: once shutdown starts, the next probe
  returns `503`.
- AC6: a check that ignores its context and is still running from an earlier probe is not started
  again; it reports `fail` until it returns. This keeps at most one stuck goroutine per check (e.g.
  `os.Stat` on a hung network mount).
- AC7: once a result exists, a probe that arrives after the TTL has expired gets the last result
  immediately and starts one shared background refresh (stale-while-revalidate); it does not wait
  for the checks. A result is served stale for at most the check timeout beyond the TTL
  (`DV_READINESS_CACHE_TTL` + `DV_READINESS_CHECK_TIMEOUT`, 3 s by default), which is the longest a
  refresh can take. Probes wait for the evaluation, as in AC5, when there is no result yet, when the
  last one is older than that bound, after a check is registered or the TTL changes, and when the TTL
  is `0`. Draining still applies on the very next probe.

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
- AC7: the Data sources page describes only what this build does. It lists connector types
  as compiled in, names SQL and HTTP connectors as roadmap items, and states that stored
  credentials are not supported yet and encrypted secret storage (FR-022) is planned for
  milestone M3. It must not claim encryption at rest before FR-022 ships (NFR-AUD-004).

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
  Hidden entries are never read: a path with any segment that starts with `.` after
  cleaning (`.env`, `.git/config`, `data/.secret.csv`) is rejected as an invalid path, so
  exposing a project directory does not expose its dotfiles (`./a.csv` still names `a.csv`).
  Unicode bidirectional controls (U+061C, U+200E, U+200F, U+202A–U+202E, U+2066–U+2069) are
  rejected so a path cannot display differently from the file it names in errors,
  `meta.executedQuery` or audit logs (CVE-2021-42574 "Trojan Source" class).
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
  count. Inference uses only the rows the frame returns: neither the row-limit overflow
  record nor rows dropped by the byte limit can change a column's type or the number of
  rows returned (the longest prefix whose types are inferred from that prefix alone and
  which fits the limits is returned). The work stays linear in the retained data: a type
  change re-converts only that column, so each cell is converted at most five times.
- AC8: host row/byte limits (NFR-PERF-003) apply; truncation sets `meta.truncated` with a
  notice. A limit the query leaves at zero is replaced by the source default (host-set,
  else 1,000,000 rows / 64 MiB estimated frame memory), so no query decodes unbounded.
  The byte limit is the *result* budget and is counted on converted values only: a row that
  fits once converted is kept even if its text is longer (a 19-digit integer costs 24 bytes,
  not its 35-byte text). Raw records held for inference have their own guard of twice the
  byte limit (cell length + 16 bytes each); it only trips for unusually verbose text (e.g.
  numbers with dozens of digits, or long text an override turns into nulls), sets
  `meta.truncated` and adds its own notice. Peak decode memory is therefore bounded by about
  three times the byte limit. Decoding honours context cancellation.
- AC9: errors never reveal the operator's absolute root path; they use the relative path
  and a stable error kind (invalid path, not found, not a regular file, too large,
  unsupported format) so the query API (B-014) can map them to HTTP statuses.
- AC10: frames are named after the file's base name; `meta.source` is `file` and
  `meta.executedQuery` is `<root>:<path>`.

### FR-016 — Query API `POST /api/v1/query` (M)
Clients run one or more queries against configured data-source instances and get frames
back, keyed by `refId`. The request/response shape follows the established dashboard-tool
convention (a batch of `refId`-tagged queries, per-query status, `207 Multi-Status` when
only some fail) so panels can render partial results.
- AC1: request body `{"queries":[{"refId","datasource":{"uid"},"query":{…},"maxRows"?}],"from"?,"to"?}`.
  `Content-Type` must be `application/json` (else `415 unsupported_media_type`). JSON is
  decoded strictly: unknown fields, trailing data, or a body over `maxBodyBytes` are `400
  invalid_request` (`413` for the size). `from`/`to` are RFC 3339; if one is given both
  are required and `from` must not be after `to`. `query` is the plugin's own query model.
- AC2: 1 to 20 queries per request; each `refId` matches `^[A-Za-z0-9_-]{1,64}$` and is
  unique in the request; `datasource.uid` is required. Violations are `400 invalid_request`
  naming the offending index, and no query runs.
- AC3: response `{"results":{"<refId>":{"status":<int>,"frames":[…]}}}` with frames in the
  FR-010 wire format, or `{"status":<int>,"error":{"code","message"}}` for a failed query.
  HTTP status: `200` when every query succeeded; when every query failed with the same
  status, that status; otherwise `207`.
- AC4: query errors map to stable codes and statuses (B-040): unknown datasource uid →
  `404 datasource_not_found`; invalid path / query / source settings → `400 invalid_query`;
  file not found → `404 not_found`; not a regular file → `400 invalid_query`; file too
  large → `413 too_large`; request byte budget used up by earlier queries → `413
  budget_exhausted` (AC9); unsupported format → `415 unsupported_format`; invalid format
  options (e.g. a two-character delimiter) → `400 invalid_query`; file content the format
  cannot decode (wrong column count, empty input) → `422 decode_failed`; per-query
  deadline → `504 timeout`; client gone → `499 cancelled`; anything else → `500
  query_failed` with a generic message (details only in the server log with the request ID).
- AC5: each query runs with its own deadline (`queryTimeout`, default 30 s,
  `DV_QUERY_TIMEOUT`); queries of one request run concurrently, at most 4 at a time.
- AC6: host result limits `queryMaxRows` (default 100 000, `DV_QUERY_MAX_ROWS`) and
  `queryMaxBytes` (default 32 MiB, `DV_QUERY_MAX_BYTES`) apply to every query (NFR-PERF-003).
  A query may lower the row limit with `maxRows` but can never raise it above the host limit.
- AC9 (per-request byte budget, B-062): `queryMaxBytes` bounds the frames retained for a
  whole request, not each query, so one request can never hold more than `queryMaxBytes`
  of results however many queries it carries. When a query starts it reserves a share of
  what is left: `remaining / queries not yet started` (counting itself), so queries get
  equal shares, and it hands back what its frames did not use when it finishes; later
  queries share what was handed back. A query whose frames hit its
  share is truncated (FR-010 notice) and gets an extra warning notice naming the
  request budget. A query that starts when nothing is left does not run and answers `413
  budget_exhausted`. A single-query request still gets the whole `queryMaxBytes`.
- AC7: until authentication ships (M3, NFR-SEC-003) the endpoint is off by default
  (`queryApiEnabled=false`, `DV_QUERY_API_ENABLED`): when off it answers `404` like any
  unknown route. When on, start-up fails unless `httpAddr` binds a loopback address, or the
  operator also sets `queryApiAllowRemote=true` (`DV_QUERY_API_ALLOW_REMOTE`), in which case
  a warning is logged at start-up (NFR-SEC-016). Both endpoints also refuse non-loopback
  `Host` values and cross-site browser requests (NFR-SEC-017), so DNS rebinding cannot turn
  the loopback gate into a same-origin read for an arbitrary web page.
- AC8: error messages never contain the operator's absolute paths (FR-011 AC9), and every
  executed query is logged with request ID, refId, datasource uid, type, status and duration,
  never the query result.

### FR-017 — Provisioned data-source instances (M)
Until M3 adds stored, editable instances, operators declare data-source instances in the
config file so the query API is usable.
- AC1: config file `datasources: [{"uid","type","name","jsonData"}]`. `uid` matches
  `^[A-Za-z0-9_-]{1,40}$` and is unique; `name` is 1–100 characters; `type` must be a
  registered source plugin. Unknown keys — including `secureJsonData`, which is refused until
  FR-022 ships encrypted secrets — fail start-up.
- AC2: every instance is checked with its plugin's `Validate` at start-up; a failure aborts
  start-up with the uid and the plugin's (path-free) message.
- AC3: `GET /api/v1/datasources` lists `{"uid","type","name"}` sorted by uid. Settings
  (`jsonData`) are not returned (least disclosure until RBAC). It follows the same
  `queryApiEnabled` gate as FR-016.

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
