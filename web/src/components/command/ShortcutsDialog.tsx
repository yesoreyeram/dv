import { X } from "lucide-react";
import { Fragment, useId, type ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { Kbd } from "@/components/ui/kbd";
import { Modal } from "@/components/ui/modal";
import { t } from "@/i18n/messages";
import { isApplePlatform } from "@/lib/hotkeys";
import { setSingleKeyShortcuts, useSingleKeyShortcuts } from "@/lib/prefs";
import { ROUTES } from "@/lib/routes";

interface Row {
  label: string;
  /** Alternatives; each is a sequence of key caps. */
  keys: string[][];
  /** Word between the caps of one alternative ("then" for sequences). */
  joiner?: string;
}

function Keys({ row }: { row: Row }) {
  return (
    <span className="flex flex-wrap items-center justify-end gap-1">
      {row.keys.map((combo, i) => (
        <Fragment key={combo.join("+")}>
          {i > 0 ? (
            <span className="text-xs text-muted-foreground">{t("shortcuts.or")}</span>
          ) : null}
          {combo.map((k, j) => (
            <Fragment key={k}>
              {j > 0 && row.joiner ? (
                <span className="text-xs text-muted-foreground">{row.joiner}</span>
              ) : null}
              <Kbd>{k}</Kbd>
            </Fragment>
          ))}
        </Fragment>
      ))}
    </span>
  );
}

function Section({ title, rows, children }: { title: string; rows: Row[]; children?: ReactNode }) {
  return (
    <section className="flex flex-col gap-1.5">
      <h3 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{title}</h3>
      <dl className="flex flex-col">
        {rows.map((row) => (
          <div
            key={row.label}
            className="flex min-h-8 items-center justify-between gap-4 border-b py-1 last:border-b-0"
          >
            <dt className="text-sm">{row.label}</dt>
            <dd>
              <Keys row={row} />
            </dd>
          </div>
        ))}
      </dl>
      {children}
    </section>
  );
}

/** Keyboard shortcuts help, with the switch for single-key shortcuts (FR-008 AC6, AC8). */
export function ShortcutsDialog({ onClose }: { onClose: () => void }) {
  const ids = useId();
  const singleKeys = useSingleKeyShortcuts();
  const mod = isApplePlatform() ? "⌘" : "Ctrl";

  const general: Row[] = [
    { label: t("shortcuts.palette"), keys: [[mod, "K"], ["/"]] },
    { label: t("shortcuts.sidebar"), keys: [[mod, "B"]] },
    { label: t("shortcuts.help"), keys: [["?"]] },
  ];
  const navigation: Row[] = ROUTES.map((r) => ({
    label: t("palette.goto", { page: t(r.label) }),
    keys: [["G", r.gotoKey.toUpperCase()]],
    joiner: t("shortcuts.then"),
  }));
  const explore: Row[] = [
    { label: t("shortcuts.run"), keys: [[mod, "Enter"]] },
    { label: t("shortcuts.resize"), keys: [["←"], ["→"], ["Home"], ["End"], ["Enter"]] },
  ];

  return (
    <Modal onClose={onClose} labelledBy={`${ids}-title`} testId="shortcuts-dialog">
      <div className="flex items-center justify-between gap-2 border-b px-4 py-2.5">
        <h2 id={`${ids}-title`} className="text-sm font-semibold">
          {t("shortcuts.title")}
        </h2>
        <Button variant="ghost" size="icon" onClick={onClose} aria-label={t("shortcuts.close")}>
          <X aria-hidden="true" />
        </Button>
      </div>
      <div className="flex max-h-[min(32rem,65vh)] flex-col gap-5 overflow-y-auto px-4 py-4">
        <Section title={t("shortcuts.general")} rows={general} />
        <Section title={t("shortcuts.navigation")} rows={navigation} />
        <Section title={t("shortcuts.explore")} rows={explore} />
        <div className="flex items-start gap-2.5 rounded-md border bg-muted/40 p-3">
          <input
            id={`${ids}-single`}
            type="checkbox"
            role="switch"
            checked={singleKeys}
            onChange={(e) => {
              setSingleKeyShortcuts(e.target.checked);
            }}
            aria-describedby={`${ids}-single-hint`}
            className="mt-0.5 size-4 accent-primary"
          />
          <div className="flex flex-col gap-0.5">
            <label htmlFor={`${ids}-single`} className="text-sm font-medium">
              {t("shortcuts.singleKeys")}
            </label>
            <p id={`${ids}-single-hint`} className="text-xs text-muted-foreground">
              {t("shortcuts.singleKeysHint")}
            </p>
          </div>
        </div>
      </div>
    </Modal>
  );
}
