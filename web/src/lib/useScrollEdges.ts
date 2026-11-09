import { useCallback, useLayoutEffect, useRef, useState, type RefObject } from "react";

/** Which logical edges of a horizontal scroller hide content (FR-005 AC7, FR-013 AC8). */
export interface ScrollEdges {
  /** Content is hidden before the inline-start edge (left in LTR, right in RTL). */
  start: boolean;
  /** Content is hidden after the inline-end edge. */
  end: boolean;
}

/** Sub-pixel slack so fractional layouts do not leave a 1 px "overflow". */
const SLACK = 1;

/**
 * Computes the edges of `el` that hide content. In RTL, Chromium, Firefox and
 * Safari report `scrollLeft` from 0 down to -max, so the distance from the
 * start edge is |scrollLeft| in both directions.
 */
export function measureEdges(el: HTMLElement): ScrollEdges {
  const max = el.scrollWidth - el.clientWidth;
  if (max <= SLACK) return { start: false, end: false };
  const fromStart = Math.abs(el.scrollLeft);
  return { start: fromStart > SLACK, end: fromStart < max - SLACK };
}

/**
 * Tracks which edges of a horizontally scrolling element hide content.
 * Scroll and resize events are passive and coalesced into one animation
 * frame; state only changes when a flag flips (F-02 threat model, D).
 * `deps` re-measures after content changes, such as a new frame.
 */
export function useScrollEdges<T extends HTMLElement>(
  deps: readonly unknown[] = [],
): [RefObject<T | null>, ScrollEdges] {
  const ref = useRef<T | null>(null);
  const [edges, setEdges] = useState<ScrollEdges>({ start: false, end: false });

  const update = useCallback(() => {
    const el = ref.current;
    if (!el) return;
    const next = measureEdges(el);
    setEdges((prev) => (prev.start === next.start && prev.end === next.end ? prev : next));
  }, []);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    update();
    let frame = 0;
    const schedule = () => {
      if (frame) return;
      frame = requestAnimationFrame(() => {
        frame = 0;
        update();
      });
    };
    el.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule, { passive: true });
    let observer: ResizeObserver | undefined;
    if (typeof ResizeObserver !== "undefined") {
      observer = new ResizeObserver(schedule);
      observer.observe(el);
      for (const child of Array.from(el.children)) observer.observe(child);
    }
    return () => {
      if (frame) cancelAnimationFrame(frame);
      el.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      observer?.disconnect();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- callers pass content deps
  }, [update, ...deps]);

  return [ref, edges];
}
