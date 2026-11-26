import type { Page } from "playwright-core";

/** Automations and its schedule form at phone and desktop widths. */
export function automationScreens() {
  return [
    {
      name: "scheduled",
      path: "/automations",
      act: async (page: Page) => {
        await page.getByRole("heading", { name: "Automations", exact: true }).waitFor();
        await page.getByRole("button", { name: "New Automation", exact: true }).waitFor();
      },
    },
    {
      name: "schedule-builder",
      path: "/automations",
      act: async (page: Page) => {
        await page.getByRole("button", { name: "New Automation", exact: true }).click();
        await page.getByLabel("Name", { exact: true }).fill("nightly-checks");
        await page
          .getByLabel("What to ask", { exact: true })
          .fill("Which checks failed overnight, and what is the smallest fix for each?");
      },
    },
  ];
}
