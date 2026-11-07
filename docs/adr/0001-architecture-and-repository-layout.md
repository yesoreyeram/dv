# 0001. Architecture and repository layout

- Status: Accepted
- Date: 2026-11-07
- Requirements: NFR-PERF-002, NFR-PORT-001, NFR-PORT-003, NFR-MAINT-004

## Context

dv must be easy to deploy (one artifact), horizontally scalable, and hard to misconfigure.
The team is small, so a monorepo with one build entry point (`make`) keeps the feedback
loop short.

## Decision

- **Modular monolith.** One Go binary (`cmd/dv`) serves the JSON API, health probes and the
  compiled SPA from the same origin. Same-origin serving lets us use a strict CSP with no
  CORS surface. Modules communicate through Go interfaces inside `internal/`, so a module
  can be split into a service later without changing callers.
- **Stateless API nodes.** All durable state (users, dashboards, secrets) lives in a
  database introduced in M3 (SQLite for single-node, PostgreSQL for HA). Caches are
  per-node and disposable.
- **Layout** (Go [standard layout](https://github.com/golang-standards/project-layout) conventions):

  ```
  cmd/dv/                 main package: wiring, config, signals
  internal/buildinfo/     version metadata injected with -ldflags
  internal/config/        defaults < JSON file < environment, validated
  internal/health/        liveness/readiness registry
  internal/httpx/         middleware (security headers, request ID, logging, rate limit, recovery)
  internal/server/        router, API handlers, static SPA serving, lifecycle
  api/                    OpenAPI document (embedded into the binary)
  features/               Gherkin specs (api/ → godog, ui/ → playwright-bdd)
  test/bdd/               godog step definitions
  web/                    React + TypeScript SPA (pnpm workspace package)
  e2e/                    Playwright + playwright-bdd UI suite (pnpm workspace package)
  deploy/                 container and compose assets
  docs/                   requirements, ADRs, threat model, roadmap, run log
  ```
- **Frontend:** React 19, TypeScript strict, Vite, Tailwind CSS v4, shadcn/ui-style
  components (copied into `web/src/components/ui`, owned by us), TanStack Query for server
  state, React Router for routing. pnpm workspaces.
- **Minimal dependencies.** The Go server uses the standard library only (net/http routing
  patterns, slog, os.Root). Each new dependency needs a justification in its PR.

## Consequences

- One image to scan, sign and ship; UI and API versions never drift.
- The SPA cannot be hosted on a separate CDN origin without relaxing CSP — acceptable.
- Splitting heavy query execution into workers later requires an internal RPC; the plugin
  interface (ADR-0002) is designed to be transport-agnostic to make that possible.
