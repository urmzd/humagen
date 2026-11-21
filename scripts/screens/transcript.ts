/**
 * Captures the transcript gallery (apps/web/transcript-gallery.html) for each
 * scene at phone and desktop width, in light and dark: the evidence for the
 * transcript's failures, connections, references and step cards. Run it
 * against a dev server already serving the console:
 *
 *   cd apps/web && pnpm exec vite --port 5391
 *   TRANSCRIPT_URL=http://127.0.0.1:5391 TRANSCRIPT_OUT=out \
 *     node --experimental-strip-types scripts/screens/transcript.ts
 *
 * Each scene names the interaction that shows it: a failed count opened to its
 * steps, a step group opened to its card.
 */
import { existsSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { chromium, type Page } from "playwright-core";

const base = process.env.TRANSCRIPT_URL ?? "http://127.0.0.1:5391";
const out = process.env.TRANSCRIPT_OUT ?? "transcript-screens";
const only = process.env.TRANSCRIPT_ONLY?.split(",").filter(Boolean);
const viewports = [
  { width: 1280, height: 800 },
  { width: 390, height: 844 },
];

type Scene = { name: string; scene: string; act?: (page: Page) => Promise<void> };

/** Opens the first step group whose line matches, when it has a button that opens it. */
async function openGroup(page: Page, text: RegExp) {
  const line = page.getByRole("button", { name: text }).first();
  if ((await line.count()) && (await line.getAttribute("aria-expanded")) === "false")
    await line.click();
}

const scenes: Scene[] = [
  {
    name: "failed-delegated",
    scene: "failed",
    act: async (page) => {
      // After: the failed count opens straight to the failed steps.
      const failed = page.getByRole("button", { name: /2 failed/ }).first();
      if (await failed.count()) await failed.click();
      else await openGroup(page, /Read 2 files/);
    },
  },
  { name: "connect-card", scene: "connect" },
  { name: "pr-chips", scene: "refs" },
  {
    name: "tool-card",
    scene: "tools",
    act: async (page) => {
      const line = page.locator('[data-testid="step-group"] button[aria-expanded="false"]').first();
      if (await line.count()) await line.click();
      // Open the command's own row to its input and output.
      const row = page.locator('[data-testid="tool-row"][data-tool="bash"] button').first();
      if (await row.count()) await row.click();
    },
  },
  { name: "running-step", scene: "running" },
];

function chrome(): string {
  const found = [
    process.env.CHROMIUM,
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    "/usr/bin/google-chrome",
    "/usr/bin/chromium",
  ].find((c) => c && existsSync(c));
  if (!found) throw new Error("No Chrome found; set CHROMIUM.");
  return found;
}

const browser = await chromium.launch({ executablePath: chrome(), args: ["--no-sandbox"] });
try {
  for (const theme of ["light", "dark"] as const)
    for (const vp of viewports) {
      const context = await browser.newContext({
        viewport: vp,
        deviceScaleFactor: 2,
        colorScheme: theme,
        reducedMotion: "reduce",
      });
      const page = await context.newPage();
      for (const s of scenes) {
        if (only && !only.includes(s.name)) continue;
        await page.goto(`${base}/transcript-gallery.html?scene=${s.scene}&theme=${theme}`, {
          waitUntil: "networkidle",
        });
        await page.locator("article").first().waitFor();
        await s.act?.(page);
        await page.waitForTimeout(400);
        const dir = join(out, theme, `${vp.width}x${vp.height}`);
        mkdirSync(dir, { recursive: true });
        await page.screenshot({ path: join(dir, `${s.name}.png`) });
      }
      await context.close();
    }
} finally {
  await browser.close();
}
