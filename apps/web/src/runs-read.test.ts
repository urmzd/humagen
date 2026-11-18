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

test("two changes inside one millisecond are told apart", () => {
  const running = [{ id: "r1", updated_at: "2026-10-01T09:00:05.123100Z" }];
  // The run ended 800 microseconds after the state the list holds.
  assert.equal(reflects(running, { run: "r1", time: "2026-10-01T09:00:05.123900Z" }), false);
  assert.equal(reflects(running, { run: "r1", time: "2026-10-01T09:00:05.123100Z" }), true);
  assert.equal(reflects(running, { run: "r1", time: "2026-10-01T09:00:05.123099999Z" }), true);
  // Fewer digits are the same instant as trailing zeros, in any zone.
  assert.equal(reflects(running, { run: "r1", time: "2026-10-01T05:00:05.1231-04:00" }), true);
  assert.equal(
    reflects(running, { run: "r1", time: "2026-10-01T05:00:05.123100001-04:00" }),
    false,
  );
  // No fraction at all.
  assert.equal(
    reflects([{ id: "r1", updated_at: "2026-10-01T09:00:05Z" }], {
      run: "r1",
      time: "2026-10-01T09:00:05.000000001Z",
    }),
    false,
  );
});
