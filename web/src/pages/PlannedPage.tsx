import { Construction } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { t } from "@/i18n/messages";

/** Explicit empty state for features not built yet (FR-005 AC6) — never fake data. */
export function PlannedPage({
  title,
  body,
  milestone,
}: {
  title: string;
  body: string;
  milestone: string;
}) {
  return (
    <>
      <PageHeader title={title} />
      <Card>
        <CardContent className="flex flex-col items-start gap-3 p-6">
          <span
            aria-hidden="true"
            className="grid size-9 place-items-center rounded-lg bg-accent text-accent-foreground"
          >
            <Construction className="size-5" />
          </span>
          <Badge>{t("planned.badge", { milestone })}</Badge>
          <p className="max-w-prose text-sm text-muted-foreground">{body}</p>
        </CardContent>
      </Card>
    </>
  );
}
