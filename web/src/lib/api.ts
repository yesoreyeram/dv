import { queryOptions } from "@tanstack/react-query";

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

export const readinessQuery = queryOptions({
  queryKey: ["system", "readiness"],
  queryFn: ({ signal }) => fetchJSON<Readiness>("/readyz", { signal, acceptStatuses: [503] }),
  refetchInterval: 15_000,
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
