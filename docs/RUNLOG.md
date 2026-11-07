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
- Follow-up in the same run: CI's minimum-Go job (go 1.24.0) showed `os.Root` letting a `../` directory symlink reach the parent directory, so the `go` directive is now 1.24.7 (security fix, noted in threat model F-03). Codex review raised 4 P2 findings (TSV quoting, parsing past the row-limit overflow, long duplicate headers, overflow row affecting notices/inference); all fixed with tests.

## 2026-11-07 ~14:45 UTC — hardening run on the open stack (no new PR)
- Triage: PRs #2, #3, #4 open and unmerged, all 7 CI checks green on each, every Codex review thread resolved. Three or more open → per the run rules, this run improves the existing PRs instead of opening a new one. `main` has not moved, so no rebase onto main was needed.
- Planned: an independent security review of the whole stack (Codex had only run code review, never its security review), then fix confirmed findings on the PR that owns the code, and re-stack.
- Shipped as new commits on the three branches (history is append-only: #2 fast-forwards, and #3/#4 merge the branch below them; stack order unchanged):
  - PR #2 ([#2](https://github.com/yesoreyeram/dv/pull/2)): rate limiter. IPv6 is now keyed by /64 (`ClientKey`). When the table is full, newcomers share one overflow bucket instead of a hard 429, and the full-table sweep runs at most once per second; it used to be O(100k) under the global lock on every newcomer request, measured at ~4 ms each. Config now rejects `DV_RATE_LIMIT_RPS=NaN`/`Inf`, which previously passed validation and silently disabled the limiter, and enforces ranges. NFR-SEC-008, threat model, and 2 new BDD scenarios.
  - PR #3 ([#3](https://github.com/yesoreyeram/dv/pull/3)): the frame byte estimate now counts the 16-byte `[]any` slot and boxed values. Real retained heap was 2.4× the estimate; it is now 1.15×, and a test asserts ≤ 1.6×. ADR-0003 and NFR-PERF-003 updated.
  - PR #4 ([#4](https://github.com/yesoreyeram/dv/pull/4)): the file source applies default limits (1M rows / 64 MiB, host-overridable) when a query leaves them at zero. Before, an 8 MiB wide CSV held ~480 MiB. `capReader` no longer returns a negative count after the size cap trips; that broke the `io.Reader` contract and panicked `bufio`. FR-011 AC8, threat model F-03, and 2 new BDD scenarios.
- Evidence: `go test -race ./...` green, total coverage 98.1 %; golangci-lint 0 issues; godog 21 api + 18 sources + 6 core scenarios; Vitest 100 %; UI BDD 13/13. Screenshots are in `docs/screenshots/feat-m0-foundation/hardening-*` and `docs/screenshots/feat-csv-file-source/hardening-*`.
- Not verified locally: govulncheck (golang.org blocked), gitleaks, and the Docker build (no daemon); all are left to CI. No dependency changes.
- Deferred: `/readyz` probe caching (B-043, plausible but not reproduced), host-level query limit config (B-044), and cluster-aware limiting (B-045).
- Next: once #2–#4 merge, B-014 `POST /api/v1/query` + B-042 provisioned instances + B-040 error mapping, then B-013 JSON.
