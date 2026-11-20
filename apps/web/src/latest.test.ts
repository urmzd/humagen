import assert from "node:assert/strict";
import { test } from "node:test";
import {
  follows,
  type LatestEvent,
  type LatestState,
  latestLook,
  latestReduce,
  latestStart,
} from "../../../packages/web/src/latestState.ts";

const after = (...events: LatestEvent[]): LatestState => events.reduce(latestReduce, latestStart);
const up: LatestEvent = { type: "moved", atEnd: false };
const down: LatestEvent = { type: "moved", atEnd: true };
const grew: LatestEvent = { type: "grew" };

test("with the end on view there is no control, whether or not an answer is being written", () => {
  assert.equal(latestLook(latestStart, false), null);
  assert.equal(latestLook(latestStart, true), null);
  assert.equal(latestLook(after(grew), true), null, "what grows in view is seen");
});

test("scrolled up under an answer being written, the control shows the work, then rests", () => {
  const away = after(up, grew, grew);
  assert.equal(latestLook(away, true), "working");
  // The answer ends while the reader is still up: no motion, and a mark for what they missed.
  assert.equal(latestLook(away, false), "unseen");
  // Scrolled up with nothing new, it is a plain arrow.
  assert.equal(latestLook(after(up), false), "plain");
  // A second answer starting shows work again.
  assert.equal(latestLook(away, true), "working");
});

test("returning to the end clears what was unseen and the control", () => {
  const back = after(up, grew, down);
  assert.deepEqual(back, latestStart);
  assert.equal(latestLook(back, true), null);
  assert.equal(latestLook(after(up, grew, down, up), false), "plain", "seen once is seen");
});

test("following stops when the reader leaves the end and resumes only at it", () => {
  assert.equal(follows(latestStart), true);
  assert.equal(follows(after(up)), false);
  assert.equal(follows(after(up, grew, grew)), false, "arriving content does not pull them down");
  assert.equal(follows(after(up, grew, down)), true);
});

test("a state that does not change is the same object, so nothing draws again", () => {
  const away = after(up, grew);
  assert.equal(latestReduce(away, grew), away);
  assert.equal(latestReduce(away, up), away);
  assert.equal(latestReduce(latestStart, down), latestStart);
});
