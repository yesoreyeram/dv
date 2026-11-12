import { useEffect, useRef } from "react";
import { isEditableTarget, resolveHotkey, SEQUENCE_TIMEOUT_MS } from "./hotkeys";
import { getSingleKeyShortcuts } from "./prefs";

export interface HotkeyHandlers {
  onPalette: () => void;
  onSidebar: () => void;
  onHelp: () => void;
  onGoto: (path: string) => void;
}

/**
 * One document-level `keydown` listener for the whole app (threat model F-14). Handlers are
 * read through a ref so the listener is attached once.
 */
export function useHotkeys(handlers: HotkeyHandlers, dialogOpen: boolean): void {
  const latest = useRef({ handlers, dialogOpen });
  useEffect(() => {
    latest.current = { handlers, dialogOpen };
  });

  useEffect(() => {
    let armedAt = Number.NEGATIVE_INFINITY;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.isComposing) return;
      const { handlers: h, dialogOpen: open } = latest.current;
      const now = performance.now();
      const armed = now - armedAt < SEQUENCE_TIMEOUT_MS;
      armedAt = Number.NEGATIVE_INFINITY;
      const action = resolveHotkey(e, {
        editable: isEditableTarget(e.target),
        dialogOpen: open,
        singleKeys: getSingleKeyShortcuts(),
        armed,
      });
      if (!action) return;
      if (action.kind === "arm") {
        armedAt = now;
        return;
      }
      e.preventDefault();
      switch (action.kind) {
        case "palette":
          h.onPalette();
          break;
        case "sidebar":
          h.onSidebar();
          break;
        case "help":
          h.onHelp();
          break;
        case "goto":
          h.onGoto(action.path);
          break;
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
    };
  }, []);
}
