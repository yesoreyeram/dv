import { expect } from "@playwright/test";
import { Then } from "./fixtures";

Then("I see the status {string}", async ({ page }, status: string) => {
  await expect(page.getByRole("status").getByText(status)).toBeVisible();
});

Then("I see the server version", async ({ page }) => {
  const version = page.getByTestId("build-Version");
  await expect(version).not.toHaveText("—");
  await expect(version).toHaveText(/\S+/);
});

Then("I see the endpoint {string} {string}", async ({ page }, method: string, path: string) => {
  const row = page.getByTestId("endpoint").filter({ hasText: path });
  await expect(row.getByText(method, { exact: true })).toBeVisible();
  await expect(row.getByText(path, { exact: true })).toBeVisible();
});
