import assert from "node:assert/strict";
import { test } from "node:test";
import { timeline } from "@humagen/core";
import { follow, settled } from "./fold.ts";
import { turnOf, type RunEvent, type Run } from "./run.ts";

const run = (over: Partial<Run> = {}): Run => ({
  id: "r1",
  tenant: "acme",
  agent: "lead",
  rev: 1,
  origin: "api",
  text: "hello?",
  root: "r1",
  depth: 0,
  thread: "r1",
  lease: 0,
  granted: 0,
  calls: 0,
  status: "running",
  created_at: "2026-10-09T00:00:00Z",
  updated_at: "2026-10-09T00:00:00Z",
  ...over,
});

let seq = 0;
const ev = (type: string, data: unknown = {}): RunEvent => {
  const n = ++seq;
  return {
    id: `e${n}`,
    source: "humagen:acme",
    type,
    channel: "humagen://acme",
    time: "2026-10-09T00:00:01Z",
    hops: 0,
    tenant: "acme",
    run: "r1",
    seq: n,
    agent: "lead",
    version: "1.0.0",
    data,
  } as RunEvent;
};

const streamed = (text: string) => {
  const f = follow(run());
  f.apply(ev("run.accepted.v1"));
  f.apply(ev("run.delta.text.v1", { text }));
  return f;
};

test("settling from the record keeps the whole answer, not only what streamed", () => {
  const f = streamed("Hello");
  const turn = f.settle(run({ status: "completed", answer: "Hello world, the full answer." }));
  assert.equal(turn.status, "done");
  assert.equal(turn.text, "Hello world, the full answer.");
});

test("when the stream and the record disagree, the record wins", () => {
  const f = streamed("Helo, garbled");
  const turn = f.settle(run({ status: "completed", answer: "Hello world." }));
  assert.equal(turn.text, "Hello world.");
  assert.equal(turn.status, "done");
});

test("a record with no answer leaves the streamed text alone", () => {
  const f = streamed("Partial");
  assert.equal(f.settle(run({ status: "completed" })).text, "Partial");
});

test("a turn that already ended is not moved by its record", () => {
  const f = streamed("Done.");
  f.apply(ev("run.completed.v1", { text: "Done." }));
  assert.deepEqual(settled(f.turn, run({ status: "failed", error: "late" })), []);
  assert.equal(f.settle(run({ status: "completed", answer: "Something else" })).text, "Done.");
});

test("a stopped record the stream did not reach shows its partial answer", () => {
  const f = follow(run());
  const turn = f.settle(run({ status: "stopped", answer: "Half an answer" }));
  assert.equal(turn.status, "stopped");
  assert.equal(turn.text, "Half an answer");
  const part = streamed("Half an");
  assert.equal(part.settle(run({ status: "stopped", answer: "Half an answer" })).text, "Half an answer");
});

test("a stopped record stops the turn, confirmed, and keeps what was said", () => {
  const f = streamed("Part one.");
  const turn = f.settle(run({ status: "stopped" }));
  assert.equal(turn.status, "stopped");
  assert.equal(turn.stop, "confirmed");
  assert.equal(turn.text, "Part one.");
});

test("the conversation link survives streaming and settling for a run in an existing thread", () => {
  const record = run({ id: "second", thread: "thread/one" });
  const f = follow(record, { href: (path) => `https://humagen.test${path}` });
  assert.equal(f.turn.sessionUrl, "https://humagen.test/conversations/thread%2Fone");
  f.apply(ev("run.accepted.v1"));
  assert.equal(f.turn.sessionUrl, "https://humagen.test/conversations/thread%2Fone");
  assert.equal(f.settle({ ...record, status: "completed", answer: "Done" }).sessionUrl, f.turn.sessionUrl);
});

test("record-loaded outcomes and evidence survive without journal events", () => {
  const record = run({status:"completed",answer:"Renew it. [[1]]",outcome:{kind:"recommendation",summary:"Renew the agreement.",decisions:["Keep current terms"],next_action:"Confirm the owner"},citations:[{id:"[1]",title:"Agreement",url:"https://example.test/agreement",snippet:"The source passage"}]});
  const turn=turnOf(record);
  assert.equal(turn.outcome?.nextAction,"Confirm the owner");
  assert.equal(turn.citations[0]?.title,"Agreement");
  assert.equal(turn.citations[0]?.snippet,"The source passage");
});


test("persisted citations recover without events and an explicit projected empty list revokes streamed references", () => {
  const record = {
    ...run(),
    status: "completed" as const,
    answer: "Recorded",
    citations: [
      {
        id: "c1",
        title: "Recorded source",
        url: "https://example.com/source",
        mode: "indexed" as const,
        copy_at: "2026-10-20T12:00:00Z",
        refresh_failed: true,
      },
    ],
  };
  const f = follow(record);
  f.settle(record);
  assert.equal(f.turn.citations[0]?.mode, "indexed");
  assert.equal(f.turn.citations[0]?.copyAt, "2026-10-20T12:00:00Z");
  assert.equal(f.turn.citations[0]?.refreshFailed, true);
  f.settle({ ...record, citations: [] });
  assert.deepEqual(f.turn.citations, []);
});

test("a run settled from its record worked from its creation to its last change", () => {
  const turn = turnOf(run({ status: "completed", answer: "Done.", updated_at: "2026-10-09T00:01:05Z" }));
  assert.equal(turn.durationMs, 65_000);
  assert.equal(turnOf(run({ status: "running", updated_at: "2026-10-09T00:01:05Z" })).durationMs, undefined);
  const f = streamed("Done.");
  assert.equal(f.settle(run({ status: "completed", answer: "Done.", updated_at: "2026-10-09T00:00:12Z" })).durationMs, 12_000);
});

// A run as the journal records it across three model steps: words, two
// reads (one refused), words, a write, the answer. The completed event
// repeats every word the run said.
const steps = (): RunEvent[] => [
  ev("run.accepted.v1"),
  ev("run.started.v1", { attempt: 1 }),
  ev("run.delta.text.v1", { text: "Reading the change first." }),
  ev("run.delta.tool.call.v1", { id: "c1", name: "file_read", phase: "requested" }),
  ev("run.delta.tool.call.v1", { id: "c1", name: "file_read", phase: "executing", arguments: { path: "a.go" } }),
  ev("run.delta.tool.done.v1", { id: "c1", name: "file_read", result: "package a", arguments: { path: "a.go" } }),
  ev("run.delta.tool.call.v1", { id: "c2", name: "github_workflow_status", phase: "executing", arguments: { ref: "f4a7" } }),
  ev("run.delta.tool.done.v1", { id: "c2", name: "github_workflow_status", result: "action failed: operations: action denied" }),
  ev("run.usage.v1", { requests: 1 }),
  ev("run.delta.text.v1", { text: " Writing notes." }),
  ev("run.delta.tool.call.v1", { id: "c3", name: "file_write", phase: "executing", arguments: { path: "notes.md", content: "a\nb\n" } }),
  ev("run.delta.tool.done.v1", { id: "c3", name: "file_write", result: "Wrote notes.md (4 bytes)." }),
  ev("run.delta.text.v1", { text: " Done." }),
  ev("run.completed.v1", { attempt: 1, text: "Reading the change first. Writing notes. Done." }),
];

test("a replayed run keeps its words and tool calls in the order they came", () => {
  const f = follow(run());
  for (const e of steps()) f.apply(e);
  const turn = f.settle(run({ status: "completed", answer: "Reading the change first. Writing notes. Done." }));
  const t = timeline(turn);
  assert.deepEqual(
    t.segments.map((s) => (s.kind === "tools" ? s.tasks.map((x) => x.id).join("+") : s.kind === "text" ? s.text : s.kind)),
    ["Reading the change first.", "c1+c2", " Writing notes.", "c3"],
  );
  assert.equal(t.tail, " Done.");
});

test("an action handed back to the model as failed text is a failed call", () => {
  const f = follow(run());
  for (const e of steps()) f.apply(e);
  assert.equal(f.turn.tasks.find((t) => t.id === "c2")?.status, "error");
  assert.equal(f.turn.tasks.find((t) => t.id === "c1")?.status, "complete");
});
