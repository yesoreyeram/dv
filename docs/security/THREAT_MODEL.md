# Threat model

Method: STRIDE per feature, updated in the same PR that introduces or changes the feature
(NFR-SEC-001). Each threat lists the mitigation and where it is verified. Residual risks
are tracked in `docs/BACKLOG.md`.

## System context (M0/M1)

```
 Browser (SPA, same origin) ──HTTPS──▶ dv server (Go, single binary)
                                         ├── /healthz, /readyz      (public probes)
                                         ├── /api/v1/*              (JSON API)
                                         ├── /api/openapi.json      (API description)
                                         └── /*                     (static SPA assets)
```

Trust boundaries: (1) internet ↔ dv server, (2) dv server ↔ data sources (from M2),
(3) dv server ↔ secret store (from M3). TLS is terminated either by the server
(`DV_TLS_CERT_FILE`/`DV_TLS_KEY_FILE`, later) or by an ingress; HSTS is emitted when the
request arrived over TLS.

Assets: availability of the service, integrity of served UI code, confidentiality of
internal topology (hostnames, IPs, versions of dependencies), and — from M3 — user
credentials and data-source secrets.

## F-01 HTTP server baseline (FR-001…FR-004, NFR-SEC-007/008/009/012/013)

| STRIDE | Threat | Mitigation | Verified by |
| --- | --- | --- | --- |
| S | Spoofed `X-Forwarded-For` to evade rate limits | Client identity = TCP peer address; forwarded headers are ignored until an explicit trusted-proxy list is configured (backlog B-021). | `ratelimit_test.go` |
| T | XSS / script injection into the SPA | Strict CSP: `default-src 'self'; script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'`; React escapes text; OpenAPI rendered as text only. | `security.feature`, `headers_test.go`, `features/ui/csp.feature` (zero violations in a real browser; injected inline script blocked) |
| T | Clickjacking | `X-Frame-Options: DENY` + `frame-ancestors 'none'` | `security.feature` |
| T | MIME sniffing of API responses | `X-Content-Type-Options: nosniff`, explicit `Content-Type` | `security.feature` |
| R | Actions not attributable | Request ID on every request and access log; audit log lands with authN (M3) | `requestid_test.go` |
| R | Log injection via request ID header | Incoming `X-Request-ID` accepted only if it matches `^[A-Za-z0-9._-]{1,64}$`, else regenerated; slog JSON handler escapes values | `security.feature` |
| I | Error messages leak internals (stack traces, IPs, DSNs) | Panic recovery returns generic JSON `500`; readiness reduces check errors to `pass`/`fail`; errors logged server-side only | `health.feature`, `recover_test.go` |
| I | Version disclosure aids attackers | `/healthz` and `/readyz` reveal no version; `/api/v1/version` will require authentication once M3 lands (backlog B-020) | Review |
| I | Referrer leakage of dashboard URLs | `Referrer-Policy: no-referrer` | `security.feature` |
| D | Request flooding | Per-client token bucket (`429` + `Retry-After`), bounded client table with idle eviction only after a bucket would have fully refilled (TTL = max(3 min, burst/rps)), so eviction never grants an early fresh burst. Applied to `/api/` only: probes are exempt and the SPA document/assets (cheap static reads) never spend the API allowance, so a low burst cannot blank the UI; static-asset flooding is left to timeouts and the ingress/CDN (residual) | `security.feature`, `httpx_test.go` |
| D | Filling the limiter table (many IPv6 addresses from one /64, or many hosts) to lock out new clients and make every newcomer pay an O(table) sweep under the global lock | IPv6 keyed by /64; full table → newcomers share one overflow bucket instead of a hard deny; full-table sweep throttled to once per second | `ratelimit_test.go` (`TestRateLimiterFullTableIsNotOSquared`, `TestClientKey`), `security.feature` |
| T | Mis-set `DV_RATE_LIMIT_RPS=NaN`/`Inf` silently disabling the limiter | Non-finite and out-of-range rate/burst rejected at start-up | `config_test.go` |
| D | Slowloris / oversized headers / bodies | `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, `MaxHeaderBytes`; `http.MaxBytesReader` on bodies | `config_test.go`, `server_test.go` |
| D | Hung readiness dependency stalls probes | Each check runs with a timeout derived from the request context | `health_test.go` |
| D | Probe flood multiplies dependency load or piles up goroutines stuck in a hung check | Concurrent probes share one evaluation (singleflight) and reuse it for `DV_READINESS_CACHE_TTL` (1 s); a check still running from an earlier probe is not restarted and reports `fail`, so at most one stuck goroutine exists per check (FR-002 AC5/AC6, B-043). The "still running" flag is cleared before a finished check hands back its result, so back-to-back probes never see a completed check as stuck (that race caused spurious 503s). Probe latency under a 500 rps flood is verified (NFR-PERF-001) | `health_test.go` (`TestFinishedCheckIsNeverReportedAsRunning`), `perf_test.go`, `health.feature` |
| E | Path traversal through static file serving | Static assets served via `http.FS(os.DirFS(root))` / `os.Root` semantics; `..` rejected; SPA fallback only for extension-less paths | `static_test.go` |
| E | Container breakout impact | Distroless non-root image, read-only root FS, `cap_drop: [ALL]`, `no-new-privileges` | `Dockerfile`, `docker-compose.yml` |

Residual risks: no TLS termination built in yet (rely on ingress); `/api/v1/version`
public until M3; rate limiting is per-instance (not cluster-wide).

## F-02 Web application shell (FR-005…FR-007, NFR-A11Y-*)

| STRIDE | Threat | Mitigation | Verified by |
| --- | --- | --- | --- |
| T | Malicious content in API responses rendered as HTML | No `dangerouslySetInnerHTML`; ESLint `react/no-danger` equivalent enforced by review; values rendered as text | Vitest, review |
| T | Supply-chain compromise of npm packages | Lockfile pinned, `--frozen-lockfile` in CI, `pnpm audit`, Dependabot, minimal dependency set, no CDN scripts (CSP forbids them) | CI |
| I | Theme preference storage leaks data | Only `"light"`/`"dark"` stored in `localStorage`; reads wrapped in try/catch | `theme.test.tsx` |
| D | Unbounded retries hammer the API | TanStack Query retry capped (2) with exponential backoff | `queryClient.ts` |

## Planned features (threats to model at implementation time)

- **F-03 File sources (M2):** path traversal, symlink escape, zip bombs, CSV formula injection on export, memory exhaustion → `os.Root`, size/row caps, streaming parsers.
- **F-04 AuthN/RBAC (M3):** credential stuffing, session fixation, CSRF, privilege escalation, IDOR → Argon2id, rotating session IDs, SameSite=strict cookies + CSRF tokens, server-side RBAC, object-level checks.
- **F-05 Secrets (M3):** key exposure, secrets in logs/responses → envelope encryption (AES-256-GCM, KEK from env/KMS), write-only API fields, slog redaction.
- **F-06 SQL sources (M4):** SQL injection, expensive queries, data exfiltration → parameterized queries, read-only transactions, statement timeouts, row limits.
- **F-07 REST/GraphQL sources (M5):** SSRF (cloud metadata, internal services, DNS rebinding) → dial-time IP checks, redirect re-validation, allow-lists, response size caps.
