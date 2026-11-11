import { Info } from "lucide-react";
import { t } from "@/i18n/messages";
import type { DataFrame } from "@/lib/frame";
import { cn } from "@/lib/utils";

/** Frame notices and the truncation signal, shared by every panel (FR-013 AC6, FR-014 AC9). */
export function FrameNotices({ frame, extra = [] }: { frame: DataFrame; extra?: string[] }) {
  const notices = frame.meta.notices ?? [];
  if (!frame.meta.truncated && notices.length === 0 && extra.length === 0) return null;
  return (
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
      {extra.map((text, i) => (
        <li
          key={`x${String(i)}`}
          data-testid="panel-notice"
          className="flex items-start gap-2 rounded-md border bg-muted p-3 text-sm"
        >
          <Info aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
          {text}
        </li>
      ))}
    </ul>
  );
}
