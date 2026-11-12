# Production readiness

Audit date: 2026-11-12, at `main` = `ffe71bc` (milestone M2 of 7) plus the workspace UI
branch `feat/pro-workspace-ui`.

**Verdict: dv is not ready to serve users over a network.** It is a sound, well-tested
single-operator tool on loopback. The engineering base is strong (hardened HTTP layer,
CSP, supply-chain checks, BDD and accessibility gates). What is missing is the product
core every network deployment needs: identity, storage, secrets, an audit trail and a
release process. This page lists the gaps and the order to close them.

## What is production grade today

| Area | State | Evidence |
| --- | --- | --- |
| HTTP hardening | Strict CSP, security headers, body and header limits, timeouts, per-client rate limit, request IDs, panic recovery | NFR-SEC-007/008/009/012/013, `csp.feature` |
| Health | Separate liveness and readiness, cached and single-flight readiness, graceful shutdown | FR-001/002, NFR-AVAIL-001/002 |
| File access | Rooted with `os.Root`, path rules, size and row limits, fuzzed path cleaning | FR-011, NFR-SEC-006 |
| Query API | Off by default, loopback-only gate with `Host` check, per-request memory bound, per-query timeout | FR-016, B-062, B-063 |
| Supply chain | Pinned toolchains, verified CI downloads, govulncheck, `pnpm audit`, gitleaks, distroless non-root image | NFR-SEC-010/011/014/015 |
| Frontend quality | 315 unit tests (97 % statements), 67 browser scenarios, axe scans in both themes, bundle budget | NFR-A11Y-001…006, NFR-PERF-004 |
| Frontend resilience | Page error boundary, validated server bodies and browser storage, no inline script or style | NFR-AVAIL-004, NFR-SEC-019 (this branch) |

## Gaps

P0 blocks any deployment beyond one operator on loopback. P1 is needed for a first
supported release. P2 can follow it.

| # | Gap | Why it matters | Pri | Backlog |
| --- | --- | --- | --- | --- |
| 1 | No authentication: every endpoint is anonymous | Anyone who can reach the port can query every provisioned source. The loopback gate is the only control. | P0 | B-023, B-020 |
| 2 | No authorisation model | No way to limit who sees which data source or (later) dashboard | P0 | B-024 |
| 3 | No persistent storage | Data sources come from a config file; nothing a user does can be saved, so dashboards and accounts cannot exist | P0 | B-022 |
| 4 | No secret storage | SQL and HTTP sources need credentials; there is nowhere safe to keep them | P0 | B-025 |
| 5 | No audit log | No record of who queried or changed what; required by NFR-AUD-001 and by most enterprise buyers | P0 | (M3, with B-023) |
| 6 | TLS and proxy trust undefined | The server speaks plain HTTP and sends HSTS; behind an ingress the rate limiter keys on the proxy address | P0 | B-079, B-021 |
| 7 | No release process | No tags, no published image (CI builds with `push: false`), no SBOM or signature, no changelog, no licence | P0 | B-078, B-034 (owner) |
| 8 | Metrics and traces not exported | Operators cannot alert on error rate, latency or query duration; only logs exist | P1 | NFR-OBS-004, B-080 |
| 9 | Deployment artefacts stop at `docker compose` | No Kubernetes manifests or chart, no sizing guidance, no backup and restore runbook | P1 | B-081 |
| 10 | Product scope: local files only | No SQL or HTTP sources (M4, M5) and no dashboards (M6); "Dashboards" is a placeholder | P1 | M4–M6 |
| 11 | Query API not load tested | Only the health endpoints have a latency budget and a load test | P1 | B-083 |
| 12 | Rate limiting is per instance | Limits multiply with replicas | P1 | B-045 |
| 13 | No scheduled security scan of `main`; actions pinned by tag | A new advisory surfaces only when a PR happens to run | P1 | B-074, B-058 |
| 14 | Client errors stay in the browser console | A page crash in the field is invisible to operators | P2 | B-082 |
| 15 | UI: English only, no route-level code splitting, no visual regression tests, preferences per browser not per user | Fine at this size; each becomes a cost as the app grows | P2 | B-017, B-084 |
| 16 | Explore: no file browser or query inspector | Users must know file paths by heart | P2 | B-064, B-041 |

## Order of work

The hourly task works the backlog top to bottom, one PR per run. Recommended order:

1. **Storage** (B-022): SQLite and Postgres behind one interface, migrations, readiness
   check. Everything below depends on it.
2. **Identity** (B-023, then B-024): local users with Argon2id, server-side sessions,
   CSRF, then roles. The UI gets a sign-in page, a user menu in the top bar and
   session-expiry handling; the query API gate moves from "loopback" to "signed in".
3. **Secrets and audit** (B-025, audit log v1): envelope encryption, log redaction, an
   append-only audit table written by the auth and query paths.
4. **Edge** (B-079, B-021): an ADR choosing ingress-terminated TLS, a native TLS
   listener, or both; trusted-proxy configuration for client addresses.
5. **Release** (B-078): tagged releases, multi-arch image in GHCR, SBOM, signature and
   provenance, changelog, upgrade policy. Needs the licence decision (B-034).
6. **Operations** (B-080, B-081, B-083): metrics and traces, Kubernetes manifests,
   query API load test.
7. Then the product milestones M4 (SQL), M5 (HTTP), M6 (dashboards).

Steps 1–5 are the production gate: with them closed, dv can be offered to a team on a
private network with file sources. Steps 6–7 make it competitive.

## Owner decisions needed

- **Licence** (B-034). Blocks any public release.
- **TLS stance** (B-079): ingress only is simpler; a native listener helps small installs.
- **First identity provider after local users**: OIDC is the usual enterprise ask; it is
  not on the roadmap yet.
- **Repository settings** (B-054): branch protection on `main` and the `hold` label.
