import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { recordQuery } from "@/lib/history";
import { PREF_KEYS } from "@/lib/prefs";
import { defaultResponder, renderApp, type Responder } from "@/test/render";

const withSources: Responder = (path) =>
  path === "/api/v1/datasources"
    ? {
        status: 200,
        body: {
          datasources: [
            { uid: "samples", type: "file", name: "Sample files" },
            { uid: "logs", type: "file", name: "Logs <b>" },
          ],
        },
      }
    : defaultResponder(path);

const palette = () => screen.getByRole("dialog", { name: "Command palette" });
const search = () => within(palette()).getByRole("combobox");
const activeTitle = () =>
  within(within(palette()).getByRole("option", { selected: true })).getByTestId("command-title")
    .textContent;
const heading = () => screen.getByRole("heading", { level: 1 }).textContent;

async function openPalette(user: ReturnType<typeof userEvent.setup>) {
  await user.keyboard("{Control>}k{/Control}");
  return palette();
}

/** A controllable `prefers-color-scheme` so OS changes can be simulated. */
function mockColorScheme(initiallyDark: boolean) {
  let dark = initiallyDark;
  const listeners = new Set<() => void>();
  vi.spyOn(window, "matchMedia").mockImplementation(
    (query) =>
      ({
        get matches() {
          return dark;
        },
        media: query,
        addEventListener: (_: string, cb: () => void) => listeners.add(cb),
        removeEventListener: (_: string, cb: () => void) => listeners.delete(cb),
      }) as unknown as MediaQueryList,
  );
  return (next: boolean) => {
    dark = next;
    for (const l of listeners) l();
  };
}

describe("workspace frame (FR-005 AC8–AC12)", () => {
  it("names the current page in the breadcrumb", async () => {
    const user = userEvent.setup();
    renderApp("/");
    const crumbs = screen.getByRole("navigation", { name: "Breadcrumb" });
    expect(within(crumbs).getByTestId("breadcrumb-current")).toHaveTextContent("Home");
    expect(within(crumbs).getByTestId("breadcrumb-current")).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(within(crumbs).getByRole("link", { name: "dv home page" })).toHaveAttribute("href", "/");
    await user.click(screen.getByRole("link", { name: "Data sources" }));
    expect(within(crumbs).getByTestId("breadcrumb-current")).toHaveTextContent("Data sources");
  });

  it("names unknown routes in the breadcrumb", () => {
    renderApp("/nope");
    expect(screen.getByTestId("breadcrumb-current")).toHaveTextContent("Page not found");
  });

  it("collapses the sidebar to a rail that keeps link names, and remembers it (AC9)", async () => {
    const user = userEvent.setup();
    const first = renderApp("/");
    const toggle = screen.getByRole("button", { name: "Collapse sidebar" });
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(toggle).toHaveAttribute("aria-controls", "primary-nav");
    expect(screen.getByRole("navigation", { name: "Primary" })).toHaveAttribute(
      "id",
      "primary-nav",
    );
    expect(screen.getByRole("link", { name: "Explore" })).not.toHaveAttribute("title");

    await user.click(toggle);
    expect(screen.getByRole("button", { name: "Expand sidebar" })).toHaveAttribute(
      "aria-expanded",
      "false",
    );
    expect(screen.getByTestId("sidebar")).toHaveAttribute("data-collapsed", "true");
    expect(window.localStorage.getItem(PREF_KEYS.sidebar)).toBe("collapsed");
    const nav = screen.getByRole("navigation", { name: "Primary" });
    expect(
      within(nav)
        .getAllByRole("link")
        .map((l) => [l.textContent, l.getAttribute("title")]),
    ).toEqual([
      ["Home", "Home"],
      ["Explore", "Explore"],
      ["Dashboards", "Dashboards"],
      ["Data sources", "Data sources"],
      ["API reference", "API reference"],
    ]);

    first.unmount();
    renderApp("/");
    expect(screen.getByTestId("sidebar")).toHaveAttribute("data-collapsed", "true");
    await user.keyboard("{Control>}b{/Control}");
    expect(screen.getByTestId("sidebar")).toHaveAttribute("data-collapsed", "false");
    await user.keyboard("{Meta>}b{/Meta}");
    expect(screen.getByTestId("sidebar")).toHaveAttribute("data-collapsed", "true");
  });

  it("shows the server state and version in the sidebar foot (AC10)", async () => {
    renderApp("/");
    const foot = screen.getByTestId("server-state");
    expect(foot).toHaveAttribute("data-state", "checking");
    expect(foot).toHaveTextContent("Checking server…");
    await waitFor(() => {
      expect(foot).toHaveAttribute("data-state", "ready");
    });
    expect(foot).toHaveTextContent("Server ready");
    expect(await within(foot).findByText("dv 1.2.3")).toBeInTheDocument();
  });

  it("shows not ready and unreachable servers in the sidebar foot", async () => {
    const first = renderApp("/", (p) =>
      p === "/readyz"
        ? { status: 503, body: { status: "not_ready", checks: { db: "fail" } } }
        : defaultResponder(p),
    );
    await waitFor(() => {
      expect(screen.getByTestId("server-state")).toHaveTextContent("Server not ready");
    });
    first.unmount();
    renderApp("/", (p) => (p === "/readyz" ? new TypeError("network") : defaultResponder(p)));
    await waitFor(() => {
      expect(screen.getByTestId("server-state")).toHaveTextContent("Server unreachable");
    });
  });

  it("hides the version text and keeps the state as a tooltip when collapsed", async () => {
    window.localStorage.setItem(PREF_KEYS.sidebar, "collapsed");
    renderApp("/");
    const foot = screen.getByTestId("server-state");
    await waitFor(() => {
      expect(foot).toHaveAttribute("title", "Server ready");
    });
    expect(within(foot).queryByText("dv 1.2.3")).toBeNull();
  });

  it("follows the OS colour scheme until a theme is chosen (AC12)", async () => {
    const setDark = mockColorScheme(false);
    const user = userEvent.setup();
    renderApp("/");
    expect(document.documentElement.dataset.theme).toBe("light");
    act(() => {
      setDark(true);
    });
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(screen.getByRole("button", { name: "Switch to light theme" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Switch to light theme" }));
    expect(window.localStorage.getItem(PREF_KEYS.theme)).toBe("light");
    act(() => {
      setDark(false);
      setDark(true);
    });
    expect(document.documentElement.dataset.theme).toBe("light");

    // "Use system theme" forgets the choice and goes back to the OS value.
    await openPalette(user);
    await user.type(search(), "system");
    expect(activeTitle()).toBe("Use system theme");
    await user.keyboard("{Enter}");
    expect(window.localStorage.getItem(PREF_KEYS.theme)).toBeNull();
    expect(document.documentElement.dataset.theme).toBe("dark");
    await openPalette(user);
    await user.type(search(), "system");
    expect(within(palette()).queryByRole("option")).toBeNull();
  });
});

describe("command palette (FR-008)", () => {
  it("opens with Ctrl+K, takes focus, makes the page inert and lists the pages (AC1–AC3)", async () => {
    const user = userEvent.setup();
    renderApp("/");
    expect(screen.queryByRole("dialog")).toBeNull();
    const dialog = await openPalette(user);
    expect(dialog).toHaveAttribute("aria-modal", "true");
    expect(search()).toHaveFocus();
    expect(search()).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("main").closest("[inert]")).not.toBeNull();
    expect(dialog.closest("[inert]")).toBeNull();

    const list = within(dialog).getByRole("listbox", { name: "Commands" });
    expect(search()).toHaveAttribute("aria-controls", list.id);
    const goTo = within(list).getByRole("group", { name: "Go to" });
    expect(
      within(goTo)
        .getAllByTestId("command-title")
        .map((n) => n.textContent),
    ).toEqual([
      "Go to Home",
      "Go to Explore",
      "Go to Dashboards",
      "Go to Data sources",
      "Go to API reference",
    ]);
    expect(within(list).getByRole("group", { name: "Preferences" })).toBeInTheDocument();
    expect(within(list).getByRole("group", { name: "Help" })).toBeInTheDocument();
    // No data sources (query API off) and no history: those groups are absent.
    expect(within(list).queryByRole("group", { name: "Explore a data source" })).toBeNull();
    expect(within(list).queryByRole("group", { name: "Recent queries" })).toBeNull();

    await user.keyboard("{Meta>}k{/Meta}");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("main").closest("[inert]")).toBeNull();
  });

  it("moves the active option with the arrow keys, Home and End, wrapping (AC2)", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await openPalette(user);
    expect(activeTitle()).toBe("Go to Home");
    const active = () => within(palette()).getByRole("option", { selected: true });
    expect(search()).toHaveAttribute("aria-activedescendant", active().id);
    await user.keyboard("{ArrowDown}");
    expect(activeTitle()).toBe("Go to Explore");
    expect(search()).toHaveAttribute("aria-activedescendant", active().id);
    await user.keyboard("{ArrowUp}{ArrowUp}");
    expect(activeTitle()).toBe("Keyboard shortcuts");
    await user.keyboard("{ArrowDown}");
    expect(activeTitle()).toBe("Go to Home");
    await user.keyboard("{End}");
    expect(activeTitle()).toBe("Keyboard shortcuts");
    await user.keyboard("{Home}");
    expect(activeTitle()).toBe("Go to Home");
    expect(within(palette()).getAllByRole("option", { selected: true })).toHaveLength(1);
  });

  it("keeps Tab inside the dialog", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await openPalette(user);
    await user.tab();
    expect(search()).toHaveFocus();
    await user.tab({ shift: true });
    expect(search()).toHaveFocus();
  });

  it("filters as you type and runs the best match with Enter (AC4, AC5)", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await openPalette(user);
    await user.type(search(), "api");
    expect(within(palette()).getAllByRole("option")).toHaveLength(1);
    expect(activeTitle()).toBe("Go to API reference");
    await user.keyboard("{Enter}");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(heading()).toBe("API reference");
  });

  it("says when nothing matches and does nothing on Enter (AC4)", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await openPalette(user);
    await user.type(search(), "  zz<b>top  ");
    expect(within(palette()).getByRole("status")).toHaveTextContent("No results for “zz<b>top”");
    expect(within(palette()).queryByRole("listbox")).toBeNull();
    expect(search()).toHaveAttribute("aria-expanded", "false");
    expect(search()).not.toHaveAttribute("aria-activedescendant");
    expect(document.querySelector("b")).toBeNull();
    await user.keyboard("{Enter}{ArrowDown}");
    expect(palette()).toBeInTheDocument();
    expect(search()).toHaveAttribute("maxlength", "100");
  });

  it("closes with Escape and gives focus back to the opener (AC1)", async () => {
    const user = userEvent.setup();
    renderApp("/");
    const opener = screen.getByRole("button", { name: "Search or jump to…" });
    expect(opener).toHaveAttribute("aria-keyshortcuts", "Control+K Meta+K");
    await user.click(opener);
    expect(search()).toHaveFocus();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(opener).toHaveFocus();
  });

  it("closes when the pointer goes down outside the dialog, not inside", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await openPalette(user);
    fireEvent.pointerDown(search());
    expect(palette()).toBeInTheDocument();
    fireEvent.pointerDown(document.body);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("runs a command on click and tracks the pointer", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await openPalette(user);
    const option = screen
      .getAllByRole("option")
      .find((o) => o.textContent.startsWith("Go to Dashboards"));
    if (!option) throw new Error("no Dashboards command");
    fireEvent.pointerMove(option);
    expect(activeTitle()).toBe("Go to Dashboards");
    await user.click(option);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(heading()).toBe("Dashboards");
  });

  it("offers provisioned data sources and opens Explore with one selected, as text (AC3, AC5)", async () => {
    const user = userEvent.setup();
    const { fetchMock } = renderApp("/", withSources);
    await openPalette(user);
    const group = await within(palette()).findByRole("group", { name: "Explore a data source" });
    expect(
      within(group)
        .getAllByTestId("command-title")
        .map((n) => n.textContent),
    ).toEqual(["Explore Sample files", "Explore Logs <b>"]);
    expect(document.querySelector("b")).toBeNull();
    await user.type(search(), "logs");
    await user.keyboard("{Enter}");
    expect(heading()).toBe("Explore");
    expect(await screen.findByRole("combobox", { name: "Data source" })).toHaveValue("logs");
    expect(screen.getByTestId("explore-empty")).toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([p]) => p === "/api/v1/query")).toBe(false);
  });

  it("offers recent queries and refills Explore without running them (AC3, AC5)", async () => {
    recordQuery({ ds: "logs", path: "old.csv" }, 1);
    for (let i = 0; i < 6; i++) recordQuery({ ds: "samples", path: `q${String(i)}.csv` }, 10 + i);
    recordQuery({ ds: "logs", path: "app.json", format: "json", maxRows: 5, flatten: true }, 99);
    const user = userEvent.setup();
    const { fetchMock } = renderApp("/explore?ds=samples&path=first.csv", withSources);
    expect(await screen.findByRole("textbox", { name: "Path" })).toHaveValue("first.csv");

    await openPalette(user);
    const group = within(palette()).getByRole("group", { name: "Recent queries" });
    // Only the five most recent until the user searches.
    expect(
      within(group)
        .getAllByTestId("command-title")
        .map((n) => n.textContent),
    ).toEqual(["app.json", "q5.csv", "q4.csv", "q3.csv", "q2.csv"]);
    expect(within(group).getAllByRole("option")[0]).toHaveTextContent("Logs <b>");
    await user.type(search(), "old.csv");
    expect(activeTitle()).toBe("old.csv");
    await user.clear(search());
    await user.type(search(), "app.json");
    await user.keyboard("{Enter}");

    // Explore was already mounted: the form is refilled from the entry.
    await waitFor(() => {
      expect(screen.getByRole("textbox", { name: "Path" })).toHaveValue("app.json");
    });
    expect(screen.getByRole("combobox", { name: "Data source" })).toHaveValue("logs");
    expect(screen.getByRole("textbox", { name: "Row limit" })).toHaveValue("5");
    expect(screen.getByTestId("explore-empty")).toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([p]) => p === "/api/v1/query")).toBe(false);
  });

  it("switches the theme and the sidebar from the palette (AC3)", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await openPalette(user);
    await user.type(search(), "dark theme");
    await user.keyboard("{Enter}");
    expect(document.documentElement.dataset.theme).toBe("dark");
    await openPalette(user);
    await user.type(search(), "light theme");
    expect(activeTitle()).toBe("Switch to light theme");
    await user.clear(search());
    await user.type(search(), "sidebar");
    expect(activeTitle()).toBe("Collapse sidebar");
    await user.keyboard("{Enter}");
    expect(screen.getByTestId("sidebar")).toHaveAttribute("data-collapsed", "true");
    await openPalette(user);
    await user.type(search(), "sidebar");
    expect(activeTitle()).toBe("Expand sidebar");
  });

  it("opens with / outside fields only (AC1, AC7)", async () => {
    const user = userEvent.setup();
    renderApp("/explore?path=a.csv", withSources);
    const path = await screen.findByRole("textbox", { name: "Path" });
    await user.type(path, "/x?g");
    expect(path).toHaveValue("a.csv/x?g");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(heading()).toBe("Explore");
    path.blur();
    await user.keyboard("/");
    expect(search()).toHaveFocus();
    // The key that opened it is not typed into the field.
    expect(search()).toHaveValue("");
  });
});

describe("keyboard shortcuts dialog and sequences (FR-008 AC6–AC8)", () => {
  it("opens with ?, the top-bar button and the palette, and lists the shortcuts", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await user.keyboard("?");
    const dialog = screen.getByRole("dialog", { name: "Keyboard shortcuts" });
    expect(within(dialog).getByRole("heading", { name: "Keyboard shortcuts" })).toBeInTheDocument();
    for (const text of [
      "Open the command palette",
      "Collapse or expand the sidebar",
      "Go to Data sources",
      "Run the query",
    ]) {
      expect(within(dialog).getByText(text)).toBeInTheDocument();
    }
    expect(within(dialog).getByRole("button", { name: "Close" })).toHaveFocus();
    await user.click(within(dialog).getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("dialog")).toBeNull();

    const button = screen.getByRole("button", { name: "Keyboard shortcuts" });
    await user.click(button);
    expect(screen.getByRole("dialog", { name: "Keyboard shortcuts" })).toBeInTheDocument();
    await user.keyboard("{Escape}");
    expect(button).toHaveFocus();

    await openPalette(user);
    await user.type(search(), "shortcuts");
    await user.keyboard("{Enter}");
    expect(screen.getByRole("dialog", { name: "Keyboard shortcuts" })).toBeInTheDocument();
    expect(screen.queryByRole("dialog", { name: "Command palette" })).toBeNull();
    // Ctrl+K replaces the help dialog with the palette.
    await user.keyboard("{Control>}k{/Control}");
    expect(palette()).toBeInTheDocument();
    expect(screen.queryByRole("dialog", { name: "Keyboard shortcuts" })).toBeNull();
  });

  it("keeps Tab inside the dialog in both directions", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await user.keyboard("?");
    const dialog = screen.getByRole("dialog", { name: "Keyboard shortcuts" });
    const close = within(dialog).getByRole("button", { name: "Close" });
    const toggle = within(dialog).getByRole("switch", { name: "Single-key shortcuts" });
    expect(close).toHaveFocus();
    await user.tab();
    expect(toggle).toHaveFocus();
    await user.tab();
    expect(close).toHaveFocus();
    await user.tab({ shift: true });
    expect(toggle).toHaveFocus();
    dialog.focus();
    await user.tab({ shift: true });
    expect(toggle).toHaveFocus();
  });

  it("goes to a page with g then a letter, within the time limit", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await user.keyboard("ge");
    expect(heading()).toBe("Explore");
    await user.keyboard("gs");
    expect(heading()).toBe("Data sources");
    await user.keyboard("gh");
    expect(heading()).toBe("Home");
    // Another key in between cancels the sequence; a lone letter does nothing.
    await user.keyboard("gxe");
    expect(heading()).toBe("Home");
    await user.keyboard("a");
    expect(heading()).toBe("Home");

    // Too slow: the sequence has expired.
    const start = performance.now();
    const clock = vi.spyOn(performance, "now").mockReturnValue(start);
    fireEvent.keyDown(document.body, { key: "g" });
    clock.mockReturnValue(start + 1600);
    fireEvent.keyDown(document.body, { key: "a" });
    expect(heading()).toBe("Home");
    // Just in time.
    fireEvent.keyDown(document.body, { key: "g" });
    clock.mockReturnValue(start + 1600 + 1400);
    fireEvent.keyDown(document.body, { key: "a" });
    expect(heading()).toBe("API reference");
  });

  it("ignores keys another handler already used and IME composition", () => {
    renderApp("/");
    const used = new KeyboardEvent("keydown", { key: "/", bubbles: true, cancelable: true });
    used.preventDefault();
    act(() => {
      document.body.dispatchEvent(used);
    });
    act(() => {
      document.body.dispatchEvent(
        new KeyboardEvent("keydown", { key: "?", bubbles: true, isComposing: true }),
      );
    });
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("can switch single-key shortcuts off, keeping the modifier ones (AC8, WCAG 2.1.4)", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await user.keyboard("?");
    const toggle = screen.getByRole("switch", { name: "Single-key shortcuts" });
    expect(toggle).toBeChecked();
    expect(toggle).toHaveAccessibleDescription(/Shortcuts with Ctrl or ⌘ keep working/);
    await user.click(toggle);
    expect(toggle).not.toBeChecked();
    expect(window.localStorage.getItem(PREF_KEYS.hotkeys)).toBe("off");
    await user.keyboard("{Escape}");

    await user.keyboard("ge");
    expect(heading()).toBe("Home");
    await user.keyboard("/");
    await user.keyboard("?");
    expect(screen.queryByRole("dialog")).toBeNull();
    await user.keyboard("{Control>}k{/Control}");
    expect(palette()).toBeInTheDocument();
  });

  it("does not fire single-key shortcuts while a dialog is open", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await user.keyboard("?");
    const dialog = screen.getByRole("dialog", { name: "Keyboard shortcuts" });
    dialog.focus();
    await user.keyboard("ge/");
    expect(heading()).toBe("Home");
    expect(screen.getByRole("dialog", { name: "Keyboard shortcuts" })).toBeInTheDocument();
    await user.keyboard("{Control>}b{/Control}");
    expect(screen.getByTestId("sidebar")).toHaveAttribute("data-collapsed", "false");
  });
});

describe("Home overview", () => {
  it("links to the pages that work today", () => {
    renderApp("/");
    const quick = screen.getByRole("navigation", { name: "Jump in" });
    expect(
      within(quick)
        .getAllByRole("link")
        .map((l) => l.getAttribute("href")),
    ).toEqual(["/explore", "/data-sources", "/api-reference"]);
    expect(within(quick).getByRole("link", { name: /^Explore data/ })).toBeInTheDocument();
  });

  it("summarises the workspace, saying when the query API is off", async () => {
    const first = renderApp("/");
    await waitFor(() => {
      expect(screen.getByTestId("stat-Data sources")).toHaveTextContent("Query API off");
    });
    await waitFor(() => {
      expect(screen.getByTestId("stat-Source types")).toHaveTextContent("0");
    });
    expect(screen.getByTestId("stat-File formats")).toHaveTextContent("0");
    first.unmount();

    renderApp("/", (p) =>
      p === "/api/v1/plugins"
        ? {
            status: 200,
            body: { sources: [{ id: "file" }], formats: [{ id: "csv" }, { id: "json" }] },
          }
        : withSources(p),
    );
    await waitFor(() => {
      expect(screen.getByTestId("stat-Data sources")).toHaveTextContent("2");
    });
    expect(screen.getByTestId("stat-Source types")).toHaveTextContent("1");
    expect(screen.getByTestId("stat-File formats")).toHaveTextContent("2");
  });

  it("shows — instead of breaking when a body has an unexpected shape", async () => {
    renderApp("/", () => ({ status: 200, body: {} }));
    await waitFor(() => {
      expect(screen.getByTestId("server-state")).toHaveAttribute("data-state", "notReady");
    });
    for (const id of ["stat-Data sources", "stat-Source types", "stat-File formats"]) {
      expect(screen.getByTestId(id)).toHaveTextContent("—");
    }
    expect(screen.getByRole("heading", { level: 1, name: "Home" })).toBeInTheDocument();
    // A readiness body without the expected fields reads as not ready, with no checks.
    expect(screen.getByText("Degraded")).toBeInTheDocument();
    expect(screen.getByText("No dependency checks are registered yet.")).toBeInTheDocument();
    expect(screen.getByTestId("build-Version")).toHaveTextContent("—");
    expect(screen.queryByTestId("page-error")).toBeNull();
  });
});
