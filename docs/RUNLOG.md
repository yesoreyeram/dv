# Run log

Each automated run appends an entry: date/time (UTC), plan, what shipped, PR link, next steps.

## 2026-11-07 — bootstrap
- Planned: create base commit on `main` so PRs have a base.
- Shipped: README.md, .gitignore, docs/RUNLOG.md (direct to main, one-time bootstrap).
- Next: M0 foundation PR.

## 2026-11-07 ~12:00 UTC — M0 foundation + M1 skeleton
- Planned: B-001…B-005 — requirements baseline, ADRs, threat model, roadmap/backlog, Go server skeleton, React shell, BDD suites, CI, container assets.
- Shipped (PR `feat/m0-foundation`): FR-001…FR-007 and the M0/M1 NFR baseline. Go server (stdlib only) with hardened middleware; 14 godog scenarios; React shell with 22 Vitest tests (100 % coverage) and 11 playwright-bdd scenarios incl. axe in light/dark; Go coverage 97.6 %; CI workflow; distroless Dockerfile + compose.
- Not verified locally: `govulncheck` (vuln.go.dev unreachable), gitleaks, Docker image build (no Docker daemon) — these run in CI only. Go toolchain is 1.24.7 locally; CI also tests on `stable` (B-030).
- Environment notes for future runs: proxy.golang.org, gopkg.in and golang.org are blocked from the sandbox; GitHub and npm are reachable. Go modules hosted on GitHub resolve with `GOPROXY=direct GOSUMDB=off`; for vanity-path modules that are only needed as `go.mod` files, serve them from a local `file://` GOPROXY fetched from raw.githubusercontent.com. Prefer stdlib or GitHub-hosted Go deps.
- Next: B-010 data-frame model, then B-011 plugin registry and B-012 CSV source (M2).
