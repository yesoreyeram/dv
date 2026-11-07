/**
 * Captures PR screenshots of the running app.
 * Usage: node scripts/screenshots.ts <baseURL> <outDir>
 * Fails if the page reports any console error (e.g. a CSP violation).
 */
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { chromium, devices } from "@playwright/test";

const [baseURL = "http://127.0.0.1:18080", outDir = "screenshots"] = process.argv.slice(2);
mkdirSync(outDir, { recursive: true });

const shots: { name: string; path: string; scheme: "light" | "dark"; mobile?: boolean }[] = [
  { name: "01-home-light", path: "/", scheme: "light" },
  { name: "02-home-dark", path: "/", scheme: "dark" },
  { name: "03-data-sources", path: "/data-sources", scheme: "light" },
  { name: "04-api-reference", path: "/api-reference", scheme: "light" },
  { name: "05-not-found-dark", path: "/no/such/page", scheme: "dark" },
  { name: "06-home-mobile", path: "/", scheme: "light", mobile: true },
];

const browser = await chromium.launch();
const errors: string[] = [];
for (const s of shots) {
  const context = await browser.newContext(
    s.mobile
      ? { ...devices["iPhone 13"], colorScheme: s.scheme }
      : { viewport: { width: 1280, height: 800 }, deviceScaleFactor: 2, colorScheme: s.scheme },
  );
  const page = await context.newPage();
  page.on("console", (m) => {
    if (m.type() === "error") errors.push(`${s.path}: ${m.text()}`);
  });
  await page.goto(new URL(s.path, baseURL).toString());
  await page.waitForLoadState("networkidle");
  await page.screenshot({ path: join(outDir, `${s.name}.png`), fullPage: true });
  await context.close();
}
await browser.close();
if (errors.length > 0) {
  console.error(errors.join("\n"));
  process.exit(1);
}
console.log(`captured ${String(shots.length)} screenshots in ${outDir}`);
