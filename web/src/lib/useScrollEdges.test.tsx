import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ScrollEdgeShadows } from "@/components/ui/scroll-edges";
import { measureEdges, useScrollEdges } from "@/lib/useScrollEdges";

/** jsdom has no layout, so scroll geometry is set by hand. */
function geometry(el: HTMLElement, scrollWidth: number, clientWidth: number, scrollLeft = 0) {
  Object.defineProperty(el, "scrollWidth", { configurable: true, value: scrollWidth });
  Object.defineProperty(el, "clientWidth", { configurable: true, value: clientWidth });
  el.scrollLeft = scrollLeft;
}

describe("measureEdges (FR-005 AC7, FR-013 AC8)", () => {
  const el = document.createElement("div");

  it("reports no edges when nothing overflows, including sub-pixel overflow", () => {
    geometry(el, 300, 300);
    expect(measureEdges(el)).toEqual({ start: false, end: false });
    geometry(el, 301, 300);
    expect(measureEdges(el)).toEqual({ start: false, end: false });
  });

  it("reports the end at the start, both in the middle, the start at the end", () => {
    geometry(el, 800, 300, 0);
    expect(measureEdges(el)).toEqual({ start: false, end: true });
    geometry(el, 800, 300, 250);
    expect(measureEdges(el)).toEqual({ start: true, end: true });
    geometry(el, 800, 300, 500);
    expect(measureEdges(el)).toEqual({ start: true, end: false });
  });

  it("uses the distance from the start edge in RTL (negative scrollLeft)", () => {
    geometry(el, 800, 300, -500);
    expect(measureEdges(el)).toEqual({ start: true, end: false });
    geometry(el, 800, 300, -100);
    expect(measureEdges(el)).toEqual({ start: true, end: true });
  });
});

function Probe({ dep }: { dep: number }) {
  const [ref, edges] = useScrollEdges<HTMLDivElement>([dep]);
  return (
    <div className="relative">
      <div ref={ref} data-testid="scroller">
        <span>content</span>
      </div>
      <ScrollEdgeShadows edges={edges} from="from-card" />
    </div>
  );
}

describe("useScrollEdges", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("updates once per animation frame on scroll and on content change", () => {
    const frames: FrameRequestCallback[] = [];
    vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => frames.push(cb));
    vi.stubGlobal("cancelAnimationFrame", vi.fn());
    const { rerender } = render(<Probe dep={1} />);
    const start = screen.getByTestId("scroll-edge-start");
    const end = screen.getByTestId("scroll-edge-end");
    expect(start).toHaveAttribute("data-visible", "false");
    expect(end).toHaveAttribute("data-visible", "false");
    expect(start).toHaveAttribute("aria-hidden", "true");

    // New content makes the region overflow: re-measured on the dependency change.
    geometry(screen.getByTestId("scroller"), 900, 300, 0);
    rerender(<Probe dep={2} />);
    expect(end).toHaveAttribute("data-visible", "true");
    expect(start).toHaveAttribute("data-visible", "false");

    // Several scroll events in one frame schedule one measurement.
    const scroller = screen.getByTestId("scroller");
    scroller.scrollLeft = 600;
    scroller.dispatchEvent(new Event("scroll"));
    scroller.dispatchEvent(new Event("scroll"));
    window.dispatchEvent(new Event("resize"));
    expect(frames).toHaveLength(1);
    act(() => {
      frames.shift()?.(0);
    });
    expect(start).toHaveAttribute("data-visible", "true");
    expect(end).toHaveAttribute("data-visible", "false");
  });

  it("observes size changes and cleans up on unmount", () => {
    const observe = vi.fn();
    const disconnect = vi.fn();
    const cancel = vi.fn();
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe = observe;
        disconnect = disconnect;
      },
    );
    vi.stubGlobal("requestAnimationFrame", () => 7);
    vi.stubGlobal("cancelAnimationFrame", cancel);
    const { unmount } = render(<Probe dep={1} />);
    // The scroller and its child are observed.
    expect(observe).toHaveBeenCalledTimes(2);
    screen.getByTestId("scroller").dispatchEvent(new Event("scroll"));
    unmount();
    expect(disconnect).toHaveBeenCalled();
    expect(cancel).toHaveBeenCalledWith(7);
  });
});
