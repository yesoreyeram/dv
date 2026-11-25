import { queryOptions } from "@tanstack/react-query";
import { decodeFrame, type DataFrame } from "@/lib/frame";

export interface BuildInfo {
  version: string;
  commit: string;
  buildDate: string;
  goVersion: string;
}

export interface Readiness {
  status: "ready" | "not_ready";
  checks: Record<string, "pass" | "fail">;
}

export interface OpenAPIOperation {
  summary?: string;
  operationId?: string;
}

export interface OpenAPIDocument {
  openapi: string;
  info: { title: string; version: string };
  paths: Record<string, Record<string, OpenAPIOperation>>;
}

/** Public metadata of a data-source plugin type (FR-015). */
export interface SourcePlugin {
  id: string;
  name: string;
  description: string;
  capabilities: string[];
}

/** Public metadata of a format plugin (FR-015). */
export interface FormatPlugin {
  id: string;
  name: string;
  description: string;
  mediaTypes: string[];
  extensions: string[];
}

export interface PluginCatalog {
  sources: SourcePlugin[];
  formats: FormatPlugin[];
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

/**
 * Fetches JSON from the same-origin API. `acceptStatuses` lets callers treat non-2xx
 * statuses that still carry a meaningful body (e.g. 503 from /readyz) as data.
 */
export async function fetchJSON<T>(
  path: string,
  init: { signal?: AbortSignal; acceptStatuses?: number[] } = {},
): Promise<T> {
  const res = await fetch(path, {
    headers: { Accept: "application/json" },
    credentials: "same-origin",
    ...(init.signal ? { signal: init.signal } : {}),
  });
  if (!res.ok && !(init.acceptStatuses ?? []).includes(res.status)) {
    throw new ApiError(res.status, `GET ${path} failed with status ${String(res.status)}`);
  }
  return (await res.json()) as T;
}

export const versionQuery = queryOptions({
  queryKey: ["system", "version"],
  queryFn: ({ signal }) => fetchJSON<BuildInfo>("/api/v1/version", { signal }),
  staleTime: 5 * 60_000,
});

/**
 * Normalises a `/readyz` body. The UI must not break on a body it does not expect (a proxy
 * error page that happens to be JSON, an older server): anything but `status: "ready"`
 * reads as not ready, and only `pass`/`fail` checks are kept.
 */
export function readReadiness(body: unknown): Readiness {
  const o = typeof body === "object" && body !== null ? (body as Record<string, unknown>) : {};
  const checks: Readiness["checks"] = {};
  if (typeof o.checks === "object" && o.checks !== null && !Array.isArray(o.checks)) {
    for (const [name, result] of Object.entries(o.checks as Record<string, unknown>)) {
      if (result === "pass" || result === "fail") checks[name] = result;
    }
  }
  return { status: o.status === "ready" ? "ready" : "not_ready", checks };
}

export const readinessQuery = queryOptions({
  queryKey: ["system", "readiness"],
  queryFn: async ({ signal }) =>
    readReadiness(await fetchJSON<unknown>("/readyz", { signal, acceptStatuses: [503] })),
  refetchInterval: 15_000,
});

export const pluginsQuery = queryOptions({
  queryKey: ["plugins", "catalog"],
  queryFn: ({ signal }) => fetchJSON<PluginCatalog>("/api/v1/plugins", { signal }),
  staleTime: 5 * 60_000,
});

export const openapiQuery = queryOptions({
  queryKey: ["meta", "openapi"],
  queryFn: ({ signal }) => fetchJSON<OpenAPIDocument>("/api/openapi.json", { signal }),
  staleTime: Infinity,
});

const METHODS = ["get", "put", "post", "delete", "patch", "head", "options"] as const;

export interface Endpoint {
  method: string;
  path: string;
  summary: string;
}

/** Flattens an OpenAPI document into a sorted endpoint list (FR-007). */
export function listEndpoints(doc: OpenAPIDocument): Endpoint[] {
  const out: Endpoint[] = [];
  for (const [path, ops] of Object.entries(doc.paths)) {
    for (const m of METHODS) {
      const op = ops[m];
      if (op) out.push({ method: m.toUpperCase(), path, summary: op.summary ?? "" });
    }
  }
  return out.sort((a, b) => a.path.localeCompare(b.path) || a.method.localeCompare(b.method));
}

/** A provisioned data-source instance, identity only (FR-017 AC3). */
export interface DataSourceSummary {
  uid: string;
  type: string;
  name: string;
}

/** `datasources: null` means the query API is disabled on this server (404, FR-016 AC7). */
export interface DataSourceList {
  datasources: DataSourceSummary[] | null;
}

export const dataSourcesQuery = queryOptions({
  queryKey: ["datasources", "list"],
  queryFn: async ({ signal }): Promise<DataSourceList> => {
    try {
      return await fetchJSON<{ datasources: DataSourceSummary[] }>("/api/v1/datasources", {
        signal,
      });
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) return { datasources: null };
      throw e;
    }
  },
  staleTime: 60_000,
});

export interface QueryError {
  code: string;
  message: string;
}

/** One query's outcome with frames already decoded and validated. */
export type QueryOutcome =
  | {
      ok: true;
      status: number;
      frames: DataFrame[];
      /** Server-side query time (FR-016 AC10); null when absent or invalid (FR-018 AC4). */
      durationMs: number | null;
    }
  | { ok: false; status: number; error: QueryError };

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function readError(v: unknown): QueryError | null {
  if (!isRecord(v)) return null;
  const { code, message } = v;
  if (typeof code !== "string") return null;
  return { code, message: typeof message === "string" ? message : "" };
}

/** A whole number of milliseconds ≥ 0, else null (FR-018 AC4: never shown as 0). */
function readDuration(v: unknown): number | null {
  return typeof v === "number" && Number.isSafeInteger(v) && v >= 0 ? v : null;
}

const DECODE_ERROR: QueryError = {
  code: "invalid_response",
  message: "The server response could not be read.",
};

/**
 * Interprets a POST /api/v1/query response for one refId (FR-016 AC3, FR-018 AC5):
 * a per-query result, or a request-level error envelope. Anything malformed becomes a
 * generic decode error rather than reaching panels.
 */
export function readQueryOutcome(httpStatus: number, body: unknown, refId: string): QueryOutcome {
  if (!isRecord(body)) return { ok: false, status: httpStatus, error: DECODE_ERROR };
  if (isRecord(body.results)) {
    const r = body.results[refId];
    if (!isRecord(r) || typeof r.status !== "number") {
      return { ok: false, status: httpStatus, error: DECODE_ERROR };
    }
    if (r.status >= 200 && r.status < 300) {
      try {
        const frames = Array.isArray(r.frames) ? r.frames.map(decodeFrame) : [];
        return { ok: true, status: r.status, frames, durationMs: readDuration(r.durationMs) };
      } catch {
        return { ok: false, status: r.status, error: DECODE_ERROR };
      }
    }
    return { ok: false, status: r.status, error: readError(r.error) ?? DECODE_ERROR };
  }
  return { ok: false, status: httpStatus, error: readError(body.error) ?? DECODE_ERROR };
}

export interface QuerySpec {
  refId: string;
  datasource: string;
  query: Record<string, unknown>;
  maxRows?: number;
}

/** Runs one query through the same-origin query API (FR-016, FR-018 AC3). */
export async function runQuery(spec: QuerySpec, signal?: AbortSignal): Promise<QueryOutcome> {
  const body = {
    queries: [
      {
        refId: spec.refId,
        datasource: { uid: spec.datasource },
        query: spec.query,
        ...(spec.maxRows === undefined ? {} : { maxRows: spec.maxRows }),
      },
    ],
  };
  const res = await fetch("/api/v1/query", {
    method: "POST",
    headers: { Accept: "application/json", "Content-Type": "application/json" },
    credentials: "same-origin",
    body: JSON.stringify(body),
    ...(signal ? { signal } : {}),
  });
  let parsed: unknown;
  try {
    parsed = await res.json();
  } catch {
    return { ok: false, status: res.status, error: DECODE_ERROR };
  }
  return readQueryOutcome(res.status, parsed, spec.refId);
}
