import assert from "node:assert/strict";
import { test } from "node:test";
import { reflects } from "./runsRead.ts";

const held = [{ id: "r1", updated_at: "2026-10-01T09:00:05.5Z" }];

test("an event no later than the run the list holds is old news", () => {
  assert.equal(reflects(held, { run: "r1", time: "2026-10-01T09:00:05.5Z" }), true);
  assert.equal(reflects(held, { run: "r1", time: "2026-10-01T09:00:01Z" }), true);
  // The list's time is written in another zone than the event's.
  assert.equal(
    reflects([{ id: "r1", updated_at: "2026-10-01T05:00:05.5-04:00" }], {
      run: "r1",
      time: "2026-10-01T09:00:05.5Z",
    }),
    true,
  );
});

test("an event later than the run held, for a run not held, or with nothing held, is news", () => {
  assert.equal(reflects(held, { run: "r1", time: "2026-10-01T09:00:06Z" }), false);
  assert.equal(reflects(held, { run: "r2", time: "2026-10-01T09:00:01Z" }), false);
  assert.equal(reflects(null, { run: "r1", time: "2026-10-01T09:00:01Z" }), false);
  assert.equal(reflects(held, { time: "2026-10-01T09:00:01Z" }), false);
  assert.equal(reflects(held, { run: "r1", time: "not a time" }), false);
});
