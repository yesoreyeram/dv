import { useQuery } from "@tanstack/react-query";
import { Database, FileCode2, RefreshCw } from "lucide-react";
import type { ReactNode } from "react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { t } from "@/i18n/messages";
import { pluginsQuery, type FormatPlugin, type SourcePlugin } from "@/lib/api";

function Section({
  id,
  title,
  description,
  icon,
  empty,
  children,
}: {
  id: string;
  title: string;
  description: string;
  icon: ReactNode;
  empty: string | null;
  children: ReactNode;
}) {
  return (
    <Card aria-labelledby={id} data-testid={id}>
      <CardHeader className="flex-row items-center gap-2.5">
        <span
          aria-hidden="true"
          className="grid size-7 shrink-0 place-items-center rounded-md bg-accent text-accent-foreground"
        >
          {icon}
        </span>
        <div className="flex flex-col gap-1">
          <CardTitle id={id}>{title}</CardTitle>
          <CardDescription className="text-xs">{description}</CardDescription>
        </div>
      </CardHeader>
      <CardContent>
        {empty ? <p className="text-sm text-muted-foreground">{empty}</p> : children}
      </CardContent>
    </Card>
  );
}

function PluginItem({
  name,
  id,
  description,
  tagsLabel,
  tags,
}: {
  name: string;
  id: string;
  description: string;
  tagsLabel: string;
  tags: string[];
}) {
  return (
    <li
      className="flex flex-col gap-1.5 rounded-md border bg-background/50 p-3"
      data-testid="plugin"
    >
      <div className="flex flex-wrap items-baseline gap-2">
        <h3 className="text-sm font-medium">{name}</h3>
        <code className="rounded-sm bg-muted px-1 py-0.5 font-mono text-[0.6875rem] text-muted-foreground">
          {id}
        </code>
      </div>
      <p className="text-sm text-muted-foreground">{description}</p>
      {tags.length > 0 ? (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="sr-only">{tagsLabel}:</span>
          {tags.map((c) => (
            <Badge key={c} variant="outline">
              {c}
            </Badge>
          ))}
        </div>
      ) : null}
    </li>
  );
}

/** Data sources page: the compiled-in plugin catalogue (FR-015 AC6). */
export function DataSourcesPage() {
  const q = useQuery(pluginsQuery);
  return (
    <>
      <PageHeader title={t("dataSources.title")} subtitle={t("dataSources.subtitle")} />
      <div className="mb-4 max-w-prose space-y-1 text-sm text-muted-foreground">
        <p>{t("dataSources.body")}</p>
        <p>{t("dataSources.secretsPlanned")}</p>
      </div>
      {q.isPending ? (
        <p role="status" aria-busy="true" className="text-sm text-muted-foreground">
          {t("dataSources.loading")}
        </p>
      ) : q.isError ? (
        <Alert>
          <p>{t("dataSources.error")}</p>
          <div>
            <Button size="sm" variant="outline" onClick={() => void q.refetch()}>
              <RefreshCw aria-hidden="true" />
              {t("dataSources.retry")}
            </Button>
          </div>
        </Alert>
      ) : (
        <div className="grid gap-3 lg:grid-cols-2" data-testid="plugin-catalogue">
          <Section
            id="plugin-sources"
            title={t("dataSources.sources")}
            description={t("dataSources.sourcesDescription")}
            icon={<Database className="size-4" />}
            empty={q.data.sources.length === 0 ? t("dataSources.noSources") : null}
          >
            <ul className="flex flex-col gap-2">
              {q.data.sources.map((s: SourcePlugin) => (
                <PluginItem
                  key={s.id}
                  {...s}
                  tagsLabel={t("dataSources.capabilities")}
                  tags={s.capabilities}
                />
              ))}
            </ul>
          </Section>
          <Section
            id="plugin-formats"
            title={t("dataSources.formats")}
            description={t("dataSources.formatsDescription")}
            icon={<FileCode2 className="size-4" />}
            empty={q.data.formats.length === 0 ? t("dataSources.noFormats") : null}
          >
            <ul className="flex flex-col gap-2">
              {q.data.formats.map((f: FormatPlugin) => (
                <PluginItem
                  key={f.id}
                  {...f}
                  tagsLabel={t("dataSources.extensions")}
                  tags={f.extensions}
                />
              ))}
            </ul>
          </Section>
        </div>
      )}
    </>
  );
}
