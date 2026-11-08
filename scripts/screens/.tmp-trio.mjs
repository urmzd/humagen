import { chromium } from "playwright-core";
const [out, session] = process.argv.slice(2);
const exe = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const browser = await chromium.launch({ executablePath: exe, args: ["--no-sandbox"], headless: true });
const pages = [["/", "home"], [`/sessions/${session}`, "session"], ["/projects/smoke-sched", "project"]];
for (const scheme of ["light", "dark"]) {
  for (const [w, h, size] of [[1280, 860, "1280"], [390, 844, "390"]]) {
    const ctx = await browser.newContext({ viewport: { width: w, height: h }, colorScheme: scheme });
    for (const [path, tag] of pages) {
      const page = await ctx.newPage();
      const errors = [];
      page.on("pageerror", (e) => errors.push(String(e).slice(0, 120)));
      await page.goto(`http://127.0.0.1:5173${path}`, { waitUntil: "load" });
      await page.waitForSelector("#composer-input", { timeout: 30000 }).catch(() => {});
      await page.waitForTimeout(2500);
      const info = await page.evaluate(() => {
        const box = document.querySelector("#composer-input");
        const form = box?.closest("form");
        const r = form?.getBoundingClientRect();
        const buttons = form ? [...form.querySelectorAll("button")].map((b) => b.getAttribute("aria-label") || b.textContent?.trim()).filter(Boolean) : [];
        return {
          overflow: document.documentElement.scrollWidth > window.innerWidth,
          placeholder: box?.getAttribute("placeholder"),
          radius: form ? getComputedStyle(form).borderRadius : null,
          height: r ? Math.round(r.height) : null,
          buttons,
        };
      });
      console.log(scheme, size, tag, JSON.stringify(info), "errors", errors.length);
      await page.screenshot({ path: `${out}/trio-${tag}-${size}-${scheme}.png` });
      await page.close();
    }
    await ctx.close();
  }
}
await browser.close();
