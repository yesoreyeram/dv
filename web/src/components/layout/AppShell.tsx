import { useQuery } from "@tanstack/react-query";
import {
  BarChart3,
  CircleCheck,
  CircleDashed,
  Keyboard,
  Moon,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
  Sun,
  TriangleAlert,
  Unplug,
  type LucideIcon,
} from "lucide-react";
import { useLayoutEffect, useState } from "react";
import { Link, NavLink, Outlet, useLocation, useNavigate } from "react-router";
import { CommandPalette } from "@/components/command/CommandPalette";
import { ShortcutsDialog } from "@/components/command/ShortcutsDialog";
import { ErrorBoundary } from "@/components/layout/ErrorBoundary";
import { Button } from "@/components/ui/button";
import { Kbd } from "@/components/ui/kbd";
import { ScrollEdgeShadows } from "@/components/ui/scroll-edges";
import { t, type MessageKey } from "@/i18n/messages";
import { readinessQuery, versionQuery } from "@/lib/api";
import { isApplePlatform } from "@/lib/hotkeys";
import { useSidebar } from "@/lib/prefs";
import { revealInRow } from "@/lib/revealInRow";
import { routeFor, ROUTES } from "@/lib/routes";
import { useHotkeys } from "@/lib/useHotkeys";
import { useScrollEdges } from "@/lib/useScrollEdges";
import { useTheme } from "@/lib/useTheme";
import { cn } from "@/lib/utils";

type Overlay = "palette" | "shortcuts" | null;
type ServerState = "checking" | "ready" | "notReady" | "unreachable";

const SERVER_STATE: Record<ServerState, { label: MessageKey; icon: LucideIcon; tone: string }> = {
  checking: { label: "shell.serverChecking", icon: CircleDashed, tone: "text-muted-foreground" },
  ready: { label: "shell.serverReady", icon: CircleCheck, tone: "text-success" },
  notReady: { label: "shell.serverNotReady", icon: TriangleAlert, tone: "text-danger" },
  unreachable: { label: "shell.serverUnreachable", icon: Unplug, tone: "text-danger" },
};

/** Sidebar foot (FR-005 AC10): readiness by icon shape and text, plus the server version. */
function ServerFoot({ collapsed }: { collapsed: boolean }) {
  const ready = useQuery(readinessQuery);
  const version = useQuery(versionQuery).data?.version;
  const state: ServerState = ready.isPending
    ? "checking"
    : ready.isError
      ? "unreachable"
      : ready.data.status === "ready"
        ? "ready"
        : "notReady";
  const { label, icon: Icon, tone } = SERVER_STATE[state];
  return (
    <div
      className="border-t px-2 py-2 text-xs text-muted-foreground max-md:hidden"
      data-testid="server-state"
      data-state={state}
      {...(collapsed ? { title: t(label) } : {})}
    >
      <p className={cn("flex h-6 items-center gap-2 px-2", collapsed && "justify-center px-0")}>
        <Icon aria-hidden="true" className={cn("size-3.5 shrink-0", tone)} />
        <span className={cn("truncate", collapsed && "sr-only")}>{t(label)}</span>
      </p>
      {!collapsed && typeof version === "string" && version !== "" ? (
        <p className="truncate px-2 ps-[1.875rem] font-mono text-[0.6875rem]">
          {t("shell.version", { version })}
        </p>
      ) : null}
    </div>
  );
}

/**
 * The workspace frame (FR-005): a top bar with breadcrumb, command palette trigger and
 * theme toggle; a collapsible sidebar from `md` up and a scrolling tab row below it; the
 * routed page inside an error boundary; and the app-wide keyboard shortcuts (FR-008).
 */
export function AppShell() {
  const { theme, toggle } = useTheme();
  const sidebar = useSidebar();
  const [overlay, setOverlay] = useState<Overlay>(null);
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const [rowRef, edges] = useScrollEdges<HTMLUListElement>([pathname]);
  const mod = isApplePlatform() ? "⌘" : "Ctrl";

  useHotkeys(
    {
      onPalette: () => {
        setOverlay((o) => (o === "palette" ? null : "palette"));
      },
      onSidebar: sidebar.toggle,
      onHelp: () => {
        setOverlay("shortcuts");
      },
      onGoto: (path) => void navigate(path),
    },
    overlay !== null,
  );

  useLayoutEffect(() => {
    const row = rowRef.current;
    const current = row?.querySelector<HTMLElement>('[aria-current="page"]');
    if (row && current) revealInRow(row, current);
  }, [pathname, rowRef]);

  const themeLabel = theme === "dark" ? t("theme.toLight") : t("theme.toDark");
  const sidebarLabel = sidebar.collapsed ? t("shell.expandSidebar") : t("shell.collapseSidebar");
  const current = routeFor(pathname);
  const closeOverlay = () => {
    setOverlay(null);
  };

  return (
    <>
      <div className="flex min-h-dvh flex-col" inert={overlay !== null}>
        <a
          href="#main"
          className="sr-only z-50 rounded-md bg-primary px-4 py-2 text-primary-foreground focus:not-sr-only focus:fixed focus:start-4 focus:top-4"
        >
          {t("a11y.skipToContent")}
        </a>
        <header className="sticky top-0 z-30 flex h-(--topbar-height) shrink-0 items-center gap-1.5 border-b bg-background/85 px-2 backdrop-blur-md md:px-3">
          <Button
            variant="ghost"
            size="icon"
            className="max-md:hidden"
            onClick={sidebar.toggle}
            aria-label={sidebarLabel}
            aria-expanded={!sidebar.collapsed}
            aria-controls="primary-nav"
            title={`${sidebarLabel} (${mod}+B)`}
          >
            {sidebar.collapsed ? (
              <PanelLeftOpen aria-hidden="true" />
            ) : (
              <PanelLeftClose aria-hidden="true" />
            )}
          </Button>
          <nav aria-label={t("shell.breadcrumb")} className="min-w-0">
            <ol className="flex items-center gap-1.5 text-sm">
              <li>
                <Link
                  to="/"
                  aria-label={t("shell.brandHome")}
                  className="flex h-8 items-center gap-2 rounded-md px-1.5 font-semibold tracking-tight hover:bg-muted"
                >
                  <span
                    aria-hidden="true"
                    className="grid size-5 place-items-center rounded-sm bg-primary text-primary-foreground"
                  >
                    <BarChart3 className="size-3" />
                  </span>
                  <span aria-hidden="true">{t("app.name")}</span>
                </Link>
              </li>
              <li aria-hidden="true" className="text-muted-foreground/70">
                /
              </li>
              <li
                aria-current="page"
                data-testid="breadcrumb-current"
                className="truncate font-medium"
              >
                {current ? t(current.label) : t("notFound.title")}
              </li>
            </ol>
          </nav>
          <div className="ms-auto flex items-center gap-1">
            <button
              type="button"
              onClick={() => {
                setOverlay("palette");
              }}
              aria-haspopup="dialog"
              aria-keyshortcuts="Control+K Meta+K"
              className="flex h-8 items-center gap-2 rounded-md border bg-card px-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground sm:w-56 lg:w-72"
            >
              <Search aria-hidden="true" className="size-4 shrink-0" />
              <span className="flex-1 truncate text-start max-sm:sr-only">{t("shell.search")}</span>
              <span aria-hidden="true" className="flex gap-0.5 max-sm:hidden">
                <Kbd>{mod}</Kbd>
                <Kbd>K</Kbd>
              </span>
            </button>
            <Button
              variant="ghost"
              size="icon"
              onClick={() => {
                setOverlay("shortcuts");
              }}
              aria-label={t("shell.shortcuts")}
              aria-haspopup="dialog"
              title={`${t("shell.shortcuts")} (?)`}
            >
              <Keyboard aria-hidden="true" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              onClick={toggle}
              aria-label={themeLabel}
              title={themeLabel}
            >
              {theme === "dark" ? <Sun aria-hidden="true" /> : <Moon aria-hidden="true" />}
            </Button>
          </div>
        </header>

        <div className="flex flex-1 flex-col md:flex-row">
          {/* The column stretches to the page height, so its background never stops short
              (B-068); the navigation inside it stays in view while the page scrolls. */}
          <div
            data-testid="sidebar"
            data-collapsed={sidebar.collapsed}
            className={cn(
              "border-b bg-sidebar md:shrink-0 md:border-e md:border-b-0 md:transition-[width] md:duration-150",
              sidebar.collapsed ? "md:w-12" : "md:w-56",
            )}
          >
            <div className="md:sticky md:top-(--topbar-height) md:flex md:h-[calc(100dvh-var(--topbar-height))] md:flex-col">
              <nav
                id="primary-nav"
                aria-label={t("nav.label")}
                className="relative mx-2 my-2 md:flex-1"
              >
                <ul
                  ref={rowRef}
                  data-testid="nav-row"
                  className="flex [scrollbar-width:thin] gap-0.5 overflow-x-auto md:flex-col md:overflow-x-visible"
                >
                  {ROUTES.map(({ to, label, icon: Icon }) => (
                    <li key={to}>
                      <NavLink
                        to={to}
                        end={to === "/"}
                        {...(sidebar.collapsed ? { title: t(label) } : {})}
                        className={({ isActive }) =>
                          cn(
                            "flex h-8 items-center gap-2.5 rounded-md px-2 text-sm font-medium whitespace-nowrap text-muted-foreground transition-colors hover:bg-muted hover:text-foreground",
                            sidebar.collapsed && "md:justify-center md:px-0",
                            isActive &&
                              "bg-accent text-accent-foreground hover:bg-accent hover:text-accent-foreground",
                          )
                        }
                      >
                        <Icon aria-hidden="true" className="size-4 shrink-0" />
                        <span className={cn(sidebar.collapsed && "md:sr-only")}>{t(label)}</span>
                      </NavLink>
                    </li>
                  ))}
                </ul>
                <ScrollEdgeShadows edges={edges} from="from-sidebar" />
              </nav>
              <ServerFoot collapsed={sidebar.collapsed} />
            </div>
          </div>
          <main
            id="main"
            tabIndex={-1}
            className="min-w-0 flex-1 px-3 py-4 focus:outline-none md:px-6 md:py-5"
          >
            <ErrorBoundary key={pathname}>
              <Outlet />
            </ErrorBoundary>
          </main>
        </div>
      </div>
      {overlay === "palette" ? (
        <CommandPalette
          onClose={closeOverlay}
          onShortcuts={() => {
            setOverlay("shortcuts");
          }}
        />
      ) : null}
      {overlay === "shortcuts" ? <ShortcutsDialog onClose={closeOverlay} /> : null}
    </>
  );
}
