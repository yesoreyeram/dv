import { describe, expect, it } from "vitest";
import {
  effectiveFormat,
  isValidRowsPath,
  jsonControls,
  jsonQueryOptions,
} from "@/lib/jsonOptions";

const formats = [
  { id: "csv", extensions: [".csv"] },
  { id: "json", extensions: [".json"] },
  { id: "ndjson", extensions: [".ndjson", ".jsonl"] },
];

describe("isValidRowsPath", () => {
  it.each(["data", "data.items", "a-b_c.D9", Array.from({ length: 16 }, () => "a").join(".")])(
    "accepts %s",
    (p) => {
      expect(isValidRowsPath(p)).toBe(true);
    },
  );
  it.each([
    "",
    "a..b",
    ".a",
    "a.",
    "a[0]",
    "$.a",
    "a b",
    "x".repeat(129),
    Array.from({ length: 17 }, () => "a").join("."),
  ])("rejects %s", (p) => {
    expect(isValidRowsPath(p)).toBe(false);
  });
});

describe("effectiveFormat", () => {
  it("prefers the explicit format", () => {
    expect(effectiveFormat("csv", "a.json", formats)).toBe("csv");
  });
  it("matches the extension case-insensitively", () => {
    expect(effectiveFormat("", " logs/App.JSONL ", formats)).toBe("ndjson");
    expect(effectiveFormat("", "a.json", formats)).toBe("json");
  });
  it("is undefined for unknown extensions", () => {
    expect(effectiveFormat("", "a.txt", formats)).toBeUndefined();
  });
});

describe("jsonControls and jsonQueryOptions", () => {
  it("shows both controls for json and only flatten for ndjson", () => {
    expect(jsonControls("json")).toEqual({ rowsPath: true, flatten: true });
    expect(jsonControls("ndjson")).toEqual({ rowsPath: false, flatten: true });
    expect(jsonControls("csv")).toEqual({ rowsPath: false, flatten: false });
    expect(jsonControls(undefined)).toEqual({ rowsPath: false, flatten: false });
  });
  it("sends only options that apply and are set", () => {
    expect(jsonQueryOptions("json", { rowsPath: " data.items ", flatten: true })).toEqual({
      rowsPath: "data.items",
      flatten: true,
    });
    expect(jsonQueryOptions("json", { rowsPath: "", flatten: false })).toBeUndefined();
    expect(jsonQueryOptions("ndjson", { rowsPath: "data", flatten: true })).toEqual({
      flatten: true,
    });
    expect(jsonQueryOptions("csv", { rowsPath: "data", flatten: true })).toBeUndefined();
  });
});
