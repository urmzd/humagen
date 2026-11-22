/**
 * Records the README and website demo from a live local preview on real
 * models, and the gallery stills beside it.
 *
 *   make dev-live            # in one terminal, after building the console
 *   make showcase            # in another
 *
 * Writes, for each of light and dark:
 *   showcase/demo-<scheme>.gif           the README hero (1200 wide)
 *   apps/site/public/demo/demo-<scheme>.mp4 and .jpg   the website hero and its poster
 *   apps/site/src/assets/console/answer-<scheme>.png   the answer the demo ends on
 *   showcase/<page>-<scheme>.png and the site's copy   agents, usage, policies
 *
 * Every frame is the real console calling real models: nothing is seeded or
 * replayed. Idle stretches longer than MAX_GAP are shortened, nothing else.
 * Needs Chrome and ffmpeg. HUMAGEN_URL defaults to http://127.0.0.1:8080.
 */
import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { type Browser, chromium } from "playwright-core";

const base = process.env.HUMAGEN_URL ?? "http://127.0.0.1:8080";
const repo = new URL("../../", import.meta.url).pathname;
const showcase = join(repo, "showcase");
const siteAssets = join(repo, "apps/site/src/assets/console");
const sitePublic = join(repo, "apps/site/public/demo");
const viewport = { width: 1280, height: 800 };
const MAX_GAP = 0.9;
const HOLD = 4;

const mention = "@lea";
const prompt =
  "Humagen v1 ships today: self-hosted, any model per agent, every run recorded. Ask researcher why teams self-host agents, and writer for a three-sentence launch note.";

/** Runs that give the Agents and Usage pages real numbers to show. */
const warmups = [
  "Ask researcher for one fact about durable job queues, and writer to turn it into one sentence.",
  "Ask writer for a two-line changelog entry: agents now show their health on the Agents page.",
  "Ask researcher what an audit log should record for an AI agent run, in three bullets.",
];

const stills: [string, string][] = [
  ["agents", "/agents"],
  ["usage", "/operations/usage"],
  ["policies", "/operations/policies"],
];

type Frame = { name: string; t: number };

const ffmpeg = (...args: string[]) => execFileSync("ffmpeg", ["-loglevel", "error", "-y", ...args]);

async function record(browser: Browser, scheme: "light" | "dark", dir: string) {
  const ctx = await browser.newContext({ viewport, deviceScaleFactor: 2, colorScheme: scheme });
  const page = await ctx.newPage();
  await page.goto(base, { waitUntil: "load" });
  await page.waitForTimeout(1500);
  const cdp = await ctx.newCDPSession(page);
  const frames: Frame[] = [];
  cdp.on("Page.screencastFrame", async (f) => {
    const name = `f${String(frames.length).padStart(5, "0")}.png`;
    writeFileSync(join(dir, name), Buffer.from(f.data, "base64"));
    frames.push({ name, t: f.metadata.timestamp ?? 0 });
    await cdp.send("Page.screencastFrameAck", { sessionId: f.sessionId }).catch(() => {});
  });
  // The screencast sees the window, not Playwright's emulated viewport, so it
  // is given the same size and scale here; otherwise frames come out at 1x
  // and cropped to the window.
  await cdp.send("Emulation.setDeviceMetricsOverride", {
    width: viewport.width,
    height: viewport.height,
    deviceScaleFactor: 2,
    mobile: false,
  });
  await cdp.send("Page.startScreencast", {
    format: "png",
    maxWidth: viewport.width * 2,
    maxHeight: viewport.height * 2,
    everyNthFrame: 1,
  });
  await page.waitForTimeout(1200);
  const box = page.locator("textarea");
  await box.click();
  await box.pressSequentially(mention, { delay: 90 });
  await page.waitForTimeout(700);
  await page.keyboard.press("Enter");
  await box.pressSequentially(prompt, { delay: 22 });
  await page.waitForTimeout(500);
  await page.keyboard.press("Enter");
  // A finished answer can be rated; a working one cannot.
  await page.getByRole("button", { name: "Helpful" }).first().waitFor({ timeout: 120_000 });
  await page.waitForTimeout(4500);
  await cdp.send("Page.stopScreencast");
  const answer = await page.locator("text=/agents?$/").count();
  if (!answer) console.warn(`${scheme}: the answer does not name its agents; check the take`);
  const raw = join(tmpdir(), `humagen-answer-${scheme}.png`);
  await page.screenshot({ path: raw });
  ffmpeg("-i", raw, "-vf", "scale=1600:-1:flags=lanczos", join(siteAssets, `answer-${scheme}.png`));
  await ctx.close();
  return frames;
}

function encode(scheme: string, dir: string, frames: Frame[]) {
  const lines = frames.map((f, i) => {
    const next = frames[i + 1];
    const dur = next ? Math.max(0.02, Math.min(next.t - f.t, MAX_GAP)) : HOLD;
    return `file '${f.name}'\nduration ${dur.toFixed(3)}`;
  });
  lines.push(`file '${frames.at(-1)?.name}'`);
  writeFileSync(join(dir, "concat.txt"), `${lines.join("\n")}\n`);
  mkdirSync(sitePublic, { recursive: true });
  const mp4 = join(sitePublic, `demo-${scheme}.mp4`);
  ffmpeg(
    ...["-f", "concat", "-safe", "0", "-i", join(dir, "concat.txt")],
    ...["-vf", "scale=1600:-2:flags=lanczos,fps=30,format=yuv420p"],
    ...["-c:v", "libx264", "-crf", "20", "-preset", "slow", "-movflags", "+faststart", mp4],
  );
  ffmpeg(
    ...["-i", mp4, "-vf"],
    "fps=12,scale=1200:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=128:stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle",
    join(showcase, `demo-${scheme}.gif`),
  );
  ffmpeg(
    "-sseof",
    "-0.1",
    "-i",
    mp4,
    "-frames:v",
    "1",
    "-update",
    "1",
    "-q:v",
    "3",
    join(sitePublic, `demo-${scheme}.jpg`),
  );
}

async function warm() {
  for (const text of warmups) {
    const res = await fetch(`${base}/api/runs`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ agent: "lead", text }),
    });
    if (!res.ok) throw new Error(`warm-up run: ${res.status} ${await res.text()}`);
  }
  await new Promise((r) => setTimeout(r, 40_000));
}

async function capture(browser: Browser) {
  for (const scheme of ["light", "dark"] as const) {
    const ctx = await browser.newContext({ viewport, deviceScaleFactor: 2, colorScheme: scheme });
    const page = await ctx.newPage();
    for (const [name, path] of stills) {
      await page.goto(base + path, { waitUntil: "load" });
      await page.waitForTimeout(2500);
      const raw = join(tmpdir(), `humagen-${name}-${scheme}.png`);
      await page.screenshot({ path: raw });
      const out = join(showcase, `${name}-${scheme}.png`);
      ffmpeg("-i", raw, "-vf", "scale=1600:-1:flags=lanczos", out);
      copyFileSync(out, join(siteAssets, `${name}-${scheme}.png`));
    }
    await ctx.close();
  }
}

const browser = await chromium.launch({ channel: "chrome" });
try {
  for (const scheme of ["light", "dark"] as const) {
    const dir = mkdtempSync(join(tmpdir(), `humagen-demo-${scheme}-`));
    const frames = await record(browser, scheme, dir);
    encode(scheme, dir, frames);
    rmSync(dir, { recursive: true, force: true });
    console.log(`${scheme}: ${frames.length} frames`);
  }
  await warm();
  await capture(browser);
} finally {
  await browser.close();
}
