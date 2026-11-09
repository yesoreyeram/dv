import { describe, expect, it } from "vitest";
import { revealInRow } from "@/lib/revealInRow";

function box(el: HTMLElement, left: number, right: number) {
  el.getBoundingClientRect = () =>
    ({ left, right, top: 0, bottom: 40, width: right - left, height: 40 }) as DOMRect;
}

function setup(scrollWidth: number, clientWidth: number) {
  const row = document.createElement("ul");
  const link = document.createElement("a");
  row.append(link);
  Object.defineProperty(row, "scrollWidth", { configurable: true, value: scrollWidth });
  Object.defineProperty(row, "clientWidth", { configurable: true, value: clientWidth });
  box(row, 8, 382);
  return { row, link };
}

describe("revealInRow (FR-005 AC7)", () => {
  it("scrolls a link clipped at the end into view with a gap for the fade", () => {
    const { row, link } = setup(700, 374);
    box(link, 346, 486);
    revealInRow(row, link);
    // 486 - (382 - 24) = 128
    expect(row.scrollLeft).toBe(128);
  });

  it("scrolls a link clipped at the start back into view", () => {
    const { row, link } = setup(700, 374);
    row.scrollLeft = 300;
    box(link, -40, 60);
    revealInRow(row, link);
    // 300 - (8 + 24 - -40) = 228
    expect(row.scrollLeft).toBe(228);
  });

  it("leaves a fully visible link and a row that does not overflow alone", () => {
    const visible = setup(700, 374);
    visible.row.scrollLeft = 50;
    box(visible.link, 100, 200);
    revealInRow(visible.row, visible.link);
    expect(visible.row.scrollLeft).toBe(50);

    const fits = setup(374, 374);
    box(fits.link, 346, 486);
    revealInRow(fits.row, fits.link);
    expect(fits.row.scrollLeft).toBe(0);
  });
});
