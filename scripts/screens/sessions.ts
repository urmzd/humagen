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
import {
  assertCalmFocus,
  assertSources,
  dressSources,
  focusLog,
  hoverRow,
  hoverState,
  previewReason,
  resizeSidebar,
} from "./sidebar-sources.ts";

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
      name: "home-projects",
      path: "/",
      act: async (page) => {
        const load = Promise.withResolvers<void>();
        await page.route(
          (url) => url.pathname === "/api/projects" && url.searchParams.has("limit"),
          async (route) => {
            await load.promise;
            await route.continue();
          },
        );
        const projects = page.getByRole("region", {
          name: defaultCopy.projects.title,
          exact: true,
        });
        let before: { y: number } | null;
        try {
          await page.reload({ waitUntil: "domcontentloaded" });
          await composer(page).waitFor();
          await page.evaluate(() => document.fonts.ready);
          assert.equal(await projects.count(), 0, "No section is drawn without project data");
          before = await composer(page).boundingBox();
        } finally {
          load.resolve();
        }
        const project = projects.getByRole("link", { name: "Launch", exact: true });
        await project.waitFor();
        assert.equal(await project.getAttribute("href"), "/projects/Launch");
        const after = await composer(page).boundingBox();
        assert.ok(before && after && Math.abs(before.y - after.y) <= 1, "The composer stays put");
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
        const alerts = page.getByRole("list", { name: "Alerts", exact: true });
        await alerts.getByRole("button", { name: "Review Approval", exact: true }).waitFor();
        const rows = alerts.getByRole("listitem");
        assert.equal(await rows.count(), 2);
        const layout = () =>
          rows.evaluateAll((items) =>
            items.map((item) => {
              const { top, height: rowHeight, right } = item.getBoundingClientRect();
              const boxes = [...item.querySelectorAll("button")].map((button) =>
                button.getBoundingClientRect(),
              );
              const lead = boxes[0]?.y ?? top;
              const title = item.querySelector("a > span");
              return {
                // The table has a track for each column; a stacked row has the box and the alert.
                table: getComputedStyle(item).gridTemplateColumns.split(" ").length > 2,
                rowHeight,
                right,
                titleLines: title
                  ? Math.round(
                      title.getBoundingClientRect().height /
                        Number.parseFloat(getComputedStyle(title).lineHeight),
                    )
                  : 0,
                buttons: boxes.map((box) => ({
                  x: box.x,
                  y: box.y - top,
                  fromLead: box.y - lead,
                  width: box.width,
                  right: box.right,
                })),
              };
            }),
          );
        type Row = Awaited<ReturnType<typeof layout>>[number];
        // A stacked title may take a second line, which moves the actions down together.
        const slots = (row: Row) =>
          row.buttons.map(({ x, fromLead, width, right }) => ({ x, fromLead, width, right }));

        const initial = await layout();
        const [first, second] = initial;
        assert.ok(first && second, "the fixture has two alerts");
        const table = first.table;
        assert.equal(second.table, table, "both rows use one layout");
        assert.equal(first.buttons.length, 3, "the fixture can manage alerts");
        if (table) {
          assert.ok(Math.abs(first.rowHeight - second.rowHeight) <= 1, "equal row heights");
          assert.deepEqual(first.buttons, second.buttons, "the same three action slots");
        } else {
          assert.deepEqual(slots(first), slots(second), "the same three action slots");
        }
        assert.ok(
          initial.every(({ buttons, right }) => buttons.every((button) => button.right < right)),
          "every action stays inside the row",
        );

        // Dynamic content can be longer than either fixture, without shifting a control.
        const secondRow = rows.nth(1);
        await secondRow.evaluate((row) => {
          const title = row.querySelector("a > span");
          const action = row.querySelector("button > span");
          if (title)
            title.textContent = "A title for an alert with unusually long details ".repeat(5);
          if (action)
            action.textContent = "Review the very long approval details before continuing";
        });
        const long = await layout();
        const lengthened = long[1];
        assert.ok(lengthened, "the lengthened alert is still listed");
        if (table) {
          assert.deepEqual(long, initial, "long labels do not resize rows or action slots");
        } else {
          // Touch has no tooltip, so a stacked row shows the second line of a long title.
          assert.equal(lengthened.titleLines, 2, "a stacked title takes two lines");
          assert.deepEqual(
            slots(lengthened),
            slots(second),
            "long labels do not move action slots",
          );
        }
        await page.reload();
        await alerts.getByRole("button", { name: "Review Approval", exact: true }).waitFor();
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
        const apps = page.getByRole("list", { name: "Apps", exact: true });
        const slack = apps.getByRole("listitem", { name: "Slack", exact: true });
        const github = apps.getByRole("listitem", { name: "GitHub", exact: true });
        const enable = slack.getByRole("button", { name: "Enable", exact: true });
        const manage = github.getByRole("button", { name: "Manage", exact: true });
        await enable.waitFor();
        await manage.waitFor();
        const first = await slack.boundingBox();
        const second = await github.boundingBox();
        assert.ok(first && second);
        assert.equal(first.x, second.x, "Apps share a column at every width");
        assert.ok(first.y + first.height <= second.y, "Apps are stacked");
        await manage.click();
        const detail = page.getByRole("dialog", { name: "GitHub", exact: true });
        await detail.waitFor();
        await page.keyboard.press("Escape");
        await detail.waitFor({ state: "hidden" });
        await manage.and(page.locator("button:focus")).waitFor();
        await manage.blur();
        await page.mouse.move(0, 0);
        // The fixture's GitHub is installed through a client, a row with no menu. Shown, in
        // the browser only, as installed from this page, it gains the menu an administrator sees.
        await page.route(/\/api\/connections\/presets(?:\?|$)/, async (route) => {
          const response = await route.fetch();
          const presets = await response.json();
          for (const preset of presets) if (preset.id === "github") preset.connected = true;
          await route.fulfill({ response, json: presets });
        });
        await page.reload();
        const more = github.getByRole("button", { name: "More for GitHub", exact: true });
        await more.waitFor();
        await page.evaluate(() => document.fonts.ready);
        const action = await manage.boundingBox();
        const menu = await more.boundingBox();
        const row = await github.boundingBox();
        assert.ok(action && menu && row);
        assert.ok(
          Math.abs(menu.y + menu.height / 2 - (action.y + action.height / 2)) <= 1,
          "the menu shares Manage's line at every width",
        );
        assert.ok(menu.x >= action.x + action.width, "the menu follows Manage");
        assert.ok(
          Math.abs(menu.x + menu.width - (row.x + row.width)) <= 1,
          "the menu ends the row",
        );
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
        const controls = page
          .locator(".humagen-composer-row > button")
          .or(page.getByRole("button", { name: /^Approval mode: / }))
          .or(page.locator('[data-layer="usage-ring"]'));
        await page.locator('[data-layer="usage-ring"]').waitFor();
        const geometry = await controls.evaluateAll((buttons) => ({
          target: matchMedia("(pointer: coarse)").matches ? 44 : 32,
          buttons: buttons.map((button) => {
            const { width, height } = button.getBoundingClientRect();
            const icon = button.querySelector("svg")?.getBoundingClientRect();
            return { width, height, icon: icon && { width: icon.width, height: icon.height } };
          }),
        }));
        assert.ok(geometry.buttons.length >= 4, "Compose, approval and usage controls are visible");
        for (const { width, height, icon } of geometry.buttons) {
          assert.equal(width, geometry.target, "Composer controls share a target width");
          assert.equal(height, geometry.target, "Composer controls share a target height");
          if (icon)
            assert.deepEqual(icon, { width: 18, height: 18 }, "Composer icons share a size");
        }
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
      name: "sidebar-sources",
      path: session,
      // DES-054: where each session came from, pinned, selected and hovered rows,
      // with keyboard focus on the conversation beside them.
      act: async (page) => {
        await readySession(page);
        await dressSources(page, ids.work.id);
        await assertSources(page);
        await hoverState(page);
        await previewReason(page);
        if ((page.viewportSize()?.width ?? 0) >= 768) {
          await focusLog(page);
          await assertCalmFocus(page);
        }
        await hoverRow(page);
      },
    },
    {
      name: "sidebar-resized",
      path: session,
      // The sidebar's edge dragged 48px wider; on a phone it stays a drawer.
      act: async (page) => {
        await readySession(page);
        await dressSources(page, ids.work.id);
        await resizeSidebar(page);
        await page.evaluate(() => localStorage.removeItem("humagen.sidebar.width"));
      },
    },
    {
      name: "session-log-focus",
      path: session,
      act: async (page) => {
        await readySession(page);
        await focusLog(page);
        await assertCalmFocus(page);
      },
    },
    {
      name: "session-menu",
      path: session,
      act: async (page) => {
        await readySession(page);
        await page.locator('h1 button[aria-haspopup="menu"]').click();
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
        await page.getByRole("button", { name: "Ask a Question", exact: true }).first().waitFor();
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
