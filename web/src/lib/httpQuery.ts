/**
 * Builds the query body for the generic HTTP connector (FR-040) from the Explore form:
 * method, path and jq rows selector come from their own fields; parameters, headers,
 * body, pagination, columns, flatten and fieldTypes come from an optional JSON object.
 * The server validates everything again; this only catches mistakes early.
 */

export const HTTP_OPTION_KEYS = [
  "params",
  "headers",
  "body",
  "pagination",
  "columns",
  "flatten",
  "fieldTypes",
] as const;

/** Bounds mirror the server (FR-040 AC2, FR-073 AC5). */
export const MAX_HTTP_PATH = 2048;
export const MAX_SELECTOR = 4096;
export const MAX_OPTIONS = 64 * 1024;

export interface HttpForm {
  method: string;
  path: string;
  rows: string;
  options: string;
}

export type HttpQueryError = "path" | "options" | "rows";

export type HttpQueryResult =
  { ok: true; query: Record<string, unknown> } | { ok: false; error: HttpQueryError };

function isPlainObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** Validates the form and returns the query body, or which field is wrong. */
export function buildHttpQuery(form: HttpForm): HttpQueryResult {
  const path = form.path.trim();
  if (!path.startsWith("/") || path.startsWith("//") || path.length > MAX_HTTP_PATH) {
    return { ok: false, error: "path" };
  }
  const rows = form.rows.trim();
  if (rows.length > MAX_SELECTOR) return { ok: false, error: "rows" };
  const query: Record<string, unknown> = { path };
  if (form.method === "POST") query.method = "POST";
  if (rows !== "") query.rows = rows;
  const raw = form.options.trim();
  if (raw !== "") {
    if (raw.length > MAX_OPTIONS) return { ok: false, error: "options" };
    let parsed: unknown;
    try {
      parsed = JSON.parse(raw);
    } catch {
      return { ok: false, error: "options" };
    }
    if (!isPlainObject(parsed)) return { ok: false, error: "options" };
    for (const [k, v] of Object.entries(parsed)) {
      if (!(HTTP_OPTION_KEYS as readonly string[]).includes(k))
        return { ok: false, error: "options" };
      query[k] = v;
    }
  }
  return { ok: true, query };
}
