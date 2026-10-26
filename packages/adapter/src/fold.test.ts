import assert from "node:assert/strict";
import { test } from "node:test";
import { follow, settled } from "./fold.ts";
import type { BusEvent, Run } from "./run.ts";

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
const ev = (kind: string, payload?: unknown): BusEvent => ({
  seq: ++seq,
  topic: "agent:lead:r1",
  run_id: "r1",
  tenant: "acme",
  agent: "lead",
  rev: 1,
  origin: "api",
  kind,
  payload: payload === undefined ? undefined : Buffer.from(JSON.stringify(payload)).toString("base64"),
  at: "2026-10-09T00:00:01Z",
});

const streamed = (text: string) => {
  const f = follow(run());
  f.apply(ev("lifecycle.accepted"));
  f.apply(ev("delta.text", { text }));
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
  f.apply(ev("lifecycle.completed", { text: "Done." }));
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
  f.apply(ev("lifecycle.accepted"));
  assert.equal(f.turn.sessionUrl, "https://humagen.test/conversations/thread%2Fone");
  assert.equal(f.settle({ ...record, status: "completed", answer: "Done" }).sessionUrl, f.turn.sessionUrl);
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
