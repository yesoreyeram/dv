import { describe, expect, it } from "vitest";
import type { DataFrame, Field } from "@/lib/frame";
import {
  MAX_SERIES,
  dashFor,
  extent,
  linear,
  nearest,
  niceYTicks,
  pathData,
  segments,
  timeOrder,
  timeSeriesShape,
  timeTicks,
  type NumberField,
  type TimeField,
} from "@/lib/timeseries";

const T0 = Date.UTC(2026, 10, 10);
const H = 3_600_000;

const tf = (values: (number | null)[]): TimeField => ({
  name: "time",
  type: "time",
  config: {},
  values,
});
const nf = (name: string, values: (number | null)[]): NumberField => ({
  name,
  type: "number",
  config: {},
  values,
});
const frameOf = (fields: Field[]): DataFrame => ({
  name: "f",
  meta: {},
  fields,
  length: fields[0]?.values.length ?? 0,
});
const id = (v: number) => v;

describe("timeSeriesShape (FR-014 AC2, AC4)", () => {
  it("uses the first time field and every number field", () => {
    const shape = timeSeriesShape(
      frameOf([
        { name: "host", type: "string", config: {}, values: ["a"] },
        tf([T0]),
        { ...tf([T0]), name: "t2" },
        nf("cpu", [1]),
        nf("mem", [2]),
      ]),
    );
    expect(shape.ok).toBe(true);
    if (!shape.ok) return;
    expect(shape.time.name).toBe("time");
    expect(shape.series.map((f) => f.name)).toEqual(["cpu", "mem"]);
    expect(shape.omitted).toBe(0);
  });

  it("explains what is missing", () => {
    expect(
      timeSeriesShape(
        frameOf([{ name: "host", type: "string", config: {}, values: ["a"] }, nf("cpu", [1])]),
      ),
    ).toEqual({ ok: false, timeFields: [], numberFields: ["cpu"] });
    expect(timeSeriesShape(frameOf([tf([T0])]))).toEqual({
      ok: false,
      timeFields: ["time"],
      numberFields: [],
    });
  });

  it(`draws at most ${String(MAX_SERIES)} series and counts the rest`, () => {
    const nums = Array.from({ length: 11 }, (_, i) => nf(`s${String(i)}`, [i]));
    const shape = timeSeriesShape(frameOf([tf([T0]), ...nums]));
    if (!shape.ok) throw new Error("expected ok");
    expect(shape.series).toHaveLength(MAX_SERIES);
    expect(shape.series[0]?.name).toBe("s0");
    expect(shape.omitted).toBe(3);
  });
});

describe("timeOrder (FR-014 AC3)", () => {
  it("skips null times and keeps sorted input as is", () => {
    expect(timeOrder(tf([T0, null, T0 + H]))).toEqual([0, 2]);
  });

  it("sorts unsorted times stably without mutating the field", () => {
    const values = [T0 + 2 * H, T0, T0 + H, T0];
    const field = tf(values);
    expect(timeOrder(field)).toEqual([1, 3, 2, 0]);
    expect(field.values).toEqual([T0 + 2 * H, T0, T0 + H, T0]);
  });
});

describe("niceYTicks (FR-014 AC5)", () => {
  it("starts at zero for non-negative data and uses round steps", () => {
    expect(niceYTicks(13.7, 66.2)).toEqual({ min: 0, max: 80, ticks: [0, 20, 40, 60, 80] });
  });

  it("spans negative data", () => {
    const y = niceYTicks(-7, 12);
    expect(y.min).toBeLessThanOrEqual(-7);
    expect(y.max).toBeGreaterThanOrEqual(12);
    expect(y.ticks).toContain(0);
  });

  it("pads a flat series", () => {
    const y = niceYTicks(5, 5);
    expect(y.min).toBe(0);
    expect(y.max).toBeGreaterThan(5);
    const neg = niceYTicks(-5, -5);
    expect(neg.min).toBeLessThan(-5);
    expect(neg.max).toBeGreaterThan(-5);
    expect(niceYTicks(0, 0).max).toBeGreaterThan(0);
  });

  it("falls back for non-finite input", () => {
    expect(niceYTicks(Number.NaN, 1)).toEqual({ min: 0, max: 1, ticks: [0, 1] });
  });

  it("keeps decimal steps exact", () => {
    expect(niceYTicks(0.1, 0.35).ticks).toEqual([0, 0.1, 0.2, 0.3, 0.4]);
  });
});

describe("timeTicks (FR-014 AC5)", () => {
  it("uses round hours for a day", () => {
    const { ticks, step } = timeTicks(T0, T0 + 23 * H, 6);
    expect(step).toBe(6 * H);
    expect(ticks).toEqual([T0, T0 + 6 * H, T0 + 12 * H, T0 + 18 * H]);
  });

  it("uses seconds for short ranges", () => {
    const { ticks, step } = timeTicks(T0 + 500, T0 + 4_200, 4);
    expect(step).toBe(1000);
    expect(ticks[0]).toBe(T0 + 1000);
  });

  it("uses calendar months for long ranges", () => {
    const { ticks } = timeTicks(Date.UTC(2025, 0, 15), Date.UTC(2026, 0, 15), 4);
    expect(ticks.map((v) => new Date(v).toISOString().slice(0, 10))).toEqual([
      "2025-04-01",
      "2025-07-01",
      "2025-10-01",
      "2026-01-01",
    ]);
  });

  it("handles a single instant", () => {
    expect(timeTicks(T0, T0).ticks).toEqual([T0]);
  });
});

describe("linear", () => {
  it("maps, rounds to 0.1 px and clamps", () => {
    const s = linear(0, 10, 0, 100);
    expect(s(5)).toBe(50);
    expect(s(1 / 3)).toBe(3.3);
    expect(s(1e12)).toBe(1e6);
  });

  it("puts a zero-width domain mid-range", () => {
    expect(linear(5, 5, 0, 100)(5)).toBe(50);
  });
});

describe("segments (FR-014 AC3, AC8)", () => {
  it("breaks lines at null values instead of drawing zero", () => {
    const times = [0, 1, 2, 3, 4, 5];
    const values = [1, 2, null, null, 5, 6];
    const segs = segments([0, 1, 2, 3, 4, 5], times, values, id, id, 100);
    expect(segs).toEqual([
      [
        [0, 1],
        [1, 2],
      ],
      [
        [4, 5],
        [5, 6],
      ],
    ]);
  });

  it("keeps a lone point as its own segment", () => {
    expect(segments([0, 1, 2], [0, 1, 2], [null, 7, null], id, id, 100)).toEqual([[[1, 7]]]);
  });

  it("decimates to first, min, max and last per pixel column and keeps spikes", () => {
    const n = 100_000;
    const times = Array.from({ length: n }, (_, i) => i);
    const values = Array.from({ length: n }, (_, i) => (i === 54_321 ? 1000 : i % 7));
    const order = times.slice();
    const width = 500;
    const x = linear(0, n - 1, 0, width);
    const started = performance.now();
    const segs = segments(order, times, values, x, id, width);
    const elapsed = performance.now() - started;
    const seg = segs[0] ?? [];
    expect(segs).toHaveLength(1);
    expect(seg.length).toBeLessThanOrEqual(4 * (width + 1));
    expect(seg.some(([, v]) => v === 1000)).toBe(true);
    expect(seg[0]).toEqual([0, 0]);
    expect(seg[seg.length - 1]?.[0]).toBe(width);
    // NFR-PERF-005 AC3: generous bound for slow CI runners.
    expect(elapsed).toBeLessThan(500);
    for (let i = 1; i < seg.length; i++) {
      expect((seg[i]?.[0] ?? 0) >= (seg[i - 1]?.[0] ?? 0)).toBe(true);
    }
  });

  it("leaves small segments untouched", () => {
    const segs = segments([0, 1, 2], [0, 0.2, 0.4], [3, 1, 2], id, id, 100);
    expect(segs[0]).toHaveLength(3);
  });
});

describe("pathData", () => {
  it("builds move/line commands", () => {
    expect(
      pathData([
        [1, 2],
        [3.5, 4],
      ]),
    ).toBe("M1 2L3.5 4");
  });
});

describe("nearest (FR-014 AC7)", () => {
  const times = [0, 10, 20, null, 30];
  const order = [0, 1, 2, 4];
  it("finds the closest time, ties to the earlier", () => {
    expect(nearest(order, times, -5)).toBe(0);
    expect(nearest(order, times, 14)).toBe(1);
    expect(nearest(order, times, 15)).toBe(1);
    expect(nearest(order, times, 16)).toBe(2);
    expect(nearest(order, times, 99)).toBe(3);
    expect(nearest([], times, 1)).toBe(-1);
  });
});

describe("extent", () => {
  it("ignores nulls and returns null when nothing is left", () => {
    expect(extent([0, 1, 2], [nf("a", [3, null, -1]), nf("b", [null, 9, null])])).toEqual([-1, 9]);
    expect(extent([0], [nf("a", [null])])).toBeNull();
  });
});

describe("dashFor (FR-014 AC4)", () => {
  it("keeps slots 1–4 solid and dashes the rest", () => {
    expect([0, 1, 2, 3].map(dashFor)).toEqual([undefined, undefined, undefined, undefined]);
    expect(new Set([4, 5, 6, 7].map(dashFor)).size).toBe(4);
  });
});
