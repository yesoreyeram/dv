import { expect } from "@playwright/test";
import { Then, When } from "./fixtures";

const rowsSelector = "Rows (jq)";
const optionsField = "Parameters, pagination and columns (JSON)";

When("I enter the rows selector {string}", async ({ page }, value: string) => {
  await page.getByRole("textbox", { name: rowsSelector }).fill(value);
});

When("I enter the rows selector:", async ({ page }, doc: string) => {
  await page.getByRole("textbox", { name: rowsSelector }).fill(doc.trim());
});

When("I enter the connection options:", async ({ page }, doc: string) => {
  await page.getByRole("textbox", { name: optionsField }).fill(doc.trim());
});

Then("the connection options field is marked invalid", async ({ page }) => {
  await expect(page.getByRole("textbox", { name: optionsField })).toHaveAttribute(
    "aria-invalid",
    "true",
  );
});

Then("the file format fields are hidden", async ({ page }) => {
  await expect(page.getByRole("textbox", { name: rowsSelector })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Format" })).toHaveCount(0);
  await expect(page.getByRole("textbox", { name: "Rows path" })).toHaveCount(0);
  await expect(page.getByRole("checkbox", { name: "Flatten nested objects" })).toHaveCount(0);
});
