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

/**
 * Rests the mouse on a failed row's state line: its words stay in view under
 * the pointer while the row's ⋮ button takes the dot's place.
 */
export async function hoverState(page: Page): Promise<void> {
  if ((page.viewportSize()?.width ?? 0) < 1024) return;
  const line = page.locator(`[data-thread-row="${SOURCE_ROWS.slackDM}"] [data-state="bad"]`);
  await line.hover();
  await page.getByRole("button", { name: /^Manage Please check my repos/ }).waitFor();
  const seen = await line.evaluate((el) => {
    const box = el.getBoundingClientRect();
    const hit = document.elementFromPoint(box.x + 8, box.y + box.height / 2);
    return {
      visible: getComputedStyle(el).visibility === "visible",
      under: !!hit && el.contains(hit),
      text: el.textContent,
    };
  });
  if (!seen.visible || !seen.under || !seen.text?.startsWith("Failed: "))
    throw new Error(`a hovered row's state must stay in view: ${JSON.stringify(seen)}`);
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
  const want = Math.min(344, Math.floor((page.viewportSize()?.width ?? 0) / 3));
  if (Math.abs(width - want) > 2)
    throw new Error(`the dragged sidebar is ${width}px, want ${want}`);
  // What is kept is what is drawn.
  const kept = await page.evaluate(() => localStorage.getItem("humagen.sidebar.width"));
  if (kept !== String(want)) throw new Error(`the dragged width is kept as ${kept}, want ${want}`);
}

/** Rests the mouse on a row that is not the open one, which shows its hover state and preview. */
export async function hoverRow(page: Page): Promise<void> {
  if ((page.viewportSize()?.width ?? 0) < 1024) return;
  const row = page.locator(`[data-thread-row="${SOURCE_ROWS.github}"]`);
  await row.hover();
  await page.locator("[data-peek]").waitFor();
}

/**
 * Each row leads with its source, said in words, and no row or the focused
 * log is drawn in the accent: red is kept for a failed reply.
 */
export async function assertSources(page: Page): Promise<void> {
  const nav = page.getByRole("complementary", { name: "Navigation" });
  const expected: [string, string][] = [
    [SOURCE_ROWS.slackDM, "slack_dm"],
    [SOURCE_ROWS.slackThread, "slack"],
    [SOURCE_ROWS.github, "github"],
    [SOURCE_ROWS.scheduled, "scheduled"],
    [SOURCE_ROWS.console, "console"],
  ];
  for (const [id, source] of expected) {
    const mark = nav.locator(`[data-thread-row="${id}"] [data-source]`);
    if ((await mark.getAttribute("data-source")) !== source)
      throw new Error(`${id} should lead with its ${source} mark`);
  }
  const failed = nav.locator(`[data-thread-row="${SOURCE_ROWS.slackDM}"] [data-state="bad"]`);
  if (
    (await failed.textContent()) !== "Failed: github-reviewer could not read the repositories (401)"
  )
    throw new Error("a failed reply is said in words, with why");
  const waiting = nav.locator(`[data-thread-row="${SOURCE_ROWS.needs}"] [data-state="warn"]`);
  if ((await waiting.textContent()) !== "Waiting for you: approve file_write")
    throw new Error("a session waiting on the reader says what for");
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
