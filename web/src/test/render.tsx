import { QueryClient } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { vi } from "vitest";
import { App } from "@/App";

export type Responder = (
  path: string,
  init?: RequestInit,
) => { status: number; body: unknown } | Error;

export const fixtures = {
  version: { version: "1.2.3", commit: "abc1234", buildDate: "2026-11-07", goVersion: "go1.24.7" },
  ready: { status: "ready", checks: {} },
  plugins: { sources: [], formats: [] },
  openapi: {
    openapi: "3.1.0",
    info: { title: "dv API", version: "0.1.0" },
    paths: {
      "/readyz": { get: { summary: "Readiness probe" } },
      "/healthz": { get: { summary: "Liveness probe" } },
      "/api/v1/version": { get: { summary: "Build information" } },
      "/api/v1/items": { post: {}, get: { summary: "List" } },
    },
  },
};

export const defaultResponder: Responder = (path) => {
  switch (path) {
    case "/api/v1/version":
      return { status: 200, body: fixtures.version };
    case "/readyz":
      return { status: 200, body: fixtures.ready };
    case "/api/openapi.json":
      return { status: 200, body: fixtures.openapi };
    case "/api/v1/plugins":
      return { status: 200, body: fixtures.plugins };
    default:
      return { status: 404, body: { error: { code: "not_found" } } };
  }
};

export function mockFetch(responder: Responder = defaultResponder) {
  const fn = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const path =
      typeof input === "string" ? input : input instanceof URL ? input.pathname : input.url;
    if (init?.signal?.aborted) {
      return Promise.reject(new DOMException("aborted", "AbortError"));
    }
    const r = responder(path, init);
    if (r instanceof Error) return Promise.reject(r);
    return Promise.resolve(
      new Response(JSON.stringify(r.body), {
        status: r.status,
        headers: { "Content-Type": "application/json" },
      }),
    );
  });
  vi.stubGlobal("fetch", fn);
  return fn;
}

export function renderApp(path = "/", responder?: Responder) {
  const fetchMock = mockFetch(responder);
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const utils = render(
    <MemoryRouter initialEntries={[path]}>
      <App queryClient={queryClient} />
    </MemoryRouter>,
  );
  return { ...utils, fetchMock, queryClient };
}
