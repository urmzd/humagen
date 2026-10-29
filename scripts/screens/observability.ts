import assert from "node:assert/strict";
import type { Page } from "playwright-core";
import { defaultCopy } from "../../packages/components/src/copy.ts";

/** The upper workspace host mounts these real API-backed Usage states. */
export function usageScreens(path = "/operations/usage") {
  const words = defaultCopy.usagePage;
  return [
    {
      name: "usage-chart",
      path,
      act: async (page: Page) => {
        await page.getByRole("button", { name: words.days(30), exact: true }).click();
        const days = page.getByRole("group", { name: words.chartLabel }).getByRole("button");
        await days.nth(29).waitFor();
        assert.equal(await days.count(), 30);
        await days.nth(29).click();
        await page.getByRole("button", { name: words.clearDay, exact: true }).waitFor();
        await page.getByRole("heading", { name: words.agents, exact: true }).waitFor();
      },
    },
    {
      name: "usage-table",
      path,
      act: async (page: Page) => {
        await page.getByRole("button", { name: words.days(7), exact: true }).click();
        await page.getByRole("button", { name: words.table, exact: true }).click();
        const table = page.getByRole("table", { name: words.chartTitle });
        await table.waitFor();
        await table.getByRole("button").last().click();
        await page.getByRole("button", { name: words.clearDay, exact: true }).waitFor();
        assert.equal(await table.getByRole("button").count(), 7);
      },
    },
  ];
}
