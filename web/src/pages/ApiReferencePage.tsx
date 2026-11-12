import { useQuery } from "@tanstack/react-query";
import { PageHeader } from "@/components/layout/PageHeader";
import { Alert } from "@/components/ui/alert";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { t } from "@/i18n/messages";
import { listEndpoints, openapiQuery } from "@/lib/api";

/** Read-only methods read as neutral, writes stand out; the method text carries the meaning. */
const METHOD_VARIANT: Record<string, BadgeProps["variant"]> = {
  GET: "default",
  HEAD: "default",
  POST: "success",
  PUT: "success",
  PATCH: "success",
  DELETE: "danger",
};

export function ApiReferencePage() {
  const q = useQuery(openapiQuery);
  return (
    <>
      <PageHeader title={t("api.title")} subtitle={t("api.subtitle")}>
        {q.data ? (
          <Badge variant="outline">
            {q.data.info.title} {q.data.info.version} · OpenAPI {q.data.openapi}
          </Badge>
        ) : null}
      </PageHeader>
      {q.isPending ? (
        <p role="status" aria-busy="true" className="text-sm text-muted-foreground">
          {t("api.loading")}
        </p>
      ) : q.isError ? (
        <Alert>
          <p>{t("api.error")}</p>
        </Alert>
      ) : (
        <Card className="overflow-x-auto">
          <table className="w-full text-sm">
            <caption className="sr-only">{t("api.title")}</caption>
            <thead className="border-b bg-muted/60 text-start text-xs tracking-wide text-muted-foreground uppercase">
              <tr>
                <th scope="col" className="px-4 py-2 text-start font-medium">
                  {t("api.method")}
                </th>
                <th scope="col" className="px-4 py-2 text-start font-medium">
                  {t("api.path")}
                </th>
                <th scope="col" className="px-4 py-2 text-start font-medium">
                  {t("api.summary")}
                </th>
              </tr>
            </thead>
            <tbody>
              {listEndpoints(q.data).map((ep) => (
                <tr
                  key={`${ep.method} ${ep.path}`}
                  className="border-b last:border-b-0 hover:bg-muted/40"
                  data-testid="endpoint"
                >
                  <td className="px-4 py-2">
                    <Badge variant={METHOD_VARIANT[ep.method] ?? "outline"} className="font-mono">
                      {ep.method}
                    </Badge>
                  </td>
                  <td className="px-4 py-2 font-mono text-xs">{ep.path}</td>
                  <td className="px-4 py-2 text-muted-foreground">{ep.summary}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}
    </>
  );
}
