import { useQuery } from "@tanstack/react-query";
import { PageHeader } from "@/components/layout/PageHeader";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { t } from "@/i18n/messages";
import { listEndpoints, openapiQuery } from "@/lib/api";

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
                <th scope="col" className="px-5 py-3 text-start font-medium">
                  {t("api.method")}
                </th>
                <th scope="col" className="px-5 py-3 text-start font-medium">
                  {t("api.path")}
                </th>
                <th scope="col" className="px-5 py-3 text-start font-medium">
                  {t("api.summary")}
                </th>
              </tr>
            </thead>
            <tbody>
              {listEndpoints(q.data).map((ep) => (
                <tr
                  key={`${ep.method} ${ep.path}`}
                  className="border-b last:border-b-0"
                  data-testid="endpoint"
                >
                  <td className="px-5 py-3">
                    <Badge className="font-mono">{ep.method}</Badge>
                  </td>
                  <td className="px-5 py-3 font-mono">{ep.path}</td>
                  <td className="px-5 py-3 text-muted-foreground">{ep.summary}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}
    </>
  );
}
