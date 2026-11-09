import { BarChart3, BookOpen, Compass, Database, Home, Moon, Sun } from "lucide-react";
import { useLayoutEffect, type ReactNode } from "react";
import { NavLink, Outlet, useLocation } from "react-router";
import { Button } from "@/components/ui/button";
import { ScrollEdgeShadows } from "@/components/ui/scroll-edges";
import { t, type MessageKey } from "@/i18n/messages";
import { revealInRow } from "@/lib/revealInRow";
import { useScrollEdges } from "@/lib/useScrollEdges";
import { useTheme } from "@/lib/useTheme";
import { cn } from "@/lib/utils";

interface NavItem {
  to: string;
  label: MessageKey;
  icon: ReactNode;
}

const NAV: NavItem[] = [
  { to: "/", label: "nav.home", icon: <Home aria-hidden="true" /> },
  { to: "/explore", label: "nav.explore", icon: <Compass aria-hidden="true" /> },
  { to: "/dashboards", label: "nav.dashboards", icon: <BarChart3 aria-hidden="true" /> },
  { to: "/data-sources", label: "nav.dataSources", icon: <Database aria-hidden="true" /> },
  { to: "/api-reference", label: "nav.apiReference", icon: <BookOpen aria-hidden="true" /> },
];

function Logo() {
  return (
    <span className="flex items-center gap-2.5">
      <span
        aria-hidden="true"
        className="grid size-8 place-items-center rounded-md bg-primary text-primary-foreground"
      >
        <BarChart3 className="size-4" />
      </span>
      <span className="flex flex-col leading-tight">
        <span className="text-base font-semibold tracking-tight">{t("app.name")}</span>
        <span className="text-xs text-muted-foreground">{t("app.tagline")}</span>
      </span>
    </span>
  );
}

export function AppShell() {
  const { theme, toggle } = useTheme();
  const toggleLabel = theme === "dark" ? t("theme.toLight") : t("theme.toDark");
  const { pathname } = useLocation();
  const [rowRef, edges] = useScrollEdges<HTMLUListElement>([pathname]);

  useLayoutEffect(() => {
    const row = rowRef.current;
    const current = row?.querySelector<HTMLElement>('[aria-current="page"]');
    if (row && current) revealInRow(row, current);
  }, [pathname, rowRef]);

  return (
    <div className="flex min-h-dvh flex-col md:flex-row">
      <a
        href="#main"
        className="sr-only z-50 rounded-md bg-primary px-4 py-2 text-primary-foreground focus:not-sr-only focus:fixed focus:start-4 focus:top-4"
      >
        {t("a11y.skipToContent")}
      </a>
      <header className="flex flex-col border-b bg-sidebar md:sticky md:top-0 md:h-dvh md:w-64 md:shrink-0 md:border-e md:border-b-0">
        <div className="flex items-center justify-between gap-2 px-4 py-4">
          <Logo />
          <Button
            variant="ghost"
            size="icon"
            onClick={toggle}
            aria-label={toggleLabel}
            title={toggleLabel}
          >
            {theme === "dark" ? <Sun aria-hidden="true" /> : <Moon aria-hidden="true" />}
          </Button>
        </div>
        <nav aria-label={t("nav.label")} className="relative mx-2 mb-3 md:flex-1">
          <ul
            ref={rowRef}
            data-testid="nav-row"
            className="flex [scrollbar-width:thin] gap-1 overflow-x-auto md:flex-col"
          >
            {NAV.map((item) => (
              <li key={item.to}>
                <NavLink
                  to={item.to}
                  end={item.to === "/"}
                  className={({ isActive }) =>
                    cn(
                      "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium whitespace-nowrap text-muted-foreground transition-colors hover:bg-muted hover:text-foreground [&_svg]:size-4",
                      isActive &&
                        "bg-accent text-accent-foreground hover:bg-accent hover:text-accent-foreground",
                    )
                  }
                >
                  {item.icon}
                  {t(item.label)}
                </NavLink>
              </li>
            ))}
          </ul>
          <ScrollEdgeShadows edges={edges} from="from-sidebar" />
        </nav>
      </header>
      <main
        id="main"
        tabIndex={-1}
        className="flex-1 px-4 py-6 focus:outline-none md:px-10 md:py-10"
      >
        <div className="mx-auto max-w-5xl">
          <Outlet />
        </div>
      </main>
    </div>
  );
}
