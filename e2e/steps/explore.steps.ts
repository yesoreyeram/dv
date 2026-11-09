import { expect, type Page } from "@playwright/test";
import { Then, When } from "./fixtures";

const resultTable = (page: Page) => page.getByTestId("table-panel").getByRole("table");
const pathField = (page: Page) => page.getByRole("textbox", { name: "Path" });
const header = (page: Page, name: string) =>
  resultTable(page).getByRole("columnheader", { name: new RegExp(`^${name}\\b`) });

When("I choose the data source {string}", async ({ page }, name: string) => {
  await page.getByRole("combobox", { name: "Data source" }).selectOption({ label: name });
});

When("I enter the path {string}", async ({ page }, path: string) => {
  await pathField(page).fill(path);
});

When("I press Run", async ({ page }) => {
  await page.getByRole("button", { name: "Run query" }).click();
});

When("I press Control+Enter in the path field", async ({ page }) => {
  await pathField(page).press("Control+Enter");
});

Then("the result table has {int} rows", async ({ page }, n: number) => {
  // One header row plus n body rows.
  await expect(resultTable(page).getByRole("row")).toHaveCount(n + 1);
});

Then("the result table has the column {string}", async ({ page }, name: string) => {
  await expect(header(page, name)).toBeVisible();
});

Then("the query statistics mention {string}", async ({ page }, text: string) => {
  await expect(page.getByTestId("query-stats")).toContainText(text);
});

When("I sort by the column {string}", async ({ page }, name: string) => {
  await header(page, name).getByRole("button").click();
});

Then(
  "the column {string} is sorted {string}",
  async ({ page }, name: string, direction: string) => {
    await expect(header(page, name)).toHaveAttribute("aria-sort", direction);
  },
);

Then(
  "the first cell of the column {string} is {string}",
  async ({ page }, name: string, value: string) => {
    const headers = await resultTable(page).getByRole("columnheader").allTextContents();
    const idx = headers.findIndex((h) => h.startsWith(name));
    if (idx < 0) throw new Error(`no column ${name} in ${headers.join(", ")}`);
    const firstRow = resultTable(page).getByRole("row").nth(1);
    await expect(firstRow.getByRole("cell").nth(idx)).toHaveText(value);
  },
);

Then("the path field contains {string}", async ({ page }, value: string) => {
  await expect(pathField(page)).toHaveValue(value);
});

Then("no result is shown", async ({ page }) => {
  await expect(page.getByTestId("explore-empty")).toBeVisible();
  await expect(page.getByTestId("table-panel")).toHaveCount(0);
});

Then("I see an error alert mentioning {string}", async ({ page }, text: string) => {
  await expect(page.getByTestId("query-error")).toContainText(text);
});

Then("the path field is marked invalid", async ({ page }) => {
  await expect(pathField(page)).toHaveAttribute("aria-invalid", "true");
});

Then("the query statistics show the query time and the total request time", async ({ page }) => {
  // FR-018 AC4: the server's durationMs and the browser round trip, labelled separately.
  await expect(page.getByTestId("query-stats")).toHaveText(
    /query time \d[\d,.\s]* ms · total request time \d[\d,.\s]* ms/,
  );
});

When("I download the result as CSV", async ({ page, state }) => {
  const [download] = await Promise.all([
    page.waitForEvent("download"),
    page.getByRole("button", { name: "Download CSV" }).click(),
  ]);
  const stream = await download.createReadStream();
  const chunks: Buffer[] = [];
  for await (const chunk of stream) chunks.push(chunk as Buffer);
  const bytes = Buffer.concat(chunks);
  // FR-018 AC7: UTF-8 with a byte order mark.
  expect([...bytes.subarray(0, 3)]).toEqual([0xef, 0xbb, 0xbf]);
  state.download = { name: download.suggestedFilename(), text: bytes.subarray(3).toString("utf8") };
});

const downloaded = (state: { download?: { name: string; text: string } }) => {
  if (!state.download) throw new Error("no file was downloaded");
  return state.download;
};

Then("the downloaded file is named {string}", ({ state }, name: string) => {
  expect(downloaded(state).name).toBe(name);
});

Then("the downloaded CSV has {int} lines", ({ state }, n: number) => {
  // CRLF line ends, with a final line break (RFC 4180).
  const { text } = downloaded(state);
  expect(text.endsWith("\r\n")).toBe(true);
  expect(text.slice(0, -2).split("\r\n")).toHaveLength(n);
});

Then("the downloaded CSV contains the line {string}", ({ state }, line: string) => {
  expect(downloaded(state).text.split("\r\n")).toContain(line);
});
