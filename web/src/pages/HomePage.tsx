import { useQuery } from "@tanstack/react-query";
import {
  ArrowRight,
  BookOpen,
  CheckCircle2,
  Compass,
  Database,
  RefreshCw,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react";
import { Link } from "react-router";
import { PageHeader } from "@/components/layout/PageHeader";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { t, type MessageKey } from "@/i18n/messages";
import { dataSourcesQuery, pluginsQuery, readinessQuery, versionQuery } from "@/lib/api";

function StatusCard() {
  const q = useQuery(readinessQuery);
  return (
    <Card aria-labelledby="status-title">
      <CardHeader>
        <CardTitle id="status-title">{t("status.title")}</CardTitle>
        <CardDescription>{t("status.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {q.isPending ? (
          <p role="status" aria-busy="true" className="text-sm text-muted-foreground">
            {t("status.loading")}
          </p>
        ) : q.isError ? (
          <Alert>
            <p>{t("status.error")}</p>
            <div>
              <Button size="sm" variant="outline" onClick={() => void q.refetch()}>
                <RefreshCw aria-hidden="true" />
                {t("status.retry")}
              </Button>
            </div>
          </Alert>
        ) : (
          <>
            <div role="status" className="flex items-center gap-3">
              {q.data.status === "ready" ? (
                <Badge variant="success" className="px-3 py-1 text-sm">
                  <CheckCircle2 aria-hidden="true" className="size-4" />
                  {t("status.operational")}
                </Badge>
              ) : (
                <Badge variant="danger" className="px-3 py-1 text-sm">
                  <TriangleAlert aria-hidden="true" className="size-4" />
                  {t("status.degraded")}
                </Badge>
              )}
            </div>
            <div>
              <h3 className="mb-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">
                {t("status.checks")}
              </h3>
              {Object.keys(q.data.checks).length === 0 ? (
                <p className="text-sm text-muted-foreground">{t("status.noChecks")}</p>
              ) : (
                <ul className="flex flex-wrap gap-2">
                  {Object.entries(q.data.checks).map(([name, result]) => (
                    <li key={name}>
                      <Badge variant={result === "pass" ? "success" : "danger"}>
                        {name}: {result}
                      </Badge>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}

function BuildCard() {
  const q = useQuery(versionQuery);
  const rows: [string, string | undefined][] = [
    [t("build.version"), q.data?.version],
    [t("build.commit"), q.data?.commit],
    [t("build.date"), q.data?.buildDate],
    [t("build.go"), q.data?.goVersion],
  ];
  return (
    <Card aria-labelledby="build-title">
      <CardHeader>
        <CardTitle id="build-title">{t("build.title")}</CardTitle>
        <CardDescription>{t("build.description")}</CardDescription>
      </CardHeader>
      <CardContent>
        {q.isError ? (
          <Alert>
            <p>{t("status.error")}</p>
          </Alert>
        ) : (
          <dl
            className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1.5 text-sm"
            aria-busy={q.isPending}
          >
            {rows.map(([label, value]) => (
              <div key={label} className="contents">
                <dt className="text-muted-foreground">{label}</dt>
                <dd className="truncate font-mono text-xs leading-5" data-testid={`build-${label}`}>
                  {value ?? "—"}
                </dd>
              </div>
            ))}
          </dl>
        )}
      </CardContent>
    </Card>
  );
}

const QUICK: { to: string; title: MessageKey; body: MessageKey; icon: LucideIcon }[] = [
  { to: "/explore", title: "home.quickExplore", body: "home.quickExploreBody", icon: Compass },
  {
    to: "/data-sources",
    title: "home.quickSources",
    body: "home.quickSourcesBody",
    icon: Database,
  },
  { to: "/api-reference", title: "home.quickApi", body: "home.quickApiBody", icon: BookOpen },
];

/** Entry points to the pages that work today; planned pages are not advertised (FR-005 AC6). */
function QuickActions() {
  return (
    <nav aria-label={t("home.quick")} className="mb-4">
      <ul className="grid gap-3 sm:grid-cols-3">
        {QUICK.map(({ to, title, body, icon: Icon }) => (
          <li key={to}>
            <Link
              to={to}
              className="group flex h-full items-start gap-3 rounded-lg border bg-card p-3 shadow-xs transition-colors hover:border-primary/60 hover:bg-accent/40"
            >
              <span
                aria-hidden="true"
                className="grid size-7 shrink-0 place-items-center rounded-md bg-accent text-accent-foreground"
              >
                <Icon className="size-4" />
              </span>
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="text-sm font-medium">{t(title)}</span>
                <span className="text-xs text-muted-foreground">{t(body)}</span>
              </span>
              <ArrowRight
                aria-hidden="true"
                className="mt-1 size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-foreground rtl:rotate-180"
              />
            </Link>
          </li>
        ))}
      </ul>
    </nav>
  );
}

const count = (n: number) => new Intl.NumberFormat().format(n);

/** What this server can query: provisioned sources and compiled-in plugins (FR-015, FR-017). */
function WorkspaceCard() {
  const sources = useQuery(dataSourcesQuery).data?.datasources;
  const plugins = useQuery(pluginsQuery).data;
  // Shapes are checked, not assumed: an unexpected body shows "—" instead of breaking Home.
  const stats: [string, string][] = [
    [
      t("home.statSources"),
      sources === null
        ? t("home.statQueryApiOff")
        : Array.isArray(sources)
          ? count(sources.length)
          : t("home.statUnknown"),
    ],
    [
      t("home.statSourceTypes"),
      Array.isArray(plugins?.sources) ? count(plugins.sources.length) : t("home.statUnknown"),
    ],
    [
      t("home.statFormats"),
      Array.isArray(plugins?.formats) ? count(plugins.formats.length) : t("home.statUnknown"),
    ],
  ];
  return (
    <Card aria-labelledby="workspace-title">
      <CardHeader>
        <CardTitle id="workspace-title">{t("home.workspace")}</CardTitle>
        <CardDescription>{t("home.workspaceDescription")}</CardDescription>
      </CardHeader>
      <CardContent>
        <dl className="grid grid-cols-3 gap-2">
          {stats.map(([label, value]) => (
            <div key={label} className="flex flex-col-reverse gap-1 rounded-md bg-muted/50 p-2.5">
              <dt className="text-xs text-muted-foreground">{label}</dt>
              <dd
                className="text-lg leading-none font-semibold tabular-nums"
                data-testid={`stat-${label}`}
              >
                {value}
              </dd>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  );
}

export function HomePage() {
  return (
    <>
      <PageHeader title={t("home.title")} subtitle={t("home.subtitle")} />
      <QuickActions />
      <div className="grid gap-3 lg:grid-cols-3">
        <StatusCard />
        <WorkspaceCard />
        <BuildCard />
      </div>
    </>
  );
}
