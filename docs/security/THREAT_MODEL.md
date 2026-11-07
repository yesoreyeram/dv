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
| T | XSS / script injection into the SPA | Strict CSP: `default-src 'self'; script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'`; React escapes text; OpenAPI rendered as text only. | `security.feature`, `headers_test.go` |
| T | Clickjacking | `X-Frame-Options: DENY` + `frame-ancestors 'none'` | `security.feature` |
| T | MIME sniffing of API responses | `X-Content-Type-Options: nosniff`, explicit `Content-Type` | `security.feature` |
| R | Actions not attributable | Request ID on every request and access log; audit log lands with authN (M3) | `requestid_test.go` |
| R | Log injection via request ID header | Incoming `X-Request-ID` accepted only if it matches `^[A-Za-z0-9._-]{1,64}$`, else regenerated; slog JSON handler escapes values | `security.feature` |
| I | Error messages leak internals (stack traces, IPs, DSNs) | Panic recovery returns generic JSON `500`; readiness reduces check errors to `pass`/`fail`; errors logged server-side only | `health.feature`, `recover_test.go` |
| I | Version disclosure aids attackers | `/healthz` and `/readyz` reveal no version; `/api/v1/version` will require authentication once M3 lands (backlog B-020) | Review |
| I | Referrer leakage of dashboard URLs | `Referrer-Policy: no-referrer` | `security.feature` |
| D | Request flooding | Per-client token bucket (`429` + `Retry-After`), bounded client table with idle eviction | `security.feature`, `ratelimit_test.go` |
| D | Slowloris / oversized headers / bodies | `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, `MaxHeaderBytes`; `http.MaxBytesReader` on bodies | `config_test.go`, `server_test.go` |
| D | Hung readiness dependency stalls probes | Each check runs with a timeout derived from the request context | `health_test.go` |
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

## F-08 Data-frame model and plugin registry (FR-010, FR-015, NFR-PERF-003, NFR-SEC-002)

| STRIDE | Threat | Mitigation | Verified by |
| --- | --- | --- | --- |
| T | A plugin is swapped or injected at runtime (e.g. by later code registering a shadowing ID) | Plugins are compiled in; duplicate IDs rejected; registry frozen after start-up so `Register` fails afterwards | `registry_test.go`, `plugins.feature` |
| T | Malformed wire payload (mismatched column lengths, wrong types) crashes consumers | `frame.Frame.UnmarshalJSON` and the TS decoder validate types and lengths and return errors, never panic | `frame_test.go`, `frame.test.ts`, `frame.feature` |
| I | Data-source secrets leak through API responses or logs | `plugin.SecureValues` marshals to `{key: true}` (set flags only) and implements `slog.LogValuer` → `[REDACTED]`; `String()`/`%v` redacted too | `secure_test.go`, `plugins.feature` |
| I | Plugin catalogue reveals internals | Endpoint returns only static public metadata (no instance config, hosts, versions of dependencies). Requires auth once M3 lands (B-020) | `server_test.go` |
| D | Memory exhaustion from a huge result set | `frame.Builder` enforces row and byte limits while appending; build stops at the limit and flags `meta.truncated` with a notice | `builder_test.go`, `frame.feature` |
| D | Huge or deeply nested field names / JSON values | Field names capped at 256 bytes; JSON values must be valid JSON and count towards the byte limit | `frame_test.go` |

Residual risks: the byte limit is an estimate of in-memory size, not exact allocation;
`int64` numbers beyond ±2^53 lose precision in browsers (documented in ADR-0003).

## F-03 File source and CSV/TSV format (FR-011, NFR-SEC-006, NFR-PERF-003)

Data flow: operator config → named roots opened with `os.Root` at start-up → `file` source
instance (`jsonData.root`) → query `{path, format, options, fieldTypes}` → path validation
→ `os.Root.Open` → size-capped reader → CSV/TSV decoder → frame builder with limits.

| STRIDE | Threat | Mitigation | Verified by |
| --- | --- | --- | --- |
| I/T | Path traversal (`../`, absolute paths, `C:\`, encoded separators) reads files outside the root | `fsroot.CleanPath` rejects empty/absolute paths, `..` segments, backslashes, NUL and control characters before any FS call; `os.Root` refuses escapes as a second layer | `fsroot_test.go`, `file.feature` |
| I | Symlink inside a root points outside it (`/etc/passwd`) | `os.Root` resolves every component inside the root and rejects symlinks that escape | `fsroot_test.go`, `file.feature` |
| I | Reading devices, FIFOs or directories (e.g. a FIFO blocks forever, `/dev/zero` never ends) | Only regular files: mode checked on the opened handle; FIFOs opened non-blocking by `os.Root` are rejected before any read | `fsroot_test.go` |
| T | TOCTOU: file swapped or grown between check and read | Size and type are checked with `Stat` on the opened handle, and the reader is capped at the limit (+1 byte to detect growth → error) | `fsroot_test.go` |
| D | Huge file, very long lines or a wide CSV exhausts memory | `fileMaxBytes` cap (default 64 MiB); 1024-column cap; row/byte limits stop collection early and flag truncation; context cancellation checked during decode | `csv_test.go`, `file.feature` |
| I | Error messages disclose the server's directory layout | Errors carry the relative path and a stable kind; the absolute root path is never included | `file_test.go` |
| E | A query names a root the instance is not configured for | The root comes only from the instance's `jsonData.root`, never from the query body; unknown query keys are rejected | `file_test.go` |
| S/R | Unauthenticated users read files | Out of scope until M3: the query API (B-014) will require an authenticated Viewer and audit-log reads (B-020, NFR-AUD-001). Until then roots should only contain non-sensitive data | — |

Residual risks: CSV formula injection matters on *export* (not implemented yet; the
exporter must escape `=`, `+`, `-`, `@`). Compressed files are not decoded yet, so zip
bombs are not reachable; decompression must enforce the same output cap when added.
Inference reads the whole (capped) file into memory before building columns.

## Planned features (threats to model at implementation time)

- **F-04 AuthN/RBAC (M3):** credential stuffing, session fixation, CSRF, privilege escalation, IDOR → Argon2id, rotating session IDs, SameSite=strict cookies + CSRF tokens, server-side RBAC, object-level checks.
- **F-05 Secrets (M3):** key exposure, secrets in logs/responses → envelope encryption (AES-256-GCM, KEK from env/KMS), write-only API fields, slog redaction.
- **F-06 SQL sources (M4):** SQL injection, expensive queries, data exfiltration → parameterized queries, read-only transactions, statement timeouts, row limits.
- **F-07 REST/GraphQL sources (M5):** SSRF (cloud metadata, internal services, DNS rebinding) → dial-time IP checks, redirect re-validation, allow-lists, response size caps.
