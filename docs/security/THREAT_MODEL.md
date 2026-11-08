# Threat model

Method: STRIDE per feature, updated in the same PR that introduces or changes the feature
(NFR-SEC-001). Each threat lists the mitigation and where it is verified. Residual risks
are tracked in `docs/BACKLOG.md`.

## System context (M0/M1)

```
 Browser (SPA, same origin) ──HTTPS──▶ dv server (Go, single binary)
                                         ├── /healthz, /readyz      (public probes)
                                         ├── /api/v1/*              (JSON API)
                                         ├── /api/openapi.json      (API description)
                                         └── /*                     (static SPA assets)
```

Trust boundaries: (1) internet ↔ dv server, (2) dv server ↔ data sources (from M2),
(3) dv server ↔ secret store (from M3). TLS is terminated either by the server
(`DV_TLS_CERT_FILE`/`DV_TLS_KEY_FILE`, later) or by an ingress; HSTS is emitted when the
request arrived over TLS.

Assets: availability of the service, integrity of served UI code, confidentiality of
internal topology (hostnames, IPs, versions of dependencies), and — from M3 — user
credentials and data-source secrets.

## F-01 HTTP server baseline (FR-001…FR-004, NFR-SEC-007/008/009/012/013)

| STRIDE | Threat | Mitigation | Verified by |
| --- | --- | --- | --- |
| S | Spoofed `X-Forwarded-For` to evade rate limits | Client identity = TCP peer address; forwarded headers are ignored until an explicit trusted-proxy list is configured (backlog B-021). | `ratelimit_test.go` |
| T | XSS / script injection into the SPA | Strict CSP: `default-src 'self'; script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'`; React escapes text; OpenAPI rendered as text only. | `security.feature`, `headers_test.go`, `features/ui/csp.feature` (zero violations in a real browser; injected inline script blocked) |
| T | Clickjacking | `X-Frame-Options: DENY` + `frame-ancestors 'none'` | `security.feature` |
| T | MIME sniffing of API responses | `X-Content-Type-Options: nosniff`, explicit `Content-Type` | `security.feature` |
| R | Actions not attributable | Request ID on every request and access log; audit log lands with authN (M3) | `requestid_test.go` |
| R | Log injection via request ID header | Incoming `X-Request-ID` accepted only if it matches `^[A-Za-z0-9._-]{1,64}$`, else regenerated; slog JSON handler escapes values | `security.feature` |
| I | Error messages leak internals (stack traces, IPs, DSNs) | Panic recovery returns generic JSON `500`; readiness reduces check errors to `pass`/`fail`; errors logged server-side only | `health.feature`, `recover_test.go` |
| I | Version disclosure aids attackers | `/healthz` and `/readyz` reveal no version; `/api/v1/version` will require authentication once M3 lands (backlog B-020) | Review |
| I | Referrer leakage of dashboard URLs | `Referrer-Policy: no-referrer` | `security.feature` |
| D | Request flooding | Per-client token bucket (`429` + `Retry-After`), bounded client table with idle eviction only after a bucket would have fully refilled (TTL = max(3 min, burst/rps)), so eviction never grants an early fresh burst. Applied to `/api/` only: probes are exempt and the SPA document/assets (cheap static reads) never spend the API allowance, so a low burst cannot blank the UI; static-asset flooding is left to timeouts and the ingress/CDN (residual) | `security.feature`, `httpx_test.go` |
| D | Filling the limiter table (many IPv6 addresses from one /64, or many hosts) to lock out new clients and make every newcomer pay an O(table) sweep under the global lock | IPv6 keyed by /64; full table → newcomers share one overflow bucket instead of a hard deny; full-table sweep throttled to once per second | `ratelimit_test.go` (`TestRateLimiterFullTableIsNotOSquared`, `TestClientKey`), `security.feature` |
| T | Mis-set `DV_RATE_LIMIT_RPS=NaN`/`Inf` silently disabling the limiter | Non-finite and out-of-range rate/burst rejected at start-up | `config_test.go` |
| D | Slowloris / oversized headers / bodies | `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, `MaxHeaderBytes`; `http.MaxBytesReader` on bodies | `config_test.go`, `server_test.go` |
| D | Hung readiness dependency stalls probes | Each check runs with a timeout derived from the request context | `health_test.go` |
| D | Probe flood multiplies dependency load or piles up goroutines stuck in a hung check | Concurrent probes share one evaluation (singleflight) and reuse it for `DV_READINESS_CACHE_TTL` (1 s); a check still running from an earlier probe is not restarted and reports `fail`, so at most one stuck goroutine exists per check (FR-002 AC5/AC6, B-043). The "still running" flag is cleared before a finished check hands back its result, so back-to-back probes never see a completed check as stuck (that race caused spurious 503s). Once a result exists, expired results are refreshed in the background while probes get the last result (FR-002 AC7, B-047), so a slow dependency cannot make probes queue; staleness is bounded by TTL + check timeout (3 s by default), after which probes wait for a fresh evaluation. Probe latency under a 500 rps flood is verified (NFR-PERF-001) | `health_test.go` (`TestFinishedCheckIsNeverReportedAsRunning`), `perf_test.go`, `health.feature` |
| E | Path traversal through static file serving | Static assets served via `http.FS(os.DirFS(root))` / `os.Root` semantics; `..` rejected; SPA fallback only for extension-less paths | `static_test.go` |
| E | Container breakout impact | Distroless non-root image, read-only root FS, `cap_drop: [ALL]`, `no-new-privileges` | `Dockerfile`, `docker-compose.yml` |

Residual risks: no TLS termination built in yet (rely on ingress); `/api/v1/version`
public until M3; rate limiting is per-instance (not cluster-wide).

## F-02 Web application shell (FR-005…FR-007, NFR-A11Y-*)

| STRIDE | Threat | Mitigation | Verified by |
| --- | --- | --- | --- |
| T | Malicious content in API responses rendered as HTML | No `dangerouslySetInnerHTML`; ESLint `react/no-danger` equivalent enforced by review; values rendered as text | Vitest, review |
| T | Supply-chain compromise of npm packages | Lockfile pinned, `--frozen-lockfile` in CI, `pnpm audit` (prod deps: low+ fails; all deps: moderate+ fails), scoped and documented `overrides` for unfixed transitive advisories (SECURITY.md), Dependabot, minimal dependency set, no CDN scripts (CSP forbids them) | CI `web` job |
| I | Theme preference storage leaks data | Only `"light"`/`"dark"` stored in `localStorage`; reads wrapped in try/catch | `theme.test.tsx` |
| D | Unbounded retries hammer the API | TanStack Query retry capped (2) with exponential backoff | `queryClient.ts` |
| S | UI copy overstates protections (for example "credentials are encrypted at rest" before FR-022 ships), so an operator relies on a control that does not exist and stores real secrets in plaintext config | Copy describes shipped behaviour only; planned controls are named with their milestone (FR-015 AC7, NFR-AUD-004, B-049). A catalog-wide test rejects security/compliance claims without a planned-milestone marker unless allow-listed against a shipped requirement | `messages.test.ts`, `datasources.feature` |

## F-08 Data-frame model and plugin registry (FR-010, FR-015, NFR-PERF-003, NFR-SEC-002)

| STRIDE | Threat | Mitigation | Verified by |
| --- | --- | --- | --- |
| T | A plugin is swapped or injected at runtime (e.g. by later code registering a shadowing ID) | Plugins are compiled in; duplicate IDs rejected; registry frozen after start-up so `Register` fails afterwards | `registry_test.go`, `plugins.feature` |
| T | Malformed wire payload (mismatched column lengths, wrong types) crashes consumers | `frame.Frame.UnmarshalJSON` and the TS decoder validate types and lengths and return errors, never panic | `frame_test.go`, `frame.test.ts`, `frame.feature` |
| I | Data-source secrets leak through API responses or logs | `plugin.SecureValues` marshals to `{key: true}` (set flags only) and implements `slog.LogValuer` → `[REDACTED]`; `String()`/`%v` redacted too | `secure_test.go`, `plugins.feature` |
| I | Plugin catalogue reveals internals | Endpoint returns only static public metadata (no instance config, hosts, versions of dependencies). Requires auth once M3 lands (B-020) | `server_test.go` |
| D | Memory exhaustion from a huge result set | `frame.Builder` enforces row and byte limits while appending; build stops at the limit and flags `meta.truncated` with a notice | `builder_test.go`, `frame.feature` |
| D | Huge or deeply nested field names / JSON values | Field names capped at 256 bytes; JSON values must be valid JSON and count towards the byte limit | `frame_test.go` |

Residual risks: the byte limit is an estimate of in-memory size, not exact allocation;
`int64` numbers beyond ±2^53 lose precision in browsers (documented in ADR-0003).

## F-03 File source and CSV/TSV format (FR-011, NFR-SEC-006, NFR-PERF-003)

Data flow: operator config → named roots opened with `os.Root` at start-up → `file` source
instance (`jsonData.root`) → query `{path, format, options, fieldTypes}` → path validation
→ `os.Root.Open` → size-capped reader → CSV/TSV decoder → frame builder with limits.

| STRIDE | Threat | Mitigation | Verified by |
| --- | --- | --- | --- |
| I/T | Path traversal (`../`, absolute paths, `C:\`, encoded separators) reads files outside the root | `fsroot.CleanPath` rejects empty/absolute paths, `..` segments, backslashes, NUL and control characters before any FS call; `os.Root` refuses escapes as a second layer | `fsroot_test.go`, `file.feature` |
| I | Symlink inside a root points outside it (`/etc/passwd`) | `os.Root` resolves every component inside the root and rejects symlinks that escape | `fsroot_test.go`, `file.feature` |
| I | An operator exposes a project or home directory and its dotfiles (`.env`, `.git/config`, `.aws/credentials`, `.ssh/id_rsa`) become queryable; the CSV decoder happily returns `KEY=value` lines as rows | `fsroot.CleanPath` rejects any cleaned segment starting with `.` before any FS call (FR-011 AC3, B-048). The rule applies to the requested path; a symlink inside the root whose *target* is a dotfile is content the operator (or whoever can write the root) placed there, see residual risks | `fsroot_test.go` (`TestCleanPathRejectsHiddenSegments`), `file.feature` |
| S/R | A path with bidi controls (e.g. U+202E) displays as `report.csv` in errors, `meta.executedQuery`, the UI and future audit logs while naming a different file | Bidi controls rejected in `CleanPath` (FR-011 AC3); other control characters already were | `fsroot_test.go`, `file.feature` |
| I | Reading devices, FIFOs or directories (e.g. a FIFO blocks forever, `/dev/zero` never ends) | Only regular files: mode checked on the opened handle; FIFOs opened non-blocking by `os.Root` are rejected before any read | `fsroot_test.go` |
| T | TOCTOU: file swapped or grown between check and read | Size and type are checked with `Stat` on the opened handle, and the reader is capped at the limit (+1 byte to detect growth → error); after the cap trips the reader keeps returning `(0, ErrTooLarge)` so buffered/retrying consumers cannot panic on a negative count | `fsroot_test.go` |
| D | Huge file, very long lines or a wide CSV exhausts memory | `fileMaxBytes` cap (default 64 MiB); 1024-column cap enforced by a streaming, quote-aware guard before `encoding/csv` allocates the record (a 1-record file of commas would otherwise allocate >100× its size); row/byte limits stop collection early and flag truncation (the byte limit is the result budget on converted values; raw records held for inference have a separate guard of 2× the byte limit, so peak decode memory is ≈ 3× the byte limit), and a query that sets no limits gets the source defaults (1M rows / 64 MiB) instead of "unlimited"; context cancellation checked during decode | `csv_test.go`, `file.feature` |
| D | Crafted CSV whose columns change inferred type on different rows forces repeated full re-conversion (CPU/allocation amplification, quadratic in column count) | Inference streams over column-wise state and re-converts only the column whose type changed; each column changes type at most four times, so every cell is converted at most five times (a 182 KiB staggered 128-column file went from ~924 MiB allocated to ~22 MiB) | `csv_test.go` (`TestStaggeredTypeChangesStayLinear`) |
| I | Error messages disclose the server's directory layout | Errors carry the relative path and a stable kind; the absolute root path is never included | `file_test.go` |
| E | A query names a root the instance is not configured for | The root comes only from the instance's `jsonData.root`, never from the query body; unknown query keys are rejected | `file_test.go` |
| S/R | Unauthenticated users read files | Until M3 the query API is off by default and loopback-only (F-10, NFR-SEC-016); from M3 it requires an authenticated Viewer and audit-logs reads (B-020, NFR-AUD-001). Until then roots should only contain non-sensitive data | `config_test.go`, `query.feature` |

Toolchain dependency: `os.Root` in Go 1.24.0 let a symlink to `../` inside a root reach
files in the parent directory (caught by `TestOpenErrors` in CI's minimum-version job).
`go.mod` therefore requires Go ≥ 1.24.7, so dv cannot be built with an affected
standard library; CI keeps testing both that minimum and `stable`.

Residual risks: CSV formula injection matters on *export* (not implemented yet; the
exporter must escape `=`, `+`, `-`, `@`). Compressed files are not decoded yet, so zip
bombs are not reachable; decompression must enforce the same output cap when added.
Inference reads the whole (capped) file into memory before building columns.
Hard links: `os.Root` confines path *resolution*, but a hard link inside a root to a file
elsewhere on the same file system is an ordinary regular file and is read. Creating one
needs write access to the root, and with the Linux default `fs.protected_hardlinks=1` the
creator must own or be able to read and write the target, so it grants nothing that writer
could not already copy in. Operators should mount roots read-only (as `docker-compose.yml`
does) and not give untrusted users write access to them. The same applies to symlinks
inside a root that point at hidden files in that root.

## F-09 Repository automation: merge bot, dependency updates, CI supply chain (NFR-MAINT-006…008, NFR-SEC-014/015)

Trust boundary: GitHub Actions runner with a repository token ↔ pull-request content
written by anyone who can open a PR (including Dependabot and, for public repos, forks).

| STRIDE | Threat | Mitigation | Verified by |
| --- | --- | --- | --- |
| S | A fork or unknown author gets code merged by the bot | Merge only same-repository PRs whose author is on an explicit allow-list (`DV_AUTOMERGE_AUTHORS`); fork PRs are skipped. | `automerge` unit tests; `automerge.feature` |
| T | Commit pushed after CI passed is merged untested (race) | Run's `head_sha` must equal the PR's current head, and the merge call sends `sha` so GitHub refuses if the head moved. | `TestDecide*`, `TestRunMergesWithPinnedSHA` |
| T | PR modifies the merge bot itself to merge something else | Workflow is `workflow_run`-triggered, so it runs the default-branch copy; it checks out the default branch, never the PR head, and executes no PR code. | Workflow review |
| T | A failing or pending required check is ignored | Besides the `CI` run conclusion, every check run on the head SHA must be completed with `success`/`neutral`/`skipped`, and at least one must exist. | `automerge.feature` |
| R | Nobody can tell why a PR was or was not merged | Every decision logs PR number, head SHA and reason; GitHub records the merge actor. | `TestRun*` log assertions |
| I | Token leaks into logs | Token read from env only, sent only in the `Authorization` header to `GITHUB_API_URL`; error messages include status + path, never headers. | `TestRunNeverLogsToken` |
| D | API hangs block the runner | 15 s HTTP timeout and a 5 min job timeout. | Unit test with slow server |
| E | Token can do more than merge | Job permissions `contents: write`, `pull-requests: write`, `checks: read`, nothing else; workflow default `permissions: {}`. | Workflow review |
| T | Stacked PR merges into an already-merged parent branch and its change never reaches `main` (happened to PR #5) | Bot merges only PRs targeting the default branch; owner setting "Automatically delete head branches" makes GitHub retarget stacked PRs to `main` when the parent merges. | `automerge.feature`; this PR re-lands #5 |
| T | Malicious dependency release auto-merged | Only after full CI (tests, govulncheck, pnpm audit, gitleaks, image build); non-LTS Node majors ignored; owner can add `hold`. Residual: a compromised release that passes CI. | Dependabot config review |
| T | A CI tool release asset is replaced, or the download is tampered with, and runs with the job's token (gitleaks, actionlint) | Exact version plus a SHA-256 literal committed in the workflow, passed to `scripts/fetch-verified.sh`, which writes the output only after the hash matches (no unverified file ever exists); every other downloader in a `run:` script is refused (NFR-SEC-015 AC1). Residual: downloaders the policy does not know (e.g. a Python one-liner) rely on review. | `internal/cipolicy` tests; `ci_supply_chain.feature` |
| T | `go run tool@latest` silently picks up a new, possibly compromised tool version | Exact semver required, so the module is verified against sumdb and changes only by commit (AC2). | `ci_supply_chain.feature` |
| T | An action reference follows a moving branch | `uses:` must name a tag or commit SHA (AC3). Residual: major tags such as `@v4` are mutable upstream; SHA pinning with Dependabot digest updates is tracked as B-058. | `ci_supply_chain.feature` |
| T | The build toolchain stays on a Node line after it leaves Active LTS, so security fixes arrive later and stop at end of life, while Dependabot never proposes the next major | Node pinned to the current Active LTS (26) in `mise.toml`, CI and the Dockerfile; odd majors ignored; a move to maintenance LTS creates a planned migration (NFR-MAINT-007 AC4). Residual: the Active LTS date is checked by review, not by CI. | `toolchain.feature`; `scripts/check-toolchain.sh` |
| T | One toolchain declaration drifts (CI on Node 22 while the image builds with 26), so what CI tested is not what ships | `check-toolchain.sh` compares every declaration with `mise.toml` and fails CI; each rule has a scenario that changes one file in a copy of the repo (AC5). | `toolchain.feature` |
| E | A malformed workflow expression or shell bug in `run:` widens what a step does (e.g. untrusted `${{ }}` interpolation into a script) | `actionlint` + shellcheck on every PR, including its script-injection check (AC4). | CI `Workflow lint (actionlint)` job |

## F-11 Explore page and table panel (FR-013, FR-018)

| STRIDE | Threat | Mitigation | Verified by |
| --- | --- | --- | --- |
| T | File content (cell values, field names, notices, error messages) carries markup or script that the browser executes | Everything is rendered as React text, no `dangerouslySetInnerHTML`; CSP forbids inline script; frames are validated by `decodeFrame` before rendering | `TablePanel.test.tsx`, `explore.feature` |
| I | Query state in the URL leaks something sensitive | Only the data-source uid, path, format and row limit are in the URL; no credentials exist (FR-017 refuses `secureJsonData`) | Review, `ExplorePage.test.tsx` |
| S/T | A link opened from elsewhere runs a query the user did not intend | URL state only fills the form; a query runs only when the user presses Run (FR-018 AC6); cross-site and rebinding requests are refused server-side (NFR-SEC-017) | `ExplorePage.test.tsx` |
| D | A huge frame freezes the tab | Server row/byte limits (NFR-PERF-003); the table renders one 50-row page at a time (NFR-PERF-005); requests can be cancelled | `TablePanel.test.tsx` |

## F-10 Query API and provisioned data sources (FR-016, FR-017, NFR-SEC-016, NFR-PERF-003)

Data flow: operator config `datasources[]` → validated by each plugin at start-up →
`query.Service` (uid → instance) ← `POST /api/v1/query` (strict JSON, ≤ 20 queries) →
per-query deadline + host limits → `Source.Query` → frames → per-refId result.

| STRIDE | Threat | Mitigation | Verified by |
| --- | --- | --- | --- |
| S/I | Anyone who can reach the port reads every exposed file before authentication exists | Endpoint off by default; enabling it requires a loopback `httpAddr` unless the operator explicitly sets `queryApiAllowRemote`, which logs a warning (NFR-SEC-016). Removed only when NFR-SEC-003 ships | `config_test.go`, `query.feature` |
| E | A query picks its own root, file settings or plugin type | The request names only a `datasource.uid`; type and `jsonData` come from operator config. Plugin query bodies are decoded strictly by the plugin | `query_test.go`, `file_test.go` |
| T | Malformed or oversized bodies, unknown fields, duplicate `refId`s smuggle data or confuse result keys | `MaxBody`, strict decoding with trailing-data check, `refId` pattern + uniqueness; no query runs when the request is invalid | `query_test.go`, `query.feature` |
| D | One request fans out many slow or huge queries | ≤ 20 queries, ≤ 4 concurrent per request, per-query `queryTimeout`, host row/byte limits a query can lower but never raise; API rate limiting (NFR-SEC-008) bounds requests per client | `query_test.go`, `query.feature` |
| D | One request retains up to 20 × `queryMaxBytes` of frames until its response is written (memory exhaustion with few requests) | `queryMaxBytes` is a per-request budget: each query reserves a share of what is left and returns the unused part; when nothing is left the query answers `413 budget_exhausted` without running (FR-016 AC9, NFR-PERF-003 AC2, B-062) | `budget_test.go`, `query.feature` |
| I | Error messages disclose server paths, driver errors or stack traces | Known error kinds map to fixed messages; unknown errors return a generic `query_failed` and are logged server-side with the request ID only | `query_test.go` |
| I | Provisioned secrets end up in plain config and in API responses | `secureJsonData` is refused in provisioning until FR-022; `GET /api/v1/datasources` returns uid/type/name only | `config_test.go`, `query.feature` |
| S/I | DNS rebinding: a web page the operator visits rebinds its own name to `127.0.0.1`, so the browser treats the loopback query API as same-origin and the page reads every exposed file; a cross-site page fires queries (CSRF-style, blind) | Gated endpoints accept only loopback `Host` values (`localhost`, `*.localhost`, loopback IPs) while `queryApiAllowRemote=false`; `POST /api/v1/query` refuses cross-site `Sec-Fetch-Site` and a mismatched `Origin`; checks run before the body is read; `Content-Type: application/json` already forces a CORS preflight that is never granted (NFR-SEC-017, B-063) | `query_test.go`, `gate.feature` |
| R | No record of who read what | Every query logged with request ID, refId, uid, type, status, duration (FR-016 AC8); identity added in M3 with the audit log (NFR-AUD-001) | `query_test.go` |

Residual risks: with `queryApiAllowRemote=true` and no auth, the operator has chosen to
expose the roots to the network; the warning and docs say so. A client can still issue
many sequential requests up to the rate limit; per-user quotas come with M3.

## F-12 JSON and NDJSON formats (FR-012, NFR-PERF-003, NFR-SEC-006)

Data flow: as F-03 up to the size-capped reader → `jsonformat` streaming token decoder
(optional `rowsPath` navigation) → per-row values → type inference → frame builder.

| STRIDE | Threat | Mitigation | Verified by |
| --- | --- | --- | --- |
| D | A huge top-level array (or one deep inside the document) is decoded in full before limits apply | Rows are decoded one element at a time with `json.Decoder.Token`/`More`; decoding stops after `MaxRows`+1 rows and at the 2× raw-byte guard; values skipped on the way to `rowsPath` are consumed token by token, never allocated as a whole | `json_test.go`, `json.feature` |
| D | Deeply nested input (`[[[[…` or `{"a":{"a":…`) exhausts the stack or CPU | Nesting deeper than 64 levels fails as a decode error, checked while skipping and while decoding rows | `json_test.go` |
| D | A row with thousands of distinct keys (or many rows each adding new keys) makes a frame millions of fields wide | Field union capped at 1024 (FR-012 AC3), checked as each new key appears | `json_test.go`, `json.feature` |
| D | `flatten` multiplies fields through deep nesting | Flattening stops at 8 levels and counts toward the 1024-field cap | `json_test.go` |
| T | Numbers lose precision (`9007199254740993` read as float) or become NaN/Inf | `UseNumber`; integral values parse as `int64`, others as finite `float64`, anything else is null with a warning | `json_test.go` |
| I | Syntax errors echo file content (e.g. a secret on the failing line) into API responses and logs | Errors carry only a byte offset or row number and a stable kind (decode error) | `json_test.go`, `json.feature` |
| T | `rowsPath` used as an injection vector (expressions, wildcards, recursive descent) | `rowsPath` is plain dot-separated keys from a fixed character set, at most 16 segments; no expression language is evaluated | `json_test.go` |

Residual risks: duplicate keys keep the last value, so two tools reading the same file may
disagree (documented in FR-012 AC7). A single huge string or array value inside one row is
bounded only by `fileMaxBytes`, not by the byte limit, because a row is decoded before it
can be measured.

## Planned features (threats to model at implementation time)

- **F-04 AuthN/RBAC (M3):** credential stuffing, session fixation, CSRF, privilege escalation, IDOR → Argon2id, rotating session IDs, SameSite=strict cookies + CSRF tokens, server-side RBAC, object-level checks.
- **F-05 Secrets (M3):** key exposure, secrets in logs/responses → envelope encryption (AES-256-GCM, KEK from env/KMS), write-only API fields, slog redaction.
- **F-06 SQL sources (M4):** SQL injection, expensive queries, data exfiltration → parameterized queries, read-only transactions, statement timeouts, row limits.
- **F-07 REST/GraphQL sources (M5):** SSRF (cloud metadata, internal services, DNS rebinding) → dial-time IP checks, redirect re-validation, allow-lists, response size caps.
