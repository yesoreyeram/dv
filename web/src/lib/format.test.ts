import { describe, expect, it } from "vitest";
import {
  formatCell,
  formatJSON,
  formatNumber,
  formatRelativeTime,
  formatTime,
  parseRowLimit,
  sortedRowOrder,
} from "@/lib/format";
import type { Field } from "@/lib/frame";

const num = (values: (number | null)[], config = {}): Field => ({
  name: "n",
  type: "number",
  config,
  values,
});

describe("cell formatting (FR-013 AC2, NFR-I18N-002)", () => {
  it("formats numbers with the locale, decimals and unit", () => {
    expect(formatNumber(1234.5, {}, { locale: "en-US" })).toBe("1,234.5");
    expect(formatNumber(1234.5, {}, { locale: "de-DE" })).toBe("1.234,5");
    expect(formatNumber(2, { decimals: 2, unit: "ms" }, { locale: "en-US" })).toBe("2.00 ms");
  });

  it("formats times in the requested time zone", () => {
    const ms = Date.UTC(2026, 10, 1, 23, 30);
    expect(formatTime(ms, { locale: "en-US", timeZone: "UTC" })).toBe("Nov 1, 2026, 11:30:00 PM");
    expect(formatTime(ms, { locale: "en-GB", timeZone: "Asia/Kolkata" })).toBe(
      "2 Nov 2026, 05:00:00",
    );
  });

  it("formats bools, strings and json as text and cuts long json", () => {
    const f = (type: Field["type"]) => ({ name: "x", type, config: {}, values: [] }) as Field;
    expect(formatCell(f("bool"), true)).toBe("true");
    expect(formatCell(f("bool"), false)).toBe("false");
    expect(formatCell(f("string"), "<b>hi</b>")).toBe("<b>hi</b>");
    expect(formatCell(f("json"), { a: [1, 2] })).toBe('{"a":[1,2]}');
    const long = formatJSON({ s: "x".repeat(500) });
    expect(long).toHaveLength(200);
    expect(long.endsWith("…")).toBe(true);
    expect(formatJSON(undefined)).toBe("undefined");
  });
});

describe("sorting (FR-013 AC3)", () => {
  it("sorts numbers both ways, nulls last, stable", () => {
    const f = num([3, null, 1, 3, 2, null]);
    expect(sortedRowOrder(f, "ascending")).toEqual([2, 4, 0, 3, 1, 5]);
    expect(sortedRowOrder(f, "descending")).toEqual([0, 3, 4, 2, 1, 5]);
    expect(f.values).toEqual([3, null, 1, 3, 2, null]);
  });

  it("sorts strings with a numeric collator and bools false first", () => {
    const s: Field = {
      name: "s",
      type: "string",
      config: {},
      values: ["web-10", "web-2", "Web-1"],
    };
    expect(sortedRowOrder(s, "ascending", { locale: "en" })).toEqual([2, 1, 0]);
    const b: Field = { name: "b", type: "bool", config: {}, values: [true, false, null, false] };
    expect(sortedRowOrder(b, "ascending")).toEqual([1, 3, 0, 2]);
  });

  it("sorts 100 000 rows quickly (NFR-PERF-005 AC2)", () => {
    const values = Array.from({ length: 100_000 }, (_, i) => (i * 7919) % 100_003);
    const f = num(values);
    const t0 = performance.now();
    const order = sortedRowOrder(f, "ascending");
    expect(performance.now() - t0).toBeLessThan(500);
    for (let i = 1; i < order.length; i++) {
      expect(values[order[i - 1] ?? 0] ?? 0).toBeLessThanOrEqual(values[order[i] ?? 0] ?? 0);
    }
  });
});

describe("row limit (FR-018 AC2)", () => {
  it.each([
    ["", undefined],
    [" 5 ", 5],
    ["100000", 100000],
    ["0", null],
    ["100001", null],
    ["1.5", null],
    ["-1", null],
    ["1e3", null],
    ["abc", null],
  ])("parses %j as %j", (raw, want) => {
    expect(parseRowLimit(raw)).toBe(want);
  });
});

describe("relative time (FR-018 AC9)", () => {
  const now = Date.UTC(2026, 10, 11, 12);
  const en = { locale: "en-US" };

  it.each([
    [0, "now"],
    [59_000, "now"],
    [60_000, "1 minute ago"],
    [59 * 60_000, "59 minutes ago"],
    [60 * 60_000, "1 hour ago"],
    [23 * 3_600_000, "23 hours ago"],
    [24 * 3_600_000, "yesterday"],
    [3 * 86_400_000, "3 days ago"],
  ])("formats %d ms ago as %s", (ago, want) => {
    expect(formatRelativeTime(now - ago, now, en)).toBe(want);
  });

  it("never reports the future for a clock that moved back", () => {
    expect(formatRelativeTime(now + 5 * 60_000, now, en)).toBe("now");
  });

  it("uses the locale", () => {
    expect(formatRelativeTime(now - 2 * 3_600_000, now, { locale: "de-DE" })).toBe("vor 2 Stunden");
  });
});
