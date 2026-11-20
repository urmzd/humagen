import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import type { Page } from "playwright-core";
import { defaultCopy } from "../../packages/components/src/copy.ts";
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
  // The Details button says how many calls are running ("Details, 1 working").
  await page.getByRole("button", { name: /^Details(, \d+ working)?$/ }).waitFor();
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
  const picker = browser.getByRole("button", {
    name: defaultCopy.workspaceFiles.files,
    exact: true,
  });
  if (await picker.isVisible()) {
    await picker.click();
    await page
      .getByRole("dialog", { name: defaultCopy.workspaceFiles.files, exact: true })
      .getByRole("button", { name: "plan.md", exact: true })
      .click();
  } else {
    await browser.getByRole("button", { name: "plan.md", exact: true }).click();
  }
  const editor = panel.getByRole("textbox", {
    name: defaultCopy.editor.edit("plan.md"),
    exact: true,
  });
  await editor.waitFor();
  assert.equal(
    await editor.inputValue(),
    "# Launch plan\n\n- Keep November 12\n- Developer owns the briefing\n- Review the draft on Monday\n",
    "The native work-session file must contain the completed multi-step draft",
  );
  await panel
    .getByRole("button", { name: defaultCopy.workspaceFiles.preview, exact: true })
    .click();
  await panel.getByRole("heading", { name: "Launch plan", exact: true }).waitFor();
  const owner = panel.getByText("Developer owns the briefing", { exact: true });
  await owner.waitFor();
  await owner.scrollIntoViewIfNeeded();
}

/** A bounded acceptance set. Every state is captured at each configured width and theme. */
export function sessionScreens(ids: SessionFixtures): SessionScreen[] {
  const session = `/messages/${ids.work.id}`;
  return [
    {
      name: "session-inline-code",
      path: `/messages/${ids.inlineCode.id}`,
      act: async (page) => {
        await readySession(page);
        const answer = page.getByRole("article", { name: "screens-writer message", exact: true });
        await answer.getByText("ok example.com/calc", { exact: true }).waitFor();
        await page.evaluate(() => document.fonts.ready);
        const fragments = await answer.locator(".aui-prose code").evaluateAll((nodes) =>
          nodes.flatMap((node) => {
            const parent = node.closest("p")?.getBoundingClientRect();
            if (!parent) throw new Error("Inline code must belong to prose");
            return Array.from(node.getClientRects(), (rect) => ({
              left: rect.left,
              right: rect.right,
              min: parent.left,
              max: parent.right,
            }));
          }),
        );
        assert.ok(fragments.length >= 3, "The recorded short chips and long path must render");
        for (const fragment of fragments)
          assert.ok(
            fragment.left >= fragment.min - 1 && fragment.right <= fragment.max + 1,
            `Inline code escapes its text column: ${JSON.stringify(fragment)}`,
          );
      },
    },
    {
      name: "session-uploads",
      path: session,
      act: async (page) => {
        await readySession(page);
        // Reloaded from the actual session API, rather than the composer's local draft.
        const message = page
          .getByRole("article", { name: "Message from Developer", exact: true })
          .first();
        await message.scrollIntoViewIfNeeded();
        const uploads = message.getByRole("list", { name: defaultCopy.details.used.uploads });
        await uploads.getByText("capture.png", { exact: true }).waitFor();
        await uploads.getByText("launch-notes.txt", { exact: true }).waitFor();
      },
    },
    {
      name: "composer-attachments",
      path: "/",
      act: async (page) => {
        await composer(page).waitFor();
        await page.locator('input[type="file"]').setInputFiles({
          name: "capture.png",
          mimeType: "image/png",
          buffer: readFileSync(new URL("../../showcase/ui-approval.png", import.meta.url)),
        });
        await page.getByText("capture.png", { exact: true }).waitFor();
        await composer(page).fill("Inspect this screenshot.");
      },
    },
    {
      name: "new",
      path: "/",
      act: async (page) => {
        await composer(page).waitFor();
      },
    },
    {
      // Nothing seeds an artifact (there is no model to publish one), so the
      // strip is drawn from three answered here, as the list route answers.
      name: "home-made",
      path: "/",
      act: async (page) => {
        const at = (days: number) => new Date(Date.now() - days * 86_400_000).toISOString();
        const made = (id: string, title: string, kind: string, days: number) => ({
          id,
          title,
          kind,
          mime: "text/markdown",
          size: 2048,
          sha256: id,
          version: 1,
          run: `run-${id}`,
          thread: ids.work.id,
          agent: "screens-approval",
          created_by: "screens-approval",
          created_at: at(days),
        });
        await page.route(/\/api\/artifacts\?limit=3$/, (route) =>
          route.fulfill({
            json: {
              artifacts: [
                made("a_plan", "Launch plan", "document", 0),
                made("a_budget", "Briefing budget", "spreadsheet", 2),
                made("a_deck", "Owner briefing deck", "presentation", 5),
              ],
              next: null,
            },
          }),
        );
        await page.reload({ waitUntil: "domcontentloaded" });
        await composer(page).waitFor();
        const strip = page.getByRole("region", {
          name: defaultCopy.home.recentlyMade,
          exact: true,
        });
        await strip.getByRole("link", { name: "Launch plan", exact: true }).waitFor();
        // The menu's Artifacts entry opens the full list: the strip does not link to it again.
        assert.equal(await strip.getByRole("link", { name: "All artifacts" }).count(), 0);
      },
    },
    {
      name: "navigation",
      path: "/",
      act: async (page) => {
        const opener = page.getByRole("button", { name: /^Open sidebar/ });
        if (await opener.isVisible()) await opener.click();
        const sections = page.getByRole("navigation", { name: "Sections", exact: true });
        // Settings opens from the account row, not from Sections.
        for (const name of [
          "New",
          "Sessions",
          "Alerts",
          "Projects",
          "Agents",
          "Scheduled",
          "Artifacts",
        ])
          await sections.getByRole("link", { name: new RegExp(`^${name}(?:[ ,]|$)`) }).waitFor();
      },
    },
    {
      name: "composer-menu",
      path: "/",
      act: async (page) => {
        await composer(page).fill("Keep this unsent draft while choosing context.");
        await page.getByRole("button", { name: defaultCopy.chat.plus, exact: true }).click();
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
      // Apps are the workspace's installs, on an administrator's page of their own.
      name: "connections",
      path: "/settings/apps",
      act: async (page) => {
        await page.getByRole("heading", { name: "Apps", exact: true, level: 1 }).waitFor();
        await page.getByRole("list", { name: "Apps", exact: true }).waitFor();
      },
    },
    {
      name: "connections-mcps",
      path: "/settings/plugins",
      act: async (page) => {
        await page.getByRole("heading", { name: "Plugins", exact: true }).waitFor();
        // The catalog is one region of shelves, each its own list.
        await page.getByRole("region", { name: "MCPs", exact: true }).waitFor();
      },
    },
    {
      name: "artifacts",
      path: "/artifacts",
      act: async (page) => {
        await page.getByRole("heading", { name: "Artifacts", exact: true }).waitFor();
        await page.getByRole("searchbox", { name: "Search artifacts", exact: true }).waitFor();
        // The empty state is its title as text, not a heading. Nothing seeds an
        // artifact here: there is no model to seed one with.
        await page
          .getByRole("list", { name: "Artifacts", exact: true })
          .or(page.getByText("No artifacts yet", { exact: true }))
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
    {
      name: "session",
      path: session,
      act: async (page) => {
        await readySession(page);
        const worked = page.getByTestId("worked").first();
        await worked.scrollIntoViewIfNeeded();
        const bounds = await worked.boundingBox();
        if (await page.evaluate(() => matchMedia("(pointer: coarse)").matches))
          assert.ok(bounds && bounds.height >= 44, "Worked must have a 44px touch target");
        await worked.click();
        await detailsReady(page);
        await details(page).getByRole("button", { name: "Close Details", exact: true }).click();
        await details(page).waitFor({ state: "hidden" });
        await worked.scrollIntoViewIfNeeded();
      },
    },
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
      path: `/messages/${ids.approval.id}`,
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
      path: `/messages/${ids.review.id}`,
      act: async (page) => {
        await readySession(page);
        await page.getByText(REVIEW_TITLE, { exact: true }).waitFor();
        await page.getByText(FINDING_TITLE, { exact: true }).waitFor();
        assert.equal(await page.getByText('"format"', { exact: true }).count(), 0);
      },
    },
  ];
}
