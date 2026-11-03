import assert from "node:assert/strict";
import type { Page } from "playwright-core";

/**
 * The sidebar's conversation list (DES-049): Recents with each row's dot and
 * age, its hover actions, the Projects view and Archived. The seeded rows are
 * dressed, in the browser only, with the states a live workspace has: one
 * waiting on a person, one running and one with an unread answer.
 */
const THREADS = /\/api\/threads(?:\?|$)/;
const MARKS = /\/api\/marks$/;

async function dress(page: Page, archived: string[] = []) {
  const since = new Date(Date.now() - 30 * 24 * 3_600_000).toISOString();
  let order: string[] = [];
  let listed = () => {};
  const seen = new Promise<void>((r) => {
    listed = r;
  });
  await page.route(THREADS, async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    const rows = (body.threads ?? []) as Record<string, unknown>[];
    order = rows.map((r) => String(r.id));
    const now = Date.now();
    rows.forEach((row, i) => {
      if (i === 0)
        Object.assign(row, { active: 1, waiting: 1, title: "Approve the vendor contract" });
      if (i === 1) Object.assign(row, { active: 1, waiting: 0 });
      if (i === 2)
        Object.assign(row, {
          active: 0,
          waiting: 0,
          failed: 0,
          last_agent: true,
          updated_at: new Date(now - 60_000).toISOString(),
        });
    });
    await route.fulfill({ response, json: body });
    listed();
  });
  // The marks name the last listed row as archived, so they wait for the list.
  await page.route(MARKS, async (route) => {
    await seen;
    await route.fulfill({
      json: {
        archived: archived.length ? archived : order.slice(-1),
        read: {},
        tips: ["search"],
        since,
      },
    });
  });
}

async function undress(page: Page) {
  await page.unroute(THREADS);
  await page.unroute(MARKS);
}

async function openSidebar(page: Page) {
  const phone = (page.viewportSize()?.width ?? 0) < 768;
  const button = page.getByRole("button", { name: /^Open sidebar/ });
  await page
    .getByRole("button", { name: "Close sidebar", exact: true })
    .or(button)
    .first()
    .waitFor();
  if (phone && (await button.isVisible())) await button.click();
  return page.getByRole("complementary", { name: "Navigation" });
}

async function pick(page: Page, view: RegExp) {
  const nav = page.getByRole("complementary", { name: "Navigation" });
  const filter = nav.getByRole("button", { name: /^Filter conversations/ });
  await filter.click();
  await page.getByRole("button", { name: view }).click();
  await filter.click();
}

type Scene = {
  name: string;
  path: string;
  rules?: { target: number };
  act: (page: Page) => Promise<void>;
  after?: () => Promise<void>;
};

export function sidebarViews(): Scene[] {
  let held: Page | undefined;
  const release = async () => {
    if (held) await undress(held);
    held = undefined;
  };
  const scene = (name: string, act: (page: Page) => Promise<void>): Scene => ({
    name,
    path: "/settings/profile",
    rules: { target: 24 },
    act: async (page) => {
      held = page;
      await dress(page);
      await page.reload();
      // Nothing hovered or focused from the screen before: a row's hover
      // actions would hide its age and dot.
      const vp = page.viewportSize();
      await page.mouse.move((vp?.width ?? 800) - 4, (vp?.height ?? 600) - 4);
      await act(page);
    },
    after: release,
  });
  return [
    scene("sidebar-recents", async (page) => {
      const nav = await openSidebar(page);
      const list = nav.getByRole("list", { name: "Recents" });
      await list.locator("[data-standing='needs']").first().waitFor();
      await list.locator("[data-standing='running']").first().waitFor();
      await list.locator("[data-row-age] [data-standing='unread']").first().waitFor();
      assert.equal(
        await list
          .locator("[data-standing='needs']")
          .first()
          .evaluate((el) => getComputedStyle(el).animationName),
        "none",
        "a row waiting on a person does not animate",
      );
    }),
    scene("sidebar-row-actions", async (page) => {
      const nav = await openSidebar(page);
      const row = nav.getByRole("list", { name: "Recents" }).locator("li").first();
      if ((page.viewportSize()?.width ?? 0) >= 768) {
        await row.hover();
        await row.getByRole("button", { name: /^Pin / }).waitFor();
        await row.getByRole("button", { name: /^Archive / }).waitFor();
      } else {
        await row.getByRole("button", { name: /^Manage / }).click();
        await page.getByRole("menuitem", { name: /^Archive/ }).waitFor();
      }
    }),
    scene("sidebar-projects", async (page) => {
      await openSidebar(page);
      await pick(page, /^Projects$/);
      const nav = page.getByRole("complementary", { name: "Navigation" });
      await nav.getByRole("list", { name: "Projects" }).waitFor();
      await nav.getByText("No conversations").first().waitFor({ state: "attached" });
    }),
    scene("sidebar-archived", async (page) => {
      await openSidebar(page);
      await pick(page, /^Archived/);
      const nav = page.getByRole("complementary", { name: "Navigation" });
      await nav
        .getByRole("list", { name: "Archived" })
        .getByRole("button", { name: /^Manage / })
        .first()
        .waitFor();
    }),
  ];
}
