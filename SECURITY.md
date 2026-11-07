# Security policy

## Reporting a vulnerability

Please **do not open a public issue**. Report vulnerabilities privately through
[GitHub Security Advisories](https://github.com/yesoreyeram/dv/security/advisories/new).
Include affected version/commit, reproduction steps and impact.

We aim to acknowledge reports within 3 business days and to provide a remediation plan
within 14 days. Coordinated disclosure: we will credit reporters unless asked not to.

## Supported versions

dv is pre-1.0; only the latest `main` receives security fixes.

## Security design

- Threat model: [`docs/security/THREAT_MODEL.md`](docs/security/THREAT_MODEL.md)
- Security architecture: [ADR-0004](docs/adr/0004-security-model.md)
- Security requirements: `NFR-SEC-*` in [`docs/requirements/NON_FUNCTIONAL.md`](docs/requirements/NON_FUNCTIONAL.md)

Every pull request runs golangci-lint with gosec, govulncheck, `pnpm audit`, and gitleaks
secret scanning; the release image is distroless and runs as a non-root user.

## Dependency advisories

`pnpm audit` fails the build on any advisory in a production dependency and on any
moderate-or-above advisory anywhere, dev tooling included (NFR-SEC-010). When a
transitive dependency is vulnerable and its parent has no fixed release, we add a
scoped override in `pnpm-workspace.yaml` (`parent>child`, exact patched version) and
list it here. Remove an entry once the parent's own range includes a fixed version.

| Advisory | Package | Scope | Override | Added | Review by |
| --- | --- | --- | --- | --- | --- |
| [GHSA-g3hv-75wg-m249](https://github.com/advisories/GHSA-g3hv-75wg-m249) | `minimatch` <11.0.1 (via `@typescript-eslint/typescript-estree` 8.72.1, `^10.2.2`) | dev only (ESLint) | `@typescript-eslint/typescript-estree>minimatch` → `11.0.3` | 2026-11-07 | next `typescript-eslint` minor |

Ignored advisories (`auditConfig.ignoreGhsas`): none.

## Hardening checklist for operators

- Terminate TLS at dv or a trusted ingress; HSTS is sent automatically over TLS.
- Run the container read-only with all capabilities dropped (see `deploy/docker-compose.yml`).
- Keep `/healthz` and `/readyz` reachable only from your orchestrator where possible.
- Until authentication lands (milestone M3), do not expose dv to untrusted networks.
