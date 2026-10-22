import assert from "node:assert/strict";
import { test } from "node:test";
import type { ThreadPage, ThreadTurn } from "./api.ts";
import { MAX_PAGES, mergeTurns, readFromStart } from "./turns.ts";

const turn = (n: number, status = "completed"): ThreadTurn => ({ kind: "run", run: { id: `r${n}`, status } as never });
const ids = (ts: ThreadTurn[]) => ts.map((t) => t.run?.id);

test("turns read at different times merge oldest first, each once, the later read winning", () => {
  const merged = mergeTurns([turn(1), turn(2), turn(3, "running")], [turn(3), turn(4)]);
  assert.deepEqual(ids(merged), ["r1", "r2", "r3", "r4"]);
  assert.equal(merged[2]?.run?.status, "completed");
});

test("a conversation is read from its start, and reading stops after MAX_PAGES", async () => {
  const pages = (count: number) => (before?: string): Promise<ThreadPage> => {
    const n = before ? Number(before) : count - 1;
    return Promise.resolve({ turns: [turn(n)], next: n > 0 ? String(n - 1) : null } as ThreadPage);
  };
  const whole = await readFromStart(pages(3));
  assert.deepEqual(ids(whole.turns), ["r0", "r1", "r2"]);
  assert.equal(whole.next, null);
  let reads = 0;
  const long = await readFromStart((before) => {
    reads++;
    return pages(MAX_PAGES + 5)(before);
  });
  assert.equal(reads, MAX_PAGES);
  assert.equal(long.turns.length, MAX_PAGES);
  assert.equal(long.next, "4", "where reading stopped");
});

test("a thread's announcement is one turn whichever read it came in, and is never taken for a run", () => {
  const seed: ThreadTurn = { kind: "announcement", announcement: { id: "m-7", kind: "person", by: "dana", subject: "News", at: "2026-10-01T09:00:00Z" } };
  const withdrawn: ThreadTurn = { kind: "announcement", announcement: { id: "m-7", kind: "person", by: "dana", withdrawn: true, at: "2026-10-01T09:00:00Z" } };
  const runless: ThreadTurn = { kind: "run", run: { id: "" } as never };
  const merged = mergeTurns([seed, turn(1)], [withdrawn, turn(1), runless]);
  assert.equal(merged.length, 3);
  assert.equal(merged[0]?.announcement?.withdrawn, true, "the later read wins");
});
