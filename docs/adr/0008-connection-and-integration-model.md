# 0008. Connection and integration model: one HTTP engine, presets, jq selectors, a frame pipeline

- Status: Accepted
- Date: 2026-11-25
- Requirements: FR-040, FR-041, FR-070…FR-079, NFR-SEC-005, NFR-SEC-020, NFR-SEC-021, NFR-PERF-006, NFR-MAINT-009

## Context

The product must connect to many observability, cloud and SaaS APIs, among them Azure
Monitor, Google Cloud Monitoring, AWS CloudWatch, Datadog, Splunk, Dynatrace,
AppDynamics, Sentry, GitHub, Vercel, New Relic and ThousandEyes. It must turn their
responses into one tabular model and then filter, transform and join across them.
`docs/research/CONNECTORS.md` shows that almost every target is HTTP + JSON. The targets
differ in four ways:

- authentication (basic, bearer, API keys, OAuth2, JWT, SigV4, Entra ID, workload
  identity);
- pagination (offset, page, cursor, next URL, `Link` header, async jobs);
- response shape (arrays under a key, parallel arrays, column/row tables, tuples);
- their base URLs (sites, regions, self-hosted hosts).

Writing a bespoke plugin per vendor would mean doing the security-critical parts
(outbound requests, credentials, SSRF, limits) 40 times. Grafana's Infinity plugin and
Airbyte's declarative CDK show that a generic requester + authenticator + paginator +
selector covers most APIs.

## Decision

1. **One `http` source plugin** (`internal/plugins/httpsource`) owns transport,
   authentication, pagination, limits and frame building. Vendor connectors are
   **presets**: data compiled into the binary that fill in the `http` settings (FR-075).
   A vendor gets its own plugin only when its protocol is not HTTP + JSON (SQL drivers,
   for example).
2. **Outbound requests go through `internal/netguard`.** The host builds an `http.Client`
   whose dialer checks every resolved IP against blocked ranges at connect time. Plugins
   receive this client; they never build their own. This implements the plugin-model
   promise in ADR-0002 that "plugins never open network connections on their own".
3. **Selectors use a sandboxed jq subset implemented in-house** (`internal/jq`). It has
   no new dependency.
   - We evaluated `itchyny/gojq`. It is complete and well maintained, but it does not
     enforce step, output or depth budgets (only context cancellation). Its full
     language includes `env`, `input`, `def` and recursion, which we would have to
     strip.
   - A small evaluator for the documented subset (FR-073 AC2) gives deterministic
     budgets (NFR-SEC-021), ordered objects (so column order follows the response) and
     exact large integers.
   - The interface (`jq.Compile`, `Program.Run`) hides the implementation. If the subset
     becomes limiting, the evaluator can be replaced with gojq behind the same budget
     wrapper. JSONata (FR-074) plugs into the same interface.
4. **Frames stay the unified model** (ADR-0003). Selected rows go through the JSON
   format's table builder (FR-012), so type inference, `fieldTypes`, flattening and limits
   are identical for files and APIs. Transforms and joins (FR-076, FR-078) operate on
   frames server-side, after fetching, and never fetch.
5. **Secrets before M3** are references (`{env}`/`{file}`) resolved at start-up
   (FR-079). The plugin sees only `plugin.SecureValues`. FR-022 later replaces the
   resolver with encrypted storage without changing plugins.

## Consequences

- Security review concentrates on `netguard`, the request builder and `jq`. A new vendor
  is mostly data plus fixtures (NFR-MAINT-009).
- The jq subset must be documented and kept compatible with jq 1.7 for what it supports.
  Tests compare against outputs known from jq.
- Non-JSON responses (CSV, XML from APIs) wait for format selection in FR-040 AC5 and
  FR-060.
- Advanced auth (OAuth2, SigV4, Entra ID, workload identity federation) adds token
  endpoints, and those calls also go through `netguard`.
