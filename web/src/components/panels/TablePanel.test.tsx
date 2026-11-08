import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { PAGE_SIZE, TablePanel } from "@/components/panels/TablePanel";
import type { DataFrame } from "@/lib/frame";

function frame(n: number): DataFrame {
  return {
    name: "sales.csv",
    meta: { executedQuery: "samples:sales.csv" },
    length: n,
    fields: [
      {
        name: "time",
        type: "time",
        config: {},
        values: Array.from({ length: n }, (_, i) => Date.UTC(2026, 10, 1) + i * 60_000),
      },
      {
        name: "orders",
        type: "number",
        config: { displayName: "Orders" },
        values: Array.from({ length: n }, (_, i) => (i % 2 ? null : n - i)),
      },
      {
        name: "note",
        type: "string",
        config: {},
        values: Array.from({ length: n }, (_, i) => `<img src=x onerror=alert(${String(i)})>`),
      },
    ],
  };
}

const bodyRows = () => {
  const [, body] = screen.getAllByRole("rowgroup");
  if (!body) throw new Error("no tbody");
  return within(body).getAllByRole("row");
};

describe("TablePanel (FR-013)", () => {
  it("renders a captioned table with typed headers and text-only cells", () => {
    render(<TablePanel frame={frame(3)} />);
    const table = screen.getByRole("table", { name: "sales.csv, 3 rows" });
    expect(within(table).getAllByRole("columnheader")).toHaveLength(3);
    expect(screen.getByRole("columnheader", { name: /Orders/ })).toHaveTextContent("number");
    expect(bodyRows()).toHaveLength(3);
    // Markup in cell values is shown as text, never parsed (F-11).
    expect(document.querySelector("img")).toBeNull();
    expect(screen.getAllByText("<img src=x onerror=alert(0)>")).toHaveLength(1);
    const time = document.querySelector("time");
    expect(time?.getAttribute("dateTime")).toBe("2026-11-01T00:00:00.000Z");
    expect(screen.getAllByText("empty")[0]).toHaveClass("sr-only");
    expect(screen.getByRole("region", { name: "sales.csv table, scrollable" })).toHaveAttribute(
      "tabindex",
      "0",
    );
  });

  it("cycles sort ascending, descending, none with aria-sort and nulls last", async () => {
    const user = userEvent.setup();
    render(<TablePanel frame={frame(5)} />);
    const header = screen.getByRole("columnheader", { name: /Orders/ });
    const button = within(header).getByRole("button");
    const firstOrders = () => within(bodyRows()[0] ?? document.body).getAllByRole("cell")[1];
    expect(header).toHaveAttribute("aria-sort", "none");
    await user.click(button);
    expect(header).toHaveAttribute("aria-sort", "ascending");
    expect(firstOrders()).toHaveTextContent("1");
    expect(within(bodyRows()[4] ?? document.body).getAllByRole("cell")[1]).toHaveTextContent(
      "empty",
    );
    await user.click(button);
    expect(header).toHaveAttribute("aria-sort", "descending");
    expect(firstOrders()).toHaveTextContent("5");
    await user.click(button);
    expect(header).toHaveAttribute("aria-sort", "none");
  });

  it("pages large frames and keeps only one page in the DOM (NFR-PERF-005 AC1)", async () => {
    const user = userEvent.setup();
    render(<TablePanel frame={frame(100_000)} />);
    expect(bodyRows()).toHaveLength(PAGE_SIZE);
    expect(screen.getByText("Rows 1–50 of 100,000")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Previous page" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Next page" }));
    expect(screen.getByText("Rows 51–100 of 100,000")).toBeInTheDocument();
    expect(bodyRows()).toHaveLength(PAGE_SIZE);
  });

  it("shows empty states, notices and truncation", () => {
    const { rerender } = render(<TablePanel frame={{ ...frame(0) }} />);
    expect(screen.getByText("No rows")).toBeInTheDocument();
    rerender(<TablePanel frame={{ name: "", meta: {}, fields: [], length: 0 }} />);
    expect(screen.getByText("No fields")).toBeInTheDocument();
    rerender(
      <TablePanel
        frame={{
          ...frame(1),
          meta: { truncated: true, notices: [{ severity: "info", text: "Row limit 1 reached" }] },
        }}
      />,
    );
    expect(screen.getByText("Row limit 1 reached")).toBeInTheDocument();
    expect(screen.getByText("The result was truncated by a server limit.")).toBeInTheDocument();
  });
});

describe("TablePanel scroll-edge affordance (FR-013 AC8)", () => {
  it("shows decorative edge fades next to the labelled region, hidden when nothing overflows", () => {
    render(<TablePanel frame={frame(3)} />);
    const region = screen.getByRole("region");
    expect(region).toHaveAttribute("tabindex", "0");
    const wrapper = region.parentElement;
    if (!wrapper) throw new Error("no wrapper");
    for (const id of ["scroll-edge-start", "scroll-edge-end"]) {
      const fade = within(wrapper).getByTestId(id);
      expect(fade).toHaveAttribute("aria-hidden", "true");
      expect(fade).toHaveAttribute("data-visible", "false");
    }
  });
});
