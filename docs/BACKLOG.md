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
| B-012 | CSV/TSV format + file source rooted with `os.Root` | M2 | FR-011, NFR-SEC-006 | Todo |
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
| B-031 | Benchmark for health endpoints | M1 | NFR-PERF-001 | Todo |
| B-032 | Bundle-size budget check in CI | M1 | NFR-PERF-004 | Todo |
| B-033 | Enforce `go mod tidy -diff` in CI (go.sum lacks hashes for test-only deps of godog because this run's sandbox could not reach proxy.golang.org) | M0 | NFR-SEC-014 | Todo |
| B-034 | Choose and add a LICENSE (owner decision) | M0 | — | Todo |
| B-035 | Fix moderate `minimatch` advisory GHSA-g3hv-75wg-m249 (dev-only, via typescript-eslint) with a scoped override; tighten the audit gate (prod: low+, all: moderate+) | M0 | NFR-SEC-010 AC1–AC4 | In review (PR #2) |
| B-036 | E2E scenario asserting zero CSP violations in the browser console | M1 | NFR-SEC-007 | Todo |
| B-037 | Host-provided `os.Root` / policy HTTP client handed to plugins (ADR-0002) — lands with the first plugin that needs it (B-012) | M2 | NFR-SEC-005/006 | Todo |
| B-038 | Plugin config schema in `SourceMeta` so the UI can generate editors (ADR-0002) | M2 | FR-015 | Todo |
| B-047 | Readiness: serve the previous result while one refresh runs (stale-while-revalidate), bounded by TTL + check timeout, so probes arriving during a slow check do not wait | M1 | FR-002 AC7, NFR-PERF-001 AC2 | In review (PR #2) |
| B-049 | UI copy on Data sources claimed "Credentials are encrypted at rest" before M3 ships it; reword, and add a catalog-wide guard so no message claims an unshipped security property | M1 | FR-015 AC7, NFR-AUD-004, FR-022 | In review (PR #3) |
