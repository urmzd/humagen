import assert from "node:assert/strict";
import { test } from "node:test";
import { placePeek } from "./peek.ts";

test("a preview opens beside its row, centred on it", () => {
  const row = { top: 300, bottom: 340, left: 8, right: 248 };
  const got = placePeek(row, { width: 1280, height: 800 }, { width: 288, height: 80 });
  assert.equal(got.left, 256);
  assert.equal(got.width, 288);
  assert.equal(got.top + 40, 320, "the card's middle is the row's middle");
});

test("a preview beside a row near an edge is kept inside the window", () => {
  const view = { width: 1280, height: 800 };
  const card = { width: 288, height: 80 };
  const low = placePeek({ top: 760, bottom: 790, left: 8, right: 248 }, view, card);
  assert.equal(low.top, 800 - 80 - 16, "pulled up above the bottom margin");
  const high = placePeek({ top: 0, bottom: 20, left: 8, right: 248 }, view, card);
  assert.equal(high.top, 16, "pushed down below the top margin");
  const tall = placePeek({ top: 0, bottom: 20, left: 8, right: 248 }, view, {
    ...card,
    height: 900,
  });
  assert.equal(tall.top, 16, "a card taller than the window starts at its top");
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
