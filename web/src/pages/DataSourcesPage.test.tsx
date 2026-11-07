import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { defaultResponder, fixtures, renderApp } from "@/test/render";

const catalog = {
  sources: [
    {
      id: "file",
      name: "Files",
      description: "Local files",
      capabilities: ["query", "browse"],
    },
    { id: "rest", name: "REST API", description: "HTTP JSON", capabilities: [] },
  ],
  formats: [
    {
      id: "csv",
      name: "CSV",
      description: "Comma-separated values",
      mediaTypes: ["text/csv"],
      extensions: [".csv"],
    },
  ],
};

describe("Data sources page (FR-015 AC6)", () => {
  it("lists source and format plugins from the API", async () => {
    renderApp("/data-sources", (p) =>
      p === "/api/v1/plugins" ? { status: 200, body: catalog } : defaultResponder(p),
    );
    expect(screen.getByRole("heading", { level: 1, name: "Data sources" })).toBeInTheDocument();
    const sources = await screen.findByRole("region", { name: "Data source types" });
    expect(within(sources).getAllByTestId("plugin")).toHaveLength(2);
    const [first] = within(sources).getAllByTestId("plugin");
    if (!first) throw new Error("no plugin items");
    expect(within(first).getByRole("heading", { name: "Files" })).toBeVisible();
    expect(within(first).getByText("browse")).toBeVisible();
    const formats = screen.getByRole("region", { name: "File formats" });
    expect(within(formats).getByText(".csv")).toBeVisible();
    expect(within(formats).getByText("csv")).toBeVisible();
  });

  it("states that encrypted secret storage is planned, not shipped (FR-015 AC7)", async () => {
    renderApp("/data-sources");
    expect(
      await screen.findByText("Encrypted secret storage is planned for milestone M3."),
    ).toBeVisible();
    expect(screen.getByText(/Stored credentials are not supported yet\./)).toBeVisible();
    expect(screen.queryByText(/encrypted at rest/i)).not.toBeInTheDocument();
  });

  it("shows explicit empty states when no plugins are registered", async () => {
    renderApp("/data-sources");
    expect(
      await screen.findByText(/No data-source plugins are compiled into this server yet/),
    ).toBeInTheDocument();
    expect(screen.getByText("No format plugins are compiled into this server yet.")).toBeVisible();
    expect(fixtures.plugins.sources).toHaveLength(0);
  });

  it("shows an alert with retry when the catalogue cannot be loaded", async () => {
    let fail = true;
    const user = userEvent.setup();
    const { fetchMock } = renderApp("/data-sources", (p) =>
      p === "/api/v1/plugins" && fail ? new Error("offline") : defaultResponder(p),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Could not load the plugin catalogue.",
    );
    fail = false;
    await user.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByTestId("plugin-catalogue")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/plugins", expect.anything());
  });

  it("shows a busy state while loading", () => {
    renderApp("/data-sources");
    expect(screen.getByRole("status")).toHaveTextContent("Loading plugin catalogue…");
  });
});
