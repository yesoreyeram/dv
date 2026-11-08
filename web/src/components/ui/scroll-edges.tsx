import { cn } from "@/lib/utils";
import type { ScrollEdges } from "@/lib/useScrollEdges";

/**
 * Decorative fades on the logical edges of a horizontal scroller that hide
 * content (FR-005 AC7, FR-013 AC8, NFR-A11Y-005). Place inside a `relative`
 * wrapper around the scroller. `from` is the background colour utility the
 * fade starts from, for example `from-card`. Under forced colours the
 * gradient is dropped by the browser, so a solid edge line is shown instead.
 */
export function ScrollEdgeShadows({
  edges,
  from,
  className,
}: {
  edges: ScrollEdges;
  from: string;
  className?: string;
}) {
  const base = cn(
    "pointer-events-none absolute inset-y-px w-6 to-transparent transition-opacity motion-reduce:transition-none forced-colors:w-0 forced-colors:border-[CanvasText]",
    from,
    className,
  );
  return (
    <>
      <span
        aria-hidden="true"
        data-testid="scroll-edge-start"
        data-visible={edges.start}
        className={cn(
          base,
          "start-px bg-linear-to-r rtl:bg-linear-to-l forced-colors:border-s-2",
          edges.start ? "opacity-100" : "opacity-0",
        )}
      />
      <span
        aria-hidden="true"
        data-testid="scroll-edge-end"
        data-visible={edges.end}
        className={cn(
          base,
          "end-px bg-linear-to-l rtl:bg-linear-to-r forced-colors:border-e-2",
          edges.end ? "opacity-100" : "opacity-0",
        )}
      />
    </>
  );
}
