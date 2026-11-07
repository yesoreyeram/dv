import { expect } from "@playwright/test";
import { Given, Then, When } from "./fixtures";

// NFR-SEC-007 / B-036: the browser reports every CSP violation through the
// `securitypolicyviolation` event and a console error. We record both so a page that only
// works with a looser policy fails the suite instead of silently degrading.

interface Violation {
  directive: string;
  blockedURI: string;
  sourceFile: string;
}

declare global {
  interface Window {
    __cspViolations?: Violation[];
    __dvInjectedRan?: boolean;
  }
}

const consoleViolations = new WeakMap<object, string[]>();

Given("I am recording Content Security Policy violations", async ({ page }) => {
  const messages: string[] = [];
  consoleViolations.set(page, messages);
  page.on("console", (msg) => {
    if (msg.type() === "error" && /Content Security Policy/i.test(msg.text())) {
      messages.push(msg.text());
    }
  });
  await page.addInitScript(() => {
    window.__cspViolations = [];
    document.addEventListener("securitypolicyviolation", (e) => {
      window.__cspViolations?.push({
        directive: e.effectiveDirective,
        blockedURI: e.blockedURI,
        sourceFile: e.sourceFile,
      });
    });
  });
});

async function violations(page: import("@playwright/test").Page): Promise<Violation[]> {
  return page.evaluate(() => window.__cspViolations ?? []);
}

Then(
  "the document was served with a Content Security Policy that forbids inline script",
  async ({ page }) => {
    const res = await page.request.get(page.url());
    const csp = res.headers()["content-security-policy"] ?? "";
    const scriptSrc = csp
      .split(";")
      .map((d) => d.trim())
      .find((d) => d.startsWith("script-src "));
    expect(scriptSrc, `CSP: ${csp}`).toBeDefined();
    expect(scriptSrc).not.toContain("'unsafe-inline'");
    expect(scriptSrc).not.toContain("'unsafe-eval'");
  },
);

Then("no Content Security Policy violations were reported", async ({ page }) => {
  // Let late-loading chunks and fetches settle before reading the log.
  await page.waitForLoadState("networkidle");
  expect(await violations(page)).toEqual([]);
  expect(consoleViolations.get(page) ?? []).toEqual([]);
});

When("a page script injects an inline script element", async ({ page }) => {
  await page.evaluate(() => {
    const s = document.createElement("script");
    s.textContent = "window.__dvInjectedRan = true;";
    document.body.appendChild(s);
  });
});

Then("the injected inline script did not run", async ({ page }) => {
  expect(await page.evaluate(() => window.__dvInjectedRan === true)).toBe(false);
});

Then(
  "a Content Security Policy violation for {string} was reported",
  async ({ page }, directive: string) => {
    // Browsers report the most specific directive that applied (`script-src-elem` for a
    // <script> element), which falls back to the named one when it is not set.
    const matches = (d: string) => d === directive || d.startsWith(`${directive}-`);
    await expect
      .poll(async () => (await violations(page)).some((v) => matches(v.directive)))
      .toBe(true);
  },
);
