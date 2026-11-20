import "./react.ts";
import assert from "node:assert/strict";
import { test } from "node:test";
import { emptyTurn, paragraphsAtSteps, reduce, type Task, type Turn } from "@humagen/core";
import { Presenter } from "./present.tsx";
import { SlackError } from "./slack.ts";

// Replays ported from the independent review of #393 (generated data only). A script is a list of writes;
// each write is a list of ops folded into the turn before one presenter.update.
type Op =
  | string
  | { task: string; sub?: string; title?: string; status?: Task["status"]; details?: string };
type Script = { name: string; writes: Op[][]; end?: "done" | "failed" | "stopped" };

function fold(turn: Turn, op: Op): Turn {
  if (typeof op === "string") return reduce(turn, { type: "text.delta", text: op });
  const was = turn.tasks.find((t) => t.id === op.task);
  const task: Task = {
    ...(was ?? {}),
    id: op.task,
    title: op.title ?? was?.title ?? op.sub ?? `Searching ${op.task}`,
    status: op.status ?? was?.status ?? "in_progress",
    ...(op.details ? { details: op.details } : {}),
    ...(op.sub ? { subagent: { name: op.sub } } : was?.subagent ? { subagent: was.subagent } : {}),
  } as Task;
  return reduce(turn, { type: "task.upserted", task });
}

type Shown = { kind: "text" | "task" | "trailer" | "start" | "stop"; v: string; id?: string };

async function replay(s: Script, native = true) {
  const calls: { method: string; args: Record<string, unknown> }[] = [];
  let n = 0;
  const slack = {
    async call<T>(method: string, args: Record<string, unknown> = {}) {
      calls.push({ method, args });
      if (!native && method === "chat.startStream")
        throw new SlackError(method, "not_allowed_token_type");
      return { ts: `1.${String(++n).padStart(4, "0")}` } as T;
    },
    async respond() {},
  };
  const p = new Presenter(slack, {
    channel: "C0GEN",
    threadTs: "1.0",
    triggerTs: "1.0",
    recipientUser: "U0GEN",
    recipientTeam: "T0GEN",
  });
  let turn = reduce(emptyTurn("t1", { name: "helper" }), {
    type: "turn.started",
    turnId: "t1",
    agent: { name: "helper" },
    sessionUrl: "https://console.example.test/c/gen-1",
  } as never);
  for (const w of s.writes) {
    for (const op of w) turn = fold(turn, op);
    await p.update(turn);
  }
  if (s.end === "failed")
    turn = reduce(turn, {
      type: "turn.failed",
      failure: { kind: "internal", message: "generated failure" },
    } as never);
  else if (s.end === "stopped") turn = reduce(turn, { type: "turn.stopped" });
  else turn = reduce(turn, { type: "turn.completed" });
  await p.finish(turn);
  const shown: Shown[] = [];
  for (const c of calls) {
    if (c.method === "chat.startStream") shown.push({ kind: "start", v: "" });
    if (c.method === "chat.stopStream") shown.push({ kind: "stop", v: "" });
    if (c.method !== "chat.appendStream" && c.method !== "chat.stopStream") continue;
    for (const ch of (c.args.chunks ?? []) as {
      type: string;
      text?: string;
      title?: string;
      id?: string;
    }[]) {
      if (ch.type === "markdown_text") {
        const trailer = /^\n\n_Continues in the next message_$/.test(ch.text ?? "");
        shown.push({ kind: trailer ? "trailer" : "text", v: ch.text ?? "" });
      }
      if (ch.type === "task_update") shown.push({ kind: "task", v: ch.title ?? "", id: ch.id });
    }
  }
  return { calls, shown, turn };
}

const squash = (x: string) => x.replace(/\s+/g, "");

/** Checks every streamed replay must pass. Returns problems found. */
function audit(s: Script, r: Awaited<ReturnType<typeof replay>>): string[] {
  const problems: string[] = [];
  const words = r.shown
    .filter((x) => x.kind === "text")
    .map((x) => x.v)
    .join("");
  // 1. Nothing dropped, duplicated or reordered (whitespace ignored).
  if (squash(words) !== squash(r.turn.text))
    problems.push(
      `text mismatch:\n  shown=${JSON.stringify(words)}\n  model=${JSON.stringify(r.turn.text)}`,
    );
  // 2. Exactly the PR's layout (modulo trailing whitespace held back).
  const laid = paragraphsAtSteps(r.turn.text, r.turn.tasks).replace(/\s+$/, "");
  if (words.replace(/\s+$/, "") !== laid)
    problems.push(
      `layout mismatch:\n  shown=${JSON.stringify(words)}\n  laid =${JSON.stringify(laid)}`,
    );
  // 3. Each card first appears after exactly the words before its step.
  for (const t of r.turn.tasks) {
    const i = r.shown.findIndex((x) => x.kind === "task" && x.id === t.id);
    if (i === -1) {
      problems.push(`card ${t.id} never shown`);
      continue;
    }
    const before = r.shown
      .slice(0, i)
      .filter((x) => x.kind === "text")
      .map((x) => x.v)
      .join("");
    const want = r.turn.text.slice(0, t.at ?? 0);
    if (squash(before) !== squash(want))
      problems.push(
        `card ${t.id} at ${t.at}: above it ${JSON.stringify(before)}, want ${JSON.stringify(want)}`,
      );
  }
  // 4. No run of three blank lines (four newlines).
  if (/\n[ \t]*\n[ \t]*\n[ \t]*\n/.test(words))
    problems.push("three newlines in a row (2+ blank lines)");
  return problems;
}

const A = (id: string, sub = "researcher", extra: Partial<Op & object> = {}) =>
  ({ task: id, sub, ...extra }) as Op;
const T = (id: string, extra: Partial<Op & object> = {}) => ({ task: id, ...extra }) as Op;
const done = (id: string) => ({ task: id, status: "complete" as const });

const long = Array.from({ length: 60 }, (_, i) => `Paragraph ${i} ${"word ".repeat(40)}`).join(
  "\n\n",
);

const scripts: Script[] = [
  {
    name: "parallel calls in one write",
    writes: [
      ["I'll ask"],
      [" both.", A("a"), A("b", "writer")],
      [done("a"), done("b"), "Both answered."],
    ],
  },
  {
    name: "nested / overlapping calls",
    writes: [["Start."], [A("a")], ["Mid.", A("b", "writer")], [done("b")], [done("a"), "End."]],
  },
  { name: "call before any text", writes: [[A("a")], [done("a"), "Answer."]] },
  {
    name: "call mid-word",
    writes: [
      ["hel", A("a")],
      [done("a"), "lo world"],
    ],
  },
  {
    name: "call mid markdown link",
    writes: [
      ["see [the doc", A("a")],
      [done("a"), "s](https://example.test/x) now"],
    ],
  },
  {
    name: "call mid inline code",
    writes: [
      ["run `go", A("a")],
      [done("a"), " test` now"],
    ],
  },
  {
    name: "call mid list item",
    writes: [
      ["- one\n- tw", A("a")],
      [done("a"), "o\n- three"],
    ],
  },
  {
    name: "call between list items",
    writes: [
      ["1. one\n", A("a")],
      [done("a"), "2. two\n3. three"],
    ],
  },
  {
    name: "call between table rows",
    writes: [
      ["| a | b |\n|---|---|\n| 1 | 2 |\n", A("a")],
      [done("a"), "| 3 | 4 |\n"],
    ],
  },
  {
    name: "call mid table row",
    writes: [
      ["| a | b |\n|---|---|\n| 1 ", A("a")],
      [done("a"), "| 2 |\n"],
    ],
  },
  {
    name: "call inside fence",
    writes: [
      ["```go\nfmt.Println(1)\n", A("a")],
      [done("a"), "fmt.Println(2)\n```\nDone."],
    ],
  },
  {
    name: "call right after closing fence",
    writes: [
      ["```go\nx\n```", A("a")],
      [done("a"), "\nAfter."],
    ],
  },
  {
    name: "call after fence and newline",
    writes: [
      ["```go\nx\n```\n", A("a")],
      [done("a"), "After."],
    ],
  },
  {
    name: "call right after opening fence",
    writes: [
      ["```", A("a")],
      [done("a"), "go\nx\n```\nAfter."],
    ],
  },
  {
    name: "call in blockquote continuation",
    writes: [
      ["> quoted line\n", A("a")],
      [done("a"), "> continues"],
    ],
  },
  {
    name: "two calls back to back",
    writes: [
      ["Text.", A("a"), A("b", "writer")],
      [done("a"), done("b"), "More."],
    ],
  },
  {
    name: "one char per write",
    writes: [
      ..."Let me check that.".split("").map((c) => [c] as Op[]),
      [A("a")],
      [done("a")],
      ..."Here it is.".split("").map((c) => [c] as Op[]),
    ],
  },
  { name: "write boundary at the step", writes: [["Before."], [A("a"), "After."], [done("a")]] },
  {
    name: "boundary at step, trailing space",
    writes: [["Before. "], [A("a"), "After."], [done("a")]],
  },
  {
    name: "text ends with newline before call",
    writes: [
      ["Let me check.\n", A("a")],
      [done("a"), "Found it."],
    ],
  },
  {
    name: "long text rolls over before call",
    writes: [
      [long.slice(0, 6000)],
      [long.slice(6000), " Now asking.", A("a")],
      [done("a"), "Answer after."],
    ],
  },
  {
    name: "long text rolls over in the same write as call",
    writes: [
      [`${long} Now asking.`, A("a")],
      [done("a"), "Answer after."],
    ],
  },
  {
    name: "card updated many times",
    writes: [
      ["Asking."],
      [A("a")],
      ...Array.from({ length: 20 }, (_, i) => [T("a", { details: `step ${i}` }), `w${i} `] as Op[]),
      [done("a"), "Final."],
    ],
  },
  {
    name: "failed call",
    writes: [
      ["Asking.", A("a")],
      [{ task: "a", status: "error" }, "It failed."],
    ],
  },
  { name: "stopped turn mid call", writes: [["Asking.", A("a")]], end: "stopped" },
  { name: "failed turn mid call", writes: [["Asking.", A("a")]], end: "failed" },
  { name: "no answer after call", writes: [["I'll ask.", A("a")], [done("a")]] },
  {
    name: "tool (non-agent) step",
    writes: [
      ["Searching.", T("s1")],
      [done("s1"), "Found."],
    ],
  },
];

for (const s of scripts) {
  test(`replay: ${s.name}`, async () => {
    const r = await replay(s);
    const problems = audit(s, r);
    assert.deepEqual(problems, [], problems.join("\n"));
  });
}

test("final streamed blocks do not repeat the answer", async () => {
  const r = await replay(scripts[0] as Script);
  const stop = [...r.calls].reverse().find((c) => c.method === "chat.stopStream");
  assert.ok(!JSON.stringify(stop?.args.blocks ?? []).includes("Both answered"));
});
