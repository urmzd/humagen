import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Run, RunEvent } from "./adapter.ts";
import type { Thread } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { type FocusLoader, MessageFocus } from "./MessageFocus.tsx";
import {
  AnswerRefs,
  ResponseActivity,
  SessionReferences,
  type TraceLoader,
} from "./MessageRefs.tsx";
import { OriginButton, SessionPlace } from "./SessionPlace.tsx";
import { placeOf, referenceOf } from "./trace.ts";

const words = defaultCopy.trace;
let restore: (() => void) | undefined;
afterEach(() => {
  cleanup();
  restore?.();
  restore = undefined;
});

const at = (s: number) => new Date(Date.UTC(2026, 10, 7, 12, 0, s)).toISOString();
const hex = (c: string) => `r${c.repeat(32)}`;
const R1 = hex("1");
const R2 = hex("2");
const R3 = hex("3");
const run = (id: string, extra: Partial<Run> = {}): Run => ({
  id,
  tag_id: id,
  tenant: "acme",
  agent: "humagen",
  rev: 1,
  origin: "slack",
  text: "why did the deploy fail",
  root: R1,
  depth: 0,
  thread: "s_root",
  lease: 0,
  granted: 0,
  spent: 12000,
  calls: 1,
  status: "completed",
  created_at: at(0),
  updated_at: at(14),
  citations: [{ id: "c1", title: "Deploy runbook", url: "https://docs.example/runbook" }],
  ...extra,
});
const ev = (type: string, data: unknown, s: number) =>
  ({
    id: `${type}-${s}`,
    source: "t",
    type,
    channel: "c",
    time: at(s),
    hops: 0,
    data,
  }) as unknown as RunEvent;

const root = run(R1, { picked: "It asks about a deploy" });
const ops = run(R2, {
  agent: "ops",
  parent: R1,
  depth: 1,
  spent: 0,
  citations: [],
  answer: "Migration 0042 timed out.",
  created_at: at(2),
  updated_at: at(9),
});
const analyst = run(R3, {
  agent: "analyst",
  parent: R2,
  depth: 2,
  citations: [],
  answer: "It took 41s.",
  created_at: at(3),
});
const events: Record<string, RunEvent[]> = {
  [R1]: [
    ev(
      "run.delta.tool.call.v1",
      { id: "g", name: "get_context", phase: "requested", arguments: { request: "the thread" } },
      1,
    ),
    ev(
      "run.delta.tool.done.v1",
      {
        id: "g",
        result: JSON.stringify({
          text: "Ana: it failed",
          sources: [{ title: "#eng thread", url: "https://app.slack.com/x" }],
        }),
      },
      2,
    ),
    ev("run.delta.tool.call.v1", { id: "s", name: "search", phase: "requested" }, 3),
    ev("run.delta.tool.done.v1", { id: "s", result: "ok" }, 4),
    ev("run.usage.v1", { accounting_id: "a", model: "anthropic/claude-sonnet" }, 5),
  ],
  [R2]: [
    ev("run.delta.tool.call.v1", { id: "b", name: "deploy_logs", phase: "requested" }, 6),
    ev("run.delta.tool.done.v1", { id: "b", error: "403" }, 7),
    ev(
      "run.parked.v1",
      {
        question: {
          id: "q",
          kind: "approval",
          prompt: "p",
          tool: { name: "retry_deploy", call_id: "x" },
        },
      },
      8,
    ),
    ev("run.resumed.v1", { answer: { question_id: "q", decision: "approve", by_name: "Ana" } }, 9),
  ],
};
const loader: TraceLoader = {
  trace: async () => ({ runs: [root, ops, analyst], spans: [] }),
  events: async (id) => ({ events: events[id] ?? [] }),
};

test("under an answer: quiet chips, and one View activity when more than one agent worked on it", async () => {
  const opened: string[] = [];
  render(<AnswerRefs run={root} eager loader={loader} onActivity={(id) => opened.push(id)} />);
  const refs = await screen.findByTestId("answer-refs");
  await within(refs).findByText("#eng thread");
  const chips = [...refs.querySelectorAll("[data-chip]")].map((c) => [
    c.getAttribute("data-chip"),
    c.textContent,
  ]);
  // A tool that worked is not a chip; it lives in the activity.
  assert.deepEqual(chips, [
    ["context", "#eng thread"],
    ["source", "Deploy runbook"],
  ]);
  assert.equal(within(refs).queryByText(/took|\$0\.01|14s/), null, "no time or cost inline");
  // Nothing nested in the message column: no rows, no chevrons, one control.
  assert.equal(within(refs).queryByText("Migration 0042 timed out."), null);
  assert.equal(refs.querySelectorAll("[aria-expanded]").length, 0);
  const view = within(refs).getByTestId("view-activity");
  assert.match(view.textContent ?? "", new RegExp(`${words.viewActivity}.*${words.agents(3)}`));
  fireEvent.click(view);
  assert.deepEqual(opened, [R1]);
});

test("a single-agent answer has its chips and no View activity", async () => {
  const alone = run(R1, { calls: 0 });
  const one: TraceLoader = {
    trace: async () => ({ runs: [alone], spans: [] }),
    events: loader.events,
  };
  render(<AnswerRefs run={alone} eager loader={one} onActivity={() => {}} />);
  const refs = await screen.findByTestId("answer-refs");
  await within(refs).findByText("#eng thread");
  assert.equal(within(refs).queryByTestId("view-activity"), null);
});

test("a response's activity is every run behind it, flat and in time order, with references, time and cost", async () => {
  render(<ResponseActivity run={root} onClose={() => {}} loader={loader} />);
  const list = await screen.findByTestId("activity-list");
  assert.match(screen.getByTestId("activity-pick").textContent ?? "", /It asks about a deploy/);
  const rows = within(list).getAllByTestId("activity-row");
  assert.deepEqual(
    rows.map((r) => r.getAttribute("data-run")),
    [R1, R2, R3],
    "parent, child and grandchild, flattened by start time",
  );
  const [first, second, third] = rows;
  assert.match(first?.textContent ?? "", /14s · \$0\.01 · anthropic\/claude-sonnet/);
  assert.ok(within(second!).getByText("Migration 0042 timed out."));
  // What a person should see sits with its run: the approval and who decided, the tool that failed.
  assert.ok(within(second!).getByText(`${words.decided.allow} retry_deploy ${words.by("Ana")}`));
  assert.ok(within(second!).getByText(words.failedTool("deploy_logs")));
  assert.ok(within(third!).getByText("It took 41s."));
  assert.equal(
    within(second!)
      .getByRole("link", { name: words.focus("ops") })
      .getAttribute("href"),
    `/messages/${R2}`,
  );
});

test("Details collects each message's references, with its time, cost and model, origin first", async () => {
  render(
    <SessionReferences
      runs={[root, ops]}
      origin={referenceOf("slack://T1/C1/1.2")}
      loader={loader}
    />,
  );
  const rows = await screen.findAllByTestId("reference-row");
  assert.equal(rows.length, 1, "only answers people see, not the runs they called");
  await waitFor(() => assert.match(rows[0]?.textContent ?? "", /anthropic\/claude-sonnet/));
  assert.match(rows[0]?.textContent ?? "", /14s · \$0\.01 · anthropic\/claude-sonnet/);
  assert.match(rows[0]?.textContent ?? "", new RegExp(words.delegatedTo("ops")));
  const first = rows[0]?.querySelector("[data-chip]");
  assert.equal(first?.getAttribute("data-chip"), "origin");
  assert.equal(
    first?.querySelector("a")?.getAttribute("href"),
    "https://app.slack.com/client/T1/C1/thread/C1-1.2",
  );
});

test("the header points back to where the session started, and says nothing for the web", () => {
  const slack = placeOf(
    { surface: { client: "acme", channel: "C1", ts: "1.2" }, visibility: "shared" },
    "T1",
  );
  render(
    <>
      <OriginButton place={slack} />
      <SessionPlace place={slack} sender="Ana" />
    </>,
  );
  assert.equal(
    screen.getByRole("link", { name: words.place.openIn("Slack") }).getAttribute("href"),
    "https://app.slack.com/client/T1/C1/thread/C1-1.2",
  );
  assert.match(
    screen.getByTestId("session-place").textContent ?? "",
    /Slack thread.*from Ana.*Slack channel/,
  );
  cleanup();
  const web = placeOf({ visibility: "private" });
  render(
    <>
      <OriginButton place={web} />
      <SessionPlace place={web} sender="Ana" />
    </>,
  );
  assert.equal(screen.queryByTestId("origin-button"), null);
  assert.equal(screen.queryByTestId("session-place"), null);
});

test("a nested message opens at its own address under a breadcrumb back to the root", async () => {
  const thread = {
    id: "s_root",
    title: "Nightly deploy",
    project: "",
    channel: "general",
    visibility: "shared",
    participants: [],
    version: 1,
    created_by: "ana",
    created_at: at(0),
    updated_at: at(0),
    surface: { client: "acme", channel: "C1", ts: "1.2" },
  } as Thread;
  const focus: FocusLoader = {
    ...loader,
    tree: async () => [root, ops, analyst],
    thread: async () => thread,
  };
  const net = stubFetch({
    "GET /api/clients": () => ({ clients: [], caps: { max: 1, max_per_surface: 1 } }),
  });
  restore = net.restore;
  const went: unknown[] = [];
  render(<MessageFocus id={R2} go={(r) => went.push(r)} loader={focus} />);
  assert.ok(await screen.findByText("Migration 0042 timed out."));
  const crumbs = screen.getByRole("navigation", { name: words.breadcrumb });
  assert.deepEqual(
    within(crumbs)
      .getAllByRole("link")
      .map((a) => [a.textContent, a.getAttribute("href")]),
    [
      ["Nightly deploy", "/messages/s_root"],
      ["humagen", `/messages/${R1}`],
    ],
  );
  fireEvent.click(within(crumbs).getByRole("link", { name: "Nightly deploy" }));
  assert.deepEqual(went, [{ page: "conversation", id: "s_root" }]);
  // Its own activity, flat: the grandchild under it, opening at its own address.
  const rows = within(screen.getByTestId("activity-list")).getAllByTestId("activity-row");
  assert.deepEqual(
    rows.map((r) => r.getAttribute("data-run")),
    [R3],
  );
  await waitFor(() => assert.ok(screen.getByRole("link", { name: words.place.openIn("Slack") })));
});

test("the session menu is a compact list: plain rows, Delete last after a rule", async () => {
  const { SessionActions } = await import("./SessionActions.tsx");
  const net = stubFetch({ "GET /api/marks": () => ({ archived: [], read: {}, tips: [] }) });
  restore = net.restore;
  render(
    <SessionActions
      thread={{
        id: "s_x",
        title: "x",
        version: 1,
        visibility: "private",
        project: "",
        channel: "general",
        participants: [],
        created_by: "ada",
        created_at: "",
        updated_at: "",
      }}
      canManage
      onChanged={() => {}}
      onUpdated={() => {}}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Session actions" }));
  const menu = await screen.findByRole("menu", { name: "Session actions" });
  const kids = [...menu.children].map((el) => (el.tagName === "HR" ? "—" : el.textContent));
  assert.deepEqual(kids, ["Rename", "Archive", "—", "Delete"]);
  for (const item of within(menu).getAllByRole("menuitem"))
    assert.doesNotMatch(item.className, /\bborder\b/, `${item.textContent} has no border`);
  assert.match(within(menu).getByRole("menuitem", { name: "Delete" }).className, /text-aui-bad/);
});
