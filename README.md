# dv — secure data visualization platform

`dv` is an enterprise-grade, security-first data visualization platform. It connects to
SQL databases, REST/GraphQL APIs and file/object storage, normalizes many formats
(JSON, CSV, TSV, NDJSON, Parquet, XML, YAML, Excel) into a common data-frame model, and
lets users build queries, panels and dashboards.

> **Status:** M0 foundation + M1 skeleton. The server, health probes, OpenAPI document and
> the UI shell work today; data sources, panels and dashboards are on the
> [roadmap](docs/ROADMAP.md).

## Architecture at a glance

- **Single Go binary** (`cmd/dv`, standard library only) serving the JSON API, probes and the
  compiled SPA from one origin, behind a hardened middleware chain (CSP & security headers,
  request IDs, W3C trace context, structured `slog` access logs, panic recovery, body limits,
  per-client rate limiting, server timeouts, graceful shutdown). See
  [ADR-0001](docs/adr/0001-architecture-and-repository-layout.md).
- **Web UI** (`web/`): React 19 + TypeScript (strict), Vite, Tailwind CSS v4,
  shadcn/ui-style components, TanStack Query, React Router.
- **Plugin model** for sources and formats ([ADR-0002](docs/adr/0002-data-source-plugin-model.md))
  feeding a columnar data-frame model ([ADR-0003](docs/adr/0003-data-frame-model.md)).
- **Security model**: [ADR-0004](docs/adr/0004-security-model.md),
  [threat model](docs/security/THREAT_MODEL.md), [SECURITY.md](SECURITY.md).

## Quick start

Requirements: Go ≥ 1.24.7 (earlier 1.24 releases let `os.Root` escape through symlinks to `..`), Node ≥ 22.12 (CI and the image use Node 26, the Active LTS line), pnpm 10 (`corepack enable`), GNU make. With [mise](https://mise.jdx.dev), `mise install` installs the pinned versions from `mise.toml`.

```bash
make deps          # go mod download + pnpm install
make run           # builds web + server, serves http://127.0.0.1:8080
make dev           # API via `go run` + Vite dev server with HMR on http://127.0.0.1:5173
```

Or with containers: `make compose-up` (http://127.0.0.1:8080).

```bash
curl -s localhost:8080/healthz         # {"status":"ok"}
curl -s localhost:8080/readyz          # {"status":"ready","checks":{"file-roots":"pass","plugins":"pass"}}
curl -s localhost:8080/api/v1/version  # build info
curl -s localhost:8080/api/v1/plugins  # compiled-in data-source and format plugins
curl -s localhost:8080/api/openapi.json
```

## Configuration

Defaults < JSON file (`DV_CONFIG_FILE`) < environment. Invalid configuration fails at start-up.

| Variable | Default | Description |
| --- | --- | --- |
| `DV_HTTP_ADDR` | `:8080` | Listen address |
| `DV_WEB_DIR` | _(empty)_ | Directory of the built SPA; empty = API only |
| `DV_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `DV_LOG_FORMAT` | `json` | `json` or `text` |
| `DV_RATE_LIMIT_RPS` / `DV_RATE_LIMIT_BURST` | `20` / `40` | Per-client token bucket (IPv4 address / IPv6 /64); `0` RPS disables, otherwise 0.01–1e6 RPS and burst 1–1e6 |
| `DV_READ_HEADER_TIMEOUT`, `DV_READ_TIMEOUT`, `DV_WRITE_TIMEOUT`, `DV_IDLE_TIMEOUT` | `5s`, `30s`, `60s`, `120s` | HTTP server timeouts |
| `DV_SHUTDOWN_TIMEOUT` | `15s` | Graceful drain window |
| `DV_READINESS_CHECK_TIMEOUT` | `2s` | Per-check timeout for `/readyz` |
| `DV_READINESS_CACHE_TTL` | `1s` | How long a `/readyz` result is reused; concurrent probes always share one run. After it expires, the last result is still served for up to `DV_READINESS_CHECK_TIMEOUT` while one refresh runs in the background. `0` disables reuse, max `1m` |
| `DV_MAX_HEADER_BYTES` / `DV_MAX_BODY_BYTES` | `65536` / `1048576` | Request size limits |
| `DV_FILE_ROOTS` | _(empty)_ | Directories the `file` source may read, as `name=/abs/dir,other=/abs/dir2` (JSON: `fileRoots: {"name": "/abs/dir"}`). Opened with `os.Root`; nothing outside them is readable. `make run`/`make dev`/compose expose `deploy/samples` as `samples` |
| `DV_FILE_MAX_BYTES` | `67108864` | Largest file the `file` source reads (1 KiB – 1 GiB) |

JSON file keys use camelCase (`httpAddr`, `shutdownTimeout: "15s"`, …); unknown keys are rejected.

## Development

```bash
make lint      # golangci-lint (incl. gosec), go vet, ESLint, tsc --noEmit, Prettier
make test      # go test -race + Vitest with coverage thresholds
make bdd       # godog (features/api) + playwright-bdd with axe (features/ui)
make scan      # govulncheck + pnpm audit
make perf      # probe p99 under 500 rps open-loop load on one core (NFR-PERF-001)
make bench     # probe benchmarks
make size      # initial JS/CSS gzip budget (NFR-PERF-004)
make help      # everything else
```

Specifications live in [`features/`](features) (Gherkin) and
[`docs/requirements/`](docs/requirements). Planning: [roadmap](docs/ROADMAP.md),
[backlog](docs/BACKLOG.md), [run log](docs/RUNLOG.md). See [CONTRIBUTING.md](CONTRIBUTING.md).
