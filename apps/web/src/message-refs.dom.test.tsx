import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { answerOf, foldAll } from "@humagen/core";
import { responseStreams } from "@humagen/core/fixtures/responses";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Run, RunEvent } from "./adapter.ts";
import type { Thread } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { type FocusLoader, MessageFocus } from "./MessageFocus.tsx";
import { AnswerRefs, SessionReferences } from "./MessageRefs.tsx";
import { ResponseActivityPanel } from "./ResponseActivity.tsx";
import { type ResponseSource, type RunLoader, replaySource, runEvents } from "./responseSource.ts";
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
  agent: "lead",
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
  answer: "The token expired; ops rotated it.",
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

const root = run(R1);
const ops = run(R2, {
  agent: "ops",
  parent: R1,
  depth: 1,
  spent: 0,
  citations: [],
  text: "Find why the nightly deploy failed",
  answer: "The deploy token expired.",
  created_at: at(2),
  updated_at: at(9),
});
const analyst = run(R3, {
  agent: "analyst",
  parent: R2,
  depth: 2,
  citations: [],
  text: "How long did it run?",
  answer: "41s.",
  created_at: at(3),
  updated_at: at(5),
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
      { id: "g", result: JSON.stringify({ text: "Ana: it failed" }) },
      1,
    ),
    ev(
      "run.delta.tool.call.v1",
      { id: "c", name: "call_agent", phase: "requested", arguments: { agent: "ops" } },
      2,
    ),
    ev("run.delta.tool.done.v1", { id: "c", name: "call_agent", result: "done" }, 9),
    ev("run.usage.v1", { accounting_id: "a", model: "anthropic/claude-sonnet" }, 13),
  ],
  [R2]: [
    ev(
      "run.delta.tool.call.v1",
      { id: "b", name: "deploy_logs", phase: "requested", arguments: { job: "nightly" } },
      4,
    ),
    ev("run.delta.tool.done.v1", { id: "b", error: "403" }, 6),
    ev(
      "run.parked.v1",
      {
        question: {
          id: "q",
          kind: "approval",
          prompt: "Retry the deploy?",
          tool: { name: "retry_deploy", call_id: "x" },
        },
      },
      7,
    ),
    ev("run.delta.tool.call.v1", { id: "x", name: "retry_deploy", phase: "requested" }, 7),
    ev("run.resumed.v1", { answer: { question_id: "q", decision: "approve", by_name: "Ana" } }, 8),
    ev("run.delta.tool.done.v1", { id: "x", result: "ok" }, 8),
  ],
};
const loader: RunLoader = {
  tree: async () => [root, ops, analyst],
  events: async (id) => ({ events: events[id] ?? [] }),
};

test("today's runs read as one response: agent calls nested, tools, approvals, answer, cost and model", () => {
  const state = foldAll(runEvents(root, [root, ops, analyst], events));
  assert.equal(state.status, "done");
  assert.equal(answerOf(state).text, "The token expired; ops rotated it.");
  const call = state.content.find((b) => b.type === "tool" && b.kind === "agent");
  assert.ok(call && call.type === "tool");
  assert.equal(call.agent, "ops");
  assert.equal(call.title, "Find why the nightly deploy failed");
  assert.equal(call.status, "done");
  assert.equal(call.result?.text, "The deploy token expired.");
  const opsTools = call.content.flatMap((b) =>
    b.type === "tool" ? [[b.name, b.status, b.decidedBy]] : [],
  );
  // In the order they started: analyst was called before the logs were read.
  assert.deepEqual(opsTools, [
    ["agent", "done", undefined],
    ["deploy_logs", "error", undefined],
    ["retry_deploy", "done", "Ana"],
  ]);
  assert.equal(
    state.tools[Object.keys(state.tools).find((k) => state.tools[k]?.agent === "analyst") ?? ""]
      ?.depth,
    2,
  );
  // The old delegation tool is the agent call itself, not a second row.
  assert.equal(
    Object.values(state.tools).some((t) => t.name === "call_agent"),
    false,
  );
  assert.equal(state.costUsd, 0.012);
  assert.deepEqual(Object.keys(state.byModel ?? {}), ["anthropic/claude-sonnet"]);
});

test("under an answer: its chips, and Worked … that opens its activity", async () => {
  const opened: string[] = [];
  render(<AnswerRefs run={root} eager loader={loader} onActivity={(id) => opened.push(id)} />);
  const refs = await screen.findByTestId("answer-refs");
  const worked = await within(refs).findByTestId("worked");
  assert.equal(worked.getAttribute("aria-expanded"), "false");
  assert.equal(worked.textContent, words.worked("14s", 2, 3));
  assert.deepEqual(
    [...refs.querySelectorAll("[data-chip]")].map((c) => c.textContent),
    ["Deploy runbook"],
  );
  assert.equal(within(refs).queryByText(/\$0\.01/), null, "no cost inline");
  fireEvent.click(worked);
  assert.deepEqual(opened, [R1]);
});

test("an answer that used no tool and called no one has no Worked", async () => {
  const alone = run(R1, { calls: 0 });
  const quiet: RunLoader = { tree: async () => [alone], events: async () => ({ events: [] }) };
  render(<AnswerRefs run={alone} eager loader={quiet} onActivity={() => {}} />);
  await screen.findByText("Deploy runbook");
  await new Promise((r) => setTimeout(r, 20));
  assert.equal(screen.queryByTestId("worked"), null);
});

test("two agents in parallel are two lanes; the root's own answer stays beside the panel", async () => {
  render(
    <ResponseActivityPanel
      source={replaySource("p", responseStreams.parallelAgents)}
      onClose={() => {}}
    />,
  );
  const lanes = await screen.findAllByTestId("activity-lane");
  assert.deepEqual(
    lanes.map((l) => l.getAttribute("data-agent")),
    ["humagen", "security-reviewer", "perf-reviewer"],
  );
  assert.match(
    screen.getByTestId("activity-summary").textContent ?? "",
    /Worked 5\.8s · 2 agents · 3 tools · \$0\.02/,
  );
  assert.equal(screen.queryByText(/^## security-reviewer/), null, "the answer is not repeated");
  // Parts that went well are closed; nothing waits or failed here.
  for (const part of screen.getAllByTestId("activity-part"))
    assert.equal(part.querySelector("details")?.open, false);
});

test("a question two agents deep opens where it waits and is answered there", async () => {
  const net = stubFetch({ "POST /api/messages/msg_nested/answer": () => ({}) });
  restore = net.restore;
  render(
    <ResponseActivityPanel
      source={replaySource("n", responseStreams.nestedApproval)}
      messageId="msg_nested"
      onClose={() => {}}
    />,
  );
  const deploy = (await screen.findAllByTestId("activity-part")).find(
    (p) => p.getAttribute("data-tool") === "htu_r_deployer_1",
  );
  assert.ok(deploy);
  assert.equal(deploy.querySelector("details")?.open, true, "awaiting opens by default");
  const failed = screen
    .getAllByTestId("activity-part")
    .find((p) => p.getAttribute("data-status") === "error");
  assert.equal(failed?.querySelector("details")?.open, true, "a failure opens by default");
  fireEvent.click(within(deploy).getByRole("button", { name: "Approve" }));
  await waitFor(() =>
    assert.deepEqual(net.seen.find((r) => r.method === "POST")?.body, {
      tool_use_id: "htu_r_deployer_1",
      answer: "approve",
    }),
  );
});

test("a live stream says when a call starts waiting, politely, and a replay says nothing", async () => {
  let push: ((e: (typeof responseStreams.nestedApproval)[number]) => void) | undefined;
  const live: ResponseSource = {
    key: "live",
    live: true,
    open: (onEvent) => {
      push = onEvent;
      return () => {};
    },
  };
  render(<ResponseActivityPanel source={live} onClose={() => {}} />);
  const politely = () =>
    screen.getByTestId("response-activity").querySelector('[aria-live="polite"]') as HTMLElement;
  const region = politely();
  assert.equal(region.getAttribute("aria-live"), "polite");
  for (const e of responseStreams.nestedApproval) push?.(e);
  await waitFor(() =>
    assert.match(region.textContent ?? "", /Rotate the deploy token is waiting for you/),
  );
  cleanup();
  render(
    <ResponseActivityPanel
      source={replaySource("r", responseStreams.stopped)}
      onClose={() => {}}
    />,
  );
  await screen.findAllByTestId("activity-lane");
  assert.equal(politely().textContent, "");
});

test("Details collects each answer's references, with how it worked, its cost and model, origin first", async () => {
  render(
    <SessionReferences
      runs={[root, ops]}
      origin={referenceOf("slack://T1/C1/1.2")}
      loader={loader}
    />,
  );
  const rows = await screen.findAllByTestId("reference-row");
  assert.equal(rows.length, 1, "only answers people see, not the runs they called");
  await waitFor(() => assert.match(rows[0]?.textContent ?? "", /claude-sonnet/));
  assert.match(
    rows[0]?.textContent ?? "",
    /Worked 14s · 2 agents · 3 tools · \$0\.01 · claude-sonnet/,
  );
  assert.match(rows[0]?.textContent ?? "", new RegExp(words.delegatedTo("ops, analyst")));
  const first = rows[0]?.querySelector("[data-chip]");
  assert.equal(first?.getAttribute("data-chip"), "origin");
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

test("a nested message opens at its own address under a breadcrumb, with its own activity", async () => {
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
  const focus: FocusLoader = { ...loader, thread: async () => thread };
  const net = stubFetch({
    "GET /api/clients": () => ({ clients: [], caps: { max: 1, max_per_surface: 1 } }),
  });
  restore = net.restore;
  const went: unknown[] = [];
  render(<MessageFocus id={R2} go={(r) => went.push(r)} loader={focus} />);
  assert.ok(await screen.findByText("The deploy token expired."));
  const crumbs = screen.getByRole("navigation", { name: words.breadcrumb });
  assert.deepEqual(
    within(crumbs)
      .getAllByRole("link")
      .map((a) => [a.textContent, a.getAttribute("href")]),
    [
      ["Nightly deploy", "/messages/s_root"],
      ["lead", `/messages/${R1}`],
    ],
  );
  fireEvent.click(within(crumbs).getByRole("link", { name: "Nightly deploy" }));
  assert.deepEqual(went, [{ page: "conversation", id: "s_root" }]);
  assert.deepEqual(
    screen.getAllByTestId("activity-lane").map((l) => l.getAttribute("data-agent")),
    ["ops", "analyst"],
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
