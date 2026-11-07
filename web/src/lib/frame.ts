/**
 * Client-side decoder for the dv data-frame wire format (ADR-0003, FR-010 AC7).
 *
 * Wire shape (column-major):
 *   {"schema":{"name","meta","fields":[{"name","type","config"}]},"data":{"values":[[…],…]}}
 * Times are UTC epoch milliseconds; null is JSON null. The decoder validates everything it
 * reads, so malformed or hostile payloads raise `FrameDecodeError` instead of reaching
 * panels.
 */

export type FieldType = "time" | "number" | "string" | "bool" | "json";

export interface FieldConfig {
  displayName?: string;
  unit?: string;
  decimals?: number;
}

/** Maps a field type to the JS type of its non-null values. */
export interface FieldValueTypes {
  time: number; // epoch ms, UTC
  number: number;
  string: string;
  bool: boolean;
  json: unknown;
}

export type Field<T extends FieldType = FieldType> = {
  [K in T]: {
    name: string;
    type: K;
    config: FieldConfig;
    values: (FieldValueTypes[K] | null)[];
  };
}[T];

export interface Notice {
  severity: "info" | "warning";
  text: string;
}

export interface FrameMeta {
  source?: string;
  executedQuery?: string;
  truncated?: boolean;
  notices?: Notice[];
}

export interface DataFrame {
  name: string;
  meta: FrameMeta;
  fields: Field[];
  /** Number of rows (all fields have equal length). */
  length: number;
}

export class FrameDecodeError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "FrameDecodeError";
  }
}

const FIELD_TYPES: readonly FieldType[] = ["time", "number", "string", "bool", "json"];

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function fail(msg: string): never {
  throw new FrameDecodeError(msg);
}

function checkCell(type: FieldType, v: unknown): boolean {
  if (v === null) return true;
  switch (type) {
    case "time":
      return Number.isSafeInteger(v);
    case "number":
      return typeof v === "number" && Number.isFinite(v);
    case "string":
      return typeof v === "string";
    case "bool":
      return typeof v === "boolean";
    case "json":
      return true;
  }
}

function decodeConfig(raw: unknown, name: string): FieldConfig {
  if (raw === undefined) return {};
  if (!isObject(raw)) fail(`field "${name}": config must be an object`);
  const cfg: FieldConfig = {};
  if (typeof raw.displayName === "string") cfg.displayName = raw.displayName;
  if (typeof raw.unit === "string") cfg.unit = raw.unit;
  if (Number.isInteger(raw.decimals)) cfg.decimals = raw.decimals as number;
  return cfg;
}

function decodeMeta(raw: unknown): FrameMeta {
  if (raw === undefined) return {};
  if (!isObject(raw)) fail("meta must be an object");
  const meta: FrameMeta = {};
  if (typeof raw.source === "string") meta.source = raw.source;
  if (typeof raw.executedQuery === "string") meta.executedQuery = raw.executedQuery;
  if (raw.truncated === true) meta.truncated = true;
  if (Array.isArray(raw.notices)) {
    meta.notices = raw.notices.filter(
      (n): n is Notice =>
        isObject(n) &&
        (n.severity === "info" || n.severity === "warning") &&
        typeof n.text === "string",
    );
  }
  return meta;
}

/** Decodes and validates one wire-format frame. */
export function decodeFrame(raw: unknown): DataFrame {
  if (!isObject(raw) || !isObject(raw.schema) || !isObject(raw.data)) {
    fail("frame must have schema and data objects");
  }
  const { schema, data } = raw;
  const name = typeof schema.name === "string" ? schema.name : "";
  const rawFields = schema.fields;
  const rawValues = data.values;
  if (!Array.isArray(rawFields)) fail("schema.fields must be an array");
  if (!Array.isArray(rawValues)) fail("data.values must be an array");
  if (rawValues.length !== rawFields.length) {
    fail(
      `schema has ${String(rawFields.length)} fields but data has ${String(rawValues.length)} columns`,
    );
  }
  const seen = new Set<string>();
  let length = -1;
  const fields = rawFields.map((f: unknown, i: number): Field => {
    if (!isObject(f) || typeof f.name !== "string" || f.name === "") {
      fail(`field ${String(i)}: name must be a non-empty string`);
    }
    const fname = f.name;
    if (seen.has(fname)) fail(`duplicate field name "${fname}"`);
    seen.add(fname);
    const type = f.type as FieldType;
    if (!FIELD_TYPES.includes(type)) fail(`field "${fname}": unsupported type`);
    const values: unknown = rawValues[i];
    if (!Array.isArray(values)) fail(`field "${fname}": values must be an array`);
    if (length === -1) length = values.length;
    else if (values.length !== length) fail(`field "${fname}": column length mismatch`);
    values.forEach((v, row) => {
      if (!checkCell(type, v)) fail(`field "${fname}" row ${String(row)}: expected ${type}`);
    });
    return { name: fname, type, config: decodeConfig(f.config, fname), values };
  });
  return { name, meta: decodeMeta(schema.meta), fields, length: Math.max(length, 0) };
}
