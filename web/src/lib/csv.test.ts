import { afterEach, describe, expect, it, vi } from "vitest";
import {
  BOM,
  csvCell,
  csvFileName,
  downloadFrameCSV,
  frameToCSV,
  neutraliseFormula,
  quoteCell,
} from "@/lib/csv";
import type { DataFrame, Field } from "@/lib/frame";

const field = (type: Field["type"], values: unknown[], name = "f", config = {}): Field =>
  ({ name, type, config, values }) as Field;

const frame = (fields: Field[], name = "data"): DataFrame => ({
  name,
  meta: {},
  fields,
  length: fields[0]?.values.length ?? 0,
});

describe("CSV formula neutralisation (NFR-SEC-018 AC1)", () => {
  it.each(["=1+1", "+1", "-1+2", "@SUM(A1)", "\tcmd", "\rcmd", "  =HYPERLINK(x)"])(
    "prefixes %j",
    (v) => {
      expect(neutraliseFormula(v)).toBe(`'${v}`);
    },
  );

  it.each(["plain", "", "1=1", "a-b", " x", "'quoted"])("leaves %j alone", (v) => {
    expect(neutraliseFormula(v)).toBe(v);
  });

  it("neutralises text and json cells and headers but not numbers (AC2)", () => {
    expect(csvCell(field("string", []), "=cmd|' /C calc'!A0")).toBe("'=cmd|' /C calc'!A0");
    // A json number such as -1 is text in the file, so it is neutralised like a string.
    expect(csvCell(field("json", []), -1)).toBe("'-1");
    expect(csvCell(field("json", []), "-x")).toBe('"""-x"""');
    expect(csvCell(field("number", []), -3)).toBe("-3");
    const csv = frameToCSV(frame([field("number", [1], "=evil")]));
    expect(csv).toBe(`${BOM}'=evil\r\n1\r\n`);
  });
});

describe("RFC 4180 quoting (NFR-SEC-018 AC3)", () => {
  it("quotes separators, quotes and line breaks", () => {
    expect(quoteCell("a,b")).toBe('"a,b"');
    expect(quoteCell('say "hi"')).toBe('"say ""hi"""');
    expect(quoteCell("x\ny")).toBe('"x\ny"');
    expect(quoteCell("x\ry")).toBe('"x\ry"');
    expect(quoteCell("plain")).toBe("plain");
  });

  it("cannot add columns or rows", () => {
    const csv = frameToCSV(frame([field("string", ["a,b\r\nc,d"], "s")]));
    // One quoted cell: the embedded CRLF and comma stay inside the quotes.
    expect(csv).toBe(`${BOM}s\r\n"a,b\r\nc,d"\r\n`);
  });
});

describe("raw cell values (FR-018 AC7)", () => {
  it("writes raw, locale-independent values", () => {
    expect(csvCell(field("number", []), 1234.5)).toBe("1234.5");
    expect(csvCell(field("number", []), Number.NaN)).toBe("");
    expect(csvCell(field("number", []), Number.POSITIVE_INFINITY)).toBe("");
    expect(csvCell(field("time", []), Date.UTC(2026, 10, 9, 6, 0))).toBe(
      "2026-11-09T06:00:00.000Z",
    );
    expect(csvCell(field("bool", []), true)).toBe("true");
    expect(csvCell(field("bool", []), false)).toBe("false");
    expect(csvCell(field("json", []), { a: [1, 2] })).toBe('"{""a"":[1,2]}"');
    expect(csvCell(field("string", []), null)).toBe("");
  });

  it("exports every row in the given order with display names and CRLF", () => {
    const f = frame([
      field("string", ["b", "a", "c"], "name", { displayName: "Name" }),
      field("number", [2, 1, 3], "n"),
    ]);
    expect(frameToCSV(f, [1, 0, 2])).toBe(`${BOM}Name,n\r\na,1\r\nb,2\r\nc,3\r\n`);
    expect(frameToCSV(f)).toBe(`${BOM}Name,n\r\nb,2\r\na,1\r\nc,3\r\n`);
  });

  it("exports all rows, not one page", () => {
    const values = Array.from({ length: 120 }, (_, i) => i);
    const lines = frameToCSV(frame([field("number", values, "i")])).split("\r\n");
    expect(lines).toHaveLength(1 + 120 + 1);
  });
});

describe("download file name (FR-018 AC7)", () => {
  it.each([
    ["sales.csv", "sales.csv"],
    ["SALES.CSV", "SALES.csv"],
    ["hosts.tsv", "hosts.tsv.csv"],
    ["../../etc/passwd", "_.._etc_passwd.csv"],
    ["a/b\\c d", "a_b_c_d.csv"],
    ["", "frame.csv"],
    ["...", "frame.csv"],
    ["名前", "frame.csv"],
  ])("%j → %j", (input, want) => {
    expect(csvFileName(input)).toBe(want);
  });

  it("is at most 100 characters", () => {
    const name = csvFileName("x".repeat(500));
    expect(name).toHaveLength(100);
    expect(name.endsWith(".csv")).toBe(true);
  });
});

describe("downloadFrameCSV", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it("clicks a temporary link to an object URL and revokes it", async () => {
    vi.useFakeTimers();
    const created: Blob[] = [];
    const create = vi.fn((b: Blob) => {
      created.push(b);
      return "blob:test";
    });
    const revoke = vi.fn();
    Object.assign(URL, { createObjectURL: create, revokeObjectURL: revoke });
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {
      // jsdom cannot navigate; the anchor is checked through the spy's context below.
    });
    downloadFrameCSV(frame([field("string", ["=1"], "s")], "formulas.csv"));
    expect(click).toHaveBeenCalledOnce();
    const a = click.mock.contexts[0] as HTMLAnchorElement;
    expect(a.download).toBe("formulas.csv");
    expect(a.href).toBe("blob:test");
    expect(document.body.contains(a)).toBe(false);
    expect(created[0]?.type).toBe("text/csv;charset=utf-8");
    // Blob.text() drops a leading BOM when decoding, so compare the raw bytes.
    const bytes = new Uint8Array((await created[0]?.arrayBuffer()) ?? new ArrayBuffer(0));
    expect(Array.from(bytes.slice(0, 3))).toEqual([0xef, 0xbb, 0xbf]);
    expect(new TextDecoder("utf-8", { ignoreBOM: true }).decode(bytes)).toBe(`${BOM}s\r\n'=1\r\n`);
    expect(revoke).not.toHaveBeenCalled();
    vi.runAllTimers();
    expect(revoke).toHaveBeenCalledWith("blob:test");
  });
});
