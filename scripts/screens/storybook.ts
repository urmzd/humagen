/** Checks the actual Storybook UI in Chromium. No provider or workspace writes. */
import assert from "node:assert/strict";
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { chromium } from "playwright-core";
import { collect, DESKTOP, judge, PHONE } from "./checks.ts";

const base = process.env.STORYBOOK_URL ?? "http://127.0.0.1:6006";
const executablePath =
  process.env.CHROMIUM ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
assert.ok(existsSync(executablePath), "Set CHROMIUM to an installed Chromium executable");
const out = resolve(import.meta.dirname, "../../.screens/storybook");
mkdirSync(out, { recursive: true });
const browser = await chromium.launch({ executablePath, headless: true });
const results: { story: string; theme: string; width: number }[] = [];
try {
  for (const [story, width, height] of [
    ["overview", 1440, 900],
    ["overview", 768, 1024],
    ["phone", 390, 844],
    ["phone", 320, 844],
    ["empty", 1280, 800],
    ["loading", 1280, 800],
    ["read-failed", 1280, 800],
    ["older-page-failed", 1280, 800],
    ["filter-and-open", 1280, 800],
    ["navigation", 1280, 800],
  ] as const) {
    for (const theme of ["light", "dark"]) {
      const page = await browser.newPage({ viewport: { width, height }, reducedMotion: "reduce" });
      const errors: string[] = [];
      page.on("pageerror", (error) => errors.push(error.message));
      await page.goto(
        `${base}/iframe.html?id=workspace-project-work--${story}&viewMode=story&globals=theme:${theme}`,
        { waitUntil: "networkidle" },
      );
      await page.getByRole("heading", { name: "Northstar launch", exact: true }).waitFor();
      await page.evaluate(() => document.fonts.ready);
      const problems = judge(await page.evaluate(collect), width < 768 ? PHONE : DESKTOP);
      assert.deepEqual(problems, [], `${story} ${theme} ${width}px layout`);
      assert.deepEqual(errors, [], `${story} browser errors`);
      if (story === "overview" && width === 1440) {
        const library = page.getByRole("link", { name: "Library", exact: true });
        await library.focus();
        await page.keyboard.press("Enter");
        assert.equal(await library.evaluate((element) => element === document.activeElement), true);
        const work = page
          .getByRole("main")
          .getByRole("region", { name: "Conversations", exact: true });
        await work.getByRole("button", { name: "Needs Attention 1", exact: true }).click();
        assert.equal(
          await work.getByRole("button", { name: /Check the revised pricing model/ }).count(),
          1,
        );
        assert.equal(await work.getByRole("button", { name: /Draft the launch brief/ }).count(), 0);
        await work.getByRole("button", { name: "All 4", exact: true }).click();
      }
      await page.screenshot({ path: resolve(out, `${story}-${theme}-${width}.png`) });
      results.push({ story, theme, width });
      await page.close();
    }
  }
  const gallery = await browser.newPage();
  await gallery.goto(`${base}/iframe.html?id=foundations-typography--scale&viewMode=story`, {
    waitUntil: "networkidle",
  });
  assert.notEqual(
    await gallery.evaluate(() => getComputedStyle(document.body).overflowY),
    "hidden",
    "Ordinary component stories retain document scrolling",
  );
  await gallery.close();
  writeFileSync(
    resolve(out, "checks.json"),
    JSON.stringify({ passed: results.length, cases: results }, null, 2),
  );
  process.stdout.write(
    `${results.length} Storybook browser states passed, including keyboard navigation and scoped filters.\n`,
  );
} finally {
  await browser.close();
}
