import assert from "node:assert/strict";
import { test } from "node:test";
import { placePeek, updatedWhen } from "./peek.ts";

test("a preview opens beside its row when there is room, kept inside the window", () => {
  const row = { top: 700, bottom: 740, left: 8, right: 248 };
  const got = placePeek(row, { width: 1280, height: 800 });
  assert.equal(got.left, 256);
  assert.equal(got.width, 288);
  assert.ok(got.top + 132 + 16 <= 800, "the card is pulled up into the window");
});

test("on a phone a preview opens under its row across the screen, or over it near the bottom", () => {
  const view = { width: 390, height: 844 };
  const under = placePeek({ top: 300, bottom: 344, left: 0, right: 330 }, view);
  assert.equal(under.top, 348);
  assert.equal(under.left, 35);
  assert.equal(under.width, 320);
  const over = placePeek({ top: 760, bottom: 804, left: 0, right: 330 }, view);
  assert.ok(over.top < 760);
});

test("when a conversation moved reads as a time today and a date before", () => {
  const now = new Date("2026-11-03T15:00:00");
  assert.match(updatedWhen("2026-11-03T09:05:00", now), /9:05/);
  assert.match(updatedWhen("2026-10-21T09:05:00", now), /Oct/);
  assert.match(updatedWhen("2025-10-21T09:05:00", now), /2025/);
  assert.equal(updatedWhen("not a date", now), "");
});
