import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import type { Turn } from "@humagen/core";
import { WebSurface } from "@humagen/render-web";
import { AgentMessage, Thread, useQueue } from "@humagen/web";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Run } from "./adapter.ts";
import type { BackgroundTask } from "./api.ts";
import { Composer } from "./Composer.tsx";
import { stubFetch } from "./dom.ts";
import { limitsFor, ringOf, UsageRing } from "./SessionUsage.tsx";
import { useItemStops, useSessionStop } from "./stops.tsx";

afterEach(cleanup);

const dock = defaultCopy.dock;
const at = "2026-11-20T09:00:00Z";
const run = (over: Partial<Run> = {}): Run =>
  ({
    id: "r1",
    agent: "lead",
    status: "running",
    text: "check my repos",
    thread: "s1",
    calls: 0,
    created_at: at,
    updated_at: at,
    ...over,
  }) as Run;
const work = (over: Partial<BackgroundTask> = {}): BackgroundTask =>
  ({
    id: "bg_1",
    kind: "task",
    text: "run the migration dry run",
    state: "active",
    agent: "lead",
    until: at,
    working: true,
    runs: 1,
    limit: 1,
    session: "s1",
    created_at: at,
    ...over,
  }) as BackgroundTask;

// ---------------------------------------------------------------------------
// Layers

/** The z-index scale in the shared theme, by name. */
function scale(): Record<string, number> {
  const css = readFileSync(
    new URL("../../../packages/render-web/src/theme.css", import.meta.url),
    "utf8",
  );
  const out: Record<string, number> = {};
  for (const m of css.matchAll(/--aui-z-([a-z]+):\s*(\d+);/g)) out[m[1] ?? ""] = Number(m[2]);
  return out;
}

test("the layers stack content, sticky chrome, the composer, popovers, sheets and toasts in that order", () => {
  const z = scale();
  const order = ["content", "sticky", "composer", "popover", "sheet", "toast"];
  for (const name of order) assert.ok(name in z, `--aui-z-${name} is defined`);
  for (let i = 1; i < order.length; i++)
    assert.ok(
      (z[order[i] ?? ""] ?? 0) > (z[order[i - 1] ?? ""] ?? 0),
      `${order[i]} sits above ${order[i - 1]}`,
    );
});

// ---------------------------------------------------------------------------
// One surface, a floating composer

test("the composer floats over the conversation, and the list keeps room for it as it grows", async () => {
  const proto = HTMLElement.prototype;
  const height = Object.getOwnPropertyDescriptor(proto, "offsetHeight");
  const RealObserver = globalThis.ResizeObserver;
  let tall = 120;
  const watchers: (() => void)[] = [];
  Object.defineProperty(proto, "offsetHeight", {
    configurable: true,
    get(this: HTMLElement) {
      return this.dataset.layer === "composer-dock" ? tall : 0;
    },
  });
  globalThis.ResizeObserver = class {
    cb: () => void;
    constructor(cb: () => void) {
      this.cb = cb;
      watchers.push(() => cb());
    }
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
  try {
    const { container } = render(
      <Thread label="Thread" footer={<form aria-label="Message" />}>
        <p>An answer</p>
      </Thread>,
    );
    const root = container.firstElementChild as HTMLElement;
    const layer = container.querySelector<HTMLElement>('[data-layer="composer-dock"]');
    assert.ok(layer, "the composer is in a dock over the list");
    assert.match(layer.className, /z-\(--aui-z-composer\)/);
    assert.match(layer.className, /absolute/);
    assert.ok(container.querySelector('[data-layer="composer-fade"]'), "a fade above its edge");
    // The message list is the one scrolling surface; the composer is not inside it.
    const log = screen.getByRole("log", { name: "Thread" });
    assert.ok(!log.contains(layer));
    assert.match((log.firstElementChild as HTMLElement).className, /--aui-thread-dock-h/);
    assert.equal(root.style.getPropertyValue("--aui-thread-dock-h"), "120px");
    // The composer grows (a long draft): the room at the list's end grows with it.
    tall = 260;
    act(() => {
      for (const w of watchers) w();
    });
    assert.equal(root.style.getPropertyValue("--aui-thread-dock-h"), "260px");
    // No ring boxes the log any more: its keyboard focus is a line along its top.
    assert.doesNotMatch(log.className, /focus-visible:ring/);
  } finally {
    if (height) Object.defineProperty(proto, "offsetHeight", height);
    globalThis.ResizeObserver = RealObserver;
  }
});

test("a thread with no composer keeps its own end padding and the jump control's fade", () => {
  const { container } = render(
    <Thread label="Thread">
      <p>An answer</p>
    </Thread>,
  );
  assert.equal(container.querySelector('[data-layer="composer-dock"]'), null);
  const log = screen.getByRole("log", { name: "Thread" });
  assert.match((log.firstElementChild as HTMLElement).className, /pb-12/);
});

// ---------------------------------------------------------------------------
// The composer: keys, the hint and the session's Stop

type Seen = { sent: string[]; now: string[]; stops: number };

function Bar({
  busy,
  seen,
  tags,
}: {
  busy: boolean;
  seen: Seen;
  tags?: Parameters<typeof Composer>[0]["tags"];
}) {
  const queue = useQueue({ onStart: () => {}, onInterrupt: () => {} });
  const spy = {
    ...queue,
    send: (text: string) => seen.sent.push(text),
    sendNow: (text: string) => seen.now.push(text),
  };
  return (
    <Composer
      placeholder="Reply"
      queue={spy}
      tags={tags}
      stopAll={{ busy, pending: false, onStop: () => seen.stops++ }}
      footer={<div data-testid="footer">footer</div>}
    />
  );
}

test("while anything runs the box says how to send and queue, Enter sends now and Cmd+Enter queues", () => {
  const seen: Seen = { sent: [], now: [], stops: 0 };
  render(<Bar busy seen={seen} />);
  const box = screen.getByRole("textbox") as HTMLTextAreaElement;
  assert.match(box.placeholder, /to send, .+ to queue for later/);
  fireEvent.change(box, { target: { value: "use the staging branch" } });
  fireEvent.keyDown(box, { key: "Enter" });
  assert.deepEqual(seen.now, ["use the staging branch"]);
  fireEvent.change(box, { target: { value: "then rebase" } });
  fireEvent.keyDown(box, { key: "Enter", metaKey: true });
  assert.deepEqual(seen.sent, ["then rebase"]);
  fireEvent.change(box, { target: { value: "and on Windows" } });
  fireEvent.keyDown(box, { key: "Enter", ctrlKey: true });
  assert.deepEqual(seen.sent, ["then rebase", "and on Windows"]);
  assert.ok(screen.getByTestId("footer"), "the footer sits under the box");
});

test("the session's Stop shows while anything runs and stops it all; Esc in an empty box does too", () => {
  const seen: Seen = { sent: [], now: [], stops: 0 };
  const { rerender } = render(<Bar busy={false} seen={seen} />);
  assert.equal(screen.queryByRole("button", { name: dock.stopAllLabel }), null);
  assert.equal((screen.getByRole("textbox") as HTMLTextAreaElement).placeholder, "Reply");
  rerender(<Bar busy seen={seen} />);
  fireEvent.click(screen.getByRole("button", { name: dock.stopAllLabel }));
  assert.equal(seen.stops, 1);
  fireEvent.keyDown(screen.getByRole("textbox"), { key: "Escape" });
  assert.equal(seen.stops, 2);
});

test("the @ list opens on the popover layer, above the floating composer", async () => {
  const seen: Seen = { sent: [], now: [], stops: 0 };
  render(
    <Bar
      busy={false}
      seen={seen}
      tags={{
        label: "Agents",
        options: [
          { token: "lead", label: "lead", kind: "agent", hint: "Coordinates" },
          { token: "helper", label: "helper", kind: "agent", hint: "Helps" },
        ] as never,
      }}
    />,
  );
  const box = screen.getByRole("textbox") as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: "@", selectionStart: 1, selectionEnd: 1 } });
  const list = await screen.findByRole("listbox");
  assert.match(list.className, /z-\(--aui-z-popover\)/);
  const container = box.closest(".humagen-composer-container") as HTMLElement;
  assert.match(container.className, /z-\(--aui-z-composer\)/);
});

function SessionStopFixture({ runs, background }: { runs: Run[]; background: BackgroundTask[] }) {
  const s = useSessionStop({ session: "s1", runs, background });
  return (
    <>
      <output aria-label="busy">{String(s.busy)}</output>
      <output aria-label="problem">{s.problem ?? ""}</output>
      <button type="button" onClick={() => void s.stopAll()}>
        Stop all
      </button>
    </>
  );
}

test("the session's Stop stops every run in it and its child sessions, and the background work it started", async () => {
  const { seen, restore } = stubFetch({
    "POST /api/sessions/s1/stop": () => undefined,
    "POST /api/me/background/bg_1/stop": () => work({ state: "stopped", working: false }),
  });
  try {
    render(
      <SessionStopFixture
        runs={[run(), run({ id: "r2", agent: "helper", status: "completed" })]}
        background={[work(), work({ id: "bg_2", state: "done", working: false })]}
      />,
    );
    assert.equal(screen.getByLabelText("busy").textContent, "true");
    fireEvent.click(screen.getByRole("button", { name: "Stop all" }));
    await waitFor(() => assert.ok(seen.some((s) => s.path === "/api/sessions/s1/stop")));
    const session = seen.find((s) => s.path === "/api/sessions/s1/stop");
    assert.deepEqual(session?.body, { children: true });
    assert.ok(seen.some((s) => s.path === "/api/me/background/bg_1/stop"));
    assert.ok(
      !seen.some((s) => s.path === "/api/me/background/bg_2/stop"),
      "only work still going",
    );
  } finally {
    restore();
  }
});

test("a session whose runs all ended and whose work is done shows no Stop", () => {
  render(
    <SessionStopFixture
      runs={[run({ status: "completed" }), run({ id: "r2", status: "parked" })]}
      background={[work({ state: "done", working: false })]}
    />,
  );
  assert.equal(screen.getByLabelText("busy").textContent, "false");
});

// ---------------------------------------------------------------------------
// Each item's own Stop

const callTurn = (childStatus: "in_progress" | "complete" = "in_progress"): Turn =>
  ({
    id: "r1",
    agent: { name: "lead" },
    status: "working",
    text: "Bringing in the helper.",
    tasks: [
      {
        id: "call-1",
        tool: "agent",
        title: "Check repository status",
        status: childStatus,
        input: JSON.stringify({ agent: "helper", title: "Check repository status", brief: "go" }),
        subagent: { name: "helper", runId: "child-1" },
        at: 23,
      },
      {
        id: "bg-start",
        tool: "task_start",
        title: "Start a task",
        status: "complete",
        output: JSON.stringify({ id: "bg_1", kind: "task", state: "active" }),
        at: 23,
      },
    ],
    citations: [],
    interrupts: [],
    notices: [],
  }) as unknown as Turn;

function ItemsFixture({ onAgentStop }: { onAgentStop: () => void }) {
  const stops = useItemStops({
    run: run(),
    allowed: true,
    background: [work()],
    agentStop: { onStop: onAgentStop, pending: false },
  });
  return (
    <WebSurface>
      <AgentMessage
        turn={callTurn()}
        stop={{ onStop: onAgentStop, pending: false }}
        itemStops={stops}
        disclaimer={false}
      />
    </WebSurface>
  );
}

test("a call to another agent has its own Stop, which stops that agent's run and then reads Stopped, not Failed", async () => {
  let ended = false;
  const { seen, restore } = stubFetch({
    "POST /api/runs/child-1/stop": () => {
      ended = true;
      return run({ id: "child-1", agent: "helper", status: "stopped" });
    },
    "GET /api/runs/child-1": () =>
      run({ id: "child-1", agent: "helper", status: ended ? "stopped" : "running" }),
  });
  try {
    render(<ItemsFixture onAgentStop={() => {}} />);
    const call = document.querySelector<HTMLElement>('[data-aui="agent-call"]');
    assert.ok(call);
    const stop = within(call).getByRole("button", { name: dock.stopLabel("helper") });
    assert.equal(stop.textContent?.trim(), dock.stop, "a visible word, not a hover icon");
    fireEvent.click(stop);
    await waitFor(() =>
      assert.ok(seen.some((s) => s.method === "POST" && s.path === "/api/runs/child-1/stop")),
    );
    await waitFor(() => assert.equal(call.dataset.status, "stopped"));
    assert.ok(within(call).getByLabelText(dock.stoppedLabel("helper")));
    assert.equal(within(call).queryByText("Failed"), null);
    // Stopping the helper opened nothing: Stop is its own action, not the row's.
    assert.ok(!seen.some((s) => s.path.startsWith("/api/runs/child-1/events")));
  } finally {
    restore();
  }
});

test("background work a step started is stopped from that step's line", async () => {
  const { seen, restore } = stubFetch({
    "POST /api/me/background/bg_1/stop": () => work({ state: "stopped", working: false }),
  });
  try {
    render(<ItemsFixture onAgentStop={() => {}} />);
    const what = dock.backgroundWork("task", "run the migration dry run");
    fireEvent.click(await screen.findByRole("button", { name: dock.stopLabel(what) }));
    await waitFor(() =>
      assert.ok(seen.some((s) => s.method === "POST" && s.path === "/api/me/background/bg_1/stop")),
    );
    await waitFor(() => assert.ok(screen.getByLabelText(dock.stoppedLabel(what))));
  } finally {
    restore();
  }
});

test("the working agent's own Stop is a word at the top of its answer", () => {
  let stopped = 0;
  render(<ItemsFixture onAgentStop={() => stopped++} />);
  const header = screen.getByRole("article", { name: "lead message" }).querySelector("header");
  assert.ok(header);
  // The mark stops it too (DES-025); the word is the one in plain sight.
  const word = within(header)
    .getAllByRole("button", { name: dock.stopLabel("lead") })
    .find((b) => b.textContent?.trim() === dock.stop);
  assert.ok(word);
  fireEvent.click(word);
  assert.equal(stopped, 1);
});

// ---------------------------------------------------------------------------
// Usage

const usage = (over: Record<string, unknown> = {}) => ({
  runs: 3,
  usage: { prompt_tokens: 48_000, completion_tokens: 2_000 },
  agents: [
    { agent: "lead", runs: 2, usage: { prompt_tokens: 40_000, completion_tokens: 1_500 } },
    { agent: "helper", runs: 1, usage: { prompt_tokens: 8_000, completion_tokens: 500 } },
  ],
  context: { run: "r1", agent: "lead", model: "opus", window: 200_000, tokens: 60_000 },
  working: 1,
  truncated: false,
  ...over,
});
const scope = (over: Record<string, unknown> = {}) => ({
  scope: { kind: "tenant" },
  period: "month",
  period_start: "2026-11-01T00:00:00Z",
  period_end: "2026-12-01T13:00:00Z",
  version: 1,
  warn_at: 80,
  action: "block",
  runs: { used: 10, limit: 500 },
  concurrent: { used: 1, limit: 0 },
  spend: { used: 0, limit: 0, metered: false, unpriced_calls: 0 },
  tokens: { used: 100_000, limit: 1_000_000 },
  max_lease: 0,
  state: "ok",
  ...over,
});

function open() {
  fireEvent.click(screen.getByRole("button", { name: /Open usage$/ }));
  return screen.getByRole("dialog", { name: dock.usage.title });
}

test("the usage popover shows the session first, then each limit with when it resets, and See all", async () => {
  const { restore } = stubFetch({
    "GET /api/threads/s1/usage": () => usage(),
    "GET /api/usage": () => ({ scopes: [scope()] }),
  });
  try {
    render(<UsageRing thread="s1" project="Launch" agents={["lead"]} version={1} />);
    const ring = await screen.findByRole("button", { name: /Context: 30% used/ });
    assert.equal(ring.dataset.tone, "plain", "colorless while nothing is near");
    const pop = open();
    assert.match(pop.className, /z-\(--aui-z-popover\)/);
    assert.match(pop.className, /max-sm:z-\(--aui-z-sheet\)/, "a sheet on a phone");
    assert.ok(within(pop).getByText(dock.usage.session));
    assert.ok(within(pop).getByText(dock.usage.contextUsed("60K", "200K")));
    assert.ok(within(pop).getByText(dock.usage.topAgents));
    assert.ok(within(pop).getByText("lead"));
    assert.ok(within(pop).getByText("helper"));
    assert.ok(within(pop).getByText("Monthly tokens"));
    assert.ok(within(pop).getAllByText(/^Resets /).length >= 2);
    assert.equal(
      within(pop)
        .getByRole("link", { name: /See all/ })
        .getAttribute("href"),
      "/settings/usage",
    );
  } finally {
    restore();
  }
});

test("usage loads as skeleton bars, and with no limits says so", async () => {
  let answer: (v: unknown) => void = () => {};
  const { restore } = stubFetch({
    "GET /api/threads/s1/usage": () => new Promise((r) => (answer = r)),
    "GET /api/usage": () => ({ scopes: [] }),
  });
  try {
    render(<UsageRing thread="s1" agents={[]} version={1} />);
    const pop = open();
    assert.ok(within(pop).getAllByRole("status", { name: dock.usage.loading }).length > 0);
    answer(usage());
    await waitFor(() => assert.ok(within(pop).getByText(dock.usage.noLimits)));
    assert.ok(within(pop).getByText(dock.usage.session));
  } finally {
    restore();
  }
});

test("near a limit the ring takes a color and says which; over it the popover says what is blocked and when it resets", async () => {
  const { restore } = stubFetch({
    "GET /api/threads/s1/usage": () => usage(),
    "GET /api/usage": () => ({
      scopes: [
        scope({ tokens: { used: 900_000, limit: 1_000_000 }, state: "warn" }),
        scope({
          scope: { kind: "agent", name: "lead" },
          period: "day",
          runs: { used: 8, limit: 8 },
          tokens: { used: 0, limit: 0 },
          state: "over",
        }),
      ],
    }),
  });
  try {
    render(<UsageRing thread="s1" agents={["lead"]} version={1} />);
    const ring = await screen.findByRole("button", { name: /Agent lead Daily runs: 100% used/ });
    assert.equal(ring.dataset.tone, "over");
    const pop = open();
    const alert = within(pop).getByRole("alert");
    assert.match(alert.textContent ?? "", /New runs are blocked until/);
    assert.ok(pop.querySelector('[data-limit="near"]'), "the workspace's tokens are near");
  } finally {
    restore();
  }
});

test("usage that cannot be read offers Retry, and a reader without billing sees the session alone", async () => {
  let fail = true;
  const { seen, restore } = stubFetch({
    "GET /api/threads/s1/usage": () =>
      fail ? new Response(JSON.stringify({ error: "down" }), { status: 500 }) : usage(),
    "GET /api/usage": () =>
      fail
        ? new Response(JSON.stringify({ error: "down" }), { status: 500 })
        : new Response(JSON.stringify({ error: "no" }), { status: 403 }),
  });
  try {
    render(<UsageRing thread="s1" agents={[]} version={1} />);
    const pop = open();
    const retry = await within(pop).findByRole("button", { name: dock.usage.retry });
    fail = false;
    fireEvent.click(retry);
    await waitFor(() => assert.ok(within(pop).getByText(dock.usage.limitsHidden)));
    assert.equal(seen.filter((s) => s.path === "/api/threads/s1/usage").length, 2);
  } finally {
    restore();
  }
});

test("Esc puts the usage popover away and gives focus back to the ring", async () => {
  const { restore } = stubFetch({
    "GET /api/threads/s1/usage": () => usage(),
    "GET /api/usage": () => ({ scopes: [] }),
  });
  try {
    render(<UsageRing thread="s1" agents={[]} version={1} />);
    const ring = await screen.findByRole("button", { name: /Open usage$/ });
    const pop = open();
    assert.equal(document.activeElement, pop, "focus moves into the popover");
    fireEvent.keyDown(pop, { key: "Escape" });
    assert.equal(screen.queryByRole("dialog"), null);
    assert.equal(document.activeElement, ring);
  } finally {
    restore();
  }
});

test("limitsFor keeps the limits that hold this session, fullest first; ringOf prefers a limit near its end", () => {
  const rows = limitsFor(
    [
      scope({ tokens: { used: 100, limit: 1000 } }),
      scope({
        scope: { kind: "project", name: "Launch" },
        runs: { used: 9, limit: 10 },
        tokens: { used: 0, limit: 0 },
      }),
      scope({ scope: { kind: "project", name: "Other" }, runs: { used: 10, limit: 10 } }),
      scope({ scope: { kind: "agent", name: "helper" }, runs: { used: 1, limit: 2 } }),
    ] as never,
    { project: "Launch/web", agents: ["lead"] },
  );
  assert.deepEqual(
    rows.map((r) => r.scope),
    ["Project Launch", "Workspace", "Workspace"],
  );
  assert.equal(rows[0]?.near, true);
  const ring = ringOf(usage() as never, rows);
  assert.equal(ring?.what, "Project Launch Monthly runs");
  assert.equal(ringOf(usage() as never, [])?.what, dock.usage.context);
  // A scope over one of its limits does not make its other measures over.
  const mixed = limitsFor(
    [
      scope({
        state: "over",
        runs: { used: 10, limit: 10 },
        tokens: { used: 860, limit: 1000 },
      }),
    ] as never,
    { agents: [] },
  );
  assert.deepEqual(
    mixed.map((r) => [r.label, r.near, r.over]),
    [
      ["Monthly runs", true, true],
      ["Monthly tokens", true, false],
    ],
  );
});
