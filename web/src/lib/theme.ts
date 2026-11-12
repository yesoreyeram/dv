import { PREF_KEYS, readEnum, subscribePrefs, writeRaw } from "./prefs";

export type Theme = "light" | "dark";
/** `system` means nothing is stored and the OS colour scheme decides (FR-005 AC12). */
export type ThemePreference = Theme | "system";

export const THEME_STORAGE_KEY = PREF_KEYS.theme;

const DARK_QUERY = "(prefers-color-scheme: dark)";

export function getThemePreference(): ThemePreference {
  return readEnum<ThemePreference>(THEME_STORAGE_KEY, ["light", "dark"], "system");
}

export function systemTheme(): Theme {
  return window.matchMedia(DARK_QUERY).matches ? "dark" : "light";
}

/** Stored preference first, then the OS colour scheme (FR-005 AC4). */
export function getInitialTheme(): Theme {
  const pref = getThemePreference();
  return pref === "system" ? systemTheme() : pref;
}

export function applyTheme(theme: Theme): void {
  document.documentElement.dataset.theme = theme;
}

export function persistTheme(theme: Theme): void {
  writeRaw(THEME_STORAGE_KEY, theme);
}

/** Stores the choice (or forgets it for `system`) and applies the resulting theme. */
export function setThemePreference(pref: ThemePreference): void {
  writeRaw(THEME_STORAGE_KEY, pref === "system" ? null : pref);
  applyTheme(getInitialTheme());
}

/** Fires when the stored preference or the OS colour scheme changes. */
export function subscribeTheme(cb: () => void): () => void {
  const unsubscribe = subscribePrefs(cb);
  const mq = window.matchMedia(DARK_QUERY);
  mq.addEventListener("change", cb);
  return () => {
    unsubscribe();
    mq.removeEventListener("change", cb);
  };
}
