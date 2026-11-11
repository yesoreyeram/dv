import { useQuery } from "@tanstack/react-query";
import { ChartLine, Play, RefreshCw, Square, Table2, TerminalSquare } from "lucide-react";
import {
  useEffect,
  useId,
  useRef,
  useState,
  type SyntheticEvent,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import { useSearchParams } from "react-router";
import { PageHeader } from "@/components/layout/PageHeader";
import { TablePanel } from "@/components/panels/TablePanel";
import { TimeSeriesPanel } from "@/components/panels/TimeSeriesPanel";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { t } from "@/i18n/messages";
import {
  dataSourcesQuery,
  pluginsQuery,
  runQuery,
  type DataSourceSummary,
  type QueryOutcome,
} from "@/lib/api";
import { parseRowLimit } from "@/lib/format";
import {
  effectiveFormat,
  isValidRowsPath,
  jsonControls,
  jsonQueryOptions,
} from "@/lib/jsonOptions";
import { cn } from "@/lib/utils";

const REF_ID = "A";

const inputClass =
  "h-9 w-full rounded-md border bg-card px-3 text-sm shadow-xs focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-ring aria-[invalid=true]:border-danger";

type RunState =
  | { kind: "idle" }
  | { kind: "running" }
  | { kind: "cancelled" }
  | { kind: "done"; outcome: QueryOutcome; durationMs: number }
  | { kind: "failed"; message: string };

function Field({
  id,
  label,
  hint,
  error,
  children,
}: {
  id: string;
  label: string;
  hint?: string;
  error?: string | null;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={id} className="text-sm font-medium">
        {label}
      </label>
      {children}
      {error ? (
        <p id={`${id}-error`} className="text-xs text-danger">
          {error}
        </p>
      ) : hint ? (
        <p id={`${id}-hint`} className="text-xs text-muted-foreground">
          {hint}
        </p>
      ) : null}
    </div>
  );
}

function QueryForm({ sources }: { sources: DataSourceSummary[] }) {
  const [params, setParams] = useSearchParams();
  const formats = useQuery(pluginsQuery).data?.formats ?? [];
  const ids = useId();

  // URL state only fills the form; nothing runs until the user asks (FR-018 AC6).
  const initialDs = params.get("ds");
  const [ds, setDs] = useState(
    sources.some((s) => s.uid === initialDs) ? (initialDs ?? "") : (sources[0]?.uid ?? ""),
  );
  const [path, setPath] = useState(params.get("path") ?? "");
  const [format, setFormat] = useState(params.get("format") ?? "");
  const [maxRows, setMaxRows] = useState(params.get("maxRows") ?? "");
  const [rowsPath, setRowsPath] = useState(params.get("rowsPath") ?? "");
  const [flatten, setFlatten] = useState(params.get("flatten") === "1");
  const [errors, setErrors] = useState<{ path?: string; maxRows?: string; rowsPath?: string }>({});
  const [run, setRun] = useState<RunState>({ kind: "idle" });
  const [frameIdx, setFrameIdx] = useState(0);
  const [view, setView] = useState<View>(parseView(params.get("view")));
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => () => abortRef.current?.abort(), []);

  // JSON options apply to the format the query will use (FR-012 AC9).
  const effFormat = effectiveFormat(format, path, formats);
  const controls = jsonControls(effFormat);

  const execute = async () => {
    const rows = parseRowLimit(maxRows);
    const next: typeof errors = {};
    if (path.trim() === "") next.path = t("explore.pathRequired");
    if (rows === null) next.maxRows = t("explore.maxRowsInvalid");
    if (controls.rowsPath && rowsPath.trim() !== "" && !isValidRowsPath(rowsPath.trim())) {
      next.rowsPath = t("explore.rowsPathInvalid");
    }
    setErrors(next);
    if (next.path || next.maxRows || next.rowsPath) return;

    const options = jsonQueryOptions(effFormat, { rowsPath, flatten });
    const search: Record<string, string> = { ds, path: path.trim() };
    if (format) search.format = format;
    if (rows !== undefined) search.maxRows = String(rows);
    if (typeof options?.rowsPath === "string") search.rowsPath = options.rowsPath;
    if (options?.flatten === true) search.flatten = "1";
    if (view === "timeseries") search.view = view;
    setParams(search, { replace: true });

    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    setRun({ kind: "running" });
    setFrameIdx(0);
    const started = performance.now();
    try {
      const outcome = await runQuery(
        {
          refId: REF_ID,
          datasource: ds,
          query: {
            path: path.trim(),
            ...(format ? { format } : {}),
            ...(options ? { options } : {}),
          },
          ...(rows === undefined || rows === null ? {} : { maxRows: rows }),
        },
        ctrl.signal,
      );
      if (ctrl.signal.aborted || abortRef.current !== ctrl) return;
      setRun({ kind: "done", outcome, durationMs: Math.round(performance.now() - started) });
    } catch (e) {
      // A newer run replaced this one (FR-018 AC3): its state belongs to that run.
      if (abortRef.current !== ctrl) return;
      if (ctrl.signal.aborted || (e instanceof DOMException && e.name === "AbortError")) {
        setRun({ kind: "cancelled" });
        return;
      }
      setRun({ kind: "failed", message: t("status.error") });
    } finally {
      if (abortRef.current === ctrl) abortRef.current = null;
    }
  };

  const onSubmit = (e: SyntheticEvent) => {
    e.preventDefault();
    void execute();
  };
  const onKeyDown = (e: KeyboardEvent<HTMLInputElement | HTMLSelectElement>) => {
    if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      void execute();
    }
  };
  // The view only changes how the result is drawn; it never re-runs the query (FR-014 AC1).
  const changeView = (v: View) => {
    setView(v);
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (v === "timeseries") next.set("view", v);
        else next.delete("view");
        return next;
      },
      { replace: true },
    );
  };
  const cancel = () => {
    abortRef.current?.abort();
    setRun({ kind: "cancelled" });
  };

  const running = run.kind === "running";
  const describedBy = (id: string, err?: string) => (err ? `${id}-error` : `${id}-hint`);
  const pathId = `${ids}-path`;
  const rowsId = `${ids}-rows`;
  const rowsPathId = `${ids}-rowspath`;
  const flattenId = `${ids}-flatten`;

  return (
    <div className="grid gap-6 lg:grid-cols-[minmax(18rem,22rem)_1fr]">
      <Card aria-labelledby={`${ids}-qt`}>
        <CardHeader className="flex-row items-start gap-3">
          <span
            aria-hidden="true"
            className="grid size-9 shrink-0 place-items-center rounded-md bg-accent text-accent-foreground"
          >
            <TerminalSquare className="size-4" />
          </span>
          <div className="flex flex-col gap-1">
            <CardTitle id={`${ids}-qt`}>{t("explore.queryTitle")}</CardTitle>
            <CardDescription>{t("explore.queryDescription")}</CardDescription>
          </div>
        </CardHeader>
        <CardContent>
          <form
            noValidate
            onSubmit={onSubmit}
            className="flex flex-col gap-4"
            aria-describedby={`${ids}-shortcut`}
          >
            <Field id={`${ids}-ds`} label={t("explore.datasource")}>
              <select
                id={`${ids}-ds`}
                className={inputClass}
                onKeyDown={onKeyDown}
                value={ds}
                onChange={(e) => {
                  setDs(e.target.value);
                }}
              >
                {sources.map((s) => (
                  <option key={s.uid} value={s.uid}>
                    {s.name}
                  </option>
                ))}
              </select>
            </Field>
            <Field
              id={pathId}
              label={t("explore.path")}
              hint={t("explore.pathHint")}
              error={errors.path ?? null}
            >
              <input
                id={pathId}
                className={cn(inputClass, "font-mono")}
                onKeyDown={onKeyDown}
                value={path}
                onChange={(e) => {
                  setPath(e.target.value);
                }}
                aria-invalid={errors.path ? true : undefined}
                aria-describedby={describedBy(pathId, errors.path)}
                aria-required="true"
                autoComplete="off"
                spellCheck={false}
                maxLength={1024}
              />
            </Field>
            <Field id={`${ids}-fmt`} label={t("explore.format")}>
              <select
                id={`${ids}-fmt`}
                className={inputClass}
                onKeyDown={onKeyDown}
                value={format}
                onChange={(e) => {
                  setFormat(e.target.value);
                }}
              >
                <option value="">{t("explore.formatAuto")}</option>
                {formats.map((f) => (
                  <option key={f.id} value={f.id}>
                    {f.name}
                  </option>
                ))}
              </select>
            </Field>
            {controls.rowsPath ? (
              <Field
                id={rowsPathId}
                label={t("explore.rowsPath")}
                hint={t("explore.rowsPathHint")}
                error={errors.rowsPath ?? null}
              >
                <input
                  id={rowsPathId}
                  className={cn(inputClass, "font-mono")}
                  onKeyDown={onKeyDown}
                  value={rowsPath}
                  onChange={(e) => {
                    setRowsPath(e.target.value);
                  }}
                  aria-invalid={errors.rowsPath ? true : undefined}
                  aria-describedby={describedBy(rowsPathId, errors.rowsPath)}
                  autoComplete="off"
                  spellCheck={false}
                  maxLength={2063}
                />
              </Field>
            ) : null}
            {controls.flatten ? (
              <div className="flex items-start gap-2">
                <input
                  id={flattenId}
                  type="checkbox"
                  className="mt-0.5 size-4 accent-primary"
                  checked={flatten}
                  onChange={(e) => {
                    setFlatten(e.target.checked);
                  }}
                  aria-describedby={`${flattenId}-hint`}
                />
                <div className="flex flex-col gap-0.5">
                  <label htmlFor={flattenId} className="text-sm font-medium">
                    {t("explore.flatten")}
                  </label>
                  <p id={`${flattenId}-hint`} className="text-xs text-muted-foreground">
                    {t("explore.flattenHint")}
                  </p>
                </div>
              </div>
            ) : null}
            <Field
              id={rowsId}
              label={t("explore.maxRows")}
              hint={t("explore.maxRowsHint")}
              error={errors.maxRows ?? null}
            >
              <input
                id={rowsId}
                className={inputClass}
                onKeyDown={onKeyDown}
                inputMode="numeric"
                value={maxRows}
                onChange={(e) => {
                  setMaxRows(e.target.value);
                }}
                aria-invalid={errors.maxRows ? true : undefined}
                aria-describedby={describedBy(rowsId, errors.maxRows)}
                autoComplete="off"
                maxLength={6}
              />
            </Field>
            <div className="flex flex-wrap gap-2">
              <Button type="submit" disabled={running}>
                <Play aria-hidden="true" />
                {running ? t("explore.running") : t("explore.run")}
              </Button>
              {running ? (
                <Button type="button" variant="outline" onClick={cancel}>
                  <Square aria-hidden="true" />
                  {t("explore.cancel")}
                </Button>
              ) : null}
            </div>
            <p id={`${ids}-shortcut`} className="text-xs text-muted-foreground">
              {t("explore.shortcut")}
            </p>
          </form>
        </CardContent>
      </Card>

      <section aria-labelledby={`${ids}-rt`} aria-busy={running} className="min-w-0">
        <Card>
          <CardHeader className="flex-row flex-wrap items-center justify-between gap-3">
            <CardTitle id={`${ids}-rt`}>{t("explore.resultTitle")}</CardTitle>
            <ViewPicker view={view} onChange={changeView} />
          </CardHeader>
          <CardContent>
            <Result
              run={run}
              frameIdx={frameIdx}
              setFrameIdx={setFrameIdx}
              view={view}
              onShowTable={() => {
                changeView("table");
              }}
            />
          </CardContent>
        </Card>
      </section>
    </div>
  );
}

type View = "table" | "timeseries";

/** Only the known value selects the chart; anything else is the table (threat model F-13). */
function parseView(raw: string | null): View {
  return raw === "timeseries" ? "timeseries" : "table";
}

function ViewPicker({ view, onChange }: { view: View; onChange: (v: View) => void }) {
  const name = useId();
  const options: { value: View; label: string; icon: ReactNode }[] = [
    { value: "table", label: t("explore.viewTable"), icon: <Table2 aria-hidden="true" /> },
    {
      value: "timeseries",
      label: t("explore.viewTimeSeries"),
      icon: <ChartLine aria-hidden="true" />,
    },
  ];
  return (
    <fieldset className="flex items-center gap-2">
      <legend className="sr-only">{t("explore.visualization")}</legend>
      <span aria-hidden="true" className="text-sm text-muted-foreground max-sm:hidden">
        {t("explore.visualization")}
      </span>
      <div className="inline-flex rounded-md border bg-muted p-0.5">
        {options.map((o) => (
          <label
            key={o.value}
            className={cn(
              "flex min-h-8 cursor-pointer items-center gap-1.5 rounded-sm px-2.5 text-sm [&_svg]:size-4",
              "has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-ring",
              view === o.value ? "bg-card font-medium shadow-xs" : "text-muted-foreground",
            )}
          >
            <input
              type="radio"
              name={name}
              value={o.value}
              checked={view === o.value}
              onChange={() => {
                onChange(o.value);
              }}
              className="sr-only"
            />
            {o.icon}
            {o.label}
          </label>
        ))}
      </div>
    </fieldset>
  );
}

function Result({
  run,
  frameIdx,
  setFrameIdx,
  view,
  onShowTable,
}: {
  run: RunState;
  frameIdx: number;
  setFrameIdx: (i: number) => void;
  view: View;
  onShowTable: () => void;
}) {
  const ids = useId();
  switch (run.kind) {
    case "idle":
      return (
        <p className="text-sm text-muted-foreground" data-testid="explore-empty">
          {t("explore.empty")}
        </p>
      );
    case "running":
      return (
        <p className="text-sm text-muted-foreground" role="status">
          {t("explore.running")}
        </p>
      );
    case "cancelled":
      return (
        <p className="text-sm text-muted-foreground" role="status">
          {t("explore.cancelled")}
        </p>
      );
    case "failed":
      return <Alert>{run.message}</Alert>;
    case "done":
      break;
  }
  const { outcome, durationMs } = run;
  if (!outcome.ok) {
    return (
      <Alert data-testid="query-error">
        <p className="font-medium">{t("explore.errorTitle")}</p>
        <p>
          <code className="font-mono text-xs">{outcome.error.code}</code>
          {outcome.error.message ? ` — ${outcome.error.message}` : null}
        </p>
      </Alert>
    );
  }
  if (outcome.frames.length === 0) {
    return <p className="text-sm text-muted-foreground">{t("explore.noFrames")}</p>;
  }
  const idx = Math.min(frameIdx, outcome.frames.length - 1);
  const frame = outcome.frames[idx];
  if (!frame) return null;
  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted-foreground" data-testid="query-stats" role="status">
        {t("explore.stats", {
          rows: new Intl.NumberFormat().format(frame.length),
          fields: new Intl.NumberFormat().format(frame.fields.length),
          queryTime:
            outcome.durationMs === null
              ? t("explore.unknownDuration")
              : t("explore.ms", { n: new Intl.NumberFormat().format(outcome.durationMs) }),
          total: t("explore.ms", { n: new Intl.NumberFormat().format(durationMs) }),
          query: frame.meta.executedQuery ?? frame.name,
        })}
      </p>
      {outcome.frames.length > 1 ? (
        // WAI-ARIA tabs pattern (FR-018 AC4): only the selected tab is in the Tab order,
        // so the arrow keys, Home and End move between frames and select them.
        <div role="tablist" aria-label={t("explore.frames")} className="flex flex-wrap gap-1">
          {outcome.frames.map((f, i) => (
            <button
              key={i}
              id={`${ids}-tab-${String(i)}`}
              type="button"
              role="tab"
              aria-selected={i === idx}
              aria-controls={`${ids}-panel`}
              tabIndex={i === idx ? 0 : -1}
              onKeyDown={(e) => {
                const n = outcome.frames.length;
                const target = (
                  {
                    ArrowRight: (idx + 1) % n,
                    ArrowDown: (idx + 1) % n,
                    ArrowLeft: (idx - 1 + n) % n,
                    ArrowUp: (idx - 1 + n) % n,
                    Home: 0,
                    End: n - 1,
                  } as Record<string, number | undefined>
                )[e.key];
                if (target === undefined) return;
                e.preventDefault();
                setFrameIdx(target);
                document.getElementById(`${ids}-tab-${String(target)}`)?.focus();
              }}
              onClick={() => {
                setFrameIdx(i);
              }}
              className={cn(
                "rounded-md px-3 py-1.5 text-sm focus-visible:outline-2 focus-visible:outline-ring",
                i === idx ? "bg-accent text-accent-foreground" : "hover:bg-muted",
              )}
            >
              {f.name || t("table.unnamed")}
            </button>
          ))}
        </div>
      ) : null}
      <div
        id={`${ids}-panel`}
        {...(outcome.frames.length > 1
          ? { role: "tabpanel", "aria-labelledby": `${ids}-tab-${String(idx)}` }
          : {})}
      >
        {view === "timeseries" ? (
          <TimeSeriesPanel key={idx} frame={frame} onShowTable={onShowTable} />
        ) : (
          <TablePanel key={idx} frame={frame} />
        )}
      </div>
    </div>
  );
}

/** Explore: ad-hoc query against a provisioned data source (FR-018). */
export function ExplorePage() {
  const { data, isPending, isError, refetch } = useQuery(dataSourcesQuery);
  let body: ReactNode;
  if (isPending) {
    body = (
      <p className="text-sm text-muted-foreground" role="status">
        {t("explore.loading")}
      </p>
    );
  } else if (isError) {
    body = (
      <Alert>
        <p>{t("explore.loadError")}</p>
        <div>
          <Button variant="outline" size="sm" onClick={() => void refetch()}>
            <RefreshCw aria-hidden="true" />
            {t("explore.retry")}
          </Button>
        </div>
      </Alert>
    );
  } else if (data.datasources === null) {
    body = (
      <Card data-testid="explore-disabled">
        <CardHeader>
          <CardTitle>{t("explore.disabledTitle")}</CardTitle>
          <CardDescription>{t("explore.disabledBody")}</CardDescription>
        </CardHeader>
      </Card>
    );
  } else if (data.datasources.length === 0) {
    body = <p className="text-sm text-muted-foreground">{t("explore.noSources")}</p>;
  } else {
    body = <QueryForm sources={data.datasources} />;
  }
  return (
    <>
      <PageHeader title={t("explore.title")} subtitle={t("explore.subtitle")} />
      {body}
    </>
  );
}
