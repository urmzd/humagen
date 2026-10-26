import assert from "node:assert/strict";
import type { Page } from "playwright-core";

/** Stress the real shell with long content, then wheel each column separately. */
export async function sidebarScroll(page: Page): Promise<void> {
  const phone = (page.viewportSize()?.width ?? 0) < 768;
  const content = page.locator("main .aui-scroll");
  await content.waitFor();
  await content.evaluate((element) => {
    const filler = document.createElement("div");
    filler.setAttribute("data-scroll-fixture", "");
    filler.style.height = "2400px";
    element.append(filler);
  });
  if (phone) await page.getByRole("button", { name: /^Open sidebar/ }).click();
  const sidebar = page.getByRole("complementary", { name: "Navigation", includeHidden: true });
  const list = sidebar.locator(".aui-scroll").first();
  await list.evaluate((element) => {
    const fixture = document.createElement("ul");
    fixture.setAttribute("data-scroll-fixture", "");
    fixture.style.flexShrink = "0";
    for (let i = 0; i < 80; i++) {
      const row = document.createElement("li");
      row.textContent = `Older conversation ${i + 1}`;
      row.style.padding = "8px 16px";
      fixture.append(row);
    }
    element.append(fixture);
  });
  const geometry = () =>
    sidebar.evaluate((element) => {
      const box = element.getBoundingClientRect();
      const header = element.firstElementChild?.getBoundingClientRect();
      const footer = element.lastElementChild?.getBoundingClientRect();
      return { top: box.top, bottom: box.bottom, header: header?.height, footer: footer?.bottom };
    });
  const initial = await geometry();
  assert.equal(initial.top, 0, "sidebar starts at the viewport top");
  assert.equal(initial.bottom, page.viewportSize()?.height, "sidebar fits the viewport");
  assert.equal(initial.header, 48, "sidebar header does not shrink");
  assert.equal(initial.footer, initial.bottom, "account controls stay at the bottom");
  await list.hover();
  await page.mouse.wheel(0, 600);
  await page.waitForFunction(
    () => (document.querySelector("aside > .aui-scroll")?.scrollTop ?? 0) > 0,
  );
  assert.equal(
    await content.evaluate((element) => element.scrollTop),
    0,
    "sidebar scroll leaves content still",
  );
  assert.deepEqual(await geometry(), initial, "sidebar chrome stays fixed");
  await list.evaluate((element) => {
    element.scrollTop = element.scrollHeight;
  });
  await page.mouse.wheel(0, 1000);
  // Allow wheel dispatch to finish before checking for scroll chaining.
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  );
  assert.equal(
    await content.evaluate((element) => element.scrollTop),
    0,
    "sidebar boundary does not scroll content",
  );
  assert.equal(await page.evaluate(() => window.scrollY), 0, "document stays still");
  const listTop = await list.evaluate((element) => element.scrollTop);
  if (phone) await page.getByRole("button", { name: "Close sidebar", exact: true }).click();
  await content.hover();
  await page.mouse.wheel(0, 600);
  await page.waitForFunction(
    () => (document.querySelector("main .aui-scroll")?.scrollTop ?? 0) > 0,
  );
  assert.equal(await page.evaluate(() => window.scrollY), 0, "content stays within its column");
  if (phone) await page.getByRole("button", { name: /^Open sidebar/ }).click();
  assert.equal(
    await list.evaluate((element) => element.scrollTop),
    listTop,
    "content scroll leaves sidebar position still",
  );
  assert.deepEqual(await geometry(), initial, "content scroll leaves sidebar chrome fixed");
  // Screenshots show the normal page after the long-list behavior is checked.
  await page.locator("[data-scroll-fixture]").evaluateAll((elements) => {
    for (const element of elements) element.remove();
  });
  await list.evaluate((element) => {
    element.scrollTop = 0;
  });
  await content.evaluate((element) => {
    element.scrollTop = 0;
  });
  if (phone) await page.getByRole("button", { name: "Close sidebar", exact: true }).click();
}
