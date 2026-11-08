# 0007. Merge automation and dependency-update policy

- Status: Accepted
- Date: 2026-11-08
- Requirements: NFR-MAINT-006, NFR-MAINT-007, NFR-MAINT-008, NFR-SEC-014

## Context

Automated runs and Dependabot open PRs faster than they are merged by hand. On 2026-11-07
eight Dependabot PRs were open at once, and a stacked PR (#5) merged into
`feat/m0-foundation` after that branch had already been squash-merged, so its fix never
reached `main`. The owner asked for PRs to merge automatically when CI passes.

Options considered:

1. **GitHub native auto-merge** (`gh pr merge --auto`, the pattern GitHub documents with
   `dependabot/fetch-metadata`). Needs the "Allow auto-merge" setting plus branch
   protection with required checks; without required checks a PR is "clean" immediately
   and merges before CI finishes. The setting is not enabled on this repository and cannot
   be changed from a PR.
2. **Third-party merge bots** (Mergify, Kodiak, Renovate automerge). Extra app with broad
   write access; policy lives outside the repo's tests.
3. **Own `workflow_run` job** that runs after `CI` completes, re-verifies every check run on
   the head SHA and merges with the SHA pinned.

## Decision

Option 3: `.github/workflows/automerge.yml` runs `cmd/dv-automerge`, whose rules are a pure
function in `internal/automerge` with unit tests and `features/repo/automerge.feature`.
It works without repository settings, never runs PR code (`workflow_run` uses the default
branch copy), and is safe even without branch protection because it acts only after CI
succeeded and pins the SHA. If the owner later enables branch protection with required
checks, GitHub enforces it on the bot's merge as well.

Dependency updates: Dependabot owns Go modules, npm, GitHub Actions and Docker (grouped to
limit PR count; odd Node majors ignored). Renovate is configured only for the toolchain
pins: `mise.toml` plus, through regex managers, the `go.mod` `go` directive,
`package.json#packageManager` and the CI golangci-lint version, grouped into one PR so
`check-toolchain.sh` passes. Dependabot manages none of these, so no dependency gets two
PRs. Go minor/major and Node major bumps stay manual migrations.

## Consequences

- A merge made with `GITHUB_TOKEN` does not trigger new workflow runs, so the `push`-to-
  `main` CI run does not happen for bot merges; the PR's CI run on the same tree is the
  evidence. Switching the token to a GitHub App token would restore it.
- Only PRs targeting the default branch merge; stacked PRs must be retargeted.
- A compromised dependency release that passes all CI checks could merge; the `hold` label,
  grouping and weekly cadence bound the exposure (threat model F-09).
- The workflow is only active once this file is on `main`.
