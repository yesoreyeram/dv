# 0006. Testing strategy: BDD first

- Status: Accepted
- Date: 2026-11-07
- Requirements: NFR-MAINT-001…NFR-MAINT-003, NFR-A11Y-001

## Context

Requirements must be executable so regressions are caught and traceability is mechanical.

## Decision

- **Specification first:** every feature starts as FR/NFR entries plus Gherkin scenarios
  in `features/`, tagged with requirement IDs, committed before implementation code.
- **API BDD:** `features/api/*.feature` run with [godog](https://github.com/cucumber/godog)
  from `go test ./test/bdd/...` against an in-process `httptest` server, so they are fast
  and deterministic and need no network.
- **UI BDD:** `features/ui/*.feature` run with Playwright + `playwright-bdd` (Chromium)
  against the real Go binary serving the built SPA; accessibility checked with axe-core.
- **Unit tests:** `go test -race` with coverage; Vitest + Testing Library for components.
- **Static analysis:** golangci-lint (incl. gosec), ESLint (incl. jsx-a11y), Prettier,
  `tsc --noEmit`.
- **Security scanning:** govulncheck, pnpm audit, gitleaks in CI.
- No skipped or disabled tests on `main`; flaky tests are fixed or the feature reverted.

## Consequences

- Each PR carries spec, code and tests together; reviewers read the `.feature` first.
