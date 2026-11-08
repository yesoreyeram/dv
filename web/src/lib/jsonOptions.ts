/**
 * JSON/NDJSON format options in Explore (FR-012 AC2, AC5, AC9). The server validates the
 * same rules; checking here gives inline feedback without a round trip.
 */
import type { FormatPlugin } from "@/lib/api";

const SEGMENT = "[A-Za-z0-9_-]{1,128}";
const ROWS_PATH = new RegExp(`^${SEGMENT}(\\.${SEGMENT}){0,15}$`);

/** Reports whether a rows path follows FR-012 AC2 (plain dot-separated keys). */
export function isValidRowsPath(p: string): boolean {
  return ROWS_PATH.test(p);
}

/** The format a query will use: the explicit choice, else the one matching the extension. */
export function effectiveFormat(
  explicit: string,
  path: string,
  formats: readonly Pick<FormatPlugin, "id" | "extensions">[],
): string | undefined {
  if (explicit) return explicit;
  const p = path.trim().toLowerCase();
  return formats.find((f) => f.extensions.some((ext) => p.endsWith(ext.toLowerCase())))?.id;
}

export interface JsonOptionState {
  rowsPath: string;
  flatten: boolean;
}

/** Which JSON controls Explore shows for a format. */
export function jsonControls(format: string | undefined): { rowsPath: boolean; flatten: boolean } {
  return { rowsPath: format === "json", flatten: format === "json" || format === "ndjson" };
}

/** The `query.options` object to send, or undefined when nothing applies. */
export function jsonQueryOptions(
  format: string | undefined,
  s: JsonOptionState,
): Record<string, unknown> | undefined {
  const c = jsonControls(format);
  const out: Record<string, unknown> = {};
  const rp = s.rowsPath.trim();
  if (c.rowsPath && rp !== "") out.rowsPath = rp;
  if (c.flatten && s.flatten) out.flatten = true;
  return Object.keys(out).length > 0 ? out : undefined;
}
