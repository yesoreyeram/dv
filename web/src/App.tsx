import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { Route, Routes, useLocation } from "react-router";
import { AppShell } from "@/components/layout/AppShell";
import { t } from "@/i18n/messages";
import { ApiReferencePage } from "@/pages/ApiReferencePage";
import { DataSourcesPage } from "@/pages/DataSourcesPage";
import { ExplorePage } from "@/pages/ExplorePage";
import { HomePage } from "@/pages/HomePage";
import { NotFoundPage } from "@/pages/NotFoundPage";
import { PlannedPage } from "@/pages/PlannedPage";

const TITLES: Record<string, string> = {
  "/": t("nav.home"),
  "/explore": t("nav.explore"),
  "/dashboards": t("nav.dashboards"),
  "/data-sources": t("nav.dataSources"),
  "/api-reference": t("nav.apiReference"),
};

/** Keeps document.title in sync with the route (WCAG 2.4.2). */
function DocumentTitle() {
  const { pathname } = useLocation();
  useEffect(() => {
    const page = TITLES[pathname] ?? t("notFound.title");
    document.title = `${page} · ${t("app.name")}`;
  }, [pathname]);
  return null;
}

export function App({ queryClient }: { queryClient: QueryClient }) {
  return (
    <QueryClientProvider client={queryClient}>
      <DocumentTitle />
      <Routes>
        <Route element={<AppShell />}>
          <Route index element={<HomePage />} />
          <Route path="explore" element={<ExplorePage />} />
          <Route
            path="dashboards"
            element={
              <PlannedPage
                title={t("dashboards.title")}
                body={t("dashboards.body")}
                milestone="M6"
              />
            }
          />
          <Route path="data-sources" element={<DataSourcesPage />} />
          <Route path="api-reference" element={<ApiReferencePage />} />
          <Route path="*" element={<NotFoundPage />} />
        </Route>
      </Routes>
    </QueryClientProvider>
  );
}
