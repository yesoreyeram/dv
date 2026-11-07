# Backlog

Prioritised top to bottom. Each item is sized to fit one automated run (one PR).
Status: `Todo`, `In progress`, `In review` (PR open), `Done` (merged), `Blocked`.

| # | Item | Milestone | Requirements | Status |
| --- | --- | --- | --- | --- |
| B-001 | Requirements baseline, ADRs 0001–0006, threat model, roadmap, CONTRIBUTING, SECURITY | M0 | NFR-MAINT-004/005, NFR-SEC-001 | In review |
| B-002 | Go server skeleton: config, slog, health/readiness, version, OpenAPI, hardened middleware, graceful shutdown, godog suite | M0/M1 | FR-001…004, NFR-SEC-007/008/009/012/013, NFR-OBS-001…003, NFR-AVAIL-001/002 | In review |
| B-003 | React shell: layout, navigation, theme, status + API reference pages, Vitest, playwright-bdd + axe | M1 | FR-005…007, NFR-A11Y-001/002/003 | In review |
| B-004 | CI workflow (Go lint/test/BDD, web lint/type/test/build, e2e, govulncheck, pnpm audit, gitleaks, image build), Dependabot | M0 | NFR-SEC-010/014, NFR-MAINT-001…003 | In review |
| B-005 | Dockerfile (distroless, non-root) + docker-compose with hardening | M0 | NFR-SEC-011, NFR-PORT-003 | In review (image build verified only in CI) |
| B-010 | `internal/frame`: data-frame model, JSON wire encoding, limits (+ TS decoder) | M2 | FR-010, NFR-PERF-003 | In review (PR #3) |
| B-011 | Plugin registry + `Source`/`Format` interfaces (ADR-0002) with readiness check, `GET /api/v1/plugins`, Data sources catalogue page | M2 | FR-015, NFR-SEC-002 | In review (PR #3) |
| B-012 | CSV/TSV format + file source rooted with `os.Root` (+ default query limits, capReader contract fix, inference from retained rows only) | M2 | FR-011, NFR-SEC-006 | In review (PR #4, stacked on #3) |
| B-013 | JSON format | M2 | FR-012 | Todo |
| B-014 | `POST /api/v1/query` endpoint (body limits, strict JSON) | M2 | FR-010, NFR-SEC-009 | Todo |
| B-015 | Explore page: source picker, query, table panel | M2 | FR-013, NFR-A11Y-004 | Todo |
| B-016 | Line-chart panel with table alternative | M2 | FR-014, NFR-A11Y-004 | Todo |
| B-017 | i18n message catalog + `Intl` formatting helpers | M2 | NFR-I18N-001/002 | Todo |
| B-020 | Require auth for `/api/v1/version` once M3 lands | M3 | NFR-SEC-003 | Todo |
| B-021 | Trusted-proxy configuration for client IP (rate limiting behind ingress) | M3 | NFR-SEC-008 | Todo |
| B-022 | Storage layer + migrations (SQLite, Postgres) | M3 | NFR-PERF-002 | Todo |
| B-023 | Local users, Argon2id, sessions, CSRF | M3 | FR-020 | Todo |
| B-024 | RBAC middleware + roles | M3 | FR-021, NFR-SEC-003 | Todo |
| B-025 | Envelope encryption for secrets + slog redaction handler | M3 | FR-022, NFR-SEC-002 | Todo |
| B-030 | Bump Go to latest stable (1.25+/1.26+) once the build environment can download it; CI already also tests on `stable` | M0 | NFR-MAINT-001 | Todo |
| B-031 | Benchmarks for health endpoints (`make bench`) + 500 rps open-loop load tests (`internal/loadtest`, `make perf`, isolated CI step) | M1 | NFR-PERF-001 | In review (PR #2) |
| B-032 | Bundle-size budget check in CI (initial JS 250 KB / CSS 50 KB gzip, `make size`) | M1 | NFR-PERF-004 | In review (PR #2) |
| B-033 | Enforce `go mod tidy -diff` in CI (go.sum lacks hashes for test-only deps of godog because this run's sandbox could not reach proxy.golang.org) | M0 | NFR-SEC-014 | Todo |
| B-034 | Choose and add a LICENSE (owner decision) | M0 | — | Todo |
| B-035 | Triage moderate `minimatch` advisory (dev-only, via typescript-eslint in e2e) | M0 | NFR-SEC-010 | Todo |
| B-036 | E2E scenario asserting zero CSP violations in the browser (events + console), with injected-script negative control | M1 | NFR-SEC-007 | In review (PR #2) |
| B-037 | Host-provided `os.Root` handed to plugins (ADR-0002) — `internal/fsroot` | M2 | NFR-SEC-006 | In review (PR #4) |
| B-039 | Policy HTTP client handed to plugins (ADR-0002) — lands with the REST source | M5 | NFR-SEC-005 | Todo |
| B-040 | Map file-source error kinds to HTTP statuses (400/404/413/415) in `POST /api/v1/query` | M2 | FR-011 AC9, FR-010 | Todo (with B-014) |
| B-041 | File listing capability for the file source (browse a root in the query editor) with the same path controls | M2 | FR-011, NFR-SEC-006 | Todo |
| B-042 | Provisioned data-source instances from config until M3 storage exists, so the query API can be used | M2 | FR-011, FR-015 | Todo |
| B-043 | `/readyz`: concurrent probes share one evaluation (singleflight), result reused for `DV_READINESS_CACHE_TTL` (1 s), a check stuck from an earlier probe is never restarted | M1 | FR-002 AC5/AC6, NFR-AVAIL-001, NFR-SEC-006 | In review (PR #2) |
| B-044 | Host-level query limits in config (`queryMaxRows`/`queryMaxBytes`) wired into `builtin.Options.QueryDefaults` and `POST /api/v1/query` | M2 | NFR-PERF-003 | Todo (with B-014) |
| B-045 | Cluster-aware rate limiting and trusted-proxy keying (limiter is per instance; see B-021) | M3 | NFR-SEC-008 | Todo |
| B-046 | Benchmark `/readyz` under a probe flood with a slow check, to back NFR-PERF-001 with numbers (pairs with B-031) | M1 | NFR-PERF-001, FR-002 | In review (PR #2) |
| B-038 | Plugin config schema in `SourceMeta` so the UI can generate editors (ADR-0002) | M2 | FR-015 | Todo |
| B-047 | Readiness: serve the previous result while one refresh runs (stale-while-revalidate) so probes arriving during a slow check do not wait; today they wait up to the check time (NFR-PERF-001 AC2) | M1 | FR-002, NFR-PERF-001 | Todo |
