import type { Page } from "playwright-core";

/**
 * The sidebar's rows as a live workspace has them (DES-054): sessions that
 * started in a Slack direct message, a Slack thread, on GitHub, on a schedule
 * and in the console, two pinned, one selected, one hovered, under the
 * Personal and workspace groups. The seeded sessions are kept and the rest
 * are added in the browser only, so nothing here is presented as real work.
 */
const SESSIONS = /\/api\/sessions\?/;
const PREFERENCES = /\/api\/preferences$/;
// The reader's marks, read once and sent back by every mark the page makes.
const MARKS = /\/api\/(?:marks|threads\/[^/]+\/marks)$/;

const ago = (minutes: number) => new Date(Date.now() - minutes * 60_000).toISOString();

type Row = {
  id: string;
  title: string;
  origin: string;
  source?: string;
  project: string;
  minutes: number;
  agent?: string;
  active?: number;
  waiting?: number;
  failed?: number;
  unread?: boolean;
  /** What a session listing says of it for its reader (DES-054). */
  extra?: Record<string, unknown>;
};

/** The ids the scene adds, so a test can find each row. */
export const SOURCE_ROWS = {
  slackDM: "s_screens_slack_dm",
  slackThread: "s_screens_slack_thread",
  github: "s_screens_github",
  scheduled: "s_screens_scheduled",
  console: "s_screens_console",
  needs: "s_screens_needs",
  pinnedSlack: "s_screens_pinned_slack",
  pinnedConsole: "s_screens_pinned_console",
  joined: "s_screens_joined",
  question: "s_screens_question",
} as const;

function rows(personal: string): Row[] {
  const r = SOURCE_ROWS;
  return [
    {
      id: r.slackThread,
      title: "#eng-oncall: why did the deploy roll back?",
      origin: "api",
      source: "slack",
      project: "",
      minutes: 3,
      agent: "screens-writer",
      active: 1,
    },
    {
      id: r.github,
      title: "Review PR #482: retry budget for webhooks",
      origin: "github",
      project: "",
      minutes: 35,
      agent: "screens-reviewer",
    },
    {
      id: r.slackDM,
      title: "Please check my repos; what's their status right now?",
      origin: "api",
      source: "slack_dm",
      project: "",
      minutes: 120,
      agent: "screens-reviewer",
      failed: 1,
      unread: true,
      extra: { failure: "github-reviewer could not read the repositories (401)" },
    },
    {
      id: r.joined,
      title: "Find the on-call rota for the payments team",
      origin: "api",
      source: "slack",
      project: "",
      minutes: 50,
      agent: "screens-writer",
      extra: { joined: true },
    },
    {
      id: r.question,
      title: "Triage this week's support backlog",
      origin: "api",
      project: "",
      minutes: 14,
      agent: "screens-writer",
      active: 1,
      waiting: 1,
      extra: {
        waiting_for: "Which of these three should I take on first?",
        waiting_kind: "question",
        waiting_on_you: true,
      },
    },
    {
      id: r.scheduled,
      title: "Weekly dependency digest",
      origin: "cron",
      project: "",
      minutes: 26 * 60,
      agent: "screens-writer",
    },
    {
      id: r.console,
      title: "Summarize yesterday's standup notes",
      origin: "api",
      project: personal,
      minutes: 9,
      agent: "screens-writer",
      unread: true,
    },
    {
      id: r.needs,
      title: "Approve the vendor contract",
      origin: "api",
      project: personal,
      minutes: 22,
      agent: "screens-approval",
      active: 1,
      waiting: 1,
      extra: { waiting_for: "file_write", waiting_kind: "approval", waiting_on_you: true },
    },
    {
      id: r.pinnedSlack,
      title: "Incident review: the November 18 outage",
      origin: "api",
      source: "slack",
      project: "",
      minutes: 3 * 24 * 60,
      agent: "screens-writer",
    },
    {
      id: r.pinnedConsole,
      title: "Hiring loop rubric",
      origin: "api",
      project: personal,
      minutes: 8 * 24 * 60,
      agent: "screens-writer",
    },
  ];
}

/**
 * Dresses the sidebar, reloads, and leaves it ready: on a phone the drawer
 * is open. `open` is the seeded session the page shows, which stays listed.
 */
export async function dressSources(page: Page, open: string): Promise<void> {
  const me = (await page.evaluate(() => fetch("/api/me").then((r) => r.json()))) as {
    personal_project?: string;
  };
  const added = rows(me.personal_project ?? "");
  await page.route(SESSIONS, async (route) => {
    const url = new URL(route.request().url());
    const response = await route.fetch();
    const body = (await response.json()) as { sessions?: Record<string, unknown>[] };
    if (url.searchParams.get("roots") !== "true" || !body.sessions?.length) {
      await route.fulfill({ response, json: body });
      return;
    }
    const kept = body.sessions.filter((s) => s.id === open);
    const template = (kept[0] ?? body.sessions[0]) as Record<string, unknown> & {
      conversation: Record<string, unknown>;
    };
    for (const s of body.sessions) {
      const c = s.conversation as Record<string, unknown> | undefined;
      if (c) Object.assign(c, { last_agent: false, failed: 0, active: 0, waiting: 0 });
    }
    const made = added.map((row) => ({
      ...template,
      id: row.id,
      agent: row.agent ?? template.agent,
      project: row.project,
      title: row.title,
      status: row.waiting ? "waiting" : row.active ? "working" : row.failed ? "failed" : "done",
      updated_at: ago(row.minutes),
      conversation: {
        ...template.conversation,
        id: row.id,
        title: row.title,
        named: true,
        origin: row.origin,
        ...(row.source ? { source: row.source } : {}),
        project: row.project,
        agents: [row.agent ?? "screens-writer"],
        active: row.active ?? 0,
        waiting: row.waiting ?? 0,
        failed: row.failed ?? 0,
        last_agent: row.unread ?? false,
        ...row.extra,
        messages: 1,
        updated_at: ago(row.minutes),
      },
    }));
    await route.fulfill({ response, json: { ...body, sessions: [...made, ...kept] } });
  });
  await page.route(PREFERENCES, async (route) => {
    if (route.request().method() !== "GET") return route.fallback();
    const response = await route.fetch();
    const body = await response.json();
    await route.fulfill({
      response,
      json: { ...body, pinned: [SOURCE_ROWS.pinnedSlack, SOURCE_ROWS.pinnedConsole] },
    });
  });
  // Marks start a month ago, so an agent's newer answer reads as unread. Opening
  // the selected session marks it read, and its answer keeps that start too.
  const since = ago(30 * 24 * 60);
  await page.route(MARKS, async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    await route.fulfill({ response, json: { ...body, archived: [], since } });
  });
  await page.reload();
  await page.locator('form[aria-label="Message"] textarea').waitFor();
  const phone = (page.viewportSize()?.width ?? 0) < 768;
  if (phone) await page.getByRole("button", { name: /^Open sidebar/ }).click();
  const nav = page.getByRole("complementary", { name: "Navigation" });
  await nav.locator(`[data-thread-row="${SOURCE_ROWS.slackDM}"]`).waitFor();
  await nav.locator(`[data-thread-row="${open}"]`).waitFor();
}

/**
 * Puts keyboard focus on the conversation's scrolling log, as a keyboard user
 * reaches it: from the composer, which holds visible focus, so the log's own
 * focus shows as visible too.
 */
export async function focusLog(page: Page): Promise<void> {
  await page.locator('form[aria-label="Message"] textarea').focus();
  await page.keyboard.press("Shift");
  const visible = await page
    .locator("main [role='log']")
    .first()
    .evaluate((el) => {
      (el as HTMLElement).focus({ preventScroll: true });
      return el === document.activeElement && el.matches(":focus-visible");
    });
  if (!visible) throw new Error("The conversation log did not take visible keyboard focus");
}

/** The failure badge and row menu share the row on hover and keyboard focus. */
export async function hoverState(page: Page): Promise<void> {
  if ((page.viewportSize()?.width ?? 0) < 1024) return;
  const row = page.locator(`[data-thread-row="${SOURCE_ROWS.slackDM}"]`);
  const badge = row.locator('[data-state="failed"]');
  const menu = page.getByRole("button", { name: /^Manage Please check my repos/ });

  await badge.hover();
  const hitBadge = await badge.evaluate((el) => {
    const box = el.getBoundingClientRect();
    return el.contains(document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2));
  });
  if (!hitBadge) throw new Error("the failure badge must stay under the pointer when hovered");

  for (const interaction of ["hover", "keyboard focus"]) {
    if (interaction === "keyboard focus") {
      await page.mouse.move(1, 1);
      await row.focus();
      await page.keyboard.press("Shift");
      if (!(await row.evaluate((el) => el.matches(":focus-visible"))))
        throw new Error("the failed row did not take visible keyboard focus");
    }
    const [badgeBox, menuBox, menuOpacity] = await Promise.all([
      badge.boundingBox(),
      menu.boundingBox(),
      menu.evaluate((el) => getComputedStyle(el).opacity),
    ]);
    if (!badgeBox || !menuBox || menuOpacity === "0" || !(await badge.isVisible()))
      throw new Error(`the failure badge and menu must both be visible on ${interaction}`);
    const overlaps =
      badgeBox.x < menuBox.x + menuBox.width &&
      badgeBox.x + badgeBox.width > menuBox.x &&
      badgeBox.y < menuBox.y + menuBox.height &&
      badgeBox.y + badgeBox.height > menuBox.y;
    if (overlaps) throw new Error(`the failure badge and menu overlap on ${interaction}`);
  }
}

/**
 * Drags the sidebar's edge 48px wider on a wide screen and checks the
 * sidebar follows and the width is kept.
 */
export async function resizeSidebar(page: Page): Promise<void> {
  if ((page.viewportSize()?.width ?? 0) < 1024) return;
  const edge = page.getByRole("separator", { name: "Resize sidebar" });
  const box = await edge.boundingBox();
  if (!box) throw new Error("the sidebar's edge is not drawn");
  const x = box.x + box.width / 2;
  const y = box.y + 300;
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x + 24, y);
  await page.mouse.move(x + 48, y);
  await page.mouse.up();
  const nav = page.getByRole("complementary", { name: "Navigation" });
  const width = (await nav.boundingBox())?.width ?? 0;
  // It is drawn no wider than a third of the window.
  const want = Math.min(344, (page.viewportSize()?.width ?? 0) / 3);
  if (Math.abs(width - want) > 2)
    throw new Error(`the dragged sidebar is ${width}px, want ${want}`);
  if ((await page.evaluate(() => localStorage.getItem("humagen.sidebar.width"))) !== "344")
    throw new Error("the dragged width is kept");
}

/** Rests the mouse on a row that is not the open one, which shows its hover state and preview. */
export async function hoverRow(page: Page): Promise<void> {
  if ((page.viewportSize()?.width ?? 0) < 1024) return;
  const row = page.locator(`[data-thread-row="${SOURCE_ROWS.github}"]`);
  await row.hover();
  await page.locator("[data-peek]").waitFor();
}

/** Sources and state badges fit in the same two lines on wide and narrow screens. */
export async function assertSources(page: Page): Promise<void> {
  const nav = page.getByRole("complementary", { name: "Navigation" });
  const row = (id: string) => nav.locator(`[data-thread-row="${id}"]`);
  const expected: [string, string][] = [
    [SOURCE_ROWS.slackDM, "slack_dm"],
    [SOURCE_ROWS.slackThread, "slack"],
    [SOURCE_ROWS.github, "github"],
    [SOURCE_ROWS.scheduled, "scheduled"],
    [SOURCE_ROWS.console, "console"],
  ];
  for (const [id, source] of expected) {
    const mark = row(id).locator("[data-source]");
    if ((await mark.getAttribute("data-source")) !== source)
      throw new Error(`${id} should show its ${source} mark`);
  }
  const consoleMark = row(SOURCE_ROWS.console).locator('[data-source="console"]');
  const consoleLabel = consoleMark.getByText("Web / Console", { exact: true });
  if (!(await consoleLabel.isVisible()) || (await consoleMark.locator("svg").count()) !== 1)
    throw new Error("the console source must show the Web / Console label and its glyph");

  const states = [
    [
      SOURCE_ROWS.slackDM,
      "failed",
      "Failed: github-reviewer could not read the repositories (401)",
      true,
    ],
    [SOURCE_ROWS.needs, "approval", "Waiting for you: approve file_write"],
    [
      SOURCE_ROWS.question,
      "question",
      "Waiting for you: Which of these three should I take on first?",
    ],
    [SOURCE_ROWS.slackThread, "working", "Working"],
  ] as const;
  for (const [id, state, reason, unread] of states) {
    const link = row(id);
    const badge = link.locator(`[data-state="${state}"]`);
    if (
      !(await badge.isVisible()) ||
      (await badge.locator("svg").count()) !== 1 ||
      (await badge.textContent())?.trim() !== "" ||
      (await badge.getAttribute("title")) !== reason
    )
      throw new Error(
        `the ${state} state must show an icon-only badge with the full reason as its tooltip`,
      );
    const described = await link.evaluate(
      (el, text) =>
        el
          .getAttribute("aria-describedby")
          ?.split(/\s+/)
          .some((id) => {
            const detail = document.getElementById(id);
            return detail?.classList.contains("sr-only") && detail.textContent?.trim() === text;
          }),
      unread ? `${reason}, Unread` : reason,
    );
    if (!described)
      throw new Error(`the ${state} row must describe its full reason to screen readers`);
  }

  const normal = row(SOURCE_ROWS.github);
  if ((await normal.locator("[data-state]").count()) > 0)
    throw new Error("an ordinary session must not show a status badge");
  const normalHeight = (await normal.boundingBox())?.height;
  if (!normalHeight) throw new Error("the ordinary session row is not drawn");
  for (const [id, state] of states) {
    const height = (await row(id).boundingBox())?.height;
    if (!height || Math.abs(height - normalHeight) > 0.5)
      throw new Error(`the ${state} row is ${height}px high, expected ${normalHeight}px`);
  }
  await nav
    .getByRole("region", { name: "Participated" })
    .locator(`[data-thread-row="${SOURCE_ROWS.joined}"]`)
    .waitFor();
  if ((await nav.locator(".humagen-row-dot").count()) > 0)
    throw new Error("no row carries a bare dot");
}

/**
 * The focused log draws a thin neutral line along its top edge (DES-080): no
 * accent frame, and no ring round the whole log, which would box in the
 * surface the composer floats over.
 */
export async function assertCalmFocus(page: Page): Promise<void> {
  const shadow = await page.evaluate(() => {
    const log = document.activeElement as HTMLElement | null;
    return log?.getAttribute("role") === "log" ? getComputedStyle(log).boxShadow : "";
  });
  if (!/0px 2px 0px 0px inset/.test(shadow) || /0px 0px 0px [12]px/.test(shadow))
    throw new Error(
      `the focused log should show a line along its top, not: ${shadow || "nothing"}`,
    );
}
