import assert from "node:assert/strict";
import { test } from "node:test";
import { FADE_PX, GUTTER_PX, moreOf, nudgeInto, revealAt } from "./layout.ts";

// The project tabs on a phone: nine tabs in a 350px strip, 700px of them.
const strip = { scroll: 0, width: 350, content: 700 };

test("revealAt: a chosen tab past the strip's right end is scrolled into view, with room past it", () => {
  // People sits at 458-522: off the right end, where the review found no tab underlined.
  const at = revealAt({ start: 458, end: 522 }, strip);
  assert.equal(at, 522 + FADE_PX - 350);
  assert.ok(458 >= at && 522 <= at + 350, "it shows whole");
});

test("revealAt: one cut off at the left is brought back, and one in view stays put", () => {
  assert.equal(revealAt({ start: 210, end: 280 }, { ...strip, scroll: 230 }), 210 - FADE_PX);
  assert.equal(revealAt({ start: 100, end: 170 }, { ...strip, scroll: 20 }), 20);
});

test("revealAt: it never scrolls past either end of the strip", () => {
  assert.equal(revealAt({ start: 640, end: 700 }, strip), 350, "the last tab ends the strip");
  assert.equal(revealAt({ start: 0, end: 60 }, { ...strip, scroll: 200 }), 0, "the first tab starts it");
  assert.equal(revealAt({ start: 20, end: 90 }, { scroll: 0, width: 400, content: 300 }), 0, "a strip that fits does not scroll");
});

test("revealAt: an item nearly as wide as the strip is shown whole before it is padded", () => {
  assert.equal(revealAt({ start: 300, end: 640 }, strip), 295);
});

test("moreOf: says which ends have more, so the strip can fade them", () => {
  assert.equal(moreOf(strip), "end");
  assert.equal(moreOf({ ...strip, scroll: 120 }), "both");
  assert.equal(moreOf({ ...strip, scroll: 350 }), "start");
  assert.equal(moreOf({ scroll: 0, width: 350, content: 350 }), "none");
  assert.equal(moreOf({ scroll: 0.5, width: 350, content: 350.5 }), "none", "a fraction of a pixel is not more");
});

test("nudgeInto: the composer's agent menu, 6px from the left edge of a phone, moves inside the gutter", () => {
  // A 320px menu anchored to the right of a trigger that ends at 326px.
  assert.equal(nudgeInto({ left: 6, width: 320 }, 390), GUTTER_PX - 6);
  assert.equal(nudgeInto({ left: 60, width: 320 }, 390), -(60 + 320 - (390 - GUTTER_PX)));
  assert.equal(nudgeInto({ left: 30, width: 320 }, 390), 0, "one already inside stays");
  assert.equal(nudgeInto({ left: 40, width: 380 }, 390), GUTTER_PX - 40, "one wider than the room starts at the left gutter");
});
