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

## 2026-11-07 ~19:30 UTC — B-035 npm advisory fix + stricter audit gate on the open stack (no new PR)
- Triage: PRs #2, #3, #4 open and unmerged, all 7 CI checks green on each, every review finding addressed (the one open thread on #2 is outdated and already answered in 9c0b364), `main` unchanged. Three or more open, so per the run rules this run improves the stack instead of opening a new PR.
- Planned: B-035, the only known advisory in the tree (moderate `minimatch` GHSA-g3hv-75wg-m249, dev-only via typescript-eslint), owned by PR #2. Requirements first (47254f3): NFR-SEC-010 AC1–AC4.
- Shipped on PR #2 ([#2](https://github.com/yesoreyeram/dv/pull/2)), then merged up into #3 and #4 (append-only, stack order unchanged):
  - Scoped override `@typescript-eslint/typescript-estree>minimatch` → `11.0.3` in `pnpm-workspace.yaml` (typescript-estree 8.72.1 pins `^10.2.2`; only 11.0.1+ is patched). Listed with its advisory and review trigger in `SECURITY.md`.
  - Audit gate tightened in CI and `make audit`: production deps fail on any advisory (`--prod --audit-level low`), all deps fail on moderate+. Negative control: the previous lockfile fails the new gate (exit 1) but passed the old high-only gate (exit 0).
  - Compatibility: typescript-estree calls `minimatch(path, pattern, {dot: true})` only (and only with `allowDefaultProject`, which we do not use); 80 pattern/path cases give identical results on 10.2.6 and 11.0.3.
- Evidence: `pnpm audit` clean at both levels; `pnpm install --frozen-lockfile` OK; ESLint, tsc, Prettier clean; Vitest 31/31; web build + bundle budget pass (106.7 KB JS gzip); `make build` + run, `/readyz` ready. Screenshots: `docs/screenshots/feat-m0-foundation/run9-*`. No Go changes.
- Not verified locally: govulncheck, godog, gitleaks, Docker build (golang.org/gopkg.in blocked, no daemon) — left to CI.
- Next: once #2–#4 merge, B-014 `POST /api/v1/query` + B-042 + B-040, then B-013 JSON. Still blocked here by the sandbox: B-030 (Go bump) and B-033 (`go mod tidy -diff`).
