import assert from "node:assert/strict";
import { test } from "node:test";
import type { Run, RunEvent } from "./adapter.ts";
import {
  buildTrace,
  chipsOf,
  firstLines,
  placeOf,
  readEvents,
  referenceOf,
  slackUrl,
  totals,
} from "./trace.ts";

const at = (s: number) => new Date(Date.UTC(2026, 10, 7, 12, 0, s)).toISOString();

const run = (id: string, over: Partial<Run> = {}): Run => ({
  id,
  tag_id: `api-${id}`,
  tenant: "acme",
  agent: "humagen",
  rev: 1,
  origin: "slack",
  text: "why did the deploy fail",
  root: "r1",
  depth: 0,
  thread: "t1",
  lease: 0,
  granted: 0,
  calls: 0,
  status: "completed",
  created_at: at(0),
  updated_at: at(14),
  ...over,
});

const env = (type: string, data: unknown, s: number, runId = "r1") =>
  ({
    id: `${type}-${s}`,
    source: "test",
    type,
    channel: "c",
    time: at(s),
    hops: 0,
    run: runId,
    data,
  }) as unknown as RunEvent;

test("tool calls pair their call and result, in the order they were asked", () => {
  const { tools } = readEvents([
    env("run.delta.tool.call.v1", { id: "b", name: "search", phase: "requested" }, 3),
    env(
      "run.delta.tool.call.v1",
      { id: "a", name: "logs", phase: "requested", arguments: { job: "deploy" } },
      1,
    ),
    env(
      "run.delta.tool.call.v1",
      { id: "a", name: "logs", phase: "executing", arguments: { job: "deploy" } },
      2,
    ),
    env("run.delta.tool.done.v1", { id: "a", result: "exit 1" }, 4),
    env("run.delta.tool.done.v1", { id: "b", error: "timeout" }, 5),
  ]);
  assert.deepEqual(
    tools.map((t) => [t.name, t.input, t.output, t.error, t.ms]),
    [
      ["logs", { job: "deploy" }, "exit 1", undefined, 3000],
      ["search", undefined, undefined, "timeout", 2000],
    ],
  );
});

test("a call whose arguments were kept back says so", () => {
  const { tools } = readEvents([
    env(
      "run.delta.tool.call.v1",
      { id: "a", name: "hr_lookup", phase: "requested", arguments_bytes: 120 },
      1,
    ),
  ]);
  assert.equal(tools[0]?.withheld, true);
});

test("get_context shows the request, the origin it read, the first lines and the sources", () => {
  const { context, tools } = readEvents([
    env(
      "run.delta.tool.call.v1",
      {
        id: "c",
        name: "get_context",
        phase: "requested",
        arguments: { request: "what did people say about the deploy" },
      },
      1,
    ),
    env(
      "run.delta.tool.done.v1",
      {
        id: "c",
        result: JSON.stringify({
          text: "Ana: the deploy failed at 02:10.\n\nBo: it was the migration.\nCy: agreed.",
          sources: [{ title: "#eng thread", url: "https://app.slack.com/x" }],
          reference: "slack://T1/C1/1699.1",
          truncated: true,
        }),
      },
      2,
    ),
  ]);
  assert.equal(tools.length, 0, "get_context is not listed again as a tool");
  assert.deepEqual(context[0], {
    id: "c",
    request: "what did people say about the deploy",
    reference: "slack://T1/C1/1699.1",
    summary: "Ana: the deploy failed at 02:10. Bo: it was the migration.",
    sources: [{ title: "#eng thread", url: "https://app.slack.com/x" }],
    truncated: true,
    error: undefined,
    withheld: false,
  });
});

test("approvals say how they were decided and by whom", () => {
  const { approvals } = readEvents([
    env(
      "run.parked.v1",
      {
        question: {
          id: "q1",
          kind: "approval",
          prompt: "Run deploy?",
          tool: { name: "deploy", call_id: "x" },
        },
      },
      1,
    ),
    env(
      "run.resumed.v1",
      { answer: { question_id: "q1", decision: "approve", by: "u1", by_name: "Ana", note: "ok" } },
      2,
    ),
    env("run.parked.v1", { question: { id: "q2", kind: "approval", prompt: "Delete branch?" } }, 3),
  ]);
  assert.deepEqual(
    approvals.map((a) => [a.action, a.decision, a.by, a.note]),
    [
      ["deploy", "allow", "Ana", "ok"],
      ["Delete branch?", "ask", undefined, undefined],
    ],
  );
});

test("a trace nests each run under the run that called it, with time, cost and pick", () => {
  const root = run("r1", { picked: "It asks about a deploy", spent: 12000 });
  const child = run("r2", {
    agent: "ops",
    parent: "r1",
    depth: 1,
    created_at: at(2),
    updated_at: at(9),
  });
  const grandchild = run("r3", { agent: "analyst", parent: "r2", depth: 2, created_at: at(3) });
  const orphan = run("r4", { agent: "writer", parent: "gone", depth: 1, created_at: at(4) });
  const otherTurn = run("r9", { root: "r9", agent: "writer" });
  const trace = buildTrace("r1", [grandchild, child, root, orphan, otherTurn], [], {
    r1: [env("run.delta.tool.call.v1", { id: "a", name: "session_call", phase: "requested" }, 1)],
  });
  assert.ok(trace);
  assert.equal(trace.pick, "It asks about a deploy");
  assert.equal(trace.cost, 12000);
  assert.equal(trace.ms, 14000);
  assert.deepEqual(
    trace.children.map((c) => [c.agent, c.children.map((g) => g.agent)]),
    [
      ["ops", ["analyst"]],
      ["writer", []],
    ],
  );
  assert.deepEqual(totals(trace).agents, 4);
  assert.deepEqual(totals(trace).tools, 1);
});

test("a run without events falls back to its tool spans", () => {
  const trace = buildTrace(
    "r1",
    [run("r1")],
    [
      { run_id: "r1", agent: "humagen", kind: "tool", name: "search", start: at(1), end: at(3) },
      { run_id: "r1", agent: "humagen", kind: "attempt", name: "1", start: at(0) },
    ],
    {},
  );
  assert.deepEqual(
    trace?.tools.map((t) => [t.name, t.ms]),
    [["search", 2000]],
  );
});

test("references read as places with links back", () => {
  assert.deepEqual(referenceOf("slack://T1/C1/1699.1"), {
    kind: "slack",
    label: "Slack thread",
    url: "https://app.slack.com/client/T1/C1/thread/C1-1699.1",
  });
  assert.equal(referenceOf("slack://T1/D9/1699.1").label, "Slack DM");
  assert.deepEqual(referenceOf("github://urmzd/humagen/pull/243"), {
    kind: "github",
    label: "urmzd/humagen PR #243",
    url: "https://github.com/urmzd/humagen/pull/243",
  });
  assert.equal(
    referenceOf("github://urmzd/humagen/issue/9").url,
    "https://github.com/urmzd/humagen/issues/9",
  );
  assert.equal(referenceOf("schedule://nightly").label, "Schedule nightly");
  assert.equal(referenceOf("https://example.com/a/b").label, "example.com/a/b");
  assert.equal(referenceOf("corpus:hr/doc-1").kind, "other");
  assert.equal(slackUrl("", "C1", "1"), "https://slack.com/app_redirect?channel=C1");
});

test("first lines keep two lines and cap their length", () => {
  assert.equal(firstLines("a\n\n b \nc"), "a b");
  assert.equal(firstLines("x".repeat(300)).length, 241);
});

test("a session's place: Slack by its surface, PRs by the root's parent, else the console", () => {
  assert.deepEqual(
    placeOf({ surface: { client: "acme", channel: "C1", ts: "1.2" }, visibility: "shared" }, "T1"),
    {
      kind: "slack",
      label: "Slack thread",
      url: "https://app.slack.com/client/T1/C1/thread/C1-1.2",
      origin: "slack://T1/C1/1.2",
      audience: "channel",
    },
  );
  assert.equal(
    placeOf({ surface: { client: "acme", channel: "D1", ts: "1" }, visibility: "private" })
      .audience,
    "dm",
  );
  const pr = placeOf({
    session: { parent: "github://urmzd/humagen/pull/5" },
    visibility: "shared",
  });
  assert.deepEqual(
    [pr.kind, pr.label, pr.url, pr.audience],
    ["github", "urmzd/humagen PR #5", "https://github.com/urmzd/humagen/pull/5", "shared"],
  );
  assert.deepEqual(placeOf({ session: { parent: "s_abc" }, visibility: "private" }), {
    kind: "console",
    label: "Console",
    audience: "private",
  });
});

test("a message's chips: origin first, what get_context pulled, sources, then approvals and failures only", () => {
  const node = buildTrace(
    "r1",
    [run("r1", { citations: [{ id: "c", title: "Runbook", url: "https://d/rb" }] })],
    [],
    {
      r1: [
        env(
          "run.delta.tool.call.v1",
          {
            id: "g",
            name: "get_context",
            phase: "requested",
            arguments: { request: "the thread" },
          },
          1,
        ),
        env(
          "run.delta.tool.done.v1",
          {
            id: "g",
            result: JSON.stringify({
              text: "x",
              sources: [{ title: "#eng thread", url: "https://s/t" }],
            }),
          },
          2,
        ),
        env("run.delta.tool.call.v1", { id: "a", name: "search", phase: "requested" }, 3),
        env("run.delta.tool.done.v1", { id: "a", result: "ok" }, 4),
        env("run.delta.tool.call.v1", { id: "b", name: "deploy_logs", phase: "requested" }, 5),
        env("run.delta.tool.done.v1", { id: "b", error: "403" }, 6),
        env(
          "run.parked.v1",
          {
            question: {
              id: "q",
              kind: "approval",
              prompt: "p",
              tool: { name: "retry", call_id: "x" },
            },
          },
          7,
        ),
        env("run.resumed.v1", { answer: { question_id: "q", decision: "deny", by_name: "Bo" } }, 8),
        env("run.usage.v1", { accounting_id: "a", model: "anthropic/claude-sonnet" }, 9),
      ],
    },
  );
  assert.ok(node);
  assert.equal(node.model, "anthropic/claude-sonnet");
  const chips = chipsOf(node, referenceOf("github://o/r/pull/5"));
  assert.deepEqual(
    chips.map((c) => [c.kind, c.label, c.url, c.decision, c.by]),
    [
      ["origin", "o/r PR #5", "https://github.com/o/r/pull/5", undefined, undefined],
      ["context", "#eng thread", "https://s/t", undefined, undefined],
      ["source", "Runbook", "https://d/rb", undefined, undefined],
      ["approval", "retry", undefined, "deny", "Bo"],
      ["failure", "deploy_logs", undefined, undefined, undefined],
    ],
  );
});
