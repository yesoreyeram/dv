/**
 * English message catalog (NFR-I18N-001). Components read copy through `t()` so new
 * locales can be added without touching components.
 */
export const en = {
  "app.name": "dv",
  "app.tagline": "Secure data visualization",
  "a11y.skipToContent": "Skip to main content",
  "nav.label": "Primary",
  "nav.home": "Home",
  "nav.explore": "Explore",
  "nav.dashboards": "Dashboards",
  "nav.dataSources": "Data sources",
  "nav.apiReference": "API reference",
  "theme.toDark": "Switch to dark theme",
  "theme.toLight": "Switch to light theme",
  "home.title": "Home",
  "home.subtitle": "Platform status and build information.",
  "status.title": "System status",
  "status.description": "Live readiness of this dv server.",
  "status.operational": "Operational",
  "status.degraded": "Degraded",
  "status.loading": "Checking server status…",
  "status.error": "Could not reach the dv server.",
  "status.retry": "Retry",
  "status.checks": "Readiness checks",
  "status.noChecks": "No dependency checks are registered yet.",
  "build.title": "Build",
  "build.description": "Version of the running server.",
  "build.version": "Version",
  "build.commit": "Commit",
  "build.date": "Built",
  "build.go": "Go runtime",
  "planned.badge": "Planned for milestone {milestone}",
  "explore.title": "Explore",
  "explore.body":
    "Query any connected data source and inspect the resulting data frames as tables and charts.",
  "dashboards.title": "Dashboards",
  "dashboards.body": "Compose panels into dashboards with variables and a shared time range.",
  "dataSources.title": "Data sources",
  "dataSources.body":
    "Connect files, SQL databases and HTTP APIs. Credentials are encrypted at rest and never sent back to the browser.",
  "api.title": "API reference",
  "api.subtitle": "Endpoints published in the OpenAPI document served by this server.",
  "api.loading": "Loading API description…",
  "api.error": "Could not load the API description.",
  "api.method": "Method",
  "api.path": "Path",
  "api.summary": "Summary",
  "notFound.title": "Page not found",
  "notFound.body": "The page you requested does not exist.",
  "notFound.home": "Go to Home",
} as const;

export type MessageKey = keyof typeof en;

/** Translates a key, substituting `{name}` placeholders. */
export function t(key: MessageKey, vars?: Record<string, string>): string {
  let msg: string = en[key];
  if (vars) {
    for (const [k, v] of Object.entries(vars)) {
      msg = msg.replaceAll(`{${k}}`, v);
    }
  }
  return msg;
}
