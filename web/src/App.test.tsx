import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { defaultResponder, renderApp } from "@/test/render";

describe("App shell (FR-005)", () => {
  it("renders primary navigation with the current page marked", async () => {
    renderApp("/");
    const nav = screen.getByRole("navigation", { name: "Primary" });
    const links = within(nav)
      .getAllByRole("link")
      .map((l) => l.textContent);
    expect(links).toEqual(["Home", "Explore", "Dashboards", "Data sources", "API reference"]);
    expect(within(nav).getByRole("link", { name: "Home" })).toHaveAttribute("aria-current", "page");
    await waitFor(() => {
      expect(document.title).toBe("Home · dv");
    });
  });

  it("navigates to planned sections with an explicit milestone badge", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await user.click(screen.getByRole("link", { name: "Dashboards" }));
    expect(screen.getByText("Planned for milestone M6")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Dashboards" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    await user.click(screen.getByRole("link", { name: "Explore" }));
    expect(screen.getByRole("heading", { level: 1, name: "Explore" })).toBeInTheDocument();
  });

  it("has a skip link as the first focusable element", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await user.tab();
    expect(screen.getByRole("link", { name: "Skip to main content" })).toHaveFocus();
  });

  it("toggles and persists the theme", async () => {
    const user = userEvent.setup();
    renderApp("/");
    expect(document.documentElement.dataset.theme).toBe("light");
    await user.click(screen.getByRole("button", { name: "Switch to dark theme" }));
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(window.localStorage.getItem("dv.theme")).toBe("dark");
    await user.click(screen.getByRole("button", { name: "Switch to light theme" }));
    expect(document.documentElement.dataset.theme).toBe("light");
  });

  it("renders not found for unknown routes", async () => {
    renderApp("/no/such/page");
    expect(screen.getByRole("heading", { level: 1, name: "Page not found" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Go to Home" })).toHaveAttribute("href", "/");
    await waitFor(() => {
      expect(document.title).toBe("Page not found · dv");
    });
  });
});

describe("Home status (FR-006)", () => {
  it("shows operational status and build info", async () => {
    renderApp("/");
    expect(await screen.findByText("Operational")).toBeInTheDocument();
    expect(await screen.findByText("1.2.3")).toBeInTheDocument();
    expect(screen.getByText("abc1234")).toBeInTheDocument();
    expect(screen.getByText("No dependency checks are registered yet.")).toBeInTheDocument();
  });

  it("shows degraded status and failing checks when not ready", async () => {
    renderApp("/", (p) =>
      p === "/readyz"
        ? {
            status: 503,
            body: { status: "not_ready", checks: { database: "fail", cache: "pass" } },
          }
        : defaultResponder(p),
    );
    expect(await screen.findByText("Degraded")).toBeInTheDocument();
    expect(screen.getByText("database: fail")).toBeInTheDocument();
    expect(screen.getByText("cache: pass")).toBeInTheDocument();
  });

  it("shows an alert with retry when the server is unreachable", async () => {
    const user = userEvent.setup();
    let fail = true;
    const { fetchMock } = renderApp("/", (p) =>
      fail ? new TypeError("network") : defaultResponder(p),
    );
    const alerts = await screen.findAllByRole("alert");
    expect(alerts.length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("Could not reach the dv server.").length).toBe(2);
    fail = false;
    await user.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByText("Operational")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalled();
  });

  it("shows a loading state while pending", () => {
    renderApp("/", () => ({ status: 200, body: {} }));
    expect(screen.getByText("Checking server status…")).toBeInTheDocument();
  });
});

describe("API reference (FR-007)", () => {
  it("lists every operation from the OpenAPI document", async () => {
    renderApp("/api-reference");
    const rows = await screen.findAllByTestId("endpoint");
    const text = rows.map((r) => r.textContent);
    expect(text).toEqual([
      "GET/api/v1/itemsList",
      "POST/api/v1/items",
      "GET/api/v1/versionBuild information",
      "GET/healthzLiveness probe",
      "GET/readyzReadiness probe",
    ]);
    expect(screen.getByText("dv API 0.1.0 · OpenAPI 3.1.0")).toBeInTheDocument();
  });

  it("shows an error when the document cannot be loaded", async () => {
    renderApp("/api-reference", (p) =>
      p === "/api/openapi.json" ? { status: 500, body: {} } : defaultResponder(p),
    );
    expect(await screen.findByText("Could not load the API description.")).toBeInTheDocument();
  });

  it("renders spec content as text, never HTML", async () => {
    renderApp("/api-reference", (p) =>
      p === "/api/openapi.json"
        ? {
            status: 200,
            body: {
              openapi: "3.1.0",
              info: { title: "x", version: "1" },
              paths: { "/x": { get: { summary: "<img src=x onerror=alert(1)>" } } },
            },
          }
        : defaultResponder(p),
    );
    expect(await screen.findByText("<img src=x onerror=alert(1)>")).toBeInTheDocument();
    expect(document.querySelector("img")).toBeNull();
  });
});
