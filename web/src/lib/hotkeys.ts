/** Global keyboard shortcuts (FR-008 AC1, AC6–AC8). Pure decision logic; see useHotkeys. */
import { ROUTES } from "./routes";

export type HotkeyAction =
  | { kind: "palette" }
  | { kind: "sidebar" }
  | { kind: "help" }
  | { kind: "goto"; path: string }
  /** `g` was pressed: wait for the second key of a sequence. */
  | { kind: "arm" };

/** Second keys of the `g …` sequences (FR-008 AC7). A Map, so no inherited key matches. */
export const GOTO_KEYS: ReadonlyMap<string, string> = new Map(ROUTES.map((r) => [r.gotoKey, r.to]));

export const SEQUENCE_TIMEOUT_MS = 1500;

export interface KeyInput {
  key: string;
  ctrlKey: boolean;
  metaKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
}

export interface HotkeyContext {
  /** Focus is in an input, select, textarea or contenteditable element. */
  editable: boolean;
  /** A modal dialog is open. */
  dialogOpen: boolean;
  /** The user has not switched single-key shortcuts off (WCAG 2.1.4). */
  singleKeys: boolean;
  /** `g` was pressed less than SEQUENCE_TIMEOUT_MS ago. */
  armed: boolean;
}

/** Decides what a key press does; `null` leaves it to the browser. */
export function resolveHotkey(e: KeyInput, ctx: HotkeyContext): HotkeyAction | null {
  const mod = e.ctrlKey || e.metaKey;
  const key = e.key.length === 1 ? e.key.toLowerCase() : e.key;
  if (mod) {
    if (e.altKey || e.shiftKey) return null;
    if (key === "k") return { kind: "palette" };
    if (key === "b" && !ctx.dialogOpen) return { kind: "sidebar" };
    return null;
  }
  if (e.altKey || ctx.editable || ctx.dialogOpen || !ctx.singleKeys) return null;
  if (ctx.armed) {
    const path = GOTO_KEYS.get(key);
    if (path !== undefined) return { kind: "goto", path };
  }
  if (key === "/") return { kind: "palette" };
  if (key === "?") return { kind: "help" };
  if (key === "g" && !e.shiftKey) return { kind: "arm" };
  return null;
}

/** True when typing into `target` should never trigger a single-key shortcut. */
export function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
  return target.closest('[contenteditable]:not([contenteditable="false"])') !== null;
}

/** Whether the platform's primary modifier is ⌘ (for showing shortcuts, not handling them). */
export function isApplePlatform(): boolean {
  return /Mac|iPhone|iPad/i.test(navigator.userAgent);
}
