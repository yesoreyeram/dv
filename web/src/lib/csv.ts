/**
 * CSV export of a data frame (FR-018 AC7) that is safe to open in spreadsheet apps
 * (NFR-SEC-018, OWASP "CSV Injection", CWE-1236).
 */
import type { DataFrame, Field } from "@/lib/frame";

/** First characters that make Excel, LibreOffice or Google Sheets evaluate a cell. */
const FORMULA_TRIGGERS = new Set(["=", "+", "-", "@", "\t", "\r"]);

/**
 * Prefixes text that a spreadsheet app would evaluate as a formula with `'`
 * (NFR-SEC-018 AC1). Leading spaces are skipped when looking for the trigger, because
 * some apps trim them before parsing.
 */
export function neutraliseFormula(text: string): string {
  const first = text.replace(/^ +/, "").charAt(0);
  return FORMULA_TRIGGERS.has(first) ? `'${text}` : text;
}

/** Quotes a cell when RFC 4180 requires it (NFR-SEC-018 AC3). */
export function quoteCell(text: string): string {
  return /[",\r\n]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text;
}

function compactJSON(v: unknown): string {
  try {
    const out = JSON.stringify(v) as string | undefined;
    return out ?? "";
  } catch {
    return "";
  }
}

/** Raw, locale-independent text of one cell (FR-018 AC7); numbers are never prefixed. */
export function csvCell(field: Field, v: unknown): string {
  if (v === null || v === undefined) return "";
  switch (field.type) {
    case "number":
      // AC2: plain numbers, so "-3" stays a number; NaN/Infinity are written empty.
      return typeof v === "number" && Number.isFinite(v) ? String(v) : "";
    case "time":
      return typeof v === "number" && Number.isFinite(v) ? new Date(v).toISOString() : "";
    case "bool":
      return v === true ? "true" : "false";
    case "json":
      return quoteCell(neutraliseFormula(compactJSON(v)));
    case "string":
      return quoteCell(neutraliseFormula(typeof v === "string" ? v : compactJSON(v)));
  }
}

/** UTF-8 byte order mark, so spreadsheet apps detect the encoding. */
export const BOM = "﻿";

/**
 * Serialises every row of `frame`, in `order` when given (the table's current sort),
 * with a header row of display names and CRLF line ends.
 */
export function frameToCSV(frame: DataFrame, order?: readonly number[] | null): string {
  const header = frame.fields
    .map((f) => quoteCell(neutraliseFormula(f.config.displayName ?? f.name)))
    .join(",");
  const lines = [header];
  for (let i = 0; i < frame.length; i++) {
    const r = order ? (order[i] ?? i) : i;
    lines.push(frame.fields.map((f) => csvCell(f, f.values[r])).join(","));
  }
  return BOM + lines.join("\r\n") + "\r\n";
}

const MAX_NAME = 100;

/** Download name: `[A-Za-z0-9._-]` only, ≤ 100 characters, ends in `.csv` once. */
export function csvFileName(frameName: string): string {
  let base = frameName.replace(/[^A-Za-z0-9._-]/g, "_").replace(/^[.]+/, "");
  if (/\.csv$/i.test(base)) base = base.slice(0, -4);
  base = base.slice(0, MAX_NAME - 4);
  if (base.replace(/[_.-]/g, "") === "") base = "frame";
  return `${base}.csv`;
}

/** Saves `frame` as a CSV file through a temporary object URL (no server round trip). */
export function downloadFrameCSV(frame: DataFrame, order?: readonly number[] | null): void {
  const blob = new Blob([frameToCSV(frame, order)], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  try {
    const a = document.createElement("a");
    a.href = url;
    a.download = csvFileName(frame.name);
    a.rel = "noopener";
    document.body.append(a);
    a.click();
    a.remove();
  } finally {
    // Revoke after the click has been dispatched; the download keeps its own reference.
    setTimeout(() => {
      URL.revokeObjectURL(url);
    }, 0);
  }
}
