import { useCallback, useEffect, useSyncExternalStore } from "react";
import {
  applyTheme,
  getInitialTheme,
  getThemePreference,
  setThemePreference,
  subscribeTheme,
  type Theme,
  type ThemePreference,
} from "./theme";

/**
 * The effective theme and the stored preference, shared by every caller (top bar, command
 * palette). While no preference is stored the theme follows OS changes (FR-005 AC12).
 */
export function useTheme(): {
  theme: Theme;
  preference: ThemePreference;
  toggle: () => void;
  setPreference: (pref: ThemePreference) => void;
} {
  const theme = useSyncExternalStore(subscribeTheme, getInitialTheme);
  const preference = useSyncExternalStore(subscribeTheme, getThemePreference);
  useEffect(() => {
    applyTheme(theme);
  }, [theme]);
  const toggle = useCallback(() => {
    setThemePreference(getInitialTheme() === "dark" ? "light" : "dark");
  }, []);
  return { theme, preference, toggle, setPreference: setThemePreference };
}
