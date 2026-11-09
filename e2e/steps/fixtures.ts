import { test as base, createBdd } from "playwright-bdd";

/** Per-scenario state shared between steps. */
export interface ScenarioState {
  download?: { name: string; text: string };
}

export const test = base.extend<{ state: ScenarioState }>({
  // Playwright requires a destructuring pattern for the fixture argument.
  // eslint-disable-next-line no-empty-pattern
  state: async ({}, use) => {
    await use({});
  },
});
export const { Given, When, Then } = createBdd(test);
