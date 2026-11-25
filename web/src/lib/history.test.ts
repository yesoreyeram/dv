import { describe, expect, it } from "vitest";
import {
  clearHistory,
  HISTORY_LIMIT,
  HISTORY_MAX_BYTES,
  historySearch,
  parseEntry,
  parseHistory,
  readHistory,
  recordQuery,
  restoreToken,
  sameQuery,
} from "./history";
import { PREF_KEYS } from "./prefs";

const store = (doc: unknown) => {
  window.localStorage.setItem(PREF_KEYS.history, JSON.stringify(doc));
};

describe("query history (FR-018 AC9, NFR-SEC-019)", () => {
  it("records queries newest first without duplicates", () => {
    recordQuery({ ds: "samples", path: "a.csv" }, 1000);
    recordQuery({ ds: "samples", path: "b.csv", format: "csv", maxRows: 5 }, 2000);
    recordQuery({ ds: "samples", path: "a.csv" }, 3000);
    expect(readHistory()).toEqual([
      { ds: "samples", path: "a.csv", at: 3000 },
      { ds: "samples", path: "b.csv", format: "csv", maxRows: 5, at: 2000 },
    ]);
  });

  it("treats a different option as a different query", () => {
    recordQuery({ ds: "s", path: "a.json", rowsPath: "data" }, 1);
    recordQuery({ ds: "s", path: "a.json", rowsPath: "items" }, 2);
    recordQuery({ ds: "s", path: "a.json", rowsPath: "items", flatten: true }, 3);
    expect(readHistory().map((e) => e.at)).toEqual([3, 2, 1]);
    expect(sameQuery({ ds: "s", path: "p" }, { ds: "s", path: "p", flatten: false })).toBe(true);
  });

  it("keeps at most the limit", () => {
    for (let i = 0; i < HISTORY_LIMIT + 5; i++) recordQuery({ ds: "s", path: `f${String(i)}` }, i);
    const entries = readHistory();
    expect(entries).toHaveLength(HISTORY_LIMIT);
    expect(entries[0]?.path).toBe(`f${String(HISTORY_LIMIT + 4)}`);
    expect(entries.at(-1)?.path).toBe("f5");
  });

  it("returns the same array until storage changes (stable snapshot)", () => {
    recordQuery({ ds: "s", path: "a" }, 1);
    const first = readHistory();
    expect(readHistory()).toBe(first);
    recordQuery({ ds: "s", path: "b" }, 2);
    expect(readHistory()).not.toBe(first);
  });

  it("clears", () => {
    recordQuery({ ds: "s", path: "a" }, 1);
    clearHistory();
    expect(readHistory()).toEqual([]);
    expect(window.localStorage.getItem(PREF_KEYS.history)).toBeNull();
  });

  it("refuses to record a query that would not validate", () => {
    recordQuery({ ds: "", path: "a" }, 1);
    recordQuery({ ds: "s", path: "x".repeat(1025) }, 1);
    recordQuery({ ds: "s", path: "a", maxRows: 0 }, 1);
    expect(readHistory()).toEqual([]);
  });

  it.each([
    ["not JSON", "{oops"],
    ["an object", '{"ds":"s","path":"p","at":1}'],
    ["a string", '"x"'],
    ["null", "null"],
    ["oversized", JSON.stringify([{ ds: "s", path: "p", at: 1 }]).padEnd(HISTORY_MAX_BYTES + 1)],
  ])("ignores a stored document that is %s", (_, raw) => {
    expect(parseHistory(raw)).toEqual([]);
  });

  it("drops invalid entries and keeps the valid ones", () => {
    store([
      { ds: "s", path: "ok.csv", at: 5 },
      null,
      "x",
      [],
      { ds: "s", at: 1 },
      { ds: 7, path: "p", at: 1 },
      { ds: "s", path: "", at: 1 },
      { ds: "s".repeat(65), path: "p", at: 1 },
      { ds: "s", path: "p", at: -1 },
      { ds: "s", path: "p", at: 1.5 },
      { ds: "s", path: "p", at: "1" },
      { ds: "s", path: "p", at: 1, format: 3 },
      { ds: "s", path: "p", at: 1, format: "x".repeat(33) },
      { ds: "s", path: "p", at: 1, maxRows: 100_001 },
      { ds: "s", path: "p", at: 1, maxRows: "5" },
      { ds: "s", path: "p", at: 1, rowsPath: "" },
      { ds: "s", path: "p", at: 1, flatten: "yes" },
      { ds: "s", path: "ok.json", at: 4, rowsPath: "data.items", flatten: true, maxRows: 10 },
    ]);
    expect(readHistory()).toEqual([
      { ds: "s", path: "ok.csv", at: 5 },
      { ds: "s", path: "ok.json", at: 4, rowsPath: "data.items", flatten: true, maxRows: 10 },
    ]);
  });

  it("keeps known keys only and never the prototype", () => {
    const entry = parseEntry(
      JSON.parse('{"ds":"s","path":"p","at":1,"__proto__":{"x":1},"html":"<b>","flatten":false}'),
    );
    expect(entry).toEqual({ ds: "s", path: "p", at: 1 });
    expect(Object.keys(entry ?? {})).toEqual(["ds", "path", "at"]);
    expect(({} as Record<string, unknown>).x).toBeUndefined();
  });

  it("cuts a longer stored list to the limit and removes stored duplicates", () => {
    store([
      { ds: "s", path: "dup", at: 9 },
      { ds: "s", path: "dup", at: 8 },
      ...Array.from({ length: 40 }, (_, i) => ({ ds: "s", path: `f${String(i)}`, at: i })),
    ]);
    const entries = readHistory();
    expect(entries).toHaveLength(HISTORY_LIMIT);
    expect(entries.filter((e) => e.path === "dup")).toHaveLength(1);
  });

  it("builds the Explore query string from validated fields", () => {
    expect(historySearch({ ds: "samples", path: "a b&c=d.csv" })).toBe(
      "ds=samples&path=a+b%26c%3Dd.csv",
    );
    expect(
      historySearch({
        ds: "s",
        path: "x.json",
        format: "json",
        maxRows: 7,
        rowsPath: "data.items",
        flatten: true,
      }),
    ).toBe("ds=s&path=x.json&format=json&maxRows=7&rowsPath=data.items&flatten=1");
  });

  it("hands out distinct restore tokens", () => {
    expect(restoreToken()).not.toBe(restoreToken());
  });
});
