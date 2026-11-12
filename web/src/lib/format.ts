/**
 * Locale-aware cell formatting and sorting for panels (FR-013 AC2/AC3, NFR-I18N-002).
 * Formatters are cached per locale and option set because constructing `Intl` objects is
 * comparatively expensive and tables format thousands of cells.
 */
import type { Field, FieldConfig } from "@/lib/frame";

const JSON_MAX = 200;

const numberFormats = new Map<string, Intl.NumberFormat>();
function numberFormat(locale: string | undefined, decimals: number | undefined) {
  const key = `${locale ?? ""}|${decimals === undefined ? "" : String(decimals)}`;
  let nf = numberFormats.get(key);
  if (!nf) {
    nf = new Intl.NumberFormat(
      locale,
      decimals === undefined
        ? { maximumFractionDigits: 6 }
        : { minimumFractionDigits: decimals, maximumFractionDigits: decimals },
    );
    numberFormats.set(key, nf);
  }
  return nf;
}

const dateFormats = new Map<string, Intl.DateTimeFormat>();
function dateFormat(locale: string | undefined, timeZone: string | undefined) {
  const key = `${locale ?? ""}|${timeZone ?? ""}`;
  let df = dateFormats.get(key);
  if (!df) {
    df = new Intl.DateTimeFormat(locale, {
      dateStyle: "medium",
      timeStyle: "medium",
      ...(timeZone ? { timeZone } : {}),
    });
    dateFormats.set(key, df);
  }
  return df;
}

export interface FormatOptions {
  locale?: string;
  timeZone?: string;
}

/** Formats a non-null number with the field config (decimals, unit). */
export function formatNumber(v: number, cfg: FieldConfig, opts: FormatOptions = {}): string {
  const s = numberFormat(opts.locale, cfg.decimals).format(v);
  return cfg.unit ? `${s} ${cfg.unit}` : s;
}

/** Formats epoch milliseconds (UTC) in the user's locale and time zone. */
export function formatTime(ms: number, opts: FormatOptions = {}): string {
  return dateFormat(opts.locale, opts.timeZone).format(ms);
}

/** Compact JSON, cut at 200 characters. */
export function formatJSON(v: unknown): string {
  let s: string;
  try {
    // JSON.stringify returns undefined for undefined/functions despite its declared type.
    const out = JSON.stringify(v) as string | undefined;
    s = out ?? String(v);
  } catch {
    s = String(v);
  }
  return s.length > JSON_MAX ? `${s.slice(0, JSON_MAX - 1)}…` : s;
}

/** Formats one non-null cell as display text. Null handling is the caller's job. */
export function formatCell(field: Field, v: unknown, opts: FormatOptions = {}): string {
  switch (field.type) {
    case "time":
      return formatTime(v as number, opts);
    case "number":
      return formatNumber(v as number, field.config, opts);
    case "bool":
      return v ? "true" : "false";
    case "string":
      return v as string;
    case "json":
      return formatJSON(v);
  }
}

export type SortDirection = "ascending" | "descending";

const collators = new Map<string, Intl.Collator>();
function collator(locale: string | undefined) {
  const key = locale ?? "";
  let c = collators.get(key);
  if (!c) {
    c = new Intl.Collator(locale, { numeric: true, sensitivity: "base" });
    collators.set(key, c);
  }
  return c;
}

/**
 * Returns row indexes ordered by one field. Stable, nulls last in both directions, and
 * the field values are never mutated (FR-013 AC3).
 */
export function sortedRowOrder(
  field: Field,
  direction: SortDirection,
  opts: FormatOptions = {},
): number[] {
  const values = field.values as unknown[];
  const order = values.map((_, i) => i);
  const sign = direction === "ascending" ? 1 : -1;
  let cmp: (a: unknown, b: unknown) => number;
  switch (field.type) {
    case "time":
    case "number":
      cmp = (a, b) => (a as number) - (b as number);
      break;
    case "bool":
      cmp = (a, b) => Number(a) - Number(b);
      break;
    case "string": {
      const c = collator(opts.locale);
      cmp = (a, b) => c.compare(a as string, b as string);
      break;
    }
    case "json": {
      const c = collator(opts.locale);
      cmp = (a, b) => c.compare(formatJSON(a), formatJSON(b));
      break;
    }
  }
  order.sort((i, j) => {
    const a = values[i];
    const b = values[j];
    if (a === null || b === null) {
      if (a === b) return i - j;
      return a === null ? 1 : -1;
    }
    return sign * cmp(a, b) || i - j;
  });
  return order;
}

/** Largest row limit a user may type (FR-018 AC2; the host limit still applies). */
export const MAX_ROW_LIMIT = 100_000;

/** Parses an optional row limit: "" → undefined, anything invalid → null (FR-018 AC2). */
export function parseRowLimit(raw: string): number | undefined | null {
  const s = raw.trim();
  if (s === "") return undefined;
  if (!/^\d+$/.test(s)) return null;
  const n = Number(s);
  return n >= 1 && n <= MAX_ROW_LIMIT ? n : null;
}

const RELATIVE_UNITS: readonly [Intl.RelativeTimeFormatUnit, number][] = [
  ["day", 86_400_000],
  ["hour", 3_600_000],
  ["minute", 60_000],
];

/** "5 minutes ago", "yesterday", "now": how long before `now` something happened. */
export function formatRelativeTime(at: number, now: number, opts: FormatOptions = {}): string {
  const rtf = new Intl.RelativeTimeFormat(opts.locale, { numeric: "auto" });
  const elapsed = Math.max(0, now - at);
  for (const [unit, ms] of RELATIVE_UNITS) {
    if (elapsed >= ms) return rtf.format(-Math.floor(elapsed / ms), unit);
  }
  return rtf.format(0, "second");
}
