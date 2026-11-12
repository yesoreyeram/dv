import { useRef, type KeyboardEvent, type PointerEvent } from "react";
import { cn } from "@/lib/utils";

/**
 * Vertical window splitter (WAI-ARIA window-splitter pattern, FR-018 AC8). The value is the
 * width in CSS px of the pane named by `controls`. Dragging, ←/→, Home and End change it;
 * Enter or a double click restores the default (NFR-A11Y-006 AC4).
 */
export function Splitter({
  value,
  min,
  max,
  step,
  defaultValue,
  controls,
  label,
  onChange,
  onCommit,
  className,
}: {
  value: number;
  min: number;
  max: number;
  step: number;
  defaultValue: number;
  /** id of the pane this splitter resizes. */
  controls: string;
  label: string;
  /** Called while dragging and on every key step. */
  onChange: (value: number) => void;
  /** Called when a drag ends or a key step is made: the value to persist. */
  onCommit: (value: number) => void;
  className?: string;
}) {
  const drag = useRef<{ x: number; value: number; rtl: boolean; last: number } | null>(null);
  const clamp = (v: number) => Math.min(max, Math.max(min, Math.round(v)));

  const set = (v: number) => {
    const next = clamp(v);
    onChange(next);
    onCommit(next);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const rtl = getComputedStyle(e.currentTarget).direction === "rtl";
    const grow = rtl ? "ArrowLeft" : "ArrowRight";
    const shrink = rtl ? "ArrowRight" : "ArrowLeft";
    if (e.key === grow) set(value + step);
    else if (e.key === shrink) set(value - step);
    else if (e.key === "Home") set(min);
    else if (e.key === "End") set(max);
    else if (e.key === "Enter") set(defaultValue);
    else return;
    e.preventDefault();
  };

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    e.preventDefault();
    e.currentTarget.focus();
    // Not available in every environment (jsdom); dragging still works while the pointer
    // stays over the handle.
    if (typeof e.currentTarget.setPointerCapture === "function") {
      e.currentTarget.setPointerCapture(e.pointerId);
    }
    drag.current = {
      x: e.clientX,
      value,
      rtl: getComputedStyle(e.currentTarget).direction === "rtl",
      last: value,
    };
  };
  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (!d) return;
    const delta = (e.clientX - d.x) * (d.rtl ? -1 : 1);
    d.last = clamp(d.value + delta);
    onChange(d.last);
  };
  const endDrag = () => {
    const d = drag.current;
    if (!d) return;
    drag.current = null;
    onCommit(d.last);
  };

  return (
    // A focusable separator is the window-splitter widget; jsx-a11y treats the role as static.
    // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      aria-controls={controls}
      aria-valuenow={value}
      aria-valuemin={min}
      aria-valuemax={max}
      // eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex
      tabIndex={0}
      onKeyDown={onKeyDown}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
      onDoubleClick={() => {
        set(defaultValue);
      }}
      className={cn(
        "group relative cursor-col-resize touch-none justify-center focus-visible:outline-none",
        className,
      )}
    >
      <span
        aria-hidden="true"
        className="h-full w-px rounded-full bg-border transition-[width,background-color] group-hover:w-0.5 group-hover:bg-primary group-focus-visible:w-0.5 group-focus-visible:bg-primary forced-colors:bg-[CanvasText]"
      />
      <span
        aria-hidden="true"
        className="absolute top-1/2 h-8 w-1 -translate-y-1/2 rounded-full bg-border group-hover:bg-primary group-focus-visible:bg-primary forced-colors:bg-[CanvasText]"
      />
    </div>
  );
}
