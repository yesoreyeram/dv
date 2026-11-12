import { BarChart3, BookOpen, Compass, Database, Home, type LucideIcon } from "lucide-react";
import type { MessageKey } from "@/i18n/messages";

/** The pages of the primary navigation (FR-005 AC1), in display order. */
export interface AppRoute {
  to: string;
  label: MessageKey;
  icon: LucideIcon;
  /** Second key of the `g …` sequence that opens the page (FR-008 AC7). */
  gotoKey: string;
}

export const ROUTES: readonly AppRoute[] = [
  { to: "/", label: "nav.home", icon: Home, gotoKey: "h" },
  { to: "/explore", label: "nav.explore", icon: Compass, gotoKey: "e" },
  { to: "/dashboards", label: "nav.dashboards", icon: BarChart3, gotoKey: "d" },
  { to: "/data-sources", label: "nav.dataSources", icon: Database, gotoKey: "s" },
  { to: "/api-reference", label: "nav.apiReference", icon: BookOpen, gotoKey: "a" },
];

/** The route a pathname belongs to; `undefined` for unknown paths (the not-found page). */
export function routeFor(pathname: string): AppRoute | undefined {
  return ROUTES.find((r) => r.to === pathname);
}
