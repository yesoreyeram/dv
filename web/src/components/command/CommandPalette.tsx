import { useQuery } from "@tanstack/react-query";
import { CornerDownLeft, Search } from "lucide-react";
import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { useNavigate } from "react-router";
import { Kbd } from "@/components/ui/kbd";
import { Modal } from "@/components/ui/modal";
import { t, type MessageKey } from "@/i18n/messages";
import { dataSourcesQuery } from "@/lib/api";
import {
  filterCommands,
  groupCommands,
  MAX_QUERY_LENGTH,
  normalizeQuery,
  type Command,
  type CommandGroup,
} from "@/lib/commands";
import { historySearch, restoreToken, useQueryHistory } from "@/lib/history";
import { isApplePlatform } from "@/lib/hotkeys";
import { useSidebar } from "@/lib/prefs";
import { ROUTES } from "@/lib/routes";
import { useTheme } from "@/lib/useTheme";
import { cn } from "@/lib/utils";

const GROUP_LABEL: Record<CommandGroup, MessageKey> = {
  goto: "palette.group.goto",
  explore: "palette.group.explore",
  recent: "palette.group.recent",
  preferences: "palette.group.preferences",
  help: "palette.group.help",
};

/** Recent queries listed before the user types anything; a search covers all of them. */
const RECENT_SHOWN = 5;

const groupLabel = (group: CommandGroup) => t(GROUP_LABEL[group]);

/** Command palette (FR-008): a modal combobox over every page and frequent action. */
export function CommandPalette({
  onClose,
  onShortcuts,
}: {
  onClose: () => void;
  onShortcuts: () => void;
}) {
  const navigate = useNavigate();
  const { theme, preference, setPreference } = useTheme();
  const sidebar = useSidebar();
  const sources = useQuery(dataSourcesQuery).data?.datasources ?? [];
  const history = useQueryHistory();
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const ids = useId();
  const mod = isApplePlatform() ? "⌘" : "Ctrl";
  const searching = normalizeQuery(query) !== "";

  const openExplore = (search: string) => {
    void navigate({ pathname: "/explore", search }, { state: { restore: restoreToken() } });
  };

  const commands: Command[] = [
    ...ROUTES.map((r): Command => ({
      id: `goto:${r.to}`,
      group: "goto",
      title: t("palette.goto", { page: t(r.label) }),
      shortcut: ["G", r.gotoKey.toUpperCase()],
      run: () => void navigate(r.to),
    })),
    ...sources.map((s): Command => ({
      id: `explore:${s.uid}`,
      group: "explore",
      title: t("palette.explore", { name: s.name }),
      detail: s.type,
      keywords: `${s.uid} query`,
      run: () => {
        openExplore(new URLSearchParams({ ds: s.uid }).toString());
      },
    })),
    ...(searching ? history : history.slice(0, RECENT_SHOWN)).map((h, i): Command => ({
      id: `recent:${String(i)}`,
      group: "recent",
      title: h.path,
      detail: sources.find((s) => s.uid === h.ds)?.name ?? h.ds,
      keywords: `${h.ds} ${h.format ?? ""} history`,
      run: () => {
        openExplore(historySearch(h));
      },
    })),
    {
      id: "theme:toggle",
      group: "preferences",
      title: theme === "dark" ? t("theme.toLight") : t("theme.toDark"),
      keywords: "appearance color colour mode",
      run: () => {
        setPreference(theme === "dark" ? "light" : "dark");
      },
    },
    ...(preference === "system"
      ? []
      : [
          {
            id: "theme:system",
            group: "preferences",
            title: t("theme.system"),
            keywords: "appearance color colour mode os automatic",
            run: () => {
              setPreference("system");
            },
          } satisfies Command,
        ]),
    {
      id: "sidebar:toggle",
      group: "preferences",
      title: sidebar.collapsed ? t("shell.expandSidebar") : t("shell.collapseSidebar"),
      keywords: "navigation rail menu",
      shortcut: [mod, "B"],
      run: sidebar.toggle,
    },
    {
      id: "help:shortcuts",
      group: "help",
      title: t("shortcuts.title"),
      keywords: "keys hotkeys help",
      shortcut: ["?"],
      run: onShortcuts,
    },
  ];

  const groups = groupCommands(filterCommands(commands, query, groupLabel));
  const flat = groups.flatMap(([, items]) => items);
  const index = Math.min(active, flat.length - 1);
  const positions = new Map(flat.map((cmd, i) => [cmd.id, i]));
  const optionId = (i: number) => `${ids}-opt-${String(i)}`;
  const listId = `${ids}-list`;

  useEffect(() => {
    const el = document.getElementById(`${ids}-opt-${String(index)}`);
    // jsdom has no layout, so no scrollIntoView.
    if (el && "scrollIntoView" in el) el.scrollIntoView({ block: "nearest" });
  }, [ids, index]);

  const run = (cmd: Command) => {
    // Close first: focus goes back to the opener before the command moves on.
    onClose();
    cmd.run();
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    const n = flat.length;
    if (e.key === "Enter") {
      e.preventDefault();
      const cmd = flat[index];
      if (cmd) run(cmd);
      return;
    }
    if (n === 0) return;
    const next = (
      {
        ArrowDown: (index + 1) % n,
        ArrowUp: (index - 1 + n) % n,
        Home: 0,
        End: n - 1,
      } as Record<string, number | undefined>
    )[e.key];
    if (next === undefined) return;
    e.preventDefault();
    setActive(next);
  };

  return (
    <Modal
      onClose={onClose}
      label={t("palette.title")}
      initialFocus={inputRef}
      testId="command-palette"
    >
      <div className="flex items-center gap-2 border-b px-3">
        <Search aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
        <input
          ref={inputRef}
          role="combobox"
          aria-label={t("palette.title")}
          aria-expanded={flat.length > 0}
          aria-autocomplete="list"
          aria-controls={listId}
          {...(flat.length > 0 ? { "aria-activedescendant": optionId(index) } : {})}
          placeholder={t("palette.placeholder")}
          value={query}
          maxLength={MAX_QUERY_LENGTH}
          onChange={(e) => {
            setQuery(e.target.value);
            setActive(0);
          }}
          onKeyDown={onKeyDown}
          autoComplete="off"
          spellCheck={false}
          className="h-11 min-w-0 flex-1 bg-transparent text-sm placeholder:text-muted-foreground focus:outline-none"
        />
        <Kbd aria-hidden="true">Esc</Kbd>
      </div>

      {flat.length === 0 ? (
        <p role="status" className="px-4 py-8 text-center text-sm text-muted-foreground">
          {t("palette.noResults", { query: query.trim() })}
        </p>
      ) : (
        <div
          id={listId}
          role="listbox"
          aria-label={t("palette.results")}
          className="max-h-[min(24rem,55vh)] overflow-y-auto p-1.5"
        >
          {groups.map(([group, items]) => (
            <div key={group} role="group" aria-labelledby={`${ids}-g-${group}`}>
              <div
                id={`${ids}-g-${group}`}
                className="px-2 pt-2 pb-1 text-xs font-medium tracking-wide text-muted-foreground uppercase"
              >
                {groupLabel(group)}
              </div>
              {items.map((cmd) => {
                const i = positions.get(cmd.id) ?? 0;
                const selected = i === index;
                return (
                  // The option is operated from the search field (aria-activedescendant):
                  // Enter there runs it, so the row itself takes pointer input only.
                  // eslint-disable-next-line jsx-a11y/click-events-have-key-events
                  <div
                    key={cmd.id}
                    id={optionId(i)}
                    role="option"
                    aria-selected={selected}
                    tabIndex={-1}
                    onPointerMove={() => {
                      if (!selected) setActive(i);
                    }}
                    onClick={() => {
                      run(cmd);
                    }}
                    className={cn(
                      "flex min-h-8 cursor-pointer items-center gap-2 rounded-md px-2 text-sm",
                      selected && "bg-accent text-accent-foreground",
                    )}
                  >
                    <span data-testid="command-title" className="truncate">
                      {cmd.title}
                    </span>
                    {cmd.detail ? (
                      <span className="truncate text-xs text-muted-foreground">{cmd.detail}</span>
                    ) : null}
                    {cmd.shortcut ? (
                      <span aria-hidden="true" className="ms-auto flex shrink-0 gap-1 ps-2">
                        {cmd.shortcut.map((k) => (
                          <Kbd key={k}>{k}</Kbd>
                        ))}
                      </span>
                    ) : null}
                  </div>
                );
              })}
            </div>
          ))}
        </div>
      )}

      <div
        aria-hidden="true"
        className="flex items-center gap-4 border-t bg-muted/40 px-3 py-1.5 text-xs text-muted-foreground"
      >
        <span className="flex items-center gap-1">
          <Kbd>↑</Kbd>
          <Kbd>↓</Kbd>
          {t("palette.hintNavigate")}
        </span>
        <span className="flex items-center gap-1">
          <Kbd>
            <CornerDownLeft className="size-3" />
          </Kbd>
          {t("palette.hintRun")}
        </span>
        <span className="flex items-center gap-1">
          <Kbd>Esc</Kbd>
          {t("palette.hintClose")}
        </span>
      </div>
    </Modal>
  );
}
