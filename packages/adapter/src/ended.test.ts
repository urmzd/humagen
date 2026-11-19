import assert from "node:assert/strict";
import { test } from "node:test";
import type { Run, RunEvent } from "./run.ts";
import { endedRunTurn } from "./ended.ts";

const at = "2026-11-19T00:00:00Z";
const run = (id: string): Run =>
  ({
    id,
    agent: "researcher",
    thread: "s_1",
    status: "completed",
    text: "What changed?",
    answer: "Searching.Reading.The answer.",
    created_at: at,
    updated_at: at,
  }) as unknown as Run;
const event = (id: string, seq: number, type: string, data: unknown = {}): RunEvent =>
  ({ id: `${id}-${seq}`, run: id, seq, type, time: at, agent: "researcher", data }) as unknown as RunEvent;
const events = (id: string): RunEvent[] => [
  event(id, 1, "run.accepted.v1"),
  event(id, 2, "run.started.v1"),
  event(id, 3, "run.delta.text.v1", { text: "Searching." }),
  event(id, 4, "run.delta.tool.call.v1", { id: "t1", name: "web_search", phase: "executing" }),
  event(id, 5, "run.delta.tool.done.v1", { id: "t1", name: "web_search", result: "{}" }),
  event(id, 6, "run.delta.text.v1", { text: "Reading." }),
  event(id, 7, "run.delta.tool.call.v1", { id: "t2", name: "web_fetch", phase: "executing" }),
  event(id, 8, "run.delta.tool.done.v1", { id: "t2", name: "web_fetch", result: "{}" }),
  event(id, 9, "run.delta.text.v1", { text: "The answer." }),
];

test("an ended session run is rebuilt with its steps where they happened", async () => {
  const turn = await endedRunTurn(run("r_steps"), async (id) => ({ events: events(id), complete: true }));
  assert.ok(turn);
  assert.equal(turn.text, "Searching.Reading.The answer.");
  // Each step keeps its place in the text, which is what keeps what was said
  // before it apart from the answer.
  assert.deepEqual(
    turn.tasks.map((t) => [t.id, t.status, t.at]),
    [
      ["t1", "complete", "Searching.".length],
      ["t2", "complete", "Searching.Reading.".length],
    ],
  );
});

test("a run the journal no longer holds whole keeps its record", async () => {
  const tail = async (id: string) => ({ events: events(id).slice(5), complete: false });
  assert.equal(await endedRunTurn(run("r_tail"), tail), undefined);
  assert.equal(await endedRunTurn(run("r_none"), async () => ({ events: [], complete: false })), undefined);
});
