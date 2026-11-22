import { chromium } from "playwright-core";
const out = process.argv[2];
const browser = await chromium.launch({ channel: "chrome" });
const page = await (await browser.newContext({ viewport: { width: 1280, height: 900 }, colorScheme: "dark", deviceScaleFactor: 2 })).newPage();
page.on("pageerror", (e) => console.log("PAGEERROR", String(e).slice(0, 300)));
await page.goto("http://127.0.0.1:5180/", { waitUntil: "domcontentloaded" });
await page.locator("[data-layer='composer-footer']").first().waitFor({ timeout: 20000 });
await page.locator("textarea, [contenteditable='true']").first().click();
await page.keyboard.type("@lead In one call each, at the same time, ask researcher for two facts on why teams self-host agents and ask writer for a two-sentence launch note. Then give me both.");
await page.keyboard.press("Escape");
await page.keyboard.press("Enter");
await page.waitForURL((u) => u.pathname !== "/", { timeout: 20000 });
console.log("SESSION", new URL(page.url()).pathname);
// While it works.
await page.waitForTimeout(9000);
await page.screenshot({ path: `${out}/real-working-dark-1280.png` });
// Until the answer's actions show (the turn settled), at most two minutes.
await page.locator("[data-testid='answer-refs']").first().waitFor({ timeout: 120000 }).catch(() => console.log("no answer refs"));
await page.waitForTimeout(6000);
await page.screenshot({ path: `${out}/real-done-dark-1280.png`, fullPage: false });
const info = await page.evaluate(() => JSON.stringify({ worked: document.body.innerText.match(/(Worked|Activity ·)[^\n]*/g), together: document.querySelectorAll('[data-aui="calls-together"]').length, lanes: [...document.querySelectorAll('article [data-testid="activity-lane"]')].map((l) => l.getAttribute("data-agent")), calls: document.querySelectorAll('[data-aui="agent-call"]').length }));
console.log(info);
// Open the first agent's thread in place.
const lane = page.locator("article [data-testid='activity-lane'] summary").first();
if (await lane.count()) { await lane.click(); await page.waitForTimeout(800); await page.screenshot({ path: `${out}/real-thread-open-dark-1280.png` }); }
await browser.close();
