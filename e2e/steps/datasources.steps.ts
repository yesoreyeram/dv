import { expect } from "@playwright/test";
import { Then } from "./fixtures";

Then("I see the plugin catalogue", async ({ page }) => {
  const catalogue = page.getByTestId("plugin-catalogue");
  await expect(catalogue).toBeVisible();
  await expect(catalogue.getByRole("heading", { name: "Data source types" })).toBeVisible();
  await expect(catalogue.getByRole("heading", { name: "File formats" })).toBeVisible();
});
