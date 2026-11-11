/**
 * Pure geometry for the time-series panel (FR-014). No DOM access, so it is unit tested
 * directly and stays cheap for large frames (NFR-PERF-005 AC3).
 */
import type { DataFrame, Field } from "@/lib/frame";

/** Reads an index that is in range by construction (`noUncheckedIndexedAccess`). */
export function at(a: readonly (number | null | undefined)[], i: number): number {
  return a[i] ?? 0;
}

/** Most series drawn at once (FR-014 AC4); further number fields are listed in a notice. */
export const MAX_SERIES = 8;

export type TimeField = Extract<Field, { type: "time" }>;
export type NumberField = Extract<Field, { type: "number" }>;

export type Shape =
  | { ok: true; time: TimeField; series: NumberField[]; omitted: number }
  | { ok: false; timeFields: string[]; numberFields: string[] };

const isTime = (f: Field): f is TimeField => f.type === "time";
const isNumber = (f: Field): f is NumberField => f.type === "number";

/** Picks the x field and the series of a frame, or explains why it cannot be drawn (AC2). */
export function timeSeriesShape(frame: DataFrame): Shape {
  const times = frame.fields.filter(isTime);
  const numbers = frame.fields.filter(isNumber);
  const time = times[0];
  if (!time || numbers.length === 0) {
    return {
      ok: false,
      timeFields: times.map((f) => f.name),
      numberFields: numbers.map((f) => f.name),
    };
  }
  return {
    ok: true,
    time,
    series: numbers.slice(0, MAX_SERIES),
    omitted: Math.max(0, numbers.length - MAX_SERIES),
  };
}

/** Row indices with a non-null time, in ascending time order (stable; frame untouched). */
export function timeOrder(time: TimeField): number[] {
  const idx: number[] = [];
  const v = time.values;
  for (let i = 0; i < v.length; i++) if (v[i] !== null && v[i] !== undefined) idx.push(i);
  let sorted = true;
  for (let i = 1; i < idx.length && sorted; i++) {
    sorted = at(v, at(idx, i - 1)) <= at(v, at(idx, i));
  }
  if (!sorted) idx.sort((a, b) => at(v, a) - at(v, b) || a - b);
  return idx;
}

export interface Ticks {
  min: number;
  max: number;
  ticks: number[];
}

function niceStep(raw: number): number {
  const exp = Math.floor(Math.log10(raw));
  const base = 10 ** exp;
  const f = raw / base;
  const nice = f <= 1 ? 1 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 5 ? 5 : 10;
  return nice * base;
}

/**
 * Round y ticks (AC5). Includes zero when every value is ≥ 0; a flat range is padded so
 * the line sits mid-chart instead of on an axis.
 */
export function niceYTicks(lo: number, hi: number, count = 5): Ticks {
  if (!Number.isFinite(lo) || !Number.isFinite(hi)) return { min: 0, max: 1, ticks: [0, 1] };
  const flat = hi === lo;
  if (lo >= 0) lo = 0;
  if (flat || hi === lo) {
    const pad = Math.abs(hi) > 0 ? Math.abs(hi) * 0.1 : 1;
    hi += pad;
    if (lo !== 0) lo -= pad;
  }
  const step = niceStep((hi - lo) / Math.max(1, count));
  const min = Math.floor(lo / step) * step;
  const max = Math.ceil(hi / step) * step;
  const ticks: number[] = [];
  for (let v = min, i = 0; v <= max + step / 2 && i < 50; v += step, i++) {
    ticks.push(Number(v.toPrecision(12)));
  }
  return { min, max, ticks };
}

const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const FIXED_STEPS = [
  SECOND,
  5 * SECOND,
  15 * SECOND,
  30 * SECOND,
  MINUTE,
  5 * MINUTE,
  15 * MINUTE,
  30 * MINUTE,
  HOUR,
  3 * HOUR,
  6 * HOUR,
  12 * HOUR,
  DAY,
  2 * DAY,
  7 * DAY,
];
/** Mean Gregorian month, for choosing a calendar step. */
const MONTH = 30.44 * DAY;
const MONTH_STEPS = [1, 3, 6, 12, 24, 60, 120];

export interface TimeTicks {
  ticks: number[];
  /** Step in ms (fixed steps) or months × a mean month (calendar steps), for label choice. */
  step: number;
}

/** Round time ticks between `lo` and `hi` (epoch ms, UTC) for about `count` labels (AC5). */
export function timeTicks(lo: number, hi: number, count = 6): TimeTicks {
  if (!(hi > lo)) return { ticks: [lo], step: SECOND };
  const target = (hi - lo) / Math.max(1, count);
  const fixed = FIXED_STEPS.find((s) => s >= target);
  if (fixed !== undefined) {
    const ticks: number[] = [];
    for (let v = Math.ceil(lo / fixed) * fixed; v <= hi; v += fixed) ticks.push(v);
    return { ticks, step: fixed };
  }
  const months = MONTH_STEPS.find((m) => m * MONTH >= target) ?? 120;
  const d = new Date(lo);
  // Absolute month index, rounded up to the first month boundary at or after `lo`, then
  // to a multiple of the step so ticks land on round months (Jan/Apr/Jul/Oct for 3).
  let idx = d.getUTCFullYear() * 12 + d.getUTCMonth();
  if (Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), 1) < lo) idx += 1;
  idx = Math.ceil(idx / months) * months;
  const ticks: number[] = [];
  for (let v = Date.UTC(Math.floor(idx / 12), idx % 12, 1); v <= hi && ticks.length < 200;) {
    ticks.push(v);
    idx += months;
    v = Date.UTC(Math.floor(idx / 12), idx % 12, 1);
  }
  return { ticks, step: months * MONTH };
}

export type Scale = (v: number) => number;

/** Linear scale from [d0, d1] to [r0, r1]; output rounded to 0.1 px, clamped to ±1e6. */
export function linear(d0: number, d1: number, r0: number, r1: number): Scale {
  const k = d1 === d0 ? 0 : (r1 - r0) / (d1 - d0);
  const mid = (r0 + r1) / 2;
  return (v: number) => {
    const out = k === 0 ? mid : r0 + (v - d0) * k;
    return Math.round(Math.max(-1e6, Math.min(1e6, out)) * 10) / 10;
  };
}

export type Point = readonly [x: number, y: number];

/**
 * Splits a series into gap-free segments of screen points (AC3) and, when a segment has
 * more than two points per pixel column, keeps only the first, min, max and last point of
 * each column (AC8), so spikes stay visible and the path stays small.
 */
export function segments(
  order: readonly number[],
  times: readonly (number | null)[],
  values: readonly (number | null)[],
  x: Scale,
  y: Scale,
  plotWidth: number,
): Point[][] {
  const out: Point[][] = [];
  let cur: Point[] = [];
  for (const row of order) {
    const v = values[row];
    if (v === null || v === undefined) {
      if (cur.length) out.push(cur);
      cur = [];
      continue;
    }
    cur.push([x(at(times, row)), y(v)]);
  }
  if (cur.length) out.push(cur);
  const budget = Math.max(2, 2 * plotWidth);
  return out.map((seg) => (seg.length > budget ? decimate(seg) : seg));
}

function decimate(seg: Point[]): Point[] {
  const out: Point[] = [];
  let i = 0;
  while (i < seg.length) {
    const col = Math.floor(seg[i]?.[0] ?? 0);
    let lo = i;
    let hi = i;
    let j = i + 1;
    for (; j < seg.length; j++) {
      const p = seg[j];
      if (!p || Math.floor(p[0]) !== col) break;
      if (p[1] < (seg[lo]?.[1] ?? 0)) lo = j;
      if (p[1] > (seg[hi]?.[1] ?? 0)) hi = j;
    }
    // First, min, max and last of the column, in their original order, without repeats.
    const keep = [...new Set([i, lo, hi, j - 1])].sort((a, b) => a - b);
    for (const k of keep) {
      const p = seg[k];
      if (p) out.push(p);
    }
    i = j;
  }
  return out;
}

/** SVG path data for one segment (`M x y L x y …`). */
export function pathData(seg: readonly Point[]): string {
  let d = "";
  seg.forEach(([px, py], i) => {
    d += `${i === 0 ? "M" : "L"}${String(px)} ${String(py)}`;
  });
  return d;
}

/** Index into `order` of the row whose time is nearest to `t` (binary search). */
export function nearest(
  order: readonly number[],
  times: readonly (number | null)[],
  t: number,
): number {
  let lo = 0;
  let hi = order.length - 1;
  if (hi < 0) return -1;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (at(times, at(order, mid)) < t) lo = mid + 1;
    else hi = mid;
  }
  if (lo > 0) {
    const a = at(times, at(order, lo - 1));
    const b = at(times, at(order, lo));
    if (t - a <= b - t) return lo - 1;
  }
  return lo;
}

/** Min and max of the non-null values of the given series over the given rows. */
export function extent(
  order: readonly number[],
  series: readonly NumberField[],
): [number, number] | null {
  let lo = Infinity;
  let hi = -Infinity;
  for (const f of series) {
    for (const row of order) {
      const v = f.values[row];
      if (v === null || v === undefined) continue;
      if (v < lo) lo = v;
      if (v > hi) hi = v;
    }
  }
  return lo <= hi ? [lo, hi] : null;
}

const DASHES: readonly (string | undefined)[] = [
  undefined,
  undefined,
  undefined,
  undefined,
  "6 3",
  "2 3",
  "8 3 2 3",
  "1 2",
];

/** Dash pattern per colour slot: solid for 1–4, dashed from slot 5 on (AC4, WCAG 1.4.1). */
export function dashFor(slot: number): string | undefined {
  return DASHES[slot];
}
