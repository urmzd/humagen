import assert from "node:assert/strict";
import { test } from "node:test";
import { DESKTOP, judge, PHONE, type Snapshot } from "./checks.ts";

const box = (left: number, top: number, w: number, h: number) => ({
  left,
  top,
  right: left + w,
  bottom: top + h,
});

const good: Snapshot = {
  width: 390,
  height: 844,
  scrollWidth: 390,
  titles: ["Inbox"],
  texts: [
    { text: "Inbox", boxes: [box(20, 40, 80, 30)], main: true },
    { text: "Needs you", boxes: [box(20, 80, 90, 18)], main: true },
  ],
  controls: [
    {
      label: "Open Inbox",
      box: box(260, 76, 110, 32),
      size: { width: 110, height: 32 },
      inline: false,
    },
    { label: "the docs", box: box(40, 120, 50, 16), size: { width: 50, height: 16 }, inline: true },
  ],
};

const checks = (s: Snapshot, rules = PHONE) => judge(s, rules).map((p) => p.check);

test("a page that fits passes every check", () => {
  assert.deepEqual(judge(good, PHONE), []);
  assert.deepEqual(judge(good, DESKTOP), []);
});

test("a page wider than the window fails, and sub-pixel rounding does not", () => {
  assert.deepEqual(checks({ ...good, scrollWidth: 402 }), ["overflow"]);
  assert.deepEqual(checks({ ...good, scrollWidth: 390.4 }), []);
});

test("a page needs exactly one h1", () => {
  assert.deepEqual(checks({ ...good, titles: [] }), ["title"]);
  assert.deepEqual(checks({ ...good, titles: ["Home", "Inbox"] }), ["title"]);
});

test("on a phone, text or a control past the 16px gutter fails; on a desktop it is not checked", () => {
  const text = {
    ...good,
    texts: [
      ...good.texts,
      { text: "Posted in Launch", boxes: [box(300, 100, 80, 16)], main: true },
    ],
  };
  assert.deepEqual(checks(text), ["gutter"]);
  const control = {
    ...good,
    controls: [
      ...good.controls,
      { label: "Menu", box: box(8, 8, 36, 36), size: { width: 36, height: 36 }, inline: false },
    ],
  };
  assert.deepEqual(checks(control), ["gutter"]);
  assert.deepEqual(checks(control, DESKTOP), []);
});

test("a control under 24px on either side fails, and a link inside a sentence is exempt", () => {
  const small = {
    ...good,
    controls: [
      { label: "Pin", box: box(40, 200, 20, 20), size: { width: 20, height: 20 }, inline: false },
    ],
  };
  assert.deepEqual(checks(small), ["target"]);
  assert.deepEqual(checks(small, DESKTOP), ["target"]);
  const thin = {
    ...good,
    controls: [
      {
        label: "Needs you · 1",
        box: box(40, 200, 120, 20),
        size: { width: 120, height: 20 },
        inline: false,
      },
    ],
  };
  assert.deepEqual(checks(thin), ["target"]);
  const inline = {
    ...good,
    controls: [
      { label: "here", box: box(40, 200, 20, 16), size: { width: 20, height: 16 }, inline: true },
    ],
  };
  assert.deepEqual(checks(inline), []);
});

test("a control's whole size is what counts, so a tab half scrolled out of its strip is not too small", () => {
  const tab = {
    ...good,
    controls: [
      { label: "Inbox", box: box(370, 60, 8, 36), size: { width: 64, height: 36 }, inline: false },
    ],
  };
  assert.deepEqual(checks(tab, DESKTOP), []);
});

test("text covering other text in the main column fails; lines that only touch, or text outside it, do not", () => {
  const covered = {
    ...good,
    texts: [
      { text: "Field offsite", boxes: [box(20, 100, 200, 18)], main: true },
      { text: "Posted in Field", boxes: [box(150, 104, 120, 16)], main: true },
    ],
  };
  assert.deepEqual(checks(covered), ["overlap"]);
  const touching = {
    ...good,
    texts: [
      { text: "First line", boxes: [box(20, 100, 200, 18)], main: true },
      { text: "Second line", boxes: [box(20, 117, 200, 18)], main: true },
    ],
  };
  assert.deepEqual(checks(touching), []);
  const outside = { ...good, texts: covered.texts.map((t) => ({ ...t, main: false })) };
  assert.deepEqual(checks(outside), []);
});

test("a menu or a list that opens over the page covers it by design, but its own lines must not cover each other", () => {
  const over = {
    ...good,
    texts: [
      { text: "Needs you", boxes: [box(20, 100, 200, 18)], main: true },
      { text: "@helper", boxes: [box(20, 102, 120, 18)], main: true, layer: "completion" },
    ],
  };
  assert.deepEqual(checks(over), []);
  const within = {
    ...good,
    texts: [
      { text: "@helper", boxes: [box(20, 100, 200, 18)], main: true, layer: "completion" },
      { text: "@lead", boxes: [box(40, 104, 120, 16)], main: true, layer: "completion" },
    ],
  };
  assert.deepEqual(checks(within), ["overlap"]);
});
