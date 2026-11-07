# Non-functional requirements

IDs use `NFR-<AREA>-<nnn>`. Priority is MoSCoW. "Verification" says how the requirement is
checked; anything listed as CI-enforced must be a failing job when violated.

## Security (SEC)

| ID | Requirement | Priority | Verification | Status |
| --- | --- | --- | --- | --- |
| NFR-SEC-001 | Every feature has a STRIDE threat-model entry in `docs/security/THREAT_MODEL.md` before it merges. | M | PR review checklist | In progress |
| NFR-SEC-002 | Secrets (passwords, tokens, keys, DSNs) are never logged, never returned to clients, and are encrypted at rest (AES-256-GCM). | M | Unit tests on redaction; code review | Planned (M3) — logging redaction in M0 |
| NFR-SEC-003 | AuthN required for all non-public endpoints; RBAC enforced server-side; deny by default. | M | BDD scenarios per role | Planned (M3) |
| NFR-SEC-004 | SQL sources use parameterized queries only; no string-built SQL from user input. | M | gosec G201/G202; tests | Planned (M4) |
| NFR-SEC-005 | Outbound HTTP (API/URL sources) passes an SSRF guard: scheme allow-list, DNS-resolved IP checks against private/link-local/loopback/metadata ranges, redirect re-validation, timeouts, size caps. | M | Unit + BDD tests | Planned (M5) |
| NFR-SEC-006 | File sources resolve paths inside configured roots only (no `..`, symlink escape, absolute paths); uses `os.Root`. | M | Unit + BDD tests | Planned (M2) |
| NFR-SEC-007 | Every HTTP response carries: strict `Content-Security-Policy` (no `unsafe-inline` scripts), `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY` + `frame-ancestors 'none'`, `Permissions-Policy`, `Cross-Origin-Opener-Policy: same-origin`, `Cross-Origin-Resource-Policy: same-origin`; HSTS when TLS. | M | Unit + BDD tests | In progress |
| NFR-SEC-008 | Per-client rate limiting on the API (token bucket; `429` + `Retry-After`). Clients are keyed by IPv4 address or IPv6 /64. The client table is bounded; when it is full, newcomers share one overflow bucket (no unbounded growth, no total lockout) and eviction sweeps run at most once per second. `rateLimitRps` must be `0` (disabled) or finite in [0.01, 1e6]; burst in [1, 1e6]; NaN/Inf rejected at start-up. | M | Unit + BDD tests | In progress |
| NFR-SEC-009 | Input validation: request bodies size-limited (default 1 MiB), JSON decoded with unknown-field rejection, configuration validated at startup (fail fast). | M | Unit tests | In progress |
| NFR-SEC-010 | CI runs `govulncheck`, `gosec` (via golangci-lint), `pnpm audit --audit-level high`, and secret scanning (gitleaks) on every PR; findings fail the build. | M | CI | In progress |
| NFR-SEC-011 | Containers run as non-root on a distroless/static base, read-only root filesystem, no shell, all capabilities dropped. | M | Dockerfile review; CI image build | In progress |
| NFR-SEC-012 | Panics are recovered per request, logged with a request ID, and answered with a generic `500` (no stack traces to clients). | M | Unit tests | In progress |
| NFR-SEC-013 | Server timeouts: read-header, read, write, idle set; max header bytes bounded (slowloris protection). | M | Unit tests on config | In progress |
| NFR-SEC-014 | Dependencies are minimal and pinned (go.sum, pnpm lockfile); Dependabot enabled. | S | CI `--frozen-lockfile`; review | In progress |

## Performance & scalability (PERF)

| ID | Requirement | Priority | Verification | Status |
| --- | --- | --- | --- | --- |
| NFR-PERF-001 | Health/readiness endpoints p99 < 10 ms on a single core under 500 rps. | S | Benchmark (`go test -bench`) | Planned |
| NFR-PERF-002 | Stateless API nodes; horizontal scaling behind a load balancer (state in DB/cache). | M | Architecture (ADR-0001) | In progress |
| NFR-PERF-003 | Query execution is streaming/bounded: row and byte limits per query, configurable. The byte limit must not under-estimate retained memory by more than 1.6×, and sources apply safe default limits when a query sets none. | M | Tests | In progress (frame builder limits) |
| NFR-PERF-004 | Initial UI JS bundle ≤ 250 KB gzip; routes code-split as they grow. | S | Build size check | Planned |

## Availability & resilience (AVAIL)

| ID | Requirement | Priority | Verification | Status |
| --- | --- | --- | --- | --- |
| NFR-AVAIL-001 | Graceful shutdown on SIGINT/SIGTERM: readiness flips to 503, in-flight requests drain within a configurable timeout (default 15 s). | M | Unit test | In progress |
| NFR-AVAIL-002 | Separate liveness and readiness probes suitable for Kubernetes. | M | FR-001/002 | In progress |
| NFR-AVAIL-003 | All outbound calls and data-source queries take a `context.Context` with deadlines. | M | Code review; `contextcheck` lint | In progress |

## Observability (OBS)

| ID | Requirement | Priority | Verification | Status |
| --- | --- | --- | --- | --- |
| NFR-OBS-001 | Structured logs via `log/slog` (JSON in production, text in dev), one access-log line per request with method, route, status, duration, request ID. | M | Unit tests | In progress |
| NFR-OBS-002 | Every request gets an `X-Request-ID` (validated if supplied, generated otherwise), echoed in the response and logs. | M | Unit tests | In progress |
| NFR-OBS-003 | OpenTelemetry-ready: W3C `traceparent` is parsed and its trace ID attached to logs; the HTTP layer is instrumented through a middleware seam so the OTel SDK (traces + metrics, OTLP export) can be added without touching handlers. | S | Unit tests; ADR-0005 | In progress (SDK in M7) |
| NFR-OBS-004 | Prometheus/OTLP metrics for request rate, errors, latency, query durations. | S | Tests | Planned (M7) |

## Accessibility (A11Y)

| ID | Requirement | Priority | Verification | Status |
| --- | --- | --- | --- | --- |
| NFR-A11Y-001 | UI meets WCAG 2.2 AA. | M | axe-core in Playwright BDD (zero serious/critical violations) | In progress |
| NFR-A11Y-002 | Fully keyboard operable; visible focus indicator (≥ 2 px, 3:1 contrast); skip link. | M | BDD scenarios | In progress |
| NFR-A11Y-003 | Respects `prefers-reduced-motion` and `prefers-color-scheme`. | S | Review | In progress |
| NFR-A11Y-004 | Charts expose a data-table alternative. | M | Tests | Planned (M2) |

## Internationalization (I18N)

| ID | Requirement | Priority | Verification | Status |
| --- | --- | --- | --- | --- |
| NFR-I18N-001 | All user-facing strings live in message catalogs (no hard-coded copy in components); English is the default locale. | S | Lint/review | In progress (catalog seam in M0) |
| NFR-I18N-002 | Numbers, dates and times are formatted with `Intl` using the user locale and time zone; timestamps stored/transported in UTC (RFC 3339). | M | Tests | Planned (M2) |
| NFR-I18N-003 | Layout supports RTL (`dir` attribute, logical CSS properties). | C | Review | Planned |

## Compliance & audit (AUD)

| ID | Requirement | Priority | Verification | Status |
| --- | --- | --- | --- | --- |
| NFR-AUD-001 | Security-relevant actions (login, permission changes, data-source CRUD, secret updates) are written to an append-only audit log with actor, action, target, time, outcome, request ID. | M | Tests | Planned (M3/M7) |
| NFR-AUD-002 | Personal data minimised; logs contain no PII beyond user ID; retention configurable. | S | Review | Planned |
| NFR-AUD-003 | SBOM (CycloneDX/SPDX) produced for release artifacts. | S | CI | Planned |

## Maintainability (MAINT)

| ID | Requirement | Priority | Verification | Status |
| --- | --- | --- | --- | --- |
| NFR-MAINT-001 | Lint clean: golangci-lint, ESLint, Prettier, `tsc --noEmit` (strict). | M | CI | In progress |
| NFR-MAINT-002 | Go unit tests run with `-race`; package coverage ≥ 80 % for `internal/`; Vitest coverage ≥ 80 % lines for `web/src`. | M | CI | In progress |
| NFR-MAINT-003 | Every feature has Gherkin scenarios executed in CI (godog for the API, playwright-bdd for the UI). | M | CI | In progress |
| NFR-MAINT-004 | Architectural decisions recorded as ADRs in `docs/adr/`. | M | Review | In progress |
| NFR-MAINT-005 | Each PR traces to FR/NFR IDs and updates `docs/RUNLOG.md` and `docs/BACKLOG.md`. | M | Review | In progress |

## Portability (PORT)

| ID | Requirement | Priority | Verification | Status |
| --- | --- | --- | --- | --- |
| NFR-PORT-001 | Single static Go binary (CGO disabled) serving API and built UI; runs on linux/amd64 and linux/arm64, macOS for development. | M | CI build | In progress |
| NFR-PORT-002 | Configuration via environment variables and an optional JSON config file; env overrides file overrides defaults. | M | Unit tests | In progress |
| NFR-PORT-003 | OCI image and `docker compose` definition provided. | M | CI image build | In progress |
