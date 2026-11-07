import AxeBuilder from "@axe-core/playwright";
import { expect } from "@playwright/test";
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
