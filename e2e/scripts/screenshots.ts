/**
 * Captures PR screenshots of the running app.
 * Usage: node scripts/screenshots.ts <baseURL> <outDir>
 * The server needs the query API with the sample data source (deploy/query-demo.json) for
 * the Explore shots. Fails if the page reports any console error (e.g. a CSP violation).
 */
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { chromium, devices, type Page } from "@playwright/test";

const [baseURL = "http://127.0.0.1:18080", outDir = "screenshots"] = process.argv.slice(2);
mkdirSync(outDir, { recursive: true });

interface Shot {
  name: string;
  path: string;
  scheme: "light" | "dark";
  mobile?: boolean;
  /** Interactions after the page has loaded, before the capture. */
  prepare?: (page: Page) => Promise<void>;
  /** Capture the viewport only: overlays are fixed to it. */
  overlay?: boolean;
}

const run = async (page: Page, wait: string) => {
  await page.getByRole("button", { name: "Run query" }).click();
  await page.getByTestId(wait).waitFor();
};

const shots: Shot[] = [
  { name: "01-home-dark", path: "/", scheme: "dark" },
  { name: "02-home-light", path: "/", scheme: "light" },
  {
    name: "03-explore-timeseries-dark",
    path: "/explore?ds=samples&path=cpu.csv&view=timeseries",
    scheme: "dark",
    prepare: (page) => run(page, "timeseries-chart"),
  },
  {
    name: "04-explore-table-light",
    path: "/explore?ds=samples&path=sales.csv",
    scheme: "light",
    prepare: (page) => run(page, "table-panel"),
  },
  { name: "05-explore-empty-dark", path: "/explore", scheme: "dark" },
  {
    name: "06-command-palette-dark",
    overlay: true,
    path: "/explore?ds=samples&path=cpu.csv",
    scheme: "dark",
    prepare: async (page) => {
      await run(page, "table-panel");
      await page.keyboard.press("Control+k");
      await page.getByRole("dialog", { name: "Command palette" }).waitFor();
    },
  },
  {
    name: "07-command-palette-search-light",
    overlay: true,
    path: "/",
    scheme: "light",
    prepare: async (page) => {
      await page.keyboard.press("Control+k");
      await page.getByRole("combobox").fill("ex");
    },
  },
  {
    name: "08-sidebar-rail-history-dark",
    overlay: true,
    path: "/explore?ds=samples&path=sales.csv",
    scheme: "dark",
    prepare: async (page) => {
      await page.getByRole("button", { name: "Collapse sidebar" }).click();
      await run(page, "table-panel");
      await page.getByRole("button", { name: "History", exact: true }).click();
    },
  },
  {
    name: "09-explore-wide-editor-dark",
    path: "/explore?ds=samples&path=cpu.csv&view=timeseries",
    scheme: "dark",
    prepare: async (page) => {
      await run(page, "timeseries-chart");
      const splitter = page.getByRole("separator", { name: "Resize query editor" });
      await splitter.focus();
      for (let i = 0; i < 8; i++) await page.keyboard.press("ArrowRight");
    },
  },
  {
    name: "10-keyboard-shortcuts-dark",
    overlay: true,
    path: "/",
    scheme: "dark",
    prepare: async (page) => {
      await page.getByRole("button", { name: "Keyboard shortcuts" }).click();
      await page.getByRole("dialog", { name: "Keyboard shortcuts" }).waitFor();
    },
  },
  { name: "11-data-sources-dark", path: "/data-sources", scheme: "dark" },
  { name: "12-api-reference-light", path: "/api-reference", scheme: "light" },
  { name: "13-dashboards-planned-dark", path: "/dashboards", scheme: "dark" },
  { name: "14-not-found-dark", path: "/no/such/page", scheme: "dark" },
  { name: "15-home-mobile-light", path: "/", scheme: "light", mobile: true },
  {
    name: "16-explore-mobile-dark",
    path: "/explore?ds=samples&path=cpu.csv&view=timeseries",
    scheme: "dark",
    mobile: true,
    prepare: (page) => run(page, "timeseries-chart"),
  },
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
  page.on("pageerror", (e) => {
    errors.push(`${s.path}: ${e.message}`);
  });
  await page.goto(new URL(s.path, baseURL).toString());
  await page.getByRole("heading", { level: 1 }).waitFor();
  await page.waitForLoadState("networkidle");
  await s.prepare?.(page);
  await page.waitForLoadState("networkidle");
  await page.screenshot({ path: join(outDir, `${s.name}.png`), fullPage: s.overlay !== true });
  await context.close();
}
await browser.close();
if (errors.length > 0) {
  console.error(errors.join("\n"));
  process.exit(1);
}
console.log(`captured ${String(shots.length)} screenshots in ${outDir}`);
