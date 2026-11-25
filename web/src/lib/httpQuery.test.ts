import { describe, expect, it } from "vitest";
import { buildHttpQuery, MAX_SELECTOR } from "./httpQuery";

const base = { method: "GET", path: "/repos/acme/web/issues", rows: "", options: "" };

describe("buildHttpQuery (FR-040)", () => {
  it("builds a GET query with only the path", () => {
    expect(buildHttpQuery(base)).toEqual({ ok: true, query: { path: "/repos/acme/web/issues" } });
  });

  it("adds method, rows and the option keys", () => {
    const r = buildHttpQuery({
      method: "POST",
      path: " /logs ",
      rows: " .data[] ",
      options:
        '{"params":[{"name":"a","value":"1"}],"pagination":{"type":"cursor","cursorSelector":".next","cursorParam":"c"},"body":{"q":1},"columns":[{"name":"x","selector":".x"}],"flatten":true,"fieldTypes":{"t":"time"},"headers":[]}',
    });
    expect(r).toEqual({
      ok: true,
      query: {
        path: "/logs",
        method: "POST",
        rows: ".data[]",
        params: [{ name: "a", value: "1" }],
        pagination: { type: "cursor", cursorSelector: ".next", cursorParam: "c" },
        body: { q: 1 },
        columns: [{ name: "x", selector: ".x" }],
        flatten: true,
        fieldTypes: { t: "time" },
        headers: [],
      },
    });
  });

  it.each(["", "repos", "//evil.example/x", "http://169.254.169.254/", "/" + "a".repeat(2048)])(
    "refuses the path %j",
    (path) => {
      expect(buildHttpQuery({ ...base, path })).toEqual({ ok: false, error: "path" });
    },
  );

  it.each([
    "not json",
    "[]",
    "1",
    "null",
    '{"path":"/other"}',
    '{"method":"DELETE"}',
    '{"rows":".x"}',
    '{"__proto__":{"polluted":true}}',
    "{" + '"params":[],'.repeat(6000) + '"flatten":true}',
  ])("refuses the options %j", (options) => {
    expect(buildHttpQuery({ ...base, options })).toEqual({ ok: false, error: "options" });
    expect(({} as Record<string, unknown>).polluted).toBeUndefined();
  });

  it("refuses an over-long selector", () => {
    expect(buildHttpQuery({ ...base, rows: ".".repeat(MAX_SELECTOR + 1) })).toEqual({
      ok: false,
      error: "rows",
    });
  });
});
