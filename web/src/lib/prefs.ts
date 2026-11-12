import { useSyncExternalStore } from "react";

/**
 * Per-browser display preferences in `localStorage` (NFR-SEC-019).
 *
 * Storage is untrusted input: anything that ever ran on this origin can write to it, and
 * it can be missing altogether (private mode, blocked storage). Every read is wrapped and
 * validated, and nothing here ever holds credentials or query results.
 */
export const PREF_KEYS = {
  theme: "dv.theme",
  sidebar: "dv.sidebar",
  hotkeys: "dv.hotkeys",
  split: "dv.explore.split",
  history: "dv.explore.history",
} as const;

export function readRaw(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

/** Writes (or, with `null`, removes) a value and tells subscribers; failures are ignored. */
export function writeRaw(key: string, value: string | null): void {
  try {
    if (value === null) window.localStorage.removeItem(key);
    else window.localStorage.setItem(key, value);
  } catch {
    // Storage may be unavailable; the preference still applies for this page.
  }
  notifyPrefs();
}

/** Returns the stored value only when it is exactly one of `allowed`. */
export function readEnum<T extends string>(key: string, allowed: readonly T[], fallback: T): T {
  const raw = readRaw(key);
  return allowed.find((a) => a === raw) ?? fallback;
}

/** A stored finite number clamped to [min, max]; `null` when absent or not a number. */
export function readNumber(key: string, min: number, max: number): number | null {
  const raw = readRaw(key);
  if (raw === null || raw.trim() === "") return null;
  const n = Number(raw);
  if (!Number.isFinite(n)) return null;
  return Math.min(max, Math.max(min, Math.round(n)));
}

const listeners = new Set<() => void>();

export function notifyPrefs(): void {
  for (const l of listeners) l();
}

/** Subscribes to preference changes from this tab and (`storage` event) other tabs. */
export function subscribePrefs(cb: () => void): () => void {
  listeners.add(cb);
  window.addEventListener("storage", cb);
  return () => {
    listeners.delete(cb);
    window.removeEventListener("storage", cb);
  };
}

/* ---------- sidebar (FR-005 AC9) ---------- */

export type SidebarState = "expanded" | "collapsed";

export function getSidebar(): SidebarState {
  return readEnum(PREF_KEYS.sidebar, ["expanded", "collapsed"] as const, "expanded");
}

export function setSidebar(state: SidebarState): void {
  writeRaw(PREF_KEYS.sidebar, state);
}

export function useSidebar(): { collapsed: boolean; toggle: () => void } {
  const state = useSyncExternalStore(subscribePrefs, getSidebar);
  return {
    collapsed: state === "collapsed",
    toggle: () => {
      setSidebar(getSidebar() === "collapsed" ? "expanded" : "collapsed");
    },
  };
}

/* ---------- single-key shortcuts (FR-008 AC8) ---------- */

export function getSingleKeyShortcuts(): boolean {
  return readEnum(PREF_KEYS.hotkeys, ["on", "off"] as const, "on") === "on";
}

export function setSingleKeyShortcuts(on: boolean): void {
  writeRaw(PREF_KEYS.hotkeys, on ? "on" : "off");
}

export function useSingleKeyShortcuts(): boolean {
  return useSyncExternalStore(subscribePrefs, getSingleKeyShortcuts);
}

/* ---------- Explore splitter (FR-018 AC8) ---------- */

export const SPLIT = { min: 280, max: 640, default: 360, step: 16 } as const;

export function getSplit(): number {
  return readNumber(PREF_KEYS.split, SPLIT.min, SPLIT.max) ?? SPLIT.default;
}

export function setSplit(px: number): void {
  writeRaw(PREF_KEYS.split, String(Math.min(SPLIT.max, Math.max(SPLIT.min, Math.round(px)))));
}
