# 0004. Security model

- Status: Accepted
- Date: 2026-11-07
- Requirements: NFR-SEC-001…NFR-SEC-014, NFR-AUD-001

## Context

dv holds credentials to production databases and APIs. A compromise of dv is a compromise
of every connected system, so security is a design constraint, not a feature.

## Decision

1. **Secure defaults in the HTTP layer (M0):** strict CSP without inline scripts, the full
   set of hardening headers, request size limits, server timeouts, per-client rate limits,
   panic recovery with generic errors, request IDs.
2. **Authentication (M3):** local accounts with Argon2id; OIDC as a follow-up. Server-side
   sessions in an `HttpOnly; Secure; SameSite=Strict` cookie; session ID rotated on login;
   CSRF token for state-changing requests. API tokens for automation, hashed at rest.
3. **Authorization (M3):** RBAC with built-in roles Viewer < Editor < Admin, scoped per
   organisation; deny-by-default middleware on every `/api` route; object-level checks in
   services, not only in handlers.
4. **Secrets (M3):** envelope encryption — per-record DEK (AES-256-GCM), wrapped by a KEK
   supplied via environment/KMS, never stored with the data. Secret fields are write-only
   in the API (`"configured": true` is returned instead). `slog` handlers redact keys
   matching a secret pattern.
5. **Data-source safety:** parameterized SQL only (M4); SSRF guard at dial time with
   private-range blocking and redirect re-validation (M5); file access through `os.Root`
   (M2).
6. **Supply chain:** minimal dependencies, lockfiles, govulncheck, gosec, pnpm audit,
   gitleaks and Dependabot in CI; distroless non-root image. CI itself only runs pinned,
   verified tools (SHA-256 for downloads, exact versions for `go run`, tag/SHA for
   actions) and lints its workflows with actionlint (NFR-SEC-015).
7. **Audit:** append-only audit log for security-relevant actions (M3/M7).
8. **Process:** STRIDE entry per feature in `docs/security/THREAT_MODEL.md`; responsible
   disclosure in `SECURITY.md`.

## Consequences

- Inline `<script>`/`style` attributes are unavailable to the UI; Vite output complies.
- Some convenience features (embedding dashboards in iframes) need explicit, opt-in
  relaxations later, each with its own threat-model entry.
