// Bundle-size budget (NFR-PERF-004). Pure functions so they can be unit-tested; the
// CLI wrapper lives in check-bundle-size.ts.
import { gzipSync } from "node:zlib";

/** NFR-PERF-004 AC1: initial JS ≤ 250 KB gzip. */
export const JS_BUDGET_BYTES = 250_000;
/** NFR-PERF-004 AC2: initial CSS ≤ 50 KB gzip. */
export const CSS_BUDGET_BYTES = 50_000;

export interface InitialAssets {
  scripts: string[];
  styles: string[];
}

const TAG = /<(script|link)\b([^>]*)>/gi;
const ATTR = /([a-zA-Z-]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))/g;

function attributes(raw: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const m of raw.matchAll(ATTR)) {
    const name = m[1];
    if (name !== undefined) {
      out.set(name.toLowerCase(), m[2] ?? m[3] ?? m[4] ?? "");
    }
  }
  return out;
}

/** Local asset path for an href/src, or undefined for external, data: or empty URLs. */
function localPath(url: string | undefined): string | undefined {
  if (url === undefined || url === "" || /^[a-z][a-z0-9+.-]*:/i.test(url) || url.startsWith("//")) {
    return undefined;
  }
  return url.replace(/^\/+/, "").split(/[?#]/)[0];
}

/**
 * Lists the scripts and stylesheets a built index.html loads before first render:
 * module entry scripts, modulepreload links and stylesheet links. Lazy chunks
 * (dynamic imports) are not referenced from index.html and so are not counted.
 */
export function initialAssets(html: string): InitialAssets {
  const scripts = new Set<string>();
  const styles = new Set<string>();
  for (const m of html.matchAll(TAG)) {
    const tag = (m[1] ?? "").toLowerCase();
    const attrs = attributes(m[2] ?? "");
    if (tag === "script") {
      const p = localPath(attrs.get("src"));
      if (p !== undefined && attrs.get("type") === "module") scripts.add(p);
      continue;
    }
    const rel = (attrs.get("rel") ?? "").toLowerCase().split(/\s+/);
    const p = localPath(attrs.get("href"));
    if (p === undefined) continue;
    if (rel.includes("modulepreload")) scripts.add(p);
    else if (rel.includes("stylesheet")) styles.add(p);
  }
  return { scripts: [...scripts], styles: [...styles] };
}

export const gzipSize = (data: Uint8Array): number => gzipSync(data, { level: 9 }).byteLength;

export interface Measured {
  path: string;
  gzip: number;
}

export interface BudgetReport {
  ok: boolean;
  lines: string[];
}

/** Checks measured assets against the budgets. No initial script is a failure. */
export function checkBudget(
  scripts: Measured[],
  styles: Measured[],
  jsBudget = JS_BUDGET_BYTES,
  cssBudget = CSS_BUDGET_BYTES,
): BudgetReport {
  const lines: string[] = [];
  let ok = true;
  if (scripts.length === 0) {
    ok = false;
    lines.push("FAIL no initial <script type=module> or modulepreload found in index.html");
  }
  for (const [kind, items, budget] of [
    ["JS", scripts, jsBudget],
    ["CSS", styles, cssBudget],
  ] as const) {
    const total = items.reduce((sum, a) => sum + a.gzip, 0);
    for (const a of items) lines.push(`     ${kind} ${a.path}: ${kb(a.gzip)} gzip`);
    const pass = total <= budget;
    ok &&= pass;
    lines.push(
      `${pass ? "PASS" : "FAIL"} initial ${kind} ${kb(total)} gzip (budget ${kb(budget)})`,
    );
  }
  return { ok, lines };
}

const kb = (n: number): string => `${(n / 1000).toFixed(1)} KB`;
