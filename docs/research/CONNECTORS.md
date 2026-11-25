# Connector market research (2026-11-25)

This document records the research behind the connection and integration requirements
(FR-040, FR-070…FR-079). It answers two questions:

- What do the APIs dv must integrate with look like: transport, authentication,
  pagination and response shape?
- How do comparable products expose these APIs?

The details come from each vendor's public API reference as the author knew it on the
date above. Every preset (FR-075) re-checks its vendor's current reference when it is
implemented, and records the reference it used in the preset's test fixture.

## 1. How comparable products do it

| Product | Model | What dv takes from it |
| --- | --- | --- |
| Grafana (Infinity plugin, vendor plugins) | One plugin per vendor, plus a generic "Infinity" plugin for any URL. Infinity offers a JSONPath/JSONata/UQL "root selector" and per-column selectors, pagination modes (offset, page, cursor, list) and auth (basic, bearer, API key, OAuth2 client credentials / JWT, AWS SigV4, Azure) | A **generic HTTP engine** that vendor connectors are configurations of (presets), rather than 40 bespoke plugins. A **root selector plus column selectors**, and **typed columns** |
| Datadog / New Relic | Closed integrations catalogue with hundreds of tiles; each tile is a vendor-maintained preset with auth and default queries | A **catalogue with categories**, a status per connector, and per-vendor defaults the user does not have to know |
| Power BI (Power Query "Web" connector, M language) | Generic `Web.Contents` + `Json.Document`, pagination through `List.Generate`, transforms in Power Query steps (filter, rename, merge = join, append = union) | **Ordered transformation steps after the fetch** (filter → select → join), with the step list visible and editable |
| Looker / Looker Studio | Connectors produce a typed schema (dimensions, metrics); joins are declared between explores | **A unified typed table** at the end of every connector. **Joins on declared keys** |
| Airbyte / Fivetran / Singer (ELT) | Declarative low-code connectors (Airbyte YAML CDK): requester (URL, auth), paginator (offset, page, cursor, link header), record selector (field path), schema | Proof that a **declarative preset** (requester + authenticator + paginator + selector) covers most SaaS APIs. dv's preset schema follows the same split |
| Steampipe | SQL over APIs; each table maps an endpoint with key columns and hydrate calls | **Pushdown of filters** into API parameters, which dv adds once the pipeline lands (FR-076 AC6) |

The conclusion is ADR-0008: one hardened HTTP engine handles transport, auth, pagination,
the SSRF guard and limits. On top of it sit a jq-compatible selector (and JSONata later),
data-only vendor presets, and a server-side transformation pipeline over data frames.

## 2. Target APIs

Legend for pagination: **cursor** = token in the body sent back as a parameter;
**next-url** = full URL in the body; **link** = RFC 8288 `Link: <…>; rel="next"` header;
**offset** = offset/limit parameters; **page** = page number/size parameters; **time** =
the caller walks the time range; **async** = a job is started, polled, then read.

| Connector | Category | Endpoint (default) | Authentication | Pagination | Response shape → rows |
| --- | --- | --- | --- | --- | --- |
| AWS CloudWatch (Metrics `GetMetricData`, Logs Insights) | Cloud monitoring | `monitoring.<region>.amazonaws.com`, `logs.<region>.amazonaws.com` (JSON 1.1 protocol, POST) | AWS SigV4 with access key + secret (+ session token); AssumeRole; web identity federation (IRSA / OIDC) | cursor (`NextToken`); Logs Insights is async (`StartQuery` → `GetQueryResults`) | `MetricDataResults[]` with parallel `Timestamps[]`/`Values[]` arrays to zip; Insights `results[][]` of `{field,value}` |
| Azure Monitor (Metrics, Log Analytics) | Cloud monitoring | `management.azure.com/{resourceId}/providers/microsoft.insights/metrics`; `api.loganalytics.azure.com/v1/workspaces/{id}/query` (KQL, POST) | Microsoft Entra ID OAuth2: client secret, client certificate (JWT assertion), managed identity, **workload identity federation** (federated OIDC token as `client_assertion`) | next-url (`nextLink`, ARM); Log Analytics returns one page | Metrics `value[].timeseries[].data[]`; Log Analytics `tables[].columns[]` + `rows[][]` (column/row zip) |
| Google Cloud Monitoring (formerly Stackdriver) | Cloud monitoring | `monitoring.googleapis.com/v3/projects/{p}/timeSeries` (GET, filter + interval); PromQL endpoint | Service-account key → signed JWT → OAuth2 token (JWT bearer grant); **workload identity federation** (STS token exchange); metadata server on GCE/GKE | cursor (`nextPageToken` → `pageToken`) | `timeSeries[].points[]` with `interval.endTime` and a typed `value` |
| Datadog | APM / observability | `api.<site>` (`datadoghq.com`, `datadoghq.eu`, `us3.`/`us5.datadoghq.com`, `ap1.datadoghq.com`, `ddog-gov.com`) | Two headers: `DD-API-KEY` + `DD-APPLICATION-KEY` | Metrics v1: time; logs/events v2: cursor (`meta.page.after` → `page[cursor]`) | Metrics `series[].pointlist[[ts,v]]`; logs `data[].attributes` |
| Splunk (Enterprise / Cloud) | Logs / SIEM | `https://<host>:8089/services/search/v2/jobs` (REST, `output_mode=json`) | Basic, or bearer **Splunk authentication token** | async (create job → poll `dispatchState` → `results`) with offset (`offset`/`count`); or the export endpoint (streamed) | `results[]` objects; field order from `fields[]` |
| Dynatrace | APM | `https://{env}.live.dynatrace.com/api/v2/metrics/query`, `/api/v2/logs/search`, `/api/v2/problems` | API key header with scheme prefix: `Authorization: Api-Token <t>`; platform OAuth2 client credentials | cursor (`nextPageKey`, and the next request **must send only** `nextPageKey`, no other filter parameters) | `result[].data[]` with `timestamps[]`/`values[]` to zip |
| AppDynamics (Cisco) | APM | `https://<controller>/controller/rest/applications/{app}/metric-data?output=JSON` | OAuth2 client credentials (API client → `/controller/api/oauth/access_token`); basic `user@account` | none (time range) | `[].metricValues[]` |
| Sentry | Error tracking | `https://sentry.io/api/0/organizations/{org}/issues/` (or self-hosted) | Bearer auth token | link, with Sentry's `results="true|false"` attribute: `rel="next"` is always present, and `results="false"` means stop | array of objects |
| GitHub | DevOps | `https://api.github.com` (GHES: `https://<host>/api/v3`); GraphQL `/graphql` | Bearer PAT / fine-grained token; GitHub App: RS256 JWT → installation token | link (`per_page` ≤ 100); GraphQL cursor (`pageInfo.endCursor`, `hasNextPage`) | array of objects; search APIs: `items[]` |
| Vercel | DevOps / hosting | `https://api.vercel.com/v6/deployments` (and others), `teamId` parameter | Bearer access token | cursor (`pagination.next` timestamp → `until`) | `deployments[]` |
| New Relic | APM / observability | NerdGraph GraphQL `https://api.newrelic.com/graphql` (EU: `api.eu.newrelic.com`) | User API key header `API-Key: NRAK-…` | GraphQL cursor (`nextCursor`); NRQL returns one result set | `data.actor.account.nrql.results[]` |
| Cisco ThousandEyes | Network monitoring | `https://api.thousandeyes.com/v7/...` | Bearer OAuth token | next-url (`_links.next.href`) | `tests[]`, `results[]` |
| Cloudflare | Edge / CDN | `https://api.cloudflare.com/client/v4`; GraphQL Analytics `/graphql` | Bearer API token (legacy: `X-Auth-Email` + `X-Auth-Key`) | page (`page`/`per_page`, `result_info.total_pages`); some endpoints cursor | `result[]` |
| Prometheus (and Mimir, Thanos, VictoriaMetrics) | Metrics | `/api/v1/query_range` | none, basic, bearer; tenant header `X-Scope-OrgID` | none | `data.result[].values[[ts,"v"]]` (matrix) |
| Grafana Loki | Logs | `/loki/api/v1/query_range` | basic, bearer; `X-Scope-OrgID` | time (`limit` + `direction`; the next request starts after the last timestamp) | `data.result[].values[[ts_ns, line]]` |
| Grafana Tempo / Jaeger | Traces | Tempo `/api/search`, `/api/traces/{id}`; Jaeger query `/api/traces?service=` | none, basic, bearer | `limit` only | Tempo `traces[]`; Jaeger `data[].spans[]` |
| Elasticsearch / OpenSearch | Search / logs | `/<index>/_search` (POST) | basic, `Authorization: ApiKey <b64>`, AWS SigV4 (OpenSearch Service) | `search_after` + point in time | `hits.hits[]._source` |
| ClickHouse (HTTP interface) | Database | `:8123/?query=…&default_format=JSON` or POST | basic, or `X-ClickHouse-User` / `X-ClickHouse-Key` | offset (`LIMIT`/`OFFSET` in SQL) | `data[]` with `meta[]` types |
| PagerDuty, Opsgenie, Jira, ServiceNow | Incident / ITSM | REST | token header (`Authorization: Token token=…`), basic, OAuth2 | offset (`offset`/`limit`, `more`), Jira `startAt`/`maxResults`, ServiceNow `sysparm_offset` | arrays under a key |
| OpenAQ, Open-Meteo, USGS Earthquakes, ThingSpeak | Open data / IoT | REST / GeoJSON | none, `X-API-Key`, `api_key` query parameter | page (`page`/`limit`), none | `results[]`; GeoJSON `features[].properties` |

## 3. What the requirements must therefore cover

1. **Transport**: GET and POST with a JSON body; GraphQL is POST + variables + cursor
   (FR-041). Async job APIs (Splunk, CloudWatch Logs Insights) need a start → poll → read
   step (FR-077).
2. **Authentication** (FR-071): none, basic, bearer, API keys in any header or query
   parameter with an optional scheme prefix (Dynatrace `Api-Token`, Elasticsearch
   `ApiKey`), several keys at once (Datadog), OAuth2 client credentials (with a secret or
   a signed JWT assertion), the Google JWT bearer grant, GitHub App JWT, AWS SigV4
   (static, AssumeRole, web identity), Entra ID managed identity, **workload identity
   federation** for Azure, GCP and AWS, and mTLS.
3. **Pagination** (FR-072): offset, page, cursor, next-url and link (with the Sentry
   `results` attribute). Cursor-only follow-up requests are needed for Dynatrace. Every
   style is bounded by page, row, byte and time budgets.
4. **Selectors** (FR-073, FR-074): row selection plus column zipping, for
   parallel-array (CloudWatch, Dynatrace), column/row-table (Log Analytics) and
   tuple-array (Prometheus, Loki, Datadog) shapes. jq covers all of these
   (`[.Timestamps, .Values] | transpose[]`). JSONata is the other syntax users know from
   Grafana and Node-RED.
5. **Integration** (FR-076, FR-078): filter, select/rename, computed fields, type
   conversion, sort, limit, group/aggregate, and join/union across connectors on keys or
   time.
