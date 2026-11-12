import { useSyncExternalStore } from "react";
import { PREF_KEYS, readRaw, subscribePrefs, writeRaw } from "./prefs";

/**
 * Explore query history, per browser (FR-018 AC9). It holds what the Explore URL already
 * holds (data source, path, format, limits) and never results or credentials. Entries read
 * back from storage are validated field by field (NFR-SEC-019 AC2).
 */
export interface HistoryEntry {
  ds: string;
  path: string;
  format?: string;
  maxRows?: number;
  rowsPath?: string;
  flatten?: boolean;
  /** When the query ran, epoch milliseconds. */
  at: number;
}

export type HistoryQuery = Omit<HistoryEntry, "at">;

export const HISTORY_LIMIT = 20;
/** A stored document larger than this is ignored outright. */
export const HISTORY_MAX_BYTES = 64 * 1024;

const LIMITS = { ds: 64, path: 1024, format: 32, rowsPath: 2063, maxRows: 100_000 } as const;

function text(v: unknown, max: number): string | null {
  return typeof v === "string" && v.length > 0 && v.length <= max ? v : null;
}

/** Validates one stored entry; returns `null` for anything that is not a well-formed query. */
export function parseEntry(v: unknown): HistoryEntry | null {
  if (typeof v !== "object" || v === null || Array.isArray(v)) return null;
  const o = v as Record<string, unknown>;
  const ds = text(o.ds, LIMITS.ds);
  const path = text(o.path, LIMITS.path);
  if (ds === null || path === null) return null;
  if (typeof o.at !== "number" || !Number.isSafeInteger(o.at) || o.at < 0) return null;
  const entry: HistoryEntry = { ds, path, at: o.at };
  if (o.format !== undefined) {
    const format = text(o.format, LIMITS.format);
    if (format === null) return null;
    entry.format = format;
  }
  if (o.maxRows !== undefined) {
    const n = o.maxRows;
    if (typeof n !== "number" || !Number.isInteger(n) || n < 1 || n > LIMITS.maxRows) return null;
    entry.maxRows = n;
  }
  if (o.rowsPath !== undefined) {
    const rowsPath = text(o.rowsPath, LIMITS.rowsPath);
    if (rowsPath === null) return null;
    entry.rowsPath = rowsPath;
  }
  if (o.flatten !== undefined) {
    if (typeof o.flatten !== "boolean") return null;
    if (o.flatten) entry.flatten = true;
  }
  return entry;
}

/** Parses the stored document: invalid entries are dropped, the rest capped at the limit. */
export function parseHistory(raw: string | null): HistoryEntry[] {
  if (raw === null || raw.length > HISTORY_MAX_BYTES) return [];
  let doc: unknown;
  try {
    doc = JSON.parse(raw);
  } catch {
    return [];
  }
  if (!Array.isArray(doc)) return [];
  const out: HistoryEntry[] = [];
  for (const item of doc) {
    const entry = parseEntry(item);
    if (entry && !out.some((e) => sameQuery(e, entry))) out.push(entry);
    if (out.length === HISTORY_LIMIT) break;
  }
  return out;
}

/** Two entries are the same query when every field except the time matches. */
export function sameQuery(a: HistoryQuery, b: HistoryQuery): boolean {
  return (
    a.ds === b.ds &&
    a.path === b.path &&
    a.format === b.format &&
    a.maxRows === b.maxRows &&
    a.rowsPath === b.rowsPath &&
    (a.flatten ?? false) === (b.flatten ?? false)
  );
}

// useSyncExternalStore needs a stable snapshot: re-parse only when the stored text changes.
let cache: { raw: string | null; entries: HistoryEntry[] } = { raw: null, entries: [] };

export function readHistory(): HistoryEntry[] {
  const raw = readRaw(PREF_KEYS.history);
  if (raw !== cache.raw) cache = { raw, entries: parseHistory(raw) };
  return cache.entries;
}

/** Puts the query at the head of the list, replacing an identical older entry. */
export function recordQuery(query: HistoryQuery, now: number = Date.now()): void {
  const entry = parseEntry({ ...query, at: Math.max(0, Math.round(now)) });
  if (!entry) return;
  const rest = readHistory().filter((e) => !sameQuery(e, entry));
  writeRaw(PREF_KEYS.history, JSON.stringify([entry, ...rest].slice(0, HISTORY_LIMIT)));
}

export function clearHistory(): void {
  writeRaw(PREF_KEYS.history, null);
}

/** The Explore query string for an entry: the same keys the page keeps in the URL (AC6). */
export function historySearch(entry: HistoryQuery): string {
  const p = new URLSearchParams({ ds: entry.ds, path: entry.path });
  if (entry.format !== undefined) p.set("format", entry.format);
  if (entry.maxRows !== undefined) p.set("maxRows", String(entry.maxRows));
  if (entry.rowsPath !== undefined) p.set("rowsPath", entry.rowsPath);
  if (entry.flatten === true) p.set("flatten", "1");
  return p.toString();
}

export function useQueryHistory(): HistoryEntry[] {
  return useSyncExternalStore(subscribePrefs, readHistory);
}

let restoreSeq = 0;

/**
 * A fresh token for `location.state.restore`: Explore re-reads the URL into its form when
 * it sees a new one, so opening a history entry works while Explore is already mounted.
 */
export function restoreToken(): string {
  restoreSeq += 1;
  return `r${String(restoreSeq)}`;
}
