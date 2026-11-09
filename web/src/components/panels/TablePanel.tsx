import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  ChevronLeft,
  ChevronRight,
  Download,
  Info,
} from "lucide-react";
import { useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { t } from "@/i18n/messages";
import { downloadFrameCSV } from "@/lib/csv";
import { formatCell, sortedRowOrder, type SortDirection } from "@/lib/format";
import type { DataFrame, Field } from "@/lib/frame";
import { cn } from "@/lib/utils";

/** Rows per page; only one page is ever in the DOM (FR-013 AC4, NFR-PERF-005). */
export const PAGE_SIZE = 50;

interface Sort {
  field: number;
  direction: SortDirection;
}

function nextSort(current: Sort | null, field: number): Sort | null {
  if (current?.field !== field) return { field, direction: "ascending" };
  return current.direction === "ascending" ? { field, direction: "descending" } : null;
}

const numberFormat = new Intl.NumberFormat();
const fmt = (n: number) => numberFormat.format(n);

function Cell({ field, row }: { field: Field; row: number }) {
  const v = field.values[row];
  const numeric = field.type === "number" || field.type === "time";
  const cls = cn("whitespace-nowrap px-3 py-2", numeric && "text-right tabular-nums");
  if (v === null || v === undefined) {
    return (
      <td className={cn(cls, "text-muted-foreground")}>
        <span aria-hidden="true">—</span>
        <span className="sr-only">{t("table.empty")}</span>
      </td>
    );
  }
  const text = formatCell(field, v);
  if (field.type === "time") {
    return (
      <td className={cls}>
        <time dateTime={new Date(v as number).toISOString()}>{text}</time>
      </td>
    );
  }
  return (
    <td className={cn(cls, field.type === "json" && "font-mono text-xs")} title={text}>
      {text}
    </td>
  );
}

function SortIcon({ direction }: { direction: SortDirection | undefined }) {
  if (direction === "ascending") return <ArrowUp aria-hidden="true" />;
  if (direction === "descending") return <ArrowDown aria-hidden="true" />;
  return <ArrowUpDown aria-hidden="true" className="opacity-40" />;
}

/** Renders one data frame as an accessible, sortable, paged table (FR-013). */
export function TablePanel({ frame }: { frame: DataFrame }) {
  const [sort, setSort] = useState<Sort | null>(null);
  const [page, setPage] = useState(0);
  const name = frame.name || t("table.unnamed");

  const order = useMemo(() => {
    const field = sort ? frame.fields[sort.field] : undefined;
    return sort && field ? sortedRowOrder(field, sort.direction) : null;
  }, [frame, sort]);

  const pages = Math.max(1, Math.ceil(frame.length / PAGE_SIZE));
  const current = Math.min(page, pages - 1);
  const start = current * PAGE_SIZE;
  const end = Math.min(start + PAGE_SIZE, frame.length);
  const rows: number[] = [];
  for (let i = start; i < end; i++) rows.push(order ? (order[i] ?? i) : i);

  const notices = frame.meta.notices ?? [];

  return (
    <div className="flex flex-col gap-3" data-testid="table-panel">
      {frame.meta.truncated || notices.length > 0 ? (
        <ul className="flex flex-col gap-2" aria-label={t("explore.resultTitle")}>
          {frame.meta.truncated && !notices.some((n) => n.severity === "warning") ? (
            <li className="flex items-start gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-sm">
              <Info aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
              {t("table.truncated")}
            </li>
          ) : null}
          {notices.map((n, i) => (
            <li
              key={i}
              data-testid="frame-notice"
              className={cn(
                "flex items-start gap-2 rounded-md border p-3 text-sm",
                n.severity === "warning" ? "border-amber-500/40 bg-amber-500/10" : "bg-muted",
              )}
            >
              <Info aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
              {n.text}
            </li>
          ))}
        </ul>
      ) : null}

      {frame.fields.length > 0 ? (
        <div className="flex justify-end">
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              // Every row, in the current sort order (FR-018 AC7, NFR-SEC-018).
              downloadFrameCSV(frame, order);
            }}
          >
            <Download aria-hidden="true" />
            {t("table.downloadCSV")}
          </Button>
        </div>
      ) : null}

      {frame.fields.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t("table.noFields")}</p>
      ) : (
        <div
          className="overflow-x-auto rounded-md border focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          role="region"
          aria-label={t("table.scrollRegion", { name })}
          // A scrollable region must be keyboard focusable (WCAG 2.1.1, axe
          // scrollable-region-focusable), so arrow keys can scroll wide tables.
          // eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex
          tabIndex={0}
        >
          <table className="w-full border-collapse text-sm">
            <caption className="sr-only">
              {t("table.caption", { name, rows: fmt(frame.length) })}
            </caption>
            <thead className="bg-muted/60">
              <tr>
                {frame.fields.map((f, i) => {
                  const label = f.config.displayName ?? f.name;
                  const dir = sort?.field === i ? sort.direction : undefined;
                  const numeric = f.type === "number" || f.type === "time";
                  return (
                    <th
                      key={f.name}
                      scope="col"
                      aria-sort={dir ?? "none"}
                      className={cn(
                        "border-b px-1 py-1 font-medium",
                        numeric ? "text-right" : "text-left",
                      )}
                    >
                      <button
                        type="button"
                        onClick={() => {
                          setSort((s) => nextSort(s, i));
                          setPage(0);
                        }}
                        className={cn(
                          "inline-flex w-full items-center gap-1.5 rounded px-2 py-1 hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring [&_svg]:size-3.5",
                          numeric && "flex-row-reverse",
                        )}
                      >
                        <span className="flex flex-col leading-tight">
                          <span>{label}</span>
                          <span className="text-xs font-normal text-muted-foreground">
                            {f.type}
                          </span>
                        </span>
                        <SortIcon direction={dir} />
                      </button>
                    </th>
                  );
                })}
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 ? (
                <tr>
                  <td
                    colSpan={frame.fields.length}
                    className="px-3 py-6 text-center text-muted-foreground"
                  >
                    {t("table.noRows")}
                  </td>
                </tr>
              ) : (
                rows.map((r) => (
                  <tr key={r} className="border-b last:border-b-0 hover:bg-muted/40">
                    {frame.fields.map((f) => (
                      <Cell key={f.name} field={f} row={r} />
                    ))}
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      )}

      {frame.length > 0 ? (
        <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
          <p aria-live="polite" className="text-muted-foreground">
            {t("table.page", { from: fmt(start + 1), to: fmt(end), total: fmt(frame.length) })}
          </p>
          {pages > 1 ? (
            <div className="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={current === 0}
                onClick={() => {
                  setPage(current - 1);
                }}
              >
                <ChevronLeft aria-hidden="true" />
                {t("table.previous")}
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={current >= pages - 1}
                onClick={() => {
                  setPage(current + 1);
                }}
              >
                {t("table.next")}
                <ChevronRight aria-hidden="true" />
              </Button>
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
