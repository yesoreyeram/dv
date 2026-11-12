import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  getSidebar,
  getSingleKeyShortcuts,
  getSplit,
  PREF_KEYS,
  readEnum,
  readNumber,
  readRaw,
  setSidebar,
  setSingleKeyShortcuts,
  setSplit,
  SPLIT,
  subscribePrefs,
  useSidebar,
  writeRaw,
} from "./prefs";

describe("preferences in browser storage (NFR-SEC-019)", () => {
  it("accepts only exact enum values", () => {
    expect(readEnum("k", ["a", "b"] as const, "a")).toBe("a");
    window.localStorage.setItem("k", "b");
    expect(readEnum("k", ["a", "b"] as const, "a")).toBe("b");
    for (const hostile of ["B", " b", "b ", "<img src=x>", "__proto__", ""]) {
      window.localStorage.setItem("k", hostile);
      expect(readEnum("k", ["a", "b"] as const, "a")).toBe("a");
    }
  });

  it("reads finite numbers only, rounded and clamped", () => {
    expect(readNumber("n", 10, 20)).toBeNull();
    const cases: [string, number | null][] = [
      ["15", 15],
      ["15.6", 16],
      ["5", 10],
      ["1e9", 20],
      ["-3", 10],
      ["", null],
      ["  ", null],
      ["abc", null],
      ["NaN", null],
      ["Infinity", null],
      ["12px", null],
    ];
    for (const [raw, want] of cases) {
      window.localStorage.setItem("n", raw);
      expect(readNumber("n", 10, 20), raw).toBe(want);
    }
  });

  it("survives storage that throws on read and write", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("denied");
    });
    vi.spyOn(Storage.prototype, "removeItem").mockImplementation(() => {
      throw new Error("denied");
    });
    expect(readRaw("k")).toBeNull();
    expect(() => {
      writeRaw("k", "v");
      writeRaw("k", null);
    }).not.toThrow();
    expect(getSidebar()).toBe("expanded");
    expect(getSingleKeyShortcuts()).toBe(true);
    expect(getSplit()).toBe(SPLIT.default);
  });

  it("stores the sidebar state and treats anything else as expanded (FR-005 AC9)", () => {
    expect(getSidebar()).toBe("expanded");
    setSidebar("collapsed");
    expect(window.localStorage.getItem(PREF_KEYS.sidebar)).toBe("collapsed");
    expect(getSidebar()).toBe("collapsed");
    window.localStorage.setItem(PREF_KEYS.sidebar, "hidden");
    expect(getSidebar()).toBe("expanded");
  });

  it("stores the single-key shortcut switch (FR-008 AC8)", () => {
    setSingleKeyShortcuts(false);
    expect(window.localStorage.getItem(PREF_KEYS.hotkeys)).toBe("off");
    expect(getSingleKeyShortcuts()).toBe(false);
    setSingleKeyShortcuts(true);
    expect(getSingleKeyShortcuts()).toBe(true);
  });

  it("clamps the Explore split on write and on read (FR-018 AC8)", () => {
    setSplit(10_000);
    expect(window.localStorage.getItem(PREF_KEYS.split)).toBe(String(SPLIT.max));
    setSplit(1);
    expect(getSplit()).toBe(SPLIT.min);
    window.localStorage.setItem(PREF_KEYS.split, "javascript:alert(1)");
    expect(getSplit()).toBe(SPLIT.default);
    window.localStorage.setItem(PREF_KEYS.split, "99999");
    expect(getSplit()).toBe(SPLIT.max);
  });

  it("notifies subscribers on writes and on storage events from other tabs", () => {
    const cb = vi.fn();
    const unsubscribe = subscribePrefs(cb);
    writeRaw("k", "v");
    expect(cb).toHaveBeenCalledTimes(1);
    window.dispatchEvent(new StorageEvent("storage", { key: "k" }));
    expect(cb).toHaveBeenCalledTimes(2);
    unsubscribe();
    writeRaw("k", "w");
    window.dispatchEvent(new StorageEvent("storage", { key: "k" }));
    expect(cb).toHaveBeenCalledTimes(2);
  });

  it("useSidebar toggles and follows another tab", () => {
    const { result } = renderHook(() => useSidebar());
    expect(result.current.collapsed).toBe(false);
    act(() => {
      result.current.toggle();
    });
    expect(result.current.collapsed).toBe(true);
    act(() => {
      window.localStorage.setItem(PREF_KEYS.sidebar, "expanded");
      window.dispatchEvent(new StorageEvent("storage", { key: PREF_KEYS.sidebar }));
    });
    expect(result.current.collapsed).toBe(false);
  });
});
