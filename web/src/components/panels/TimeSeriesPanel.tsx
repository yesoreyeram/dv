import { Table2 } from "lucide-react";
import {
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type PointerEvent,
} from "react";
import { FrameNotices } from "@/components/panels/FrameNotices";
import { Button } from "@/components/ui/button";
import { t } from "@/i18n/messages";
import { formatNumber, formatTime } from "@/lib/format";
import type { DataFrame } from "@/lib/frame";
import {
  MAX_SERIES,
  at,
  dashFor,
  extent,
  linear,
  nearest,
  niceYTicks,
  pathData,
  segments,
  timeOrder,
  timeSeriesShape,
  timeTicks,
  type NumberField,
} from "@/lib/timeseries";
import { cn } from "@/lib/utils";

const HEIGHT = 280;
const MARGIN = { top: 12, right: 16, bottom: 28, left: 64 };
const DEFAULT_WIDTH = 640;
const DAY = 86_400_000;

const seriesName = (f: NumberField) => f.config.displayName ?? f.name;
const numberFmt = new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 });

function timeLabelFormat(step: number): Intl.DateTimeFormat {
  if (step < 60_000) return new Intl.DateTimeFormat(undefined, { timeStyle: "medium" });
  if (step < DAY) return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" });
  if (step < 28 * DAY)
    return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });
  return new Intl.DateTimeFormat(undefined, { month: "short", year: "numeric" });
}

/** Tracks an element's content width (FR-014 AC8 sizes the decimation budget from it). */
function useWidth<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [width, setWidth] = useState(DEFAULT_WIDTH);
  useEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const measure = () => {
      if (el.clientWidth > 0) setWidth(el.clientWidth);
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => {
      ro.disconnect();
    };
  }, []);
  return [ref, width] as const;
}

/** Time-series line chart of one frame (FR-014, NFR-A11Y-004). */
export function TimeSeriesPanel({
  frame,
  onShowTable,
}: {
  frame: DataFrame;
  onShowTable: () => void;
}) {
  const shape = useMemo(() => timeSeriesShape(frame), [frame]);
  if (!shape.ok) {
    const list = (names: string[]) => (names.length ? names.join(", ") : t("ts.none"));
    return (
      <div className="flex flex-col gap-3" data-testid="timeseries-panel">
        <FrameNotices frame={frame} />
        <Unsupported
          text={t("ts.unsupported", {
            time: list(shape.timeFields),
            number: list(shape.numberFields),
          })}
          onShowTable={onShowTable}
        />
      </div>
    );
  }
  const extra =
    shape.omitted > 0
      ? [t("ts.omitted", { n: String(shape.omitted), max: String(MAX_SERIES) })]
      : [];
  return (
    <div className="flex flex-col gap-3" data-testid="timeseries-panel">
      <FrameNotices frame={frame} extra={extra} />
      <Chart shape={shape} onShowTable={onShowTable} />
    </div>
  );
}

function Unsupported({ text, onShowTable }: { text: string; onShowTable: () => void }) {
  return (
    <div className="flex flex-col items-start gap-3" data-testid="timeseries-unsupported">
      <p className="text-sm text-muted-foreground">{text}</p>
      <Button variant="outline" size="sm" onClick={onShowTable}>
        <Table2 aria-hidden="true" />
        {t("ts.showTable")}
      </Button>
    </div>
  );
}

function Chart({
  shape,
  onShowTable,
}: {
  shape: Extract<ReturnType<typeof timeSeriesShape>, { ok: true }>;
  onShowTable: () => void;
}) {
  const ids = useId();
  const [boxRef, width] = useWidth<HTMLDivElement>();
  const [hidden, setHidden] = useState<ReadonlySet<string>>(new Set());
  const [cursor, setCursor] = useState<number | null>(null);
  const [reading, setReading] = useState("");

  const { time, series } = shape;
  const order = useMemo(() => timeOrder(time), [time]);
  const visible = series.filter((f) => !hidden.has(f.name));
  const plotW = Math.max(40, width - MARGIN.left - MARGIN.right);
  const plotH = HEIGHT - MARGIN.top - MARGIN.bottom;

  const tMin = order.length ? at(time.values, at(order, 0)) : 0;
  const tMax = order.length ? at(time.values, at(order, order.length - 1)) : 0;
  const hiddenKey = [...hidden].sort().join("\u0000");
  const yExt = useMemo(
    () => extent(order, visible),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `visible` is keyed by hiddenKey
    [order, series, hiddenKey],
  );
  const y = niceYTicks(yExt?.[0] ?? 0, yExt?.[1] ?? 1);
  const xs = linear(tMin, tMax, MARGIN.left, MARGIN.left + plotW);
  const ys = linear(y.min, y.max, MARGIN.top + plotH, MARGIN.top);
  const xt = timeTicks(tMin, tMax, Math.max(2, Math.floor(plotW / 110)));
  const xFmt = timeLabelFormat(xt.step);
  const units = new Set(visible.map((f) => f.config.unit ?? ""));
  const unit = units.size === 1 ? [...units][0] : "";

  // Geometry depends only on the frame, the width and which series are visible.
  const paths = useMemo(
    () =>
      series.map((f) =>
        hidden.has(f.name) ? null : segments(order, time.values, f.values, xs, ys, plotW),
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- xs/ys are derived from the listed inputs
    [series, order, time, plotW, hiddenKey, y.min, y.max, tMin, tMax],
  );

  if (order.length === 0) {
    return <Unsupported text={t("ts.noRows")} onShowTable={onShowTable} />;
  }

  const names = series.map(seriesName);
  const summary = t("ts.summary", {
    series: visible.map(seriesName).join(", "),
    time: time.config.displayName ?? time.name,
    from: formatTime(tMin),
    to: formatTime(tMax),
    points: new Intl.NumberFormat().format(order.length),
  });

  const row = cursor === null ? null : at(order, cursor);
  const valueText = (f: NumberField, r: number) => {
    const v = f.values[r];
    return v === null || v === undefined ? "—" : formatNumber(v, f.config);
  };
  const readingFor = (r: number) =>
    t("ts.reading", {
      time: formatTime(at(time.values, r)),
      values: visible.map((f) => `${seriesName(f)} ${valueText(f, r)}`).join(", "),
    });

  const moveTo = (i: number, announce: boolean) => {
    const c = Math.max(0, Math.min(order.length - 1, i));
    setCursor(c);
    if (announce) setReading(readingFor(at(order, c)));
  };

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const cur = cursor ?? -1;
    const step = e.shiftKey ? Math.max(1, Math.round(order.length / 10)) : 1;
    const target = (
      {
        ArrowRight: cursor === null ? 0 : cur + step,
        ArrowLeft: cursor === null ? order.length - 1 : cur - step,
        Home: 0,
        End: order.length - 1,
      } as Record<string, number | undefined>
    )[e.key];
    if (e.key === "Escape") {
      setCursor(null);
      return;
    }
    if (target === undefined) return;
    e.preventDefault();
    moveTo(target, true);
  };

  const onPointerMove = (e: PointerEvent<SVGSVGElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    const px = e.clientX - rect.left;
    const tv = tMax === tMin ? tMin : tMin + ((px - MARGIN.left) / plotW) * (tMax - tMin);
    moveTo(nearest(order, time.values, tv), false);
  };

  const cx = row === null ? 0 : xs(at(time.values, row));
  const tipLeft = Math.min(Math.max(cx + 12, 0), Math.max(0, width - 220));

  return (
    <div className="flex flex-col gap-3">
      <div ref={boxRef} className="relative w-full min-w-0">
        {/* A focusable group with keyboard instructions (aria-describedby) is the plot's one
            Tab stop; role="application" would hide the page's reading keys (FR-014 AC7). */}
        {/* eslint-disable jsx-a11y/no-noninteractive-element-interactions, jsx-a11y/no-noninteractive-tabindex */}
        <div
          role="group"
          aria-label={t("ts.plot")}
          aria-describedby={`${ids}-hint`}
          // One Tab stop for the whole plot; arrow keys move the crosshair (AC7).
          tabIndex={0}
          onKeyDown={onKeyDown}
          onBlur={() => {
            setCursor(null);
          }}
          data-testid="timeseries-plot"
          className="rounded-md focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          {/* eslint-enable jsx-a11y/no-noninteractive-element-interactions, jsx-a11y/no-noninteractive-tabindex */}
          <svg
            role="img"
            aria-label={summary}
            width={width}
            height={HEIGHT}
            viewBox={`0 0 ${String(width)} ${String(HEIGHT)}`}
            className="block max-w-full touch-pan-y select-none"
            data-testid="timeseries-chart"
            onPointerMove={onPointerMove}
            onPointerLeave={() => {
              setCursor(null);
            }}
          >
            <g aria-hidden="true" className="text-[11px]">
              {y.ticks.map((v) => (
                <g key={`y${String(v)}`}>
                  <line
                    x1={MARGIN.left}
                    x2={MARGIN.left + plotW}
                    y1={ys(v)}
                    y2={ys(v)}
                    className="stroke-border"
                    strokeWidth={1}
                  />
                  <text
                    x={MARGIN.left - 8}
                    y={ys(v)}
                    dy="0.32em"
                    textAnchor="end"
                    className="fill-muted-foreground tabular-nums"
                  >
                    {unit ? formatNumber(v, { unit }) : numberFmt.format(v)}
                  </text>
                </g>
              ))}
              {xt.ticks.map((v) => (
                <text
                  key={`x${String(v)}`}
                  x={xs(v)}
                  y={MARGIN.top + plotH + 18}
                  textAnchor="middle"
                  className="fill-muted-foreground tabular-nums"
                >
                  {xFmt.format(v)}
                </text>
              ))}
              <line
                x1={MARGIN.left}
                x2={MARGIN.left + plotW}
                y1={MARGIN.top + plotH}
                y2={MARGIN.top + plotH}
                className="stroke-muted-foreground"
                strokeWidth={1}
              />
            </g>
            <g aria-hidden="true">
              {series.map((f, slot) => {
                const segs = paths[slot];
                if (!segs) return null;
                return (
                  <g
                    key={f.name}
                    className={`ts-s${String(slot + 1)}`}
                    data-testid="ts-series"
                    data-name={names[slot]}
                    data-segments={segs.length}
                  >
                    {segs.map((seg, i) =>
                      seg.length === 1 ? (
                        <circle
                          key={i}
                          className="ts-dot"
                          cx={seg[0]?.[0]}
                          cy={seg[0]?.[1]}
                          r={3}
                        />
                      ) : (
                        <path
                          key={i}
                          className="ts-line"
                          d={pathData(seg)}
                          strokeWidth={2}
                          strokeLinejoin="round"
                          strokeLinecap="round"
                          strokeDasharray={dashFor(slot)}
                        />
                      ),
                    )}
                  </g>
                );
              })}
            </g>
            {row !== null ? (
              <g aria-hidden="true">
                <line
                  x1={cx}
                  x2={cx}
                  y1={MARGIN.top}
                  y2={MARGIN.top + plotH}
                  className="stroke-muted-foreground"
                  strokeDasharray="3 3"
                />
                {series.map((f, slot) => {
                  const v = f.values[row];
                  if (hidden.has(f.name) || v === null || v === undefined) return null;
                  return (
                    <circle
                      key={f.name}
                      className={cn("ts-dot", `ts-s${String(slot + 1)}`)}
                      cx={cx}
                      cy={ys(v)}
                      r={4}
                    />
                  );
                })}
              </g>
            ) : null}
          </svg>
        </div>
        {row !== null ? (
          <div
            aria-hidden="true"
            data-testid="ts-tooltip"
            className="pointer-events-none absolute top-2 z-10 w-52 rounded-md border bg-card p-2 text-xs shadow-md"
            style={{ left: tipLeft }}
          >
            <p className="mb-1 font-medium">{formatTime(at(time.values, row))}</p>
            <ul className="flex flex-col gap-0.5">
              {series.map((f, slot) =>
                hidden.has(f.name) ? null : (
                  <li key={f.name} className="flex items-center gap-2">
                    <Swatch slot={slot} />
                    <span className="truncate">{seriesName(f)}</span>
                    <span className="ms-auto tabular-nums">{valueText(f, row)}</span>
                  </li>
                ),
              )}
            </ul>
          </div>
        ) : null}
      </div>
      <p id={`${ids}-hint`} className="sr-only">
        {t("ts.plotHint")}
      </p>
      <p className="sr-only" role="status" aria-live="polite" data-testid="ts-reading">
        {reading}
      </p>
      <ul aria-label={t("ts.legend")} className="flex flex-wrap gap-1" data-testid="ts-legend">
        {series.map((f, slot) => {
          const on = !hidden.has(f.name);
          return (
            <li key={f.name}>
              <button
                type="button"
                aria-pressed={on}
                onClick={() => {
                  setHidden((prev) => {
                    const next = new Set(prev);
                    if (next.has(f.name)) next.delete(f.name);
                    else if (series.length - next.size > 1) next.add(f.name);
                    return next;
                  });
                }}
                className={cn(
                  "flex min-h-8 items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring",
                  !on && "text-muted-foreground line-through",
                )}
              >
                <Swatch slot={slot} />
                {names[slot]}
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function Swatch({ slot }: { slot: number }) {
  return (
    <svg
      aria-hidden="true"
      width={20}
      height={8}
      className={cn("shrink-0", `ts-s${String(slot + 1)}`)}
    >
      <line
        x1={1}
        x2={19}
        y1={4}
        y2={4}
        className="ts-swatch"
        strokeWidth={3}
        strokeLinecap="round"
        strokeDasharray={dashFor(slot)}
      />
    </svg>
  );
}
