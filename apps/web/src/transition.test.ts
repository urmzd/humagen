import assert from "node:assert/strict";
import { test } from "node:test";
import { pending, settled, swap, track } from "./transition.ts";

test("a change of screen waits for the reads in flight, and no longer than it may", async () => {
  let finish = () => {};
  const read = track(new Promise<void>((resolve) => (finish = resolve)));
  assert.equal(pending(), 1);
  let done = false;
  const waiting = settled(1000).then(() => (done = true));
  await new Promise((r) => setTimeout(r, 20));
  assert.equal(done, false, "still waiting on the read");
  finish();
  await read;
  await waiting;
  assert.equal(pending(), 0);
  // A read that never lands holds the screen only up to the cap.
  track(new Promise(() => {}));
  const started = Date.now();
  await settled(60);
  assert.ok(Date.now() - started < 500);
});

test("a read that fails is counted out too", async () => {
  const before = pending();
  await track(Promise.reject(new Error("no"))).catch(() => {});
  assert.equal(pending(), before);
});

test("without view transitions a change of screen is applied at once", () => {
  const was = globalThis.document;
  globalThis.document = { visibilityState: "visible" } as Document;
  try {
    let applied = false;
    swap(() => {
      applied = true;
    });
    assert.equal(applied, true);
  } finally {
    globalThis.document = was;
  }
});
