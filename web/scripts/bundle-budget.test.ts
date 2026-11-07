import {
  checkBudget,
  CSS_BUDGET_BYTES,
  gzipSize,
  initialAssets,
  JS_BUDGET_BYTES,
} from "./bundle-budget.ts";

const builtIndex = `<!doctype html>
<html lang="en">
  <head>
    <link rel="icon" type="image/svg+xml" href="/favicon.svg" />
    <script type="module" crossorigin src="/assets/index-abc.js"></script>
    <link rel="modulepreload" crossorigin href="/assets/vendor-def.js">
    <link rel='stylesheet' crossorigin href='/assets/index-123.css?v=1'>
    <link rel="stylesheet" href="https://fonts.example/x.css">
    <script src="/legacy.js"></script>
    <script type="module" src="data:text/javascript,1"></script>
  </head>
  <body><div id="root"></div></body>
</html>`;

describe("initialAssets", () => {
  it("collects entry scripts, modulepreloads and stylesheets", () => {
    expect(initialAssets(builtIndex)).toEqual({
      scripts: ["assets/index-abc.js", "assets/vendor-def.js"],
      styles: ["assets/index-123.css"],
    });
  });

  it("ignores external, protocol-relative, inline and non-module scripts", () => {
    const html = `<script type="module" src="//cdn.example/a.js"></script>
      <script type="module">console.log(1)</script>
      <link rel="modulepreload" href="">
      <link rel="icon" href="/x.js">`;
    expect(initialAssets(html)).toEqual({ scripts: [], styles: [] });
  });

  it("counts an asset referenced twice once", () => {
    const html = `<script type="module" src="/a.js"></script><link rel="modulepreload" href="/a.js">`;
    expect(initialAssets(html).scripts).toEqual(["a.js"]);
  });
});

describe("checkBudget", () => {
  it("passes within budget and reports each asset", () => {
    const r = checkBudget([{ path: "a.js", gzip: 1000 }], [{ path: "a.css", gzip: 500 }]);
    expect(r.ok).toBe(true);
    expect(r.lines).toContain("PASS initial JS 1.0 KB gzip (budget 250.0 KB)");
    expect(r.lines).toContain("PASS initial CSS 0.5 KB gzip (budget 50.0 KB)");
    expect(r.lines.some((l) => l.includes("a.js: 1.0 KB"))).toBe(true);
  });

  it("fails when the JS total exceeds the budget", () => {
    const r = checkBudget(
      [
        { path: "a.js", gzip: JS_BUDGET_BYTES },
        { path: "b.js", gzip: 1 },
      ],
      [],
    );
    expect(r.ok).toBe(false);
    expect(r.lines).toContain("FAIL initial JS 250.0 KB gzip (budget 250.0 KB)");
  });

  it("fails when the CSS total exceeds the budget", () => {
    const r = checkBudget(
      [{ path: "a.js", gzip: 1 }],
      [{ path: "a.css", gzip: CSS_BUDGET_BYTES + 1 }],
    );
    expect(r.ok).toBe(false);
  });

  it("fails when no initial script is found, so a broken build cannot pass", () => {
    const r = checkBudget([], []);
    expect(r.ok).toBe(false);
    expect(r.lines[0]).toMatch(/no initial/);
  });

  it("is exactly at the budget inclusive", () => {
    expect(checkBudget([{ path: "a.js", gzip: 250_000 }], [], 250_000).ok).toBe(true);
  });
});

describe("gzipSize", () => {
  it("compresses repetitive data", () => {
    const data = new TextEncoder().encode("a".repeat(10_000));
    expect(gzipSize(data)).toBeLessThan(200);
  });
});
