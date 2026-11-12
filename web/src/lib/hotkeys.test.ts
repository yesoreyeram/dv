import { describe, expect, it } from "vitest";
import { isEditableTarget, resolveHotkey, type HotkeyContext, type KeyInput } from "./hotkeys";

const key = (k: string, mods: Partial<KeyInput> = {}): KeyInput => ({
  key: k,
  ctrlKey: false,
  metaKey: false,
  altKey: false,
  shiftKey: false,
  ...mods,
});
const ctx = (over: Partial<HotkeyContext> = {}): HotkeyContext => ({
  editable: false,
  dialogOpen: false,
  singleKeys: true,
  armed: false,
  ...over,
});

describe("keyboard shortcuts (FR-008 AC1, AC6–AC8)", () => {
  it("opens the palette with Ctrl/⌘+K everywhere, also in fields and dialogs", () => {
    for (const c of [ctx(), ctx({ editable: true }), ctx({ dialogOpen: true })]) {
      expect(resolveHotkey(key("k", { ctrlKey: true }), c)).toEqual({ kind: "palette" });
      expect(resolveHotkey(key("K", { metaKey: true }), c)).toEqual({ kind: "palette" });
    }
    expect(resolveHotkey(key("k", { ctrlKey: true, shiftKey: true }), ctx())).toBeNull();
    expect(resolveHotkey(key("k", { ctrlKey: true, altKey: true }), ctx())).toBeNull();
  });

  it("toggles the sidebar with Ctrl/⌘+B unless a dialog is open", () => {
    expect(resolveHotkey(key("b", { ctrlKey: true }), ctx())).toEqual({ kind: "sidebar" });
    expect(resolveHotkey(key("b", { metaKey: true }), ctx({ editable: true }))).toEqual({
      kind: "sidebar",
    });
    expect(resolveHotkey(key("b", { ctrlKey: true }), ctx({ dialogOpen: true }))).toBeNull();
    expect(resolveHotkey(key("s", { ctrlKey: true }), ctx())).toBeNull();
  });

  it("maps single keys outside fields", () => {
    expect(resolveHotkey(key("/"), ctx())).toEqual({ kind: "palette" });
    expect(resolveHotkey(key("?", { shiftKey: true }), ctx())).toEqual({ kind: "help" });
    expect(resolveHotkey(key("g"), ctx())).toEqual({ kind: "arm" });
    expect(resolveHotkey(key("x"), ctx())).toBeNull();
    expect(resolveHotkey(key("Enter"), ctx())).toBeNull();
  });

  it("completes a g sequence only while armed", () => {
    expect(resolveHotkey(key("e"), ctx({ armed: true }))).toEqual({
      kind: "goto",
      path: "/explore",
    });
    expect(resolveHotkey(key("H"), ctx({ armed: true }))).toEqual({ kind: "goto", path: "/" });
    expect(resolveHotkey(key("s"), ctx({ armed: true }))).toEqual({
      kind: "goto",
      path: "/data-sources",
    });
    expect(resolveHotkey(key("e"), ctx())).toBeNull();
    expect(resolveHotkey(key("z"), ctx({ armed: true }))).toBeNull();
    // An inherited property name is not a route.
    expect(resolveHotkey(key("constructor"), ctx({ armed: true }))).toBeNull();
  });

  it.each([
    ["in an editable field", ctx({ editable: true })],
    ["while a dialog is open", ctx({ dialogOpen: true })],
    ["when switched off (WCAG 2.1.4)", ctx({ singleKeys: false })],
  ])("ignores single keys %s", (_, c) => {
    for (const k of ["/", "?", "g"]) expect(resolveHotkey(key(k), c)).toBeNull();
    expect(resolveHotkey(key("e"), { ...c, armed: true })).toBeNull();
  });

  it("ignores single keys with Alt held", () => {
    expect(resolveHotkey(key("/", { altKey: true }), ctx())).toBeNull();
  });

  it("recognises editable targets", () => {
    const make = (html: string) => {
      document.body.innerHTML = html;
      return document.body.firstElementChild;
    };
    expect(isEditableTarget(make("<input>"))).toBe(true);
    expect(isEditableTarget(make("<textarea></textarea>"))).toBe(true);
    expect(isEditableTarget(make("<select></select>"))).toBe(true);
    expect(isEditableTarget(make("<button></button>"))).toBe(false);
    expect(isEditableTarget(make("<div></div>"))).toBe(false);
    expect(isEditableTarget(make('<div contenteditable="true"></div>'))).toBe(true);
    expect(
      isEditableTarget(make('<div contenteditable=""><b></b></div>')?.firstElementChild ?? null),
    ).toBe(true);
    expect(isEditableTarget(make('<div contenteditable="false"></div>'))).toBe(false);
    expect(isEditableTarget(null)).toBe(false);
    expect(isEditableTarget(document)).toBe(false);
    document.body.innerHTML = "";
  });
});
