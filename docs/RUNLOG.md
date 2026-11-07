# Run log

Each automated run appends an entry: date/time (UTC), plan, what shipped, PR link, next steps.

## 2026-11-07 — bootstrap
- Planned: create base commit on `main` so PRs have a base.
- Shipped: README.md, .gitignore, docs/RUNLOG.md (direct to main, one-time bootstrap).
- Next: M0 foundation PR.

## 2026-11-07 ~12:00 UTC — M0 foundation + M1 skeleton
- Planned: B-001…B-005 — requirements baseline, ADRs, threat model, roadmap/backlog, Go server skeleton, React shell, BDD suites, CI, container assets.
- Shipped ([PR #2](https://github.com/yesoreyeram/dv/pull/2), `feat/m0-foundation`): FR-001…FR-007 and the M0/M1 NFR baseline. Go server (stdlib only) with hardened middleware; 14 godog scenarios; React shell with 22 Vitest tests (100 % coverage) and 11 playwright-bdd scenarios incl. axe in light/dark; Go coverage 97.6 %; CI workflow; distroless Dockerfile + compose.
- Not verified locally: `govulncheck` (vuln.go.dev unreachable), gitleaks, Docker image build (no Docker daemon) — first CI run: all three green. Go toolchain is 1.24.7 locally; CI `stable` is go1.27.3 and caught a test that over-specified the ServeMux clean-path redirect code (301 → 307 in newer Go); fixed to accept any 3xx to the cleaned path (B-030).
- Environment notes for future runs: proxy.golang.org, gopkg.in and golang.org are blocked from the sandbox; GitHub and npm are reachable. Go modules hosted on GitHub resolve with `GOPROXY=direct GOSUMDB=off`; for vanity-path modules that are only needed as `go.mod` files, serve them from a local `file://` GOPROXY fetched from raw.githubusercontent.com. Prefer stdlib or GitHub-hosted Go deps.
- Next: B-010 data-frame model, then B-011 plugin registry and B-012 CSV source (M2).

## 2026-11-07 ~12:50 UTC — data-frame model + plugin registry
- Triage: PR #2 (M0/M1) open, all 7 CI checks green, no review comments → new work, branched from `feat/m0-foundation` (dependency stated in PR).
- Planned: B-010 data-frame model, B-011 plugin registry (+ catalogue endpoint and Data sources page so the increment is visible).
- Shipped ([PR #3](https://github.com/yesoreyeram/dv/pull/3), `feat/frame-plugin-registry`): FR-010 and new FR-015 with acceptance criteria, threat model F-08, ADR-0002/0003 implementation notes; `internal/frame` (typed columns, atomic appends, strict wire codec, row/byte-limited builder), `internal/plugin` (interfaces, frozen registry, readiness check, redacted `SecureValues`), `GET /api/v1/plugins`, TS frame decoder, Data sources page. 25 godog scenarios (19 API + 6 core), 48 Vitest tests, 12 UI BDD scenarios incl. axe; Go coverage frame 98.3 %, plugin 98.7 %.
- Not verified locally: govulncheck (golang.org blocked; no new Go deps), gitleaks, Docker build — left to CI.
- Next: B-012 CSV/TSV format + file source under `os.Root` (introduces B-037), then B-013 JSON and B-014 query endpoint.

## 2026-11-07 ~13:40 UTC — CSV/TSV file source under os.Root
- Triage: PR #2 and PR #3 open, all 7 CI checks green on both, no review comments (Codex review: no findings) → new work, stacked on `feat/frame-plugin-registry` (PR #3).
- Planned: B-012 CSV/TSV format + file source, with B-037 host-provided `os.Root`.
- Shipped (PR #4, `feat/csv-file-source`): FR-011 acceptance criteria (AC1–AC10), threat model F-03, ADR-0002 notes; `internal/fsroot` (named roots via `os.Root`, `CleanPath`, regular-file + size checks on the handle, growth cap, path-free errors, `file-roots` readiness check); `internal/plugins/csvformat` (csv/tsv, inference, overrides, limits, fuzz test); `internal/plugins/filesource` (`file` source); `fileRoots`/`DV_FILE_ROOTS`, `fileMaxBytes`/`DV_FILE_MAX_BYTES`; sample files wired into `make run`/`make dev`/compose (read-only mount). 16 new godog scenarios (features/sources), 1 new UI scenario; Go total coverage 97.9 %.
- Not verified locally: govulncheck (golang.org blocked; no new Go deps), gitleaks, Docker build — left to CI. Files can't yet be queried over HTTP: that needs B-014 (query API) and B-042 (provisioned instances).
- Next: B-014 `POST /api/v1/query` + B-042 provisioned instances (+ B-040 error mapping), then B-013 JSON format and B-015 Explore table.
