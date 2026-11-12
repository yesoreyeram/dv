/** Command palette model and search (FR-008). Pure functions; the component adds the UI. */

export type CommandGroup = "goto" | "explore" | "recent" | "preferences" | "help";

export interface Command {
  id: string;
  group: CommandGroup;
  title: string;
  /** Secondary text shown after the title (for example the data source of a recent query). */
  detail?: string;
  /** Extra words the search matches but the row does not show. */
  keywords?: string;
  /** Key caps shown at the end of the row, for example ["G", "H"]. */
  shortcut?: readonly string[];
  run: () => void;
}

export const MAX_QUERY_LENGTH = 100;

/** Trims, lower-cases and caps the search text (FR-008 AC4). */
export function normalizeQuery(raw: string): string {
  return raw.slice(0, MAX_QUERY_LENGTH).trim().toLowerCase();
}

/**
 * Rank of a command for a query, lower is better, `null` when it does not match.
 * 0: the title starts with the query; 1: a word of the title starts with it; 2: the title
 * contains it; 3: every term is somewhere in the title, detail, group label or keywords.
 * Plain substring tests only: no regular expression is built from user or server text.
 */
export function rankCommand(cmd: Command, query: string, groupLabel: string): number | null {
  if (query === "") return 0;
  const title = cmd.title.toLowerCase();
  if (title.startsWith(query)) return 0;
  if (title.includes(` ${query}`)) return 1;
  if (title.includes(query)) return 2;
  const haystack = `${title} ${cmd.detail ?? ""} ${groupLabel} ${cmd.keywords ?? ""}`.toLowerCase();
  const terms = query.split(/\s+/);
  return terms.every((term) => haystack.includes(term)) ? 3 : null;
}

/** Matching commands, best rank first; equal ranks keep their original (group) order. */
export function filterCommands(
  commands: readonly Command[],
  rawQuery: string,
  groupLabel: (group: CommandGroup) => string,
): Command[] {
  const query = normalizeQuery(rawQuery);
  const ranked: { cmd: Command; rank: number; index: number }[] = [];
  commands.forEach((cmd, index) => {
    const rank = rankCommand(cmd, query, groupLabel(cmd.group));
    if (rank !== null) ranked.push({ cmd, rank, index });
  });
  ranked.sort((a, b) => a.rank - b.rank || a.index - b.index);
  return ranked.map((r) => r.cmd);
}

/**
 * Splits a ranked list into groups, ordered by where each group first appears. The first
 * row on screen is therefore always the best match, which is what Enter runs.
 */
export function groupCommands(commands: readonly Command[]): [CommandGroup, Command[]][] {
  const out: [CommandGroup, Command[]][] = [];
  for (const cmd of commands) {
    const existing = out.find(([group]) => group === cmd.group);
    if (existing) existing[1].push(cmd);
    else out.push([cmd.group, [cmd]]);
  }
  return out;
}
