import { useQuery } from "@tanstack/react-query";
import { CheckCircle2, RefreshCw, TriangleAlert } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { t } from "@/i18n/messages";
import { readinessQuery, versionQuery } from "@/lib/api";

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
          <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm" aria-busy={q.isPending}>
            {rows.map(([label, value]) => (
              <div key={label} className="contents">
                <dt className="text-muted-foreground">{label}</dt>
                <dd className="font-mono" data-testid={`build-${label}`}>
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

export function HomePage() {
  return (
    <>
      <PageHeader title={t("home.title")} subtitle={t("home.subtitle")} />
      <div className="grid gap-6 md:grid-cols-2">
        <StatusCard />
        <BuildCard />
      </div>
    </>
  );
}
