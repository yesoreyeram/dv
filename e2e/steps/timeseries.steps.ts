import { expect, type Page } from "@playwright/test";
import { Then, When } from "./fixtures";

const chart = (page: Page) => page.getByTestId("timeseries-chart");
const plot = (page: Page) => page.getByRole("group", { name: "Chart plot" });
const legend = (page: Page) => page.getByRole("list", { name: "Series" });

When("I choose the visualization {string}", async ({ page }, name: string) => {
  // The radio is visually hidden inside its label (a segmented control), so click the label
  // the way a pointer user does, then confirm the radio took the change.
  await page
    .locator("label")
    .filter({ has: page.getByRole("radio", { name }) })
    .click();
  await expect(page.getByRole("radio", { name })).toBeChecked();
});

Then("the visualization {string} is selected", async ({ page }, name: string) => {
  await expect(page.getByRole("radio", { name })).toBeChecked();
});

Then(
  "the URL has the query parameter {string} set to {string}",
  async ({ page }, key: string, value: string) => {
    await expect.poll(() => new URL(page.url()).searchParams.get(key)).toBe(value);
  },
);

Then("a time-series chart is shown with {int} series", async ({ page }, n: number) => {
  await expect(chart(page)).toBeVisible();
  await expect(chart(page).getByTestId("ts-series")).toHaveCount(n);
});

Then("the chart's accessible name mentions {string}", async ({ page }, text: string) => {
  await expect(page.getByRole("img", { name: new RegExp(escape(text)) })).toBeVisible();
});

Then(
  "the legend lists {string}, {string} and {string}",
  async ({ page }, a: string, b: string, c: string) => {
    await expect(legend(page).getByRole("button")).toHaveText([a, b, c]);
  },
);

Then(
  "the series {string} is drawn in {int} segments",
  async ({ page }, name: string, n: number) => {
    await expect(
      chart(page).locator(`[data-testid="ts-series"][data-name="${name}"]`),
    ).toHaveAttribute("data-segments", String(n));
  },
);

When("I focus the chart plot", async ({ page }) => {
  await plot(page).focus();
});

When("I press {string} in the chart plot", async ({ page }, key: string) => {
  await plot(page).press(key);
});

Then("the chart reading mentions {string} and {string}", async ({ page }, a: string, b: string) => {
  const reading = page.getByTestId("ts-reading");
  await expect(reading).toContainText(a);
  await expect(reading).toContainText(b);
});

When("I toggle the legend entry {string}", async ({ page }, name: string) => {
  await legend(page).getByRole("button", { name, exact: true }).click();
});

Then("the legend entry {string} is not pressed", async ({ page }, name: string) => {
  await expect(legend(page).getByRole("button", { name, exact: true })).toHaveAttribute(
    "aria-pressed",
    "false",
  );
});

Then("the chart panel explains that it needs a time field and a number field", async ({ page }) => {
  await expect(page.getByTestId("timeseries-unsupported")).toContainText(
    "needs a time field and at least one number field",
  );
  await expect(chart(page)).toHaveCount(0);
});

function escape(s: string) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
