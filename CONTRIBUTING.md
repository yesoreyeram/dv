# Contributing to dv

## Workflow

1. **Pick work** from [`docs/BACKLOG.md`](docs/BACKLOG.md). Branch from `main` as
   `feat/<topic>`, `fix/<topic>` or `docs/<topic>`. Never push to `main`.
2. **Specify first.** Before any implementation code, add or update:
   - requirement entries in `docs/requirements/FUNCTIONAL.md` / `NON_FUNCTIONAL.md`
     (stable IDs, MoSCoW priority, acceptance criteria);
   - Gherkin scenarios in `features/api` (godog) or `features/ui` (playwright-bdd), tagged
     with the requirement IDs (`@FR-012`);
   - a STRIDE entry in `docs/security/THREAT_MODEL.md`;
   - an ADR in `docs/adr/` for any significant decision.
   Commit these first; implementation follows in later commits of the same PR.
3. **Implement with tests.** Unit tests alongside code (`go test -race`, Vitest); step
   definitions in `test/bdd` (Go) or `e2e/steps` (UI).
4. **Run everything locally:** `make lint test bdd scan`.
5. **Open a PR** that lists the FR/NFR IDs, security considerations, test evidence,
   screenshots (`docs/screenshots/<branch>/`) and follow-ups, and updates
   `docs/RUNLOG.md` and `docs/BACKLOG.md`.
6. **Merging.** PRs target `main` and merge automatically (squash) once the `CI` workflow
   is green for the PR's current head commit and the author is allow-listed
   ([`automerge.yml`](.github/workflows/automerge.yml), NFR-MAINT-006). Add the `hold`
   label to keep a PR open for review. Do not stack PRs on feature branches: a PR whose
   base branch merged first never reaches `main` (this happened to PR #5). If you must
   stack, retarget the child to `main` as soon as the parent merges.

## Toolchain

[`mise.toml`](mise.toml) pins Go, Node (LTS), pnpm and golangci-lint. With
[mise](https://mise.jdx.dev) installed, `mise install` sets them up; `make check-toolchain`
(also in CI) fails when `mise.toml`, `go.mod`, `package.json`, the Dockerfile and CI
disagree (NFR-MAINT-007). Dependabot updates Go modules, npm packages, GitHub Actions and
Docker images (grouped, weekly); Renovate updates only the toolchain pins (`mise.toml` and the `go.mod` `go`
directive, `packageManager` and CI golangci-lint that must match it) in one grouped PR
(NFR-MAINT-008).

### CI tools (NFR-SEC-015)

Workflows may only run tools that are pinned and verified, and `make lint-workflows`
(also a unit test in CI) enforces it:

- A binary downloaded with `curl`/`wget` is saved to a file, checked with
  `echo "<sha256>  file" | sha256sum -c -` against a hash committed in the same step, and
  only then extracted. That exact form is the only one accepted: a standalone command, one
  per file, at the top level of the step (not in `if`/`case`/loops/functions/subshells, not
  with `&&`, `||` or `&`, not after `set +e`), and no checksum manifests on disk.
  `curl … | tar` and `curl … | sh` are rejected. To bump such a tool,
  change the version and take the new hash from the release's checksums file.
- `go run`/`go install` of an external tool uses an exact `@vX.Y.Z`, never `@latest`.
- `uses:` names a tag (`@v4`) or a 40-character commit SHA; `docker://` images need an
  `@sha256:` digest.

CI also runs [actionlint](https://github.com/rhysd/actionlint) (with shellcheck) on every
workflow; install it locally (`mise use -g actionlint`, `brew install actionlint`) and
`make lint-workflows` runs it too.

## Standards

- Go: standard layout, `context.Context` first parameter for I/O, `log/slog` only, no
  global state outside `main`, errors wrapped with `%w`, no `panic` for control flow.
  New dependencies need a justification in the PR.
- TypeScript: `strict` plus `noUncheckedIndexedAccess` and `exactOptionalPropertyTypes`;
  no `any`; no `dangerouslySetInnerHTML`; user-facing copy goes through `t()` in
  `web/src/i18n/messages.ts`.
- Accessibility: WCAG 2.2 AA; axe scans must report no serious/critical violations.
- Security: never log or return secrets; parameterized SQL only; outbound HTTP via the
  SSRF-guarded client; file access via `os.Root`.
- No skipped/disabled tests, no TODO stubs presented as finished work.

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/): `feat(server): …`,
`fix(web): …`, `docs: …`, `ci: …`, `test: …`.
