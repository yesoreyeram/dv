import { fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { TimeSeriesPanel } from "@/components/panels/TimeSeriesPanel";
import type { DataFrame, Field } from "@/lib/frame";

const T0 = Date.UTC(2026, 10, 10);
const H = 3_600_000;
const HOSTILE = '<img src=x onerror="alert(1)">';

function cpu(extra: Field[] = []): DataFrame {
  return {
    name: "cpu.csv",
    meta: { notices: [{ severity: "info", text: "2 empty cells" }] },
    length: 4,
    fields: [
      { name: "time", type: "time", config: {}, values: [T0, T0 + H, T0 + 2 * H, T0 + 3 * H] },
      { name: "web-1", type: "number", config: { unit: "%" }, values: [16.4, 20, 25, 30] },
      { name: "db-1", type: "number", config: { unit: "%" }, values: [60, null, null, 51.8] },
      { name: HOSTILE, type: "number", config: {}, values: [1, 2, 3, 4] },
      ...extra,
    ],
  };
}

const series = () => screen.getAllByTestId("ts-series");
const renderPanel = (frame: DataFrame, onShowTable = vi.fn()) => {
  render(<TimeSeriesPanel frame={frame} onShowTable={onShowTable} />);
  return onShowTable;
};

describe("TimeSeriesPanel (FR-014)", () => {
  it("draws one series per number field with gaps for nulls (AC2, AC3)", () => {
    renderPanel(cpu());
    expect(series().map((g) => g.dataset.name)).toEqual(["web-1", "db-1", HOSTILE]);
    const db = series()[1];
    expect(db?.dataset.segments).toBe("2");
    // Two lone points between gaps are drawn as dots, not invisible zero-length paths.
    expect(db?.querySelectorAll("circle")).toHaveLength(2);
  });

  it("summarises the chart in its accessible name (NFR-A11Y-004 AC2)", () => {
    renderPanel(cpu());
    const img = screen.getByRole("img", { name: /^Line chart of web-1, db-1, / });
    expect(img.getAttribute("aria-label")).toMatch(/over time, from .+ to .+, 4 points\.$/);
  });

  it("renders data-supplied names as text, never as markup (threat model F-13)", () => {
    const { container } = render(<TimeSeriesPanel frame={cpu()} onShowTable={vi.fn()} />);
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByRole("button", { name: HOSTILE })).toBeInTheDocument();
  });

  it("shows frame notices above the chart (AC9)", () => {
    renderPanel(cpu());
    expect(screen.getByTestId("frame-notice")).toHaveTextContent("2 empty cells");
  });

  it("hides and shows series from the legend, keeping at least one (AC6)", async () => {
    const user = userEvent.setup();
    renderPanel(cpu());
    const legend = screen.getByRole("list", { name: "Series" });
    const web = within(legend).getByRole("button", { name: "web-1" });
    expect(web).toHaveAttribute("aria-pressed", "true");
    await user.click(web);
    expect(web).toHaveAttribute("aria-pressed", "false");
    expect(series().map((g) => g.dataset.name)).toEqual(["db-1", HOSTILE]);
    // Colour follows the field, not its rank: db-1 keeps slot 2.
    expect(series()[0]?.getAttribute("class")).toContain("ts-s2");
    await user.click(within(legend).getByRole("button", { name: "db-1" }));
    await user.click(within(legend).getByRole("button", { name: HOSTILE }));
    expect(series()).toHaveLength(1);
    expect(within(legend).getByRole("button", { name: HOSTILE })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await user.click(web);
    expect(series()).toHaveLength(2);
  });

  it("reads values with the keyboard and announces them (AC7, NFR-A11Y-004 AC4)", async () => {
    const user = userEvent.setup();
    renderPanel(cpu());
    const plot = screen.getByRole("group", { name: "Chart plot" });
    expect(plot).toHaveAccessibleDescription(/arrow keys/);
    plot.focus();
    await user.keyboard("{ArrowRight}");
    const reading = screen.getByTestId("ts-reading");
    expect(reading).toHaveTextContent(/web-1 16\.4 %, db-1 60 %/);
    await user.keyboard("{ArrowRight}");
    expect(reading).toHaveTextContent(/web-1 20 %, db-1 —/);
    expect(screen.getByTestId("ts-tooltip")).toHaveTextContent("—");
    await user.keyboard("{End}");
    expect(reading).toHaveTextContent(/db-1 51\.8 %/);
    await user.keyboard("{ArrowRight}");
    expect(reading).toHaveTextContent(/db-1 51\.8 %/);
    await user.keyboard("{Home}");
    expect(reading).toHaveTextContent(/web-1 16\.4 %/);
    await user.keyboard("{ArrowLeft}");
    expect(reading).toHaveTextContent(/web-1 16\.4 %/);
    await user.keyboard("{Escape}");
    expect(screen.queryByTestId("ts-tooltip")).toBeNull();
    await user.keyboard("{ArrowLeft}");
    expect(reading).toHaveTextContent(/db-1 51\.8 %/);
    await user.keyboard("{Shift>}{ArrowLeft}{/Shift}");
    expect(reading).toHaveTextContent(/web-1 25 %/);
  });

  it("shows a crosshair tooltip on hover and hides it on leave (AC7)", () => {
    renderPanel(cpu());
    const svg = screen.getByTestId("timeseries-chart");
    fireEvent.pointerMove(svg, { clientX: 2000, clientY: 50 });
    expect(screen.getByTestId("ts-tooltip")).toHaveTextContent(/51\.8 %/);
    // Hover does not flood the live region; only keyboard moves are announced.
    expect(screen.getByTestId("ts-reading")).toHaveTextContent("");
    fireEvent.pointerLeave(svg);
    expect(screen.queryByTestId("ts-tooltip")).toBeNull();
  });

  it("explains why a frame cannot be drawn and offers the table (AC2)", async () => {
    const user = userEvent.setup();
    const onShowTable = renderPanel({
      name: "hosts.tsv",
      meta: {},
      length: 1,
      fields: [
        { name: "host", type: "string", config: {}, values: ["a"] },
        { name: "cores", type: "number", config: {}, values: [4] },
      ],
    });
    expect(screen.getByTestId("timeseries-unsupported")).toHaveTextContent(
      "Time fields: none. Number fields: cores.",
    );
    expect(screen.queryByRole("img")).toBeNull();
    await user.click(screen.getByRole("button", { name: "Show as table" }));
    expect(onShowTable).toHaveBeenCalledOnce();
  });

  it("explains a frame whose times are all null", () => {
    renderPanel({
      name: "x",
      meta: {},
      length: 1,
      fields: [
        { name: "time", type: "time", config: {}, values: [null] },
        { name: "v", type: "number", config: {}, values: [1] },
      ],
    });
    expect(screen.getByTestId("timeseries-unsupported")).toHaveTextContent("No rows have a time");
  });

  it("lists number fields beyond the series limit in a notice (AC4)", () => {
    const extra: Field[] = Array.from({ length: 7 }, (_, i) => ({
      name: `x${String(i)}`,
      type: "number" as const,
      config: {},
      values: [1, 2, 3, 4],
    }));
    renderPanel(cpu(extra));
    expect(series()).toHaveLength(8);
    expect(screen.getByTestId("panel-notice")).toHaveTextContent(
      "2 more number fields are not drawn (at most 8); see the table.",
    );
    // Slots 5–8 carry a dash pattern so identity is not colour alone.
    expect(series()[4]?.querySelector("path")?.getAttribute("stroke-dasharray")).toBe("6 3");
    expect(series()[0]?.querySelector("path")?.getAttribute("stroke-dasharray")).toBeNull();
  });
});
