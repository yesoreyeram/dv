import AxeBuilder from "@axe-core/playwright";
import { expect, type Locator, type Page } from "@playwright/test";
import { Given, Then, When } from "./fixtures";

Given("I open the app at {string}", async ({ page }, path: string) => {
  await page.goto(path);
});

Given(
  "I open the app at {string} with color scheme {string}",
  async ({ page }, path: string, scheme: string) => {
    if (scheme !== "light" && scheme !== "dark") throw new Error(`unknown scheme ${scheme}`);
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto(path);
  },
);

Then("I see the primary navigation with links {string}", async ({ page }, csv: string) => {
  const nav = page.getByRole("navigation", { name: "Primary" });
  await expect(nav.getByRole("link")).toHaveText(csv.split(",").map((s) => s.trim()));
});

Then(
  "the navigation link {string} is marked as the current page",
  async ({ page }, name: string) => {
    const link = page.getByRole("navigation", { name: "Primary" }).getByRole("link", { name });
    await expect(link).toHaveAttribute("aria-current", "page");
  },
);

When("I click the navigation link {string}", async ({ page }, name: string) => {
  await page.getByRole("navigation", { name: "Primary" }).getByRole("link", { name }).click();
});

Then("the page heading is {string}", async ({ page }, name: string) => {
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(name);
});

Then("I see the text {string}", async ({ page }, text: string) => {
  await expect(page.getByText(text, { exact: true })).toBeVisible();
});

When("I press {string}", async ({ page }, key: string) => {
  await page.keyboard.press(key);
});

Then("the focused element is the link {string}", async ({ page }, name: string) => {
  await expect(page.getByRole("link", { name })).toBeFocused();
});

When("I click the button {string}", async ({ page }, name: string) => {
  await page.getByRole("button", { name }).click();
});

Then("the document uses the {string} theme", async ({ page }, theme: string) => {
  await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
});

Then("the page has no serious or critical accessibility violations", async ({ page }) => {
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  // Let data-driven content settle so it is included in the scan.
  await page.waitForLoadState("networkidle");
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
    .analyze();
  const blocking = results.violations.filter(
    (v) => v.impact === "serious" || v.impact === "critical",
  );
  expect(blocking.map((v) => `${v.id}: ${v.help} (${String(v.nodes.length)} nodes)`)).toEqual([]);
});

Then("I do not see the text {string}", async ({ page }, text: string) => {
  await expect(page.locator("main")).toBeVisible();
  await expect(page.getByText(text)).toHaveCount(0);
});

Given("the viewport is {int} by {int} pixels", async ({ page }, width: number, height: number) => {
  await page.setViewportSize({ width, height });
});

const navRow = (page: Page) => page.getByTestId("nav-row");

Then(
  "the navigation link {string} is fully visible in the navigation row",
  async ({ page }, name: string) => {
    const link = navRow(page).getByRole("link", { name });
    await expect(link).toHaveAttribute("aria-current", "page");
    await expect
      .poll(async () => {
        const row = await navRow(page).boundingBox();
        const box = await link.boundingBox();
        if (!row || !box) return false;
        return box.x >= row.x - 0.5 && box.x + box.width <= row.x + row.width + 0.5;
      })
      .toBe(true);
  },
);

Then("the page does not scroll sideways", async ({ page }) => {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
});

/** Asserts the decorative edge fades inside `scope` (FR-005 AC7, FR-013 AC8). */
export async function expectEdges(scope: Locator, start: boolean, end: boolean) {
  await expect(scope.getByTestId("scroll-edge-start")).toHaveAttribute(
    "data-visible",
    String(start),
  );
  await expect(scope.getByTestId("scroll-edge-end")).toHaveAttribute("data-visible", String(end));
}

const navScope = (page: Page) => page.getByRole("navigation", { name: "Primary" });

Then("the navigation row shows hidden content at the end only", async ({ page }) => {
  await expectEdges(navScope(page), false, true);
});

Then("the navigation row shows hidden content at the start", async ({ page }) => {
  await expect(navScope(page).getByTestId("scroll-edge-start")).toHaveAttribute(
    "data-visible",
    "true",
  );
});

Then("the navigation row shows no hidden content", async ({ page }) => {
  await expectEdges(navScope(page), false, false);
});
