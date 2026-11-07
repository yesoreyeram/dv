// CLI: fails the build when the initial bundle exceeds NFR-PERF-004's budget.
// Usage: node scripts/check-bundle-size.ts [distDir]   (default: dist)
import { readFileSync } from "node:fs";
import { resolve, sep } from "node:path";
import { checkBudget, gzipSize, initialAssets, type Measured } from "./bundle-budget.ts";

const dist = resolve(process.argv[2] ?? "dist");

function measure(paths: string[]): Measured[] {
  return paths.map((p) => {
    const file = resolve(dist, p);
    // index.html is build output, but never read outside dist even so.
    if (!file.startsWith(dist + sep)) throw new Error(`asset outside ${dist}: ${p}`);
    return { path: p, gzip: gzipSize(readFileSync(file)) };
  });
}

const assets = initialAssets(readFileSync(resolve(dist, "index.html"), "utf8"));
const report = checkBudget(measure(assets.scripts), measure(assets.styles));
for (const line of report.lines) console.log(line);
process.exitCode = report.ok ? 0 : 1;
