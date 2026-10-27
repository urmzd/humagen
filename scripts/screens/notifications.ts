/** Real Notifications stories: one feed, one filter panel, narrow layouts and native actions. */
import assert from "node:assert/strict";
import { mkdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { chromium } from "playwright-core";
import { collect, DESKTOP, judge, PHONE } from "./checks.ts";

const base = process.env.STORYBOOK_URL ?? "http://127.0.0.1:6007";
const out = resolve(import.meta.dirname, "../../.screens/notifications");
mkdirSync(out, { recursive: true });
const browser = await chromium.launch({
  executablePath:
    process.env.CHROMIUM ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  headless: true,
});
const results: { story: string; theme: string; width: number }[] = [];
try {
  for (const [story, width, height] of [
    ["unified", 1440, 900],
    ["unified", 768, 1024],
    ["phone", 390, 844],
    ["phone", 320, 844],
    ["empty", 1280, 800],
    ["read-failed", 1280, 800],
    ["filters", 390, 844],
    ["filters", 1440, 900],
    ["alert-rules", 390, 844],
    ["alert-rules", 1440, 900],
  ] as const) {
    for (const theme of ["light", "dark"]) {
      const page = await browser.newPage({
        viewport: { width, height },
        reducedMotion: "reduce",
        isMobile: width < 768,
        hasTouch: width < 768,
      });
      page.setDefaultTimeout(10000);
      const errors: string[] = [];
      page.on("pageerror", (error) => errors.push(error.message));
      await page.goto(
        `${base}/iframe.html?id=workspace-notifications--${story}&viewMode=story&globals=theme:${theme}`,
        { waitUntil: "networkidle" },
      );
      await page
        .getByRole("heading", {
          name: story === "alert-rules" ? "Alert Rules" : "Notifications",
          exact: true,
        })
        .waitFor();
      await page.evaluate(() => document.fonts.ready);
      assert.equal(await page.getByRole("link", { name: "Needs You", exact: true }).count(), 0);
      assert.equal(await page.getByRole("link", { name: "Updates", exact: true }).count(), 0);
      assert.equal(await page.getByRole("heading", { name: /Needs you|What's new/ }).count(), 0);
      if (story === "filters") {
        const panel = page.getByRole("dialog", { name: "Filter notifications" });
        await panel.waitFor();
        assert.equal(await panel.getByRole("combobox").count(), 2);
        await panel.getByRole("combobox", { name: "Status", exact: true }).selectOption("resolved");
        await panel.getByRole("combobox", { name: "Type", exact: true }).selectOption("mention");
        await panel.getByRole("button", { name: "Reset", exact: true }).click();
      }
      const problems = judge(await page.evaluate(collect), width < 768 ? PHONE : DESKTOP);
      assert.deepEqual(problems, [], `${story} ${theme} ${width}px`);
      assert.deepEqual(errors, [], `${story} browser errors`);
      await page.screenshot({ path: resolve(out, `${story}-${theme}-${width}.png`) });
      if (story === "unified" && width === 1440) {
        await page.locator('[data-message="file"]').click();
        const reader = page.getByRole("region", { name: "Message", exact: true });
        const detail = reader.getByText(
          "The checklist now includes supplier dates and the handover notes.",
          { exact: true },
        );
        await detail.waitFor();
        await reader.getByRole("button", { name: "Dismiss", exact: true }).click();
        await page
          .locator('[data-message="file"]')
          .getByText(/^dismissed$/i)
          .waitFor();
        await page.locator('[data-message="file"]').click();
        await page.getByRole("button", { name: "Filter notifications", exact: true }).click();
        const panel = page.getByRole("dialog", { name: "Filter notifications" });
        await panel
          .getByRole("combobox", { name: "Type", exact: true })
          .selectOption("clarification");
        await page
          .getByRole("searchbox", { name: "Search notifications", exact: true })
          .fill("unmatched");
        await detail.waitFor();
        await page.keyboard.press("Escape");
      }
      results.push({ story, theme, width });
      await page.close();
    }
  }
  writeFileSync(
    resolve(out, "checks.json"),
    JSON.stringify({ passed: results.length, cases: results }, null, 2),
  );
  process.stdout.write(`${results.length} Notifications browser states passed.\n`);
} finally {
  await browser.close();
}
