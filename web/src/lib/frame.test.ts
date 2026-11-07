import { describe, expect, it } from "vitest";
import { decodeFrame, FrameDecodeError } from "@/lib/frame";

// Same payload as the Go wire test (internal/frame/frame_test.go TestWireEncoding).
const wire = {
  schema: {
    name: "cpu",
    meta: {
      source: "csv",
      executedQuery: "SELECT 1",
      truncated: true,
      notices: [{ severity: "info", text: "hi" }, { severity: "bogus" }],
    },
    fields: [
      { name: "time", type: "time", config: {} },
      { name: "value", type: "number", config: { displayName: "Value", unit: "ms", decimals: 2 } },
      { name: "host", type: "string", config: {} },
      { name: "ok", type: "bool", config: {} },
      { name: "tags", type: "json" },
    ],
  },
  data: {
    values: [
      [1794045600000, 1794045660000, null, 1794045720000],
      [1.0, 2 ** 52, -0.25, 1e300],
      ["a", null, "c", "d"],
      [true, false, null, true],
      [{ k: "v" }, null, [1, 2], null],
    ],
  },
};

const payload = (fields: unknown[], values: unknown[]) => ({
  schema: { name: "x", fields },
  data: { values },
});

describe("decodeFrame (FR-010 AC7)", () => {
  it("decodes a valid frame into typed columns", () => {
    const f = decodeFrame(wire);
    expect(f.name).toBe("cpu");
    expect(f.length).toBe(4);
    expect(f.meta).toEqual({
      source: "csv",
      executedQuery: "SELECT 1",
      truncated: true,
      notices: [{ severity: "info", text: "hi" }],
    });
    expect(f.fields.map((x) => x.type)).toEqual(["time", "number", "string", "bool", "json"]);
    expect(f.fields[0]?.values[0]).toBe(Date.UTC(2026, 10, 7, 10));
    expect(f.fields[1]?.config).toEqual({ displayName: "Value", unit: "ms", decimals: 2 });
    expect(f.fields[4]?.config).toEqual({});
    expect(f.fields[4]?.values[2]).toEqual([1, 2]);
  });

  it("drops malformed optional meta and config members", () => {
    const f = decodeFrame({
      schema: {
        name: 1,
        meta: { source: 1, executedQuery: [], truncated: "yes", notices: "x" },
        fields: [
          { name: "a", type: "number", config: { unit: 1, decimals: 1.5, displayName: {} } },
        ],
      },
      data: { values: [[]] },
    });
    expect(f.name).toBe("");
    expect(f.meta).toEqual({});
    expect(f.fields[0]?.config).toEqual({});
  });

  it("handles frames without fields or meta", () => {
    const f = decodeFrame({ schema: { fields: [] }, data: { values: [] } });
    expect(f).toEqual({ name: "", meta: {}, fields: [], length: 0 });
  });

  it.each([
    ["non-object", null],
    ["missing data", { schema: { fields: [] } }],
    ["fields not array", { schema: { fields: {} }, data: { values: [] } }],
    ["values not array", { schema: { fields: [] }, data: { values: {} } }],
    ["column count", payload([{ name: "a", type: "number" }], [])],
    ["empty name", payload([{ name: "", type: "number" }], [[]])],
    ["field not object", payload(["a"], [[]])],
    [
      "duplicate",
      payload(
        [
          { name: "a", type: "bool" },
          { name: "a", type: "bool" },
        ],
        [[], []],
      ),
    ],
    ["bad type", payload([{ name: "a", type: "int" }], [[]])],
    ["column not array", payload([{ name: "a", type: "number" }], [{}])],
    [
      "length mismatch",
      payload(
        [
          { name: "a", type: "number" },
          { name: "b", type: "string" },
        ],
        [[1, 2], ["x"]],
      ),
    ],
    ["time string", payload([{ name: "a", type: "time" }], [["2026-01-01"]])],
    ["time fraction", payload([{ name: "a", type: "time" }], [[1.5]])],
    ["number string", payload([{ name: "a", type: "number" }], [["1"]])],
    ["string number", payload([{ name: "a", type: "string" }], [[1]])],
    ["bool string", payload([{ name: "a", type: "bool" }], [["true"]])],
    ["config not object", payload([{ name: "a", type: "bool", config: 1 }], [[]])],
    ["meta not object", { schema: { name: "x", meta: [], fields: [] }, data: { values: [] } }],
  ])("rejects %s", (_name, input) => {
    expect(() => decodeFrame(input)).toThrow(FrameDecodeError);
  });

  it("names the offending field and row", () => {
    expect(() => decodeFrame(payload([{ name: "cpu", type: "number" }], [[1, "x"]]))).toThrow(
      'field "cpu" row 1: expected number',
    );
  });
});
