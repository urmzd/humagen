import type { Page } from "playwright-core";

/** Real schedule creation and the event-condition builder at phone and desktop widths. */
export function automationScreens() {
  return [
    {
      name: "automations",
      path: "/automations",
      act: async (page: Page) => {
        await page.getByRole("heading", { name: "Automations", exact: true }).waitFor();
        await page.getByRole("heading", { name: "Schedules", exact: true }).waitFor();
        await page.getByRole("heading", { name: "GitHub triggers", exact: true }).waitFor();
      },
    },
    {
      name: "automation-builder",
      path: "/automations",
      act: async (page: Page) => {
        await page.getByRole("button", { name: "New GitHub Automation" }).click();
        await page.getByLabel("Name", { exact: true }).fill("Investigate failed checks");
        await page.getByLabel("Repository", { exact: true }).fill("urmzd/humagen");
        await page
          .getByLabel("Instructions")
          .fill("Investigate the failing check and explain the smallest fix with evidence.");
      },
    },
  ];
}
