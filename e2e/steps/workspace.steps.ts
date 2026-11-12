import { expect, type Page } from "@playwright/test";
import { Then, When } from "./fixtures";

const palette = (page: Page) => page.getByRole("dialog", { name: "Command palette" });
const sidebar = (page: Page) => page.getByTestId("sidebar");
const splitter = (page: Page) => page.getByRole("separator", { name: "Resize query editor" });

Then("the breadcrumb shows the current page {string}", async ({ page }, name: string) => {
  const crumbs = page.getByRole("navigation", { name: "Breadcrumb" });
  await expect(crumbs.locator('[aria-current="page"]')).toHaveText(name);
});

Then("the sidebar shows the server state {string}", async ({ page }, text: string) => {
  await expect(page.getByTestId("server-state")).toContainText(text);
});

Then("the command palette is open", async ({ page }) => {
  await expect(palette(page)).toBeVisible();
  await expect(palette(page).getByRole("combobox")).toBeFocused();
});

Then("the command palette is closed", async ({ page }) => {
  await expect(palette(page)).toHaveCount(0);
});

When("I type {string} in the command palette", async ({ page }, text: string) => {
  await palette(page).getByRole("combobox").fill(text);
});

Then("the command palette search field has focus", async ({ page }) => {
  await expect(palette(page).getByRole("combobox")).toBeFocused();
});

Then("the active command is {string}", async ({ page }, title: string) => {
  const active = palette(page).getByRole("option", { selected: true });
  await expect(active).toHaveCount(1);
  await expect(active.getByTestId("command-title")).toHaveText(title);
  // The combobox points at it (WAI-ARIA combobox pattern, FR-008 AC2).
  const id = await active.getAttribute("id");
  await expect(palette(page).getByRole("combobox")).toHaveAttribute(
    "aria-activedescendant",
    id ?? "",
  );
});

Then("the command palette shows no results for {string}", async ({ page }, query: string) => {
  await expect(palette(page).getByRole("status")).toHaveText(`No results for “${query}”`);
  await expect(palette(page).getByRole("option")).toHaveCount(0);
});

Then("the focused element is the button {string}", async ({ page }, name: string) => {
  await expect(page.getByRole("button", { name })).toBeFocused();
});

Then("the sidebar is collapsed", async ({ page }) => {
  await expect(sidebar(page)).toHaveAttribute("data-collapsed", "true");
  await expect(page.getByRole("button", { name: "Expand sidebar" })).toHaveAttribute(
    "aria-expanded",
    "false",
  );
  // An icon rail: narrower than any label.
  await expect.poll(async () => (await sidebar(page).boundingBox())?.width ?? 0).toBeLessThan(64);
});

Then("the sidebar is expanded", async ({ page }) => {
  await expect(sidebar(page)).toHaveAttribute("data-collapsed", "false");
  await expect(page.getByRole("button", { name: "Collapse sidebar" })).toHaveAttribute(
    "aria-expanded",
    "true",
  );
  await expect
    .poll(async () => (await sidebar(page).boundingBox())?.width ?? 0)
    .toBeGreaterThan(160);
});

When("I reload the page", async ({ page }) => {
  await page.reload();
});

When("I type {string} in the path field", async ({ page }, text: string) => {
  await page.getByRole("textbox", { name: "Path" }).pressSequentially(text);
});

Then("the dialog {string} is open", async ({ page }, name: string) => {
  await expect(page.getByRole("dialog", { name })).toBeVisible();
});

When("I switch off {string}", async ({ page }, name: string) => {
  const control = page.getByRole("switch", { name });
  await control.uncheck();
  await expect(control).not.toBeChecked();
});

Then("the query editor is {int} pixels wide", async ({ page }, width: number) => {
  await expect
    .poll(async () =>
      Math.round((await page.getByTestId("explore-editor").boundingBox())?.width ?? 0),
    )
    .toBe(width);
  await expect(splitter(page)).toHaveAttribute("aria-valuenow", String(width));
});

When("I focus the splitter", async ({ page }) => {
  await splitter(page).focus();
  await expect(splitter(page)).toBeFocused();
});

When("I drag the splitter {int} pixels to the right", async ({ page }, delta: number) => {
  const box = await splitter(page).boundingBox();
  if (!box) throw new Error("the splitter is not rendered");
  const x = box.x + box.width / 2;
  const y = box.y + box.height / 2;
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x + delta / 2, y, { steps: 4 });
  await page.mouse.move(x + delta, y, { steps: 4 });
  await page.mouse.up();
});

Then("the splitter is not shown", async ({ page }) => {
  await expect(page.getByTestId("explore-editor")).toBeVisible();
  await expect(splitter(page)).toBeHidden();
});

When("I open the query history", async ({ page }) => {
  const button = page.getByRole("button", { name: "History", exact: true });
  if ((await button.getAttribute("aria-expanded")) !== "true") await button.click();
  await expect(page.getByTestId("history-panel")).toBeVisible();
});

Then("the query history lists {string}", async ({ page }, csv: string) => {
  await expect(page.getByTestId("history-path")).toHaveText(csv.split(",").map((s) => s.trim()));
});

When("I choose the history entry {string}", async ({ page }, path: string) => {
  await page.getByTestId("history-entry").filter({ hasText: path }).click();
  await expect(page.getByTestId("history-panel")).toHaveCount(0);
});

When("I clear the query history", async ({ page }) => {
  await page.getByRole("button", { name: "Clear history" }).click();
});

Then("the query history is empty", async ({ page }) => {
  await expect(page.getByTestId("history-empty")).toBeVisible();
  await expect(page.getByTestId("history-entry")).toHaveCount(0);
  expect(await page.evaluate(() => window.localStorage.getItem("dv.explore.history"))).toBeNull();
});
