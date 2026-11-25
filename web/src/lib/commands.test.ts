import { describe, expect, it } from "vitest";
import {
  filterCommands,
  groupCommands,
  MAX_QUERY_LENGTH,
  normalizeQuery,
  rankCommand,
  type Command,
  type CommandGroup,
} from "./commands";

const noop = () => undefined;
const cmd = (id: string, group: CommandGroup, title: string, extra: Partial<Command> = {}) => ({
  id,
  group,
  title,
  run: noop,
  ...extra,
});

const commands: Command[] = [
  cmd("home", "goto", "Go to Home"),
  cmd("explore", "goto", "Go to Explore"),
  cmd("sources", "goto", "Go to Data sources"),
  cmd("samples", "explore", "Explore Sample files", { detail: "file", keywords: "samples query" }),
  cmd("recent", "recent", "cpu.csv", { detail: "Sample files", keywords: "samples history" }),
  cmd("theme", "preferences", "Switch to dark theme", { keywords: "appearance colour" }),
  cmd("help", "help", "Keyboard shortcuts"),
];
const label = (g: CommandGroup) =>
  ({
    goto: "Go to",
    explore: "Explore a data source",
    recent: "Recent queries",
    preferences: "Preferences",
    help: "Help",
  })[g];
const at = (i: number): Command => {
  const c = commands[i];
  if (!c) throw new Error(`no command at ${String(i)}`);
  return c;
};
const ids = (q: string) => filterCommands(commands, q, label).map((c) => c.id);

describe("command palette search (FR-008 AC4)", () => {
  it("returns every command in order for an empty query", () => {
    expect(ids("")).toEqual(commands.map((c) => c.id));
    expect(ids("   ")).toEqual(commands.map((c) => c.id));
  });

  it("ranks a title prefix above a word start above any other match", () => {
    expect(ids("explore")).toEqual(["samples", "explore"]);
    expect(ids("data")).toEqual(["sources", "samples"]);
    expect(rankCommand(at(3), "explore", "")).toBe(0);
    expect(rankCommand(at(1), "explore", "")).toBe(1);
    expect(rankCommand(at(5), "ark", "")).toBe(2);
    expect(rankCommand(at(5), "colour", "")).toBe(3);
    expect(rankCommand(at(5), "zzz", "")).toBeNull();
  });

  it("matches every term, in any order, across title, detail, group and keywords", () => {
    expect(ids("theme dark")).toEqual(["theme"]);
    expect(ids("samples history")).toEqual(["recent"]);
    expect(ids("preferences")).toEqual(["theme"]);
    expect(ids("dark light")).toEqual([]);
  });

  it("is case-insensitive and trims", () => {
    expect(ids("  DATA SOURCES ")).toEqual(["sources"]);
  });

  it("treats regular-expression characters as plain text", () => {
    expect(ids(".*")).toEqual([]);
    expect(ids("cpu.csv")).toEqual(["recent"]);
    expect(ids("(a+)+$")).toEqual([]);
  });

  it("caps the query length", () => {
    expect(normalizeQuery("A".repeat(500))).toHaveLength(MAX_QUERY_LENGTH);
  });

  it("groups by first appearance so the first row is the best match", () => {
    const groups = groupCommands(filterCommands(commands, "explore", label));
    expect(groups.map(([g, items]) => [g, items.map((c) => c.id)])).toEqual([
      ["explore", ["samples"]],
      ["goto", ["explore"]],
    ]);
    expect(groupCommands(commands).map(([g]) => g)).toEqual([
      "goto",
      "explore",
      "recent",
      "preferences",
      "help",
    ]);
  });
});
