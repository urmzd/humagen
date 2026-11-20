import "./react.ts";
import assert from "node:assert/strict";
import { test } from "node:test";
import { follow, type RunEvent } from "@humagen/adapter";
import { timeline } from "@humagen/core";
import { Presenter } from "./present.tsx";

/**
 * Generated events in the order a run that calls another agent sends them:
 * a few words, the rest of the sentence, the call to the agent, its end, and
 * the answer. The Slack app folds events as they come and writes to Slack at
 * most every flush, so the first words go out alone and the rest of the
 * sentence goes out together with the call.
 */
let seq = 0;
const ev = (type: string, data: unknown): RunEvent =>
  ({
    id: `e${++seq}`,
    source: "humagen:generated",
    type,
    channel: "humagen://generated",
    time: "2026-11-19T00:00:00Z",
    run: "run-1",
    data,
  }) as unknown as RunEvent;

const said = " bring in @researcher to look up the very latest Go release details on the web.";
const answer =
  "Here are three notable changes in the latest stable Go release, **Go 1.23**:\n\n- Iterators: range over functions.\n- Timers are collected once unreferenced.\n- `go vet` checks more `slices` misuse.";

/** The batches the app flushes: each one is folded, then written. */
const batches: RunEvent[][] = [
  [ev("run.delta.text.v1", { text: "I'll" })],
  [
    ev("run.delta.text.v1", { text: said }),
    ev("run.delta.tool.call.v1", {
      id: "call-1",
      name: "agent",
      phase: "requested",
      arguments: { agent: "researcher", task: "Find the latest Go release" },
    }),
    ev("run.action.taken.v1", {
      name: "agent",
      data: { run: "run-2", agent: "researcher", tool_call_id: "call-1" },
    }),
  ],
  [
    ev("run.delta.tool.done.v1", { id: "call-1", name: "agent", result: "Go 1.23 notes" }),
    ev("run.delta.text.v1", { text: answer }),
  ],
];

/** Replays the batches into the real presenter against a Slack that only records. */
async function replay() {
  const calls: { method: string; args: Record<string, unknown> }[] = [];
  const slack = {
    async call<T>(method: string, args: Record<string, unknown> = {}) {
      calls.push({ method, args });
      return { ts: "1.0001" } as T;
    },
    async respond() {},
  };
  const presenter = new Presenter(slack, {
    channel: "C1",
    threadTs: "1.0",
    triggerTs: "1.0",
    recipientUser: "U1",
    recipientTeam: "T1",
  });
  const f = follow({ id: "run-1", agent: "humagen" });
  for (const batch of batches) {
    for (const e of batch) f.apply(e);
    await presenter.update(f.turn);
  }
  const turn = f.push({ type: "turn.completed" });
  await presenter.finish(turn);
  // What Slack was asked to show, in order: words and task cards.
  const shown: string[] = [];
  for (const c of calls) {
    if (c.method !== "chat.appendStream") continue;
    for (const chunk of c.args.chunks as { type: string; text?: string; title?: string }[]) {
      if (chunk.type === "markdown_text") shown.push(`text:${chunk.text}`);
      if (chunk.type === "task_update") shown.push(`task:${chunk.title}`);
    }
  }
  return { shown, turn };
}

test("an agent call's card sits where the call came in the words, and the answer starts a paragraph", async () => {
  const { shown } = await replay();
  const card = shown.findIndex((s) => s.startsWith("task:"));
  const words = shown.filter((s) => s.startsWith("text:")).map((s) => s.slice(5));
  // Everything said before the call is above its card.
  assert.equal(
    shown
      .slice(0, card)
      .map((s) => s.slice(5))
      .join(""),
    `I'll${said}`,
  );
  // What was said after it begins a paragraph of its own.
  assert.ok(words.join("").includes(`on the web.\n\nHere are three`), words.join(""));
  assert.ok(!words.join("").includes("web.Here"));
});

test("a call to an agent is labelled with its name once", async () => {
  const { shown } = await replay();
  assert.deepEqual(
    shown.filter((s) => s.startsWith("task:")).map((s) => s.slice(5)),
    ["researcher", "researcher"],
  );
});

test("the console reads the same events as words, the call, then the answer", async () => {
  const { turn } = await replay();
  const t = timeline(turn);
  const kinds = [...t.segments.map((s) => s.kind), t.tail ? "tail" : ""].filter(Boolean);
  assert.deepEqual(kinds, ["text", "call", "tail"]);
  assert.equal((t.segments[0] as { text: string }).text, `I'll${said}`);
  assert.ok(t.tail.startsWith("Here are three"));
});
