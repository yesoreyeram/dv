# Roadmap

Milestones are delivered as a sequence of small PRs. Each PR traces to FR/NFR IDs in
`docs/requirements/`. Status is updated by every run.

| Milestone | Goal | Key requirements | Status |
| --- | --- | --- | --- |
| **M0** Foundation | Monorepo layout, Makefile, CI (lint/test/BDD/security), docs, requirements baseline, ADRs, threat model | NFR-MAINT-*, NFR-SEC-010/011/014, NFR-SEC-001 | Done (#2); merge automation + toolchain pins in PR #14 |
| **M1** API skeleton + UI shell | Go server with health/readiness, version, OpenAPI, hardened middleware, graceful shutdown; React shell with navigation, theming, status page | FR-001…FR-007, NFR-SEC-007/008/009/012/013, NFR-OBS-001…003, NFR-AVAIL-001/002, NFR-A11Y-001/002 | Done (#2) |
| **M2** Data frames + files | Data-frame model, plugin registry, CSV/TSV/JSON file sources under `os.Root`, query API, table + line-chart panels in Explore | FR-010…FR-015, NFR-SEC-006, NFR-PERF-003, NFR-A11Y-004, NFR-I18N-002 | In progress (frame model, registry, CSV/TSV file source merged in #3/#4; AC8 fix re-landed in PR #14; next: query API, JSON, Explore) |
| **M3** Auth, RBAC, secrets | Storage layer (SQLite/Postgres + migrations), local users, sessions, CSRF, RBAC, envelope-encrypted secrets, audit log v1 | FR-020…FR-022, NFR-SEC-002/003, NFR-AUD-001 | Planned |
| **M4** SQL sources | Postgres, MySQL, SQLite sources; safe query editor with parameters, timeouts, row limits | FR-030…FR-033, NFR-SEC-004 | Planned |
| **M5** API sources | REST/JSON and GraphQL sources behind the SSRF guard | FR-040, FR-041, NFR-SEC-005 | Planned |
| **M6** Dashboards | Dashboard CRUD with versioned JSON schema, grid layout, variables, time range | FR-050…FR-052 | Planned |
| **M7** Breadth & operations | NDJSON/XML/YAML/Parquet/Excel, S3-compatible storage, caching, audit-log UI, OpenTelemetry traces/metrics, SBOM | FR-060…FR-064, NFR-OBS-004, NFR-AUD-003 | Planned |
