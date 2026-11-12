import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { readQueryOutcome } from "@/lib/api";
import { readHistory, recordQuery } from "@/lib/history";
import { PREF_KEYS } from "@/lib/prefs";
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
          formats: [
            { id: "csv", name: "CSV", description: "", mediaTypes: [], extensions: [] },
            { id: "json", name: "JSON", description: "", mediaTypes: [], extensions: [".json"] },
            {
              id: "ndjson",
              name: "NDJSON",
              description: "",
              mediaTypes: [],
              extensions: [".ndjson", ".jsonl"],
            },
          ],
        },
      };
    }
    if (path === "/api/v1/query") return query(JSON.parse(init?.body as string));
    return defaultResponder(path);
  };
}

const ok = () => ({
  status: 200,
  body: { results: { A: { status: 200, durationMs: 1234, frames: [wireFrame] } } },
});

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
      /2 rows · 2 fields · query time 1,234 ms · total request time \d+ ms · samples:sales\.csv/,
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

  it.each([
    ["missing", undefined],
    ["negative", -1],
    ["fractional", 1.5],
    ["a string", "12"],
  ])("shows an unknown query time as — when durationMs is %s (AC4)", async (_, d) => {
    const user = userEvent.setup();
    renderApp(
      "/explore?path=sales.csv",
      responder(() => ({
        status: 200,
        body: { results: { A: { status: 200, durationMs: d, frames: [wireFrame] } } },
      })),
    );
    await user.click(await screen.findByRole("button", { name: "Run query" }));
    expect(await screen.findByTestId("query-stats")).toHaveTextContent(
      /query time — · total request time \d+ ms/,
    );
  });

  it("runs with Ctrl+Enter from the path field", async () => {
    const user = userEvent.setup();
    renderApp("/explore", responder(ok));
    const path = await screen.findByRole("textbox", { name: "Path" });
    await user.type(path, "sales.csv");
    await user.keyboard("{Control>}{Enter}{/Control}");
    expect(await screen.findByRole("table")).toBeInTheDocument();
  });

  it("offers JSON options for the format the query will use (FR-012 AC9)", async () => {
    const user = userEvent.setup();
    const seen: unknown[] = [];
    renderApp(
      "/explore",
      responder((b) => {
        seen.push(b);
        return ok();
      }),
    );
    const path = await screen.findByRole("textbox", { name: "Path" });
    expect(screen.queryByRole("textbox", { name: "Rows path" })).toBeNull();
    expect(screen.queryByRole("checkbox", { name: "Flatten nested objects" })).toBeNull();

    await user.type(path, "events.JSONL");
    expect(screen.queryByRole("textbox", { name: "Rows path" })).toBeNull();
    expect(
      screen.getByRole("checkbox", { name: "Flatten nested objects" }),
    ).toHaveAccessibleDescription("Turns nested objects into fields such as owner.team.");

    await user.clear(path);
    await user.type(path, "services.json");
    const rowsPath = screen.getByRole("textbox", { name: "Rows path" });
    await user.type(rowsPath, "data..services");
    await user.click(screen.getByRole("button", { name: "Run query" }));
    expect(rowsPath).toHaveAttribute("aria-invalid", "true");
    expect(rowsPath).toHaveAccessibleDescription(
      "Use dot-separated keys of letters, digits, _ or - (at most 16).",
    );
    expect(seen).toEqual([]);

    await user.clear(rowsPath);
    await user.type(rowsPath, "data.services");
    await user.click(screen.getByRole("checkbox", { name: "Flatten nested objects" }));
    await user.click(screen.getByRole("button", { name: "Run query" }));
    expect(await screen.findByRole("table")).toBeInTheDocument();
    expect(seen).toEqual([
      {
        queries: [
          {
            refId: "A",
            datasource: { uid: "samples" },
            query: { path: "services.json", options: { rowsPath: "data.services", flatten: true } },
          },
        ],
      },
    ]);

    // An explicit CSV format hides the JSON options and sends none.
    await user.selectOptions(screen.getByRole("combobox", { name: "Format" }), "csv");
    expect(screen.queryByRole("textbox", { name: "Rows path" })).toBeNull();
  });

  it("restores JSON options from the URL", async () => {
    renderApp("/explore?ds=samples&path=a.json&rowsPath=data.items&flatten=1", responder(ok));
    expect(await screen.findByRole("textbox", { name: "Rows path" })).toHaveValue("data.items");
    expect(screen.getByRole("checkbox", { name: "Flatten nested objects" })).toBeChecked();
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

  it("keeps a replacement run active when a running query is superseded (AC3)", async () => {
    const user = userEvent.setup();
    const signals: AbortSignal[] = [];
    const { fetchMock } = renderApp("/explore", responder(ok));
    const base = fetchMock.getMockImplementation();
    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      if (input !== "/api/v1/query") return base ? base(input, init) : Promise.reject(new Error());
      const signal = init?.signal ?? new AbortController().signal;
      signals.push(signal);
      return new Promise<Response>((_, reject) => {
        signal.addEventListener("abort", () => {
          reject(new DOMException("aborted", "AbortError"));
        });
      });
    });
    const path = await screen.findByRole("textbox", { name: "Path" });
    await user.type(path, "slow.csv");
    await user.keyboard("{Control>}{Enter}{/Control}");
    await user.keyboard("{Control>}{Enter}{/Control}");
    expect(signals).toHaveLength(2);
    expect(signals[0]?.aborted).toBe(true);
    expect(signals[1]?.aborted).toBe(false);
    // The superseded request's rejection must not end the replacement run.
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.queryByText("The query was cancelled.")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Running…" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(signals[1]?.aborted).toBe(true);
    expect(await screen.findByText("The query was cancelled.")).toBeInTheDocument();
  });

  it("moves between frame tabs with the arrow keys, Home and End (AC4)", async () => {
    const user = userEvent.setup();
    const named = (name: string) => ({ ...wireFrame, schema: { ...wireFrame.schema, name } });
    renderApp(
      "/explore?path=x.csv",
      responder(() => ({
        status: 200,
        body: {
          results: { A: { status: 200, frames: [named("one"), named("two"), named("three")] } },
        },
      })),
    );
    await user.click(await screen.findByRole("button", { name: "Run query" }));
    const one = await screen.findByRole("tab", { name: "one" });
    one.focus();
    await user.keyboard("{ArrowRight}");
    expect(screen.getByRole("tab", { name: "two" })).toHaveFocus();
    expect(screen.getByRole("tab", { name: "two" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("table", { name: "two, 2 rows" })).toBeInTheDocument();
    await user.keyboard("{End}");
    expect(screen.getByRole("tab", { name: "three" })).toHaveFocus();
    await user.keyboard("{ArrowRight}");
    expect(screen.getByRole("tab", { name: "one" })).toHaveFocus();
    await user.keyboard("{ArrowLeft}");
    expect(screen.getByRole("tab", { name: "three" })).toHaveFocus();
    await user.keyboard("{Home}");
    expect(screen.getByRole("tab", { name: "one" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("table", { name: "one, 2 rows" })).toBeInTheDocument();
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
  describe("visualization picker (FR-014 AC1)", () => {
    const tsFrame = {
      schema: {
        name: "cpu.csv",
        fields: [
          { name: "time", type: "time" },
          { name: "web-1", type: "number" },
        ],
      },
      data: {
        values: [
          [Date.UTC(2026, 10, 10), Date.UTC(2026, 10, 10, 1)],
          [16.4, 19.9],
        ],
      },
    };
    const tsOk = () => ({
      status: 200,
      body: { results: { A: { status: 200, frames: [tsFrame] } } },
    });

    it("switches between table and time series without re-running the query", async () => {
      const user = userEvent.setup();
      const { fetchMock } = renderApp("/explore?path=cpu.csv", responder(tsOk));
      const table = await screen.findByRole("radio", { name: "Table" });
      expect(table).toBeChecked();
      await user.click(screen.getByRole("button", { name: "Run query" }));
      expect(await screen.findByRole("table", { name: "cpu.csv, 2 rows" })).toBeInTheDocument();
      await user.click(screen.getByRole("radio", { name: "Time series" }));
      expect(screen.getByRole("img", { name: /^Line chart of web-1 over time/ })).toBeVisible();
      expect(screen.queryByRole("table")).toBeNull();
      await user.click(screen.getByRole("radio", { name: "Table" }));
      expect(screen.getByRole("table", { name: "cpu.csv, 2 rows" })).toBeInTheDocument();
      const runs = fetchMock.mock.calls.filter(([p]) => p === "/api/v1/query");
      expect(runs).toHaveLength(1);
    });

    it("keeps the chosen view when the query runs again", async () => {
      const user = userEvent.setup();
      renderApp("/explore?path=cpu.csv&view=timeseries", responder(tsOk));
      expect(await screen.findByRole("radio", { name: "Time series" })).toBeChecked();
      expect(screen.getByTestId("explore-empty")).toBeInTheDocument();
      await user.click(screen.getByRole("button", { name: "Run query" }));
      expect(await screen.findByTestId("timeseries-chart")).toBeInTheDocument();
      await user.click(screen.getByRole("button", { name: "Run query" }));
      expect(await screen.findByTestId("timeseries-chart")).toBeInTheDocument();
      expect(screen.getByRole("radio", { name: "Time series" })).toBeChecked();
    });

    it("falls back to the table for an unknown view value", async () => {
      renderApp("/explore?view=%3Cscript%3E", responder(tsOk));
      expect(await screen.findByRole("radio", { name: "Table" })).toBeChecked();
    });

    it("offers the table when the frame cannot be charted", async () => {
      const user = userEvent.setup();
      renderApp("/explore?path=sales.csv&view=timeseries", responder(ok));
      await user.click(await screen.findByRole("button", { name: "Run query" }));
      await user.click(await screen.findByRole("button", { name: "Show as table" }));
      expect(screen.getByRole("radio", { name: "Table" })).toBeChecked();
      expect(screen.getByRole("table", { name: "sales.csv, 2 rows" })).toBeInTheDocument();
    });
  });

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

describe("Explore workspace: splitter and result states (FR-018 AC8, AC10)", () => {
  it("resizes the editor from the keyboard and remembers the width (AC8)", async () => {
    const user = userEvent.setup();
    const first = renderApp("/explore", responder(ok));
    const sep = await screen.findByRole("separator", { name: "Resize query editor" });
    const editor = screen.getByTestId("explore-editor");
    expect(sep).toHaveAttribute("aria-controls", editor.id);
    expect(sep).toHaveAttribute("aria-valuenow", "360");
    expect(editor.parentElement?.style.getPropertyValue("--split")).toBe("360px");
    sep.focus();
    await user.keyboard("{ArrowRight}");
    expect(sep).toHaveAttribute("aria-valuenow", "376");
    expect(editor.parentElement?.style.getPropertyValue("--split")).toBe("376px");
    expect(window.localStorage.getItem(PREF_KEYS.split)).toBe("376");
    first.unmount();

    renderApp("/explore", responder(ok));
    expect(await screen.findByRole("separator")).toHaveAttribute("aria-valuenow", "376");
  });

  it.each([
    ["not a number", "wide", "360"],
    ["too large", "5000", "640"],
    ["too small", "-1", "280"],
  ])("handles a stored width that is %s", async (_, stored, want) => {
    window.localStorage.setItem(PREF_KEYS.split, stored);
    renderApp("/explore", responder(ok));
    expect(await screen.findByRole("separator")).toHaveAttribute("aria-valuenow", want);
  });

  it("shows an empty state with the shortcut, then a skeleton while running (AC10)", async () => {
    const user = userEvent.setup();
    let release: (() => void) | undefined;
    const fetchMock = renderApp("/explore?path=slow.csv", responder(ok)).fetchMock;
    const real = fetchMock.getMockImplementation();
    fetchMock.mockImplementation((input, init) =>
      input === "/api/v1/query"
        ? new Promise<Response>((resolve) => {
            release = () => {
              resolve(new Response(JSON.stringify(ok().body), { status: 200 }));
            };
          })
        : (real?.(input, init) ?? Promise.reject(new Error("no fetch"))),
    );
    const empty = await screen.findByTestId("explore-empty");
    expect(empty).toHaveTextContent("Press Run query to see the result.");
    expect(empty).toHaveTextContent("Ctrl+Enter or ⌘+Enter");

    await user.click(screen.getByRole("button", { name: "Run query" }));
    const running = await screen.findByTestId("explore-running");
    expect(within(running).getByRole("status")).toHaveTextContent("Running…");
    expect(screen.getByRole("region", { name: "Result" })).toHaveAttribute("aria-busy", "true");
    release?.();
    expect(await screen.findByRole("table", { name: "sales.csv, 2 rows" })).toBeInTheDocument();
    expect(screen.queryByTestId("explore-running")).toBeNull();
  });
});

describe("Explore query history (FR-018 AC9, NFR-SEC-019)", () => {
  const openHistory = async (user: ReturnType<typeof userEvent.setup>) => {
    await user.click(screen.getByRole("button", { name: "History" }));
    return screen.getByTestId("history-panel");
  };

  it("records each run with the fields of the URL and never the result", async () => {
    const user = userEvent.setup();
    renderApp("/explore", responder(ok));
    await user.type(await screen.findByRole("textbox", { name: "Path" }), "data.json");
    await user.type(screen.getByRole("textbox", { name: "Rows path" }), "data.items");
    await user.click(screen.getByRole("checkbox", { name: "Flatten nested objects" }));
    await user.type(screen.getByRole("textbox", { name: "Row limit" }), "25");
    await user.click(screen.getByRole("button", { name: "Run query" }));
    await screen.findByRole("table");

    const entries = readHistory();
    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      ds: "samples",
      path: "data.json",
      maxRows: 25,
      rowsPath: "data.items",
      flatten: true,
    });
    expect(Object.keys(entries[0] ?? {}).sort()).toEqual(
      ["at", "ds", "flatten", "maxRows", "path", "rowsPath"].sort(),
    );
    expect(window.localStorage.getItem(PREF_KEYS.history)).not.toContain("emea");

    // Running the same query again keeps one entry.
    await user.click(screen.getByRole("button", { name: "Run query" }));
    await screen.findByRole("table");
    expect(readHistory()).toHaveLength(1);
  });

  it("does not record a query that failed validation", async () => {
    const user = userEvent.setup();
    renderApp("/explore", responder(ok));
    await user.click(await screen.findByRole("button", { name: "Run query" }));
    expect(screen.getByRole("textbox", { name: "Path" })).toHaveAttribute("aria-invalid", "true");
    expect(readHistory()).toEqual([]);
  });

  it("lists entries, refills the form without running, and closes (AC9, AC6)", async () => {
    recordQuery({ ds: "logs", path: "app.csv", format: "csv", maxRows: 7 }, Date.now() - 120_000);
    recordQuery({ ds: "gone", path: "old.tsv" }, Date.now() - 3 * 86_400_000);
    recordQuery({ ds: "samples", path: "<img src=x onerror=alert(1)>.csv" }, Date.now());
    const user = userEvent.setup();
    const { fetchMock } = renderApp("/explore?ds=samples&path=now.csv", responder(ok));
    const button = await screen.findByRole("button", { name: "History" });
    expect(button).toHaveAttribute("aria-expanded", "false");
    const panel = await openHistory(user);
    expect(button).toHaveAttribute("aria-expanded", "true");
    expect(button).toHaveAttribute("aria-controls", panel.id);
    expect(panel).toHaveAccessibleName("Recent queries");

    const rows = within(panel).getAllByTestId("history-entry");
    expect(rows.map((r) => within(r).getByTestId("history-path").textContent)).toEqual([
      "<img src=x onerror=alert(1)>.csv",
      "old.tsv",
      "app.csv",
    ]);
    expect(document.querySelector("img")).toBeNull();
    expect(rows[0]).toHaveTextContent("Sample files · now");
    expect(rows[1]).toHaveTextContent("gone (not available) · 3 days ago");
    expect(rows[2]).toHaveTextContent("Logs · csv · ≤ 7 · 2 minutes ago");
    expect(within(panel).getByText(/Results are never stored/)).toBeInTheDocument();

    const third = rows[2];
    if (!third) throw new Error("expected three history entries");
    await user.click(third);
    expect(screen.queryByTestId("history-panel")).toBeNull();
    await waitFor(() => {
      expect(screen.getByRole("textbox", { name: "Path" })).toHaveValue("app.csv");
    });
    expect(screen.getByRole("combobox", { name: "Data source" })).toHaveValue("logs");
    expect(screen.getByRole("combobox", { name: "Format" })).toHaveValue("csv");
    expect(screen.getByRole("textbox", { name: "Row limit" })).toHaveValue("7");
    expect(screen.getByTestId("explore-empty")).toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([p]) => p === "/api/v1/query")).toBe(false);
  });

  it("falls back to the first source for an entry whose source is gone", async () => {
    recordQuery({ ds: "gone", path: "old.tsv" });
    const user = userEvent.setup();
    renderApp("/explore", responder(ok));
    await screen.findByRole("button", { name: "History" });
    const panel = await openHistory(user);
    await user.click(within(panel).getByTestId("history-entry"));
    await waitFor(() => {
      expect(screen.getByRole("textbox", { name: "Path" })).toHaveValue("old.tsv");
    });
    expect(screen.getByRole("combobox", { name: "Data source" })).toHaveValue("samples");
  });

  it("keeps a result on screen when a run updates the URL (no remount)", async () => {
    const user = userEvent.setup();
    renderApp("/explore", responder(ok));
    await user.type(await screen.findByRole("textbox", { name: "Path" }), "a.csv");
    await user.click(screen.getByRole("button", { name: "Run query" }));
    expect(await screen.findByRole("table")).toBeInTheDocument();
    await openHistory(user);
    await user.click(screen.getByTestId("history-entry"));
    // Restoring is explicit: the form is refilled and the old result is cleared.
    await waitFor(() => {
      expect(screen.getByTestId("explore-empty")).toBeInTheDocument();
    });
    await user.click(screen.getByRole("button", { name: "Run query" }));
    expect(await screen.findByRole("table")).toBeInTheDocument();
  });

  it("clears the history and closes on Escape or an outside press", async () => {
    recordQuery({ ds: "samples", path: "a.csv" });
    const user = userEvent.setup();
    renderApp("/explore", responder(ok));
    const button = await screen.findByRole("button", { name: "History" });
    let panel = await openHistory(user);
    await user.click(within(panel).getByRole("button", { name: "Clear history" }));
    expect(within(panel).getByTestId("history-empty")).toHaveTextContent("No queries yet.");
    expect(within(panel).queryByRole("button", { name: "Clear history" })).toBeNull();
    expect(window.localStorage.getItem(PREF_KEYS.history)).toBeNull();

    await user.keyboard("{Escape}");
    expect(screen.queryByTestId("history-panel")).toBeNull();
    expect(button).toHaveFocus();

    panel = await openHistory(user);
    await user.keyboard("a");
    expect(panel).toBeInTheDocument();
    await user.click(screen.getByRole("heading", { level: 1 }));
    expect(screen.queryByTestId("history-panel")).toBeNull();

    await openHistory(user);
    await user.click(button);
    expect(screen.queryByTestId("history-panel")).toBeNull();
  });

  it("ignores a tampered history document", async () => {
    window.localStorage.setItem(PREF_KEYS.history, '[{"ds":"samples","path":7,"at":1},"x",null]');
    const user = userEvent.setup();
    renderApp("/explore", responder(ok));
    await screen.findByRole("button", { name: "History" });
    const panel = await openHistory(user);
    expect(within(panel).getByTestId("history-empty")).toBeInTheDocument();
  });
});
