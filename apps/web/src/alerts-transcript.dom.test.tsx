import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Run } from "../../../packages/client/src/generated/types.ts";
import type { Revision, ThreadMessage, ThreadPage } from "./api.ts";
import { stubEventSource, stubFetch } from "./dom.ts";
import { ThreadRoom } from "./Room.tsx";
import type { Alert } from "./sessionCenter.ts";

// A session's alerts sit in its transcript where they happened: right after
// the answer they are about, or at their own time. Each is one line, the
// same alert raised again on one answer is one line with a count, and a
// settled one stays in the history, muted.

let net: ReturnType<typeof stubFetch> | undefined;
let restoreEvents: (() => void) | undefined;
afterEach(() => {
  cleanup();
  restoreEvents?.();
  restoreEvents = undefined;
  net?.restore();
  net = undefined;
});

const SID = "s_alerts";

const reviewer = (name: string): Revision => ({
  rev: 1,
  spec: { name, description: "", provider: "offline", model: "offline", system_prompt: "." },
  created_at: "2026-11-22T09:00:00Z",
  created_by: "ada",
  hash: name,
});

const message = (id: string, text: string, at: string): ThreadMessage =>
  ({
    id,
    thread: SID,
    by: "ada",
    name: "Ada",
    text,
    mentions: [],
    via: "console",
    at,
  }) as ThreadMessage;

const run = (id: string, agent: string, at: string): Run =>
  ({
    id,
    tag_id: id,
    tenant: "default",
    agent,
    rev: 1,
    origin: "api",
    text: "Review it",
    root: id,
    depth: 0,
    thread: SID,
    lease: 0,
    granted: 0,
    spent: 0,
    calls: 0,
    status: "completed",
    answer: "Looks fine.",
    citations: [],
    created_at: at,
    updated_at: at,
  }) as unknown as Run;

const page = {
  thread: {
    id: SID,
    project: "",
    channel: "general",
    visibility: "private",
    participants: [],
    version: 1,
    created_by: "ada",
    created_at: "2026-11-22T09:00:00Z",
    updated_at: "2026-11-22T09:00:00Z",
    session: {},
  },
  turns: [
    { kind: "message", message: message("m-1", "Review pull request 412", "2026-11-22T09:00:00Z") },
    // A server time with an offset and a fraction, as Go writes it.
    { kind: "run", run: run("r-1", "github-reviewer", "2026-11-22T04:00:01.123456-05:00") },
    { kind: "message", message: message("m-2", "Check the format too", "2026-11-22T09:05:00Z") },
    { kind: "run", run: run("r-2", "code-reviewer", "2026-11-22T09:05:01Z") },
  ],
  next: null,
  answers: { agent: "github-reviewer", from: "session" },
} as unknown as ThreadPage;

const base = { session: SID, rule: "", severity: "warning", priority: "normal" } as const;
const eval_ = (id: string, agent: string, on: string, at: string): Alert => ({
  ...base,
  id,
  kind: "eval",
  agent,
  message: on,
  run_id: on,
  action: "review",
  title: `${agent} needs review: format`,
  state: "open",
  at,
});

function room(
  alerts: Alert[],
  routes: Record<string, (body: unknown) => unknown> = {},
  onOpenThread?: (id: string) => void,
) {
  restoreEvents = stubEventSource();
  net = stubFetch({
    [`GET /api/sessions/${SID}`]: () => ({ id: SID, messages: [], children: [], links: [] }),
    [`GET /api/threads/${SID}`]: () => page,
    "GET /api/sessions/options": () => ({ images: [], imageRegistries: [], repos: [] }),
    "GET /api/people": () => ({ people: [], next: null }),
    "GET /api/alerts": () => ({ alerts }),
    ...routes,
  });
  render(
    <ThreadRoom
      id={SID}
      me="ada"
      initial={page}
      tick={0}
      agents={[reviewer("github-reviewer"), reviewer("code-reviewer")]}
      projects={[]}
      root={null}
      workspaceName="Acme"
      canRun
      alertAccess={{ write: true }}
      onOpenThread={onOpenThread}
      onOpenChannel={() => {}}
      onChanged={() => {}}
    />,
  );
}

/** The transcript in reading order: each turn by its id, each alert line by its title. */
const reading = () =>
  [...document.querySelectorAll<HTMLElement>("[data-turn-id], [data-alert-line]")]
    .map((el) =>
      el.hasAttribute("data-alert-line")
        ? `alert:${el.getAttribute("aria-label")?.replace(/^Alert: /, "")}`
        : `turn:${el.getAttribute("data-turn-id")}`,
    )
    // An answer marks its turn on more than one element.
    .filter((entry, i, all) => entry !== all[i - 1]);

test("alerts sit in the transcript where they happened: after their answer, or at their own time", async () => {
  room(
    [
      // Raised in the wrong order on purpose: placement is by time, not by arrival.
      eval_("a-dup-2", "code-reviewer", "r-2", "2026-11-22T09:07:00Z"),
      eval_("a-1", "github-reviewer", "r-1", "2026-11-22T09:01:00Z"),
      eval_("a-dup-1", "code-reviewer", "r-2", "2026-11-22T09:06:00Z"),
      // A specialist's answer that is not a turn here: at its own time.
      {
        ...base,
        id: "a-child",
        kind: "run_failed",
        agent: "security-reviewer",
        run_id: "r-child",
        title: "security-reviewer failed",
        state: "open",
        priority: "urgent",
        at: "2026-11-22T09:06:30Z",
      },
      // No message and no run: at its own time, between the two exchanges.
      {
        ...base,
        id: "a-plain",
        kind: "session",
        title: "Confirm the owner",
        state: "open",
        at: "2026-11-22T09:03:00Z",
      },
      // Older than every turn, and settled: first, muted.
      {
        ...base,
        id: "a-old",
        kind: "session",
        title: "Pick a reviewer",
        state: "resolved",
        at: "2026-11-22T08:00:00Z",
      },
    ],
    {},
    () => {},
  );
  await screen.findByRole("region", { name: "Alert: security-reviewer failed" });
  assert.deepEqual(reading(), [
    "alert:Pick a reviewer",
    "turn:m-1",
    "turn:r-1",
    "alert:github-reviewer needs review: format",
    "alert:Confirm the owner",
    "turn:m-2",
    "turn:r-2",
    "alert:code-reviewer needs review: format",
    "alert:security-reviewer failed",
  ]);

  // One quiet line: a status, the agent by its shown name, the reason, one action.
  const line = screen.getByRole("region", { name: "Alert: github-reviewer needs review: format" });
  assert.ok(within(line).getByRole("img", { name: "Waiting" }));
  assert.ok(within(line).getByText("Github Reviewer"));
  assert.ok(within(line).getByText("Needs review: format"));
  assert.equal(within(line).queryByText(/@github-reviewer/), null);
  // Beside its own answer, a link back to that answer goes nowhere new.
  assert.equal(within(line).queryByRole("link"), null);
  assert.deepEqual(
    within(line)
      .getAllByRole("button")
      .map((b) => b.getAttribute("aria-label") ?? b.textContent),
    ["Review", "Mark Done", "More alert actions"],
  );

  // The same alert raised twice on one answer is one line with a count.
  const repeated = screen.getAllByRole("region", {
    name: "Alert: code-reviewer needs review: format",
  });
  assert.equal(repeated.length, 1);
  assert.ok(within(repeated[0]!).getByText("Raised 2 times"));

  // A specialist's answer that is not on view opens at its own address.
  const child = screen.getByRole("region", { name: "Alert: security-reviewer failed" });
  assert.equal(
    within(child).getByRole("link", { name: "View" }).getAttribute("href"),
    "/messages/r-child",
  );

  // Settled, it stays where it happened, muted, with nothing left to do there.
  const old = screen.getByRole("region", { name: "Alert: Pick a reviewer" });
  assert.equal(old.getAttribute("data-alert-state"), "resolved");
  assert.ok(within(old).getByText("Done"));
  assert.equal(within(old).queryByRole("button", { name: "Mark Done" }), null);

  // The header goes to the first alert still waiting, counting a repeated one once.
  const jump = screen.getByRole("button", { name: "Go to the first of 4 open alerts" });
  fireEvent.click(jump);
  assert.equal(document.activeElement, line);
});

test("marking a repeated alert done settles every copy, and Undo opens them again", async () => {
  const states: Record<string, string> = { "a-dup-1": "open", "a-dup-2": "open" };
  const writes: { id: string; state: string }[] = [];
  const alerts = () => [
    {
      ...eval_("a-dup-1", "code-reviewer", "r-2", "2026-11-22T09:06:00Z"),
      state: states["a-dup-1"],
    },
    {
      ...eval_("a-dup-2", "code-reviewer", "r-2", "2026-11-22T09:07:00Z"),
      state: states["a-dup-2"],
    },
  ];
  const patch = (id: string) => (body: unknown) => {
    const state = (body as { state: string }).state;
    states[id] = state;
    writes.push({ id, state });
    return { ...eval_(id, "code-reviewer", "r-2", "2026-11-22T09:06:00Z"), state };
  };
  room([], {
    "GET /api/alerts": () => ({ alerts: alerts() }),
    "PATCH /api/alerts/a-dup-1": patch("a-dup-1"),
    "PATCH /api/alerts/a-dup-2": patch("a-dup-2"),
  });
  const name = { name: "Alert: code-reviewer needs review: format" };
  const line = await screen.findByRole("region", name);
  fireEvent.click(within(line).getByRole("button", { name: "Mark Done" }));
  await waitFor(() =>
    assert.equal(screen.getByRole("region", name).getAttribute("data-alert-state"), "resolved"),
  );
  assert.deepEqual(
    writes.map((w) => w.state),
    ["resolved", "resolved"],
  );
  assert.deepEqual(writes.map((w) => w.id).sort(), ["a-dup-1", "a-dup-2"]);
  // Nothing waits now: the header has no way to an open alert.
  assert.equal(screen.queryByRole("button", { name: /open alert/ }), null);
  fireEvent.click(screen.getByRole("button", { name: "Undo" }));
  await waitFor(() =>
    assert.equal(screen.getByRole("region", name).getAttribute("data-alert-state"), "open"),
  );
  assert.deepEqual(
    writes.slice(2).map((w) => w.state),
    ["open", "open"],
  );
  assert.ok(screen.getByRole("button", { name: "Go to the open alert" }));
});

test("Review asks the agent about the newest copy, and Snooze waits in the menu", async () => {
  const reviewed: string[] = [];
  const snoozed: unknown[] = [];
  let opened = "";
  room(
    [eval_("a-1", "github-reviewer", "r-1", "2026-11-22T09:01:00Z")],
    {
      "POST /api/alerts/a-1/review": () => {
        reviewed.push("a-1");
        return { id: "s_review" };
      },
      "PATCH /api/alerts/a-1": (body) => {
        snoozed.push(body);
        return {
          ...eval_("a-1", "github-reviewer", "r-1", "2026-11-22T09:01:00Z"),
          state: "snoozed",
        };
      },
    },
    (id) => {
      opened = id;
    },
  );
  const line = await screen.findByRole("region", {
    name: "Alert: github-reviewer needs review: format",
  });
  fireEvent.click(within(line).getByRole("button", { name: "Review" }));
  await waitFor(() => assert.equal(opened, "s_review"));
  assert.deepEqual(reviewed, ["a-1"]);
  assert.equal(within(line).queryByRole("button", { name: "Snooze 1h" }), null);
  fireEvent.click(within(line).getByRole("button", { name: "More alert actions" }));
  fireEvent.click(within(line).getByRole("menuitem", { name: "Snooze 1h" }));
  await waitFor(() => assert.equal(snoozed.length, 1));
  assert.equal((snoozed[0] as { state: string }).state, "snoozed");
});
