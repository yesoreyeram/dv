import { Link } from "react-router";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { t } from "@/i18n/messages";

export function NotFoundPage() {
  return (
    <>
      <PageHeader title={t("notFound.title")} subtitle={t("notFound.body")} />
      <Button asChild variant="outline">
        <Link to="/">{t("notFound.home")}</Link>
      </Button>
    </>
  );
}
