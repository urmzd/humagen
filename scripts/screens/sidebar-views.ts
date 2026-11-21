import assert from "node:assert/strict";
import type { Page } from "playwright-core";

/**
 * The sidebar's conversation list (DES-054): Recents grouped by day with each
 * row's dot, its hover actions, the Projects view and Archived. The seeded rows are
 * dressed, in the browser only, with the states a live workspace has: one
 * waiting on a person, one running and one with an unread answer.
 */
const THREADS = /\/api\/threads(?:\?|$)/;
const MARKS = /\/api\/marks$/;
const SESSIONS = /\/api\/sessions\?/;

async function dress(page: Page, tips: string[] = ["search"]) {
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
  // The sidebar lists sessions (#436). The seeded ones are failed runs with
  // nothing said in them, which the sidebar leaves to Alerts, so each is
  // dressed as holding one message to be listed under its project.
  await page.route(SESSIONS, async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    for (const session of (body.sessions ?? []) as { conversation?: Record<string, unknown> }[])
      if (session.conversation && !session.conversation.messages) session.conversation.messages = 1;
    await route.fulfill({ response, json: body });
  });
  // The marks name the last listed row as archived, so they wait for the list.
  await page.route(MARKS, async (route) => {
    await seen;
    await route.fulfill({
      json: {
        archived: order.slice(-1),
        read: {},
        tips,
        since,
      },
    });
  });
}

async function undress(page: Page) {
  await page.unroute(THREADS);
  await page.unroute(MARKS);
  await page.unroute(SESSIONS);
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
  const scene = (name: string, act: (page: Page) => Promise<void>, tips?: string[]): Scene => ({
    name,
    path: "/settings/profile",
    rules: { target: 24 },
    act: async (page) => {
      held = page;
      await dress(page, tips);
      await page.reload();
      // Nothing hovered or focused from the screen before: a row's hover
      // actions would hide its dot.
      const vp = page.viewportSize();
      await page.mouse.move((vp?.width ?? 800) - 4, (vp?.height ?? 600) - 4);
      await act(page);
    },
    after: release,
  });
  return [
    scene("sidebar-search", async (page) => {
      const nav = await openSidebar(page);
      await nav.getByRole("button", { name: "Search", exact: true }).waitFor();
      const sections = nav.getByRole("navigation", { name: "Sections" });
      for (const name of ["New", "Notifications", "History", "Scheduled", "Customize"])
        await sections.getByRole("link", { name: new RegExp(`^${name}\\b`) }).waitFor();
      await sections.getByRole("button", { name: "More", exact: true }).click();
      for (const name of ["Projects", "People", "Library", "Operations"])
        await page.getByRole("link", { name, exact: true }).waitFor();
      await page.keyboard.press("Escape");
    }),
    // DES-055: a row's preview, on hover with a mouse and a long press on a phone.
    scene("sidebar-preview", async (page) => {
      const nav = await openSidebar(page);
      // Sessions are grouped by project (#436), and a group can start folded:
      // unfold the first group that holds a session and preview its first row.
      const group = nav
        .getByRole("list", { name: "Projects" })
        .locator("details")
        .filter({ has: page.locator("[data-thread-row]") })
        .first();
      await group.waitFor({ state: "attached" });
      if (!(await group.evaluate((el) => (el as HTMLDetailsElement).open)))
        await group.locator("summary").click();
      const row = group.locator("[data-thread-row]").first();
      await row.waitFor();
      if ((page.viewportSize()?.width ?? 0) >= 768) {
        await row.hover();
      } else {
        const box = await row.boundingBox();
        assert.ok(box);
        await row.dispatchEvent("touchstart", {
          touches: [{ identifier: 1, clientX: box.x + 20, clientY: box.y + 10 }],
        });
      }
      const card = page.locator("[data-peek]");
      await card.waitFor();
      assert.match((await card.textContent()) ?? "", /(ago|Just now)$/);
      assert.equal(
        await row.getAttribute("aria-describedby"),
        await card.getAttribute("id"),
        "the row names its card while it shows",
      );
      if ((page.viewportSize()?.width ?? 0) >= 768) {
        const [r, c] = await Promise.all([row.boundingBox(), card.boundingBox()]);
        assert.ok(r && c);
        assert.ok(
          Math.abs(r.y + r.height / 2 - (c.y + c.height / 2)) <= 1,
          "the card is centred on its row",
        );
      }
    }),
    scene("sidebar-recents", async (page) => {
      const nav = await openSidebar(page);
      const list = nav.getByRole("list", { name: "Recents" });
      await list.locator("[data-standing='needs']").first().waitFor();
      await list.locator("[data-standing='running']").first().waitFor();
      await list.locator("[data-standing='unread']").first().waitFor();
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
      const row = nav
        .getByRole("list", { name: "Recents" })
        .locator("li:has(> [data-thread-row])")
        .first();
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
      await pick(page, /^Archived(?: \(\d+\))?$/);
      const nav = page.getByRole("complementary", { name: "Navigation" });
      await nav
        .getByRole("list", { name: "Archived" })
        .getByRole("button", { name: /^Manage / })
        .first()
        .waitFor();
    }),
  ];
}

/** DES-055: the wave's preview of a turn carries its author's mark. */
export function wavePreview(path: string): Scene {
  return {
    name: "conversation-wave-preview",
    path,
    act: async (page) => {
      const wave = page.getByRole("slider", { name: "Turns in this conversation" });
      await wave.waitFor();
      await wave.focus();
      await page.keyboard.press("Home");
      const tip = page.getByRole("tooltip");
      await tip.waitFor();
      assert.ok(
        (await tip.locator("[data-mark='person'], .aui-initials").count()) > 0,
        "the preview shows who said it",
      );
    },
  };
}

/** DES-057: the composer's chrome, the project in Home's bar and a conversation's approval mode. */
export function composerChrome(conversation: string): Scene[] {
  return [
    {
      name: "home-project-above",
      path: "/",
      act: async (page) => {
        assert.equal(await page.locator("[data-home-above]").count(), 0);
        await page
          .locator(".humagen-home")
          .getByRole("button", { name: /^Approval mode: / })
          .waitFor();
        assert.equal(await page.getByRole("button", { name: /^Select project/ }).count(), 0);
      },
    },
    {
      name: "composer-approval-mode",
      path: `/conversations/${conversation}`,
      // The menu open: Ask and Auto under their heading, each with its one line.
      act: async (page) => {
        await page.getByRole("button", { name: /^Approval mode: / }).click();
        await page.getByRole("menu", { name: "Approval mode" }).waitFor();
      },
    },
  ];
}
