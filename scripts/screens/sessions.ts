import assert from "node:assert/strict";
import type { Page } from "playwright-core";
import {
  ALERT_TITLE,
  FINDING_TITLE,
  REVIEW_TITLE,
  type SessionFixtures,
} from "./session-fixtures.ts";

export interface SessionScreen {
  name: string;
  path: string;
  act?: (page: Page) => Promise<void>;
}

const composer = (page: Page) => page.locator('form[aria-label="Message"] textarea');
const details = (page: Page) => page.getByRole("dialog", { name: "Details", exact: true });

async function readySession(page: Page): Promise<void> {
  await composer(page).waitFor();
  await page.getByRole("button", { name: "Details", exact: true }).waitFor();
}

async function detailsReady(page: Page): Promise<void> {
  const panel = details(page);
  await panel.waitFor();
  const sections = panel.getByRole("tablist", { name: "Session details" });
  assert.deepEqual(await sections.getByRole("tab").allTextContents(), [
    "Files",
    "Sources",
    "Steps",
    "Sessions",
  ]);
  const viewport = page.viewportSize();
  assert.ok(viewport);
  assert.equal(await panel.evaluate((el) => el.matches(":modal")), viewport.width < 1024);
  const bounds = await panel.boundingBox();
  assert.ok(bounds && bounds.x >= -1 && bounds.x + bounds.width <= viewport.width + 1);
  if (viewport.width >= 1024) {
    const box = await composer(page).boundingBox();
    assert.ok(
      box && box.width > 0 && box.x + box.width <= bounds.x + 1,
      "Details reflows the composer rather than covering it",
    );
  }
}

async function filesReady(page: Page): Promise<void> {
  await detailsReady(page);
  const panel = details(page);
  const browser = panel.locator('[data-slot="file-browser"]');
  await browser.waitFor();
  const picker = browser.getByRole("button", { name: "Files", exact: true });
  if (await picker.isVisible()) {
    await picker.click();
    await page
      .getByRole("dialog", { name: "Files", exact: true })
      .getByRole("button", { name: "plan.md", exact: true })
      .click();
  } else {
    await browser.getByRole("button", { name: "plan.md", exact: true }).click();
  }
  const editor = panel.getByRole("textbox", { name: "Edit plan.md", exact: true });
  await editor.waitFor();
  assert.equal(
    await editor.inputValue(),
    "# Launch plan\n\n- Keep November 12\n- Developer owns the briefing\n- Review the draft on Monday\n",
    "The native work-session file must contain the completed multi-step draft",
  );
  await panel.getByRole("button", { name: "Preview", exact: true }).click();
  await panel.getByRole("heading", { name: "Launch plan", exact: true }).waitFor();
  const owner = panel.getByText("Developer owns the briefing", { exact: true });
  await owner.waitFor();
  await owner.scrollIntoViewIfNeeded();
}

/** A bounded acceptance set. Every state is captured at each configured width and theme. */
export function sessionScreens(ids: SessionFixtures): SessionScreen[] {
  const session = `/sessions/${ids.work.id}`;
  return [
    {
      name: "new",
      path: "/",
      act: async (page) => {
        await composer(page).waitFor();
      },
    },
    {
      name: "navigation",
      path: "/",
      act: async (page) => {
        const opener = page.getByRole("button", { name: /^Open sidebar/ });
        if (await opener.isVisible()) await opener.click();
        const sections = page.getByRole("navigation", { name: "Sections", exact: true });
        for (const name of [
          "New",
          "Sessions",
          "Alerts",
          "Agents",
          "Connections",
          "Artifacts",
          "Settings",
        ])
          await sections.getByRole("link", { name: new RegExp(`^${name}(?:,|$)`) }).waitFor();
      },
    },
    {
      name: "composer-menu",
      path: "/",
      act: async (page) => {
        await composer(page).fill("Keep this unsent draft while choosing context.");
        await page
          .getByRole("button", { name: "Add files, agents or a project", exact: true })
          .click();
        await page.getByRole("dialog", { name: "Add to this conversation", exact: true }).waitFor();
        assert.equal(
          await composer(page).inputValue(),
          "Keep this unsent draft while choosing context.",
        );
      },
    },
    {
      name: "sessions",
      path: "/sessions",
      act: async (page) => {
        await page.getByRole("heading", { name: "Sessions", exact: true }).waitFor();
        await page.locator(`main a[href="${session}"]`).first().waitFor();
      },
    },
    {
      name: "alerts",
      path: "/alerts",
      act: async (page) => {
        await page.getByRole("heading", { name: "Alerts", exact: true }).waitFor();
        await page.getByText(ALERT_TITLE, { exact: true }).waitFor();
      },
    },
    {
      name: "agents",
      path: "/agents",
      act: async (page) => {
        await page.getByRole("heading", { name: "Agents", exact: true }).waitFor();
        await page.getByRole("button", { name: "New agent", exact: true }).waitFor();
      },
    },
    {
      name: "agent-builder-entry",
      path: "/agents/new",
      act: async (page) => {
        await page.getByRole("heading", { name: "New agent", exact: true }).waitFor();
        await page.getByRole("button", { name: "New agent", exact: true }).waitFor();
      },
    },
    {
      name: "connections",
      path: "/connections",
      act: async (page) => {
        await page.getByRole("heading", { name: "Connections", exact: true }).waitFor();
        await page.getByRole("tablist", { name: "Connection kinds" }).waitFor();
      },
    },
    {
      name: "connections-knowledge",
      path: "/connections",
      act: async (page) => {
        await page.getByRole("tab", { name: "Data sources", exact: true }).click();
        await page.getByRole("heading", { name: "BigQuery", exact: true }).first().waitFor();
      },
    },
    {
      name: "artifacts",
      path: "/artifacts",
      act: async (page) => {
        await page.getByRole("heading", { name: "Deliverables", exact: true }).waitFor();
        await page.getByRole("searchbox", { name: "Search artifacts", exact: true }).waitFor();
        await page
          .getByRole("list", { name: "Artifacts", exact: true })
          .or(page.getByRole("heading", { name: "No artifacts yet", exact: true }))
          .waitFor();
      },
    },
    {
      name: "settings",
      path: "/settings/appearance",
      act: async (page) => {
        await page.getByRole("heading", { name: "Appearance", exact: true }).waitFor();
      },
    },
    { name: "session", path: session, act: readySession },
    {
      name: "session-menu",
      path: session,
      act: async (page) => {
        await readySession(page);
        await page.getByRole("button", { name: "Session actions", exact: true }).click();
        const menu = page.getByRole("menu", { name: "Session actions", exact: true });
        await menu.getByRole("menuitem", { name: "Rename", exact: true }).waitFor();
        await menu.getByRole("menuitem", { name: "Archive", exact: true }).waitFor();
      },
    },
    {
      name: "session-feedback",
      path: session,
      act: async (page) => {
        await readySession(page);
        const answer = page
          .getByRole("article", { name: "screens-writer message", exact: true })
          .last();
        await answer.click({ position: { x: 4, y: 4 } });
        const down = answer.getByRole("button", { name: "Not helpful", exact: true }).last();
        if ((await down.getAttribute("aria-pressed")) === "true") {
          await down.click();
          await page.waitForFunction(
            (el) => el?.getAttribute("aria-pressed") === "false",
            await down.elementHandle(),
          );
        }
        await down.click();
        const dialog = page.getByRole("dialog", { name: "What went wrong?", exact: true });
        await dialog.getByRole("button", { name: "Incomplete", exact: true }).click();
        await dialog.getByRole("textbox").fill("Include the owner and the next review date.");
      },
    },
    ...(["files", "sources", "steps", "sessions"] as const).map((panel) => ({
      name: `session-details-${panel}`,
      path: `${session}?panel=${panel}`,
      act: panel === "files" ? filesReady : detailsReady,
    })),
    {
      name: "session-approval",
      path: `/sessions/${ids.approval.id}`,
      act: async (page) => {
        await readySession(page);
        await page.getByRole("button", { name: "Ask a question", exact: true }).first().waitFor();
        await page
          .getByRole("button", { name: /^Approve(?::|$)/ })
          .first()
          .waitFor();
      },
    },
    {
      name: "session-review",
      path: `/sessions/${ids.review.id}`,
      act: async (page) => {
        await readySession(page);
        await page.getByText(REVIEW_TITLE, { exact: true }).waitFor();
        await page.getByText(FINDING_TITLE, { exact: true }).waitFor();
        assert.equal(await page.getByText('"format"', { exact: true }).count(), 0);
      },
    },
  ];
}
