import { expect } from "@playwright/test";
import { Then } from "./fixtures";

Then("I see the plugin catalogue", async ({ page }) => {
  const catalogue = page.getByTestId("plugin-catalogue");
  await expect(catalogue).toBeVisible();
  await expect(catalogue.getByRole("heading", { name: "Data source types" })).toBeVisible();
  await expect(catalogue.getByRole("heading", { name: "File formats" })).toBeVisible();
});

Then("the catalogue lists the data source type {string}", async ({ page }, name: string) => {
  const section = page.getByTestId("plugin-sources");
  await expect(section.getByRole("heading", { level: 3, name, exact: true })).toBeVisible();
});

Then("the catalogue lists the file format {string}", async ({ page }, name: string) => {
  const section = page.getByTestId("plugin-formats");
  await expect(section.getByRole("heading", { level: 3, name, exact: true })).toBeVisible();
});
