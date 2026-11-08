import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { readQueryOutcome } from "@/lib/api";
import { defaultResponder, renderApp, type Responder } from "@/test/render";

const sources = {
  datasources: [
    { uid: "samples", type: "file", name: "Sample files" },
    { uid: "logs", type: "file", name: "Logs" },
  ],
};

const wireFrame = {
  schema: {
    name: "sales.csv",
    meta: { source: "file", executedQuery: "samples:sales.csv" },
    fields: [
      { name: "region", type: "string" },
      { name: "orders", type: "number" },
    ],
  },
  data: {
    values: [
      ["emea", "apac"],
      [128, 94],
    ],
  },
};

function responder(query: (body: unknown) => { status: number; body: unknown }): Responder {
  return (path, init) => {
    if (path === "/api/v1/datasources") return { status: 200, body: sources };
    if (path === "/api/v1/plugins") {
      return {
        status: 200,
        body: {
          sources: [],
          formats: [{ id: "csv", name: "CSV", description: "", mediaTypes: [], extensions: [] }],
        },
      };
    }
    if (path === "/api/v1/query") return query(JSON.parse(init?.body as string));
    return defaultResponder(path);
  };
}

const ok = () => ({ status: 200, body: { results: { A: { status: 200, frames: [wireFrame] } } } });

describe("Explore page (FR-018)", () => {
  it("runs a query and renders the frame in the table panel (AC1, AC3, AC4)", async () => {
    const user = userEvent.setup();
    const seen: unknown[] = [];
    const { fetchMock } = renderApp(
      "/explore",
      responder((b) => {
        seen.push(b);
        return ok();
      }),
    );
    const ds = await screen.findByRole("combobox", { name: "Data source" });
    expect(ds).toHaveValue("samples");
    await user.selectOptions(ds, "logs");
    await user.type(screen.getByRole("textbox", { name: "Path" }), "sales.csv");
    await user.selectOptions(screen.getByRole("combobox", { name: "Format" }), "csv");
    await user.type(screen.getByRole("textbox", { name: "Row limit" }), "10");
    await user.click(screen.getByRole("button", { name: "Run query" }));
    expect(await screen.findByRole("table", { name: "sales.csv, 2 rows" })).toBeInTheDocument();
    expect(screen.getByTestId("query-stats")).toHaveTextContent(
      /2 rows · 2 fields · \d+ ms · samples:sales\.csv/,
    );
    expect(seen).toEqual([
      {
        queries: [
          {
            refId: "A",
            datasource: { uid: "logs" },
            query: { path: "sales.csv", format: "csv" },
            maxRows: 10,
          },
        ],
      },
    ]);
    const call = fetchMock.mock.calls.find(([p]) => p === "/api/v1/query");
    const init = call?.[1];
    expect(init?.method).toBe("POST");
    expect(new Headers(init?.headers).get("Content-Type")).toBe("application/json");
    expect(init?.credentials).toBe("same-origin");
  });

  it("runs with Ctrl+Enter from the path field", async () => {
    const user = userEvent.setup();
    renderApp("/explore", responder(ok));
    const path = await screen.findByRole("textbox", { name: "Path" });
    await user.type(path, "sales.csv");
    await user.keyboard("{Control>}{Enter}{/Control}");
    expect(await screen.findByRole("table")).toBeInTheDocument();
  });

  it("validates inline and sends nothing when the form is invalid (AC2)", async () => {
    const user = userEvent.setup();
    const { fetchMock } = renderApp("/explore", responder(ok));
    await user.click(await screen.findByRole("button", { name: "Run query" }));
    const path = screen.getByRole("textbox", { name: "Path" });
    expect(path).toHaveAttribute("aria-invalid", "true");
    expect(path).toHaveAccessibleDescription("Enter a file path.");
    await user.type(path, "a.csv");
    await user.type(screen.getByRole("textbox", { name: "Row limit" }), "0");
    await user.click(screen.getByRole("button", { name: "Run query" }));
    expect(screen.getByRole("textbox", { name: "Row limit" })).toHaveAccessibleDescription(
      "Enter a whole number from 1 to 100000.",
    );
    expect(fetchMock.mock.calls.some(([p]) => p === "/api/v1/query")).toBe(false);
  });

  it("shows per-query and request-level errors as text (AC5)", async () => {
    const user = userEvent.setup();
    let next: { status: number; body: unknown } = {
      status: 404,
      body: {
        results: {
          A: { status: 404, error: { code: "not_found", message: "<b>file not found</b>" } },
        },
      },
    };
    renderApp(
      "/explore",
      responder(() => next),
    );
    await user.type(await screen.findByRole("textbox", { name: "Path" }), "missing.csv");
    await user.click(screen.getByRole("button", { name: "Run query" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("not_found — <b>file not found</b>");
    expect(alert.querySelector("b")).toBeNull();

    next = { status: 403, body: { error: { code: "host_not_allowed", message: "nope" } } };
    await user.click(screen.getByRole("button", { name: "Run query" }));
    await waitFor(() => {
      expect(screen.getByRole("alert")).toHaveTextContent("host_not_allowed");
    });
  });

  it("restores the form from the URL without running it, and ignores unknown sources (AC6)", async () => {
    const { fetchMock } = renderApp("/explore?ds=nope&path=hosts.tsv&maxRows=2", responder(ok));
    expect(await screen.findByRole("combobox", { name: "Data source" })).toHaveValue("samples");
    expect(screen.getByRole("textbox", { name: "Path" })).toHaveValue("hosts.tsv");
    expect(screen.getByRole("textbox", { name: "Row limit" })).toHaveValue("2");
    expect(screen.getByTestId("explore-empty")).toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([p]) => p === "/api/v1/query")).toBe(false);
  });

  it("explains how to enable the query API when it is off (AC1)", async () => {
    renderApp("/explore");
    const card = await screen.findByTestId("explore-disabled");
    expect(within(card).getByText("The query API is not enabled on this server")).toBeVisible();
    expect(card).toHaveTextContent("DV_QUERY_API_ENABLED");
    expect(screen.queryByRole("button", { name: "Run query" })).toBeNull();
  });

  it("says when no data sources are provisioned", async () => {
    renderApp("/explore", (p) =>
      p === "/api/v1/datasources"
        ? { status: 200, body: { datasources: [] } }
        : defaultResponder(p),
    );
    expect(
      await screen.findByText("No data sources are provisioned on this server."),
    ).toBeInTheDocument();
  });

  it("cancels a running query (AC3)", async () => {
    const user = userEvent.setup();
    let signal: AbortSignal | undefined;
    const { fetchMock } = renderApp("/explore", responder(ok));
    const base = fetchMock.getMockImplementation();
    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      if (input !== "/api/v1/query") return base ? base(input, init) : Promise.reject(new Error());
      signal = init?.signal ?? undefined;
      return new Promise<Response>((_, reject) => {
        signal?.addEventListener("abort", () => {
          reject(new DOMException("aborted", "AbortError"));
        });
      });
    });
    await user.type(await screen.findByRole("textbox", { name: "Path" }), "slow.csv");
    await user.click(screen.getByRole("button", { name: "Run query" }));
    expect(screen.getByRole("button", { name: "Running…" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(signal?.aborted).toBe(true);
    expect(await screen.findByText("The query was cancelled.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Run query" })).toBeEnabled();
  });

  it("shows a frame tab per frame", async () => {
    const user = userEvent.setup();
    const second = { ...wireFrame, schema: { ...wireFrame.schema, name: "second" } };
    renderApp(
      "/explore",
      responder(() => ({
        status: 200,
        body: { results: { A: { status: 200, frames: [wireFrame, second] } } },
      })),
    );
    await user.type(await screen.findByRole("textbox", { name: "Path" }), "x.csv");
    await user.click(screen.getByRole("button", { name: "Run query" }));
    const tabs = await screen.findAllByRole("tab");
    expect(tabs).toHaveLength(2);
    expect(tabs[0]).toHaveAttribute("aria-selected", "true");
    await user.click(screen.getByRole("tab", { name: "second" }));
    expect(screen.getByRole("table", { name: "second, 2 rows" })).toBeInTheDocument();
  });
});

describe("readQueryOutcome (FR-018 AC5)", () => {
  it("rejects malformed bodies and frames with a generic decode error", () => {
    for (const body of [null, [], { results: {} }, { results: { A: { status: "200" } } }]) {
      const o = readQueryOutcome(200, body, "A");
      expect(o.ok ? "" : o.error.code).toBe("invalid_response");
    }
    const bad = readQueryOutcome(
      200,
      { results: { A: { status: 200, frames: [{ schema: {}, data: {} }] } } },
      "A",
    );
    expect(bad.ok ? "" : bad.error.code).toBe("invalid_response");
    const env = readQueryOutcome(415, { error: { code: "unsupported_media_type" } }, "A");
    expect(env).toEqual({
      ok: false,
      status: 415,
      error: { code: "unsupported_media_type", message: "" },
    });
  });
});
