import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, render } from "@testing-library/react";
import { CHART_TEXT_PX, chartLayout, RunsChart } from "./Analytics.tsx";
import type { Bucket } from "./api.ts";

// The chart's labels are text like any other: 12 CSS pixels or more, at every
// width. A drawing scaled to its container shrinks them with it (a 920-unit
// chart in a 358px card showed 12-unit labels at 4.67px), so the chart is laid
// out at the width it is given and these tests hold it to that.

const FLOOR_PX = 12;
/** The widths a chart is drawn at: inside a card on a 390px phone, a small tablet, a desktop. */
const WIDTHS = [284, 326, 358, 600, 920, 1100];

function buckets(count: number, stepMs: number, scale: number): Bucket[] {
  const end = Date.UTC(2026, 10, 18, 12);
  return Array.from({ length: count }, (_, i) => ({
    start: new Date(end - (count - i) * stepMs).toISOString(),
    completed: Math.round(scale * (0.4 + ((i * 7) % 10) / 10)),
    failed: i % 5 === 0 ? Math.round(scale / 8) : 0,
    stopped: i % 9 === 0 ? Math.round(scale / 16) : 0,
    active: i > count - 3 ? Math.round(scale / 10) : 0,
  })) as Bucket[];
}
const fixtures = [
  { name: "an hour of minutes", data: buckets(60, 60_000, 4), wide: false },
  { name: "a day of quarter hours", data: buckets(96, 900_000, 40), wide: false },
  {
    name: "a week of two-hour steps, four-digit counts",
    data: buckets(84, 7_200_000, 900),
    wide: true,
  },
  { name: "one bucket", data: buckets(1, 60_000, 2), wide: false },
];

const patched: (() => void)[] = [];
afterEach(() => {
  cleanup();
  for (const undo of patched.splice(0)) undo();
});

/** Gives every element the width a real layout would, which happy-dom does not compute. */
function containerWidth(width: number): void {
  const proto = window.HTMLElement.prototype;
  const original = proto.getBoundingClientRect;
  proto.getBoundingClientRect = () => ({ width, height: 0 }) as DOMRect;
  patched.push(() => {
    proto.getBoundingClientRect = original;
  });
}

/** The size a label is on screen: its font size times the drawing's scale. */
function renderedSizes(svg: Element): number[] {
  const drawn = Number(svg.getAttribute("width"));
  const units = Number((svg.getAttribute("viewBox") ?? "").split(" ")[2]);
  return [...svg.querySelectorAll("text")].map(
    (t) => Number(t.getAttribute("font-size")) * (drawn / units),
  );
}

test("chart text is 12px or more in a 390px phone's card, and at every other width", () => {
  for (const width of WIDTHS)
    for (const f of fixtures) {
      containerWidth(width);
      const { container } = render(<RunsChart buckets={f.data} wide={f.wide} />);
      const svg = container.querySelector("svg[role=img]");
      assert.ok(svg, `${f.name} at ${width}px draws once it is measured`);
      // One unit is one CSS pixel: the drawing is as wide as its container, not scaled to it.
      assert.equal(svg.getAttribute("width"), String(width));
      assert.equal(svg.getAttribute("viewBox")?.split(" ")[2], String(width));
      assert.doesNotMatch(svg.getAttribute("class") ?? "", /\bw-full\b/);
      const sizes = renderedSizes(svg);
      assert.ok(sizes.length >= 3, `${f.name} at ${width}px has its labels`);
      for (const px of sizes)
        assert.ok(px >= FLOOR_PX, `${f.name} at ${width}px: a label renders at ${px}px`);
      cleanup();
      for (const undo of patched.splice(0)) undo();
    }
});

test("an unmeasured chart draws nothing instead of guessing a scale", () => {
  const { container } = render(<RunsChart buckets={fixtures[0]?.data ?? []} wide={false} />);
  assert.equal(container.querySelector("svg[role=img]"), null);
});

test("chart labels never touch or leave the chart, and a phone shows fewer of them", () => {
  assert.ok(CHART_TEXT_PX >= FLOOR_PX);
  for (const f of fixtures) {
    const counts: number[] = [];
    for (const width of WIDTHS) {
      const l = chartLayout(f.data, width, f.wide);
      assert.equal(l.width, width);
      counts.push(l.labels.length);
      assert.ok(l.labels.length >= 1 && l.labels.length <= 6, `${f.name} at ${width}px`);
      l.labels.forEach((label, i) => {
        assert.ok(
          label.x - label.width / 2 >= 0,
          `${f.name} at ${width}px: "${label.text}" is cut on the left`,
        );
        assert.ok(
          label.x + label.width / 2 <= width,
          `${f.name} at ${width}px: "${label.text}" is cut on the right`,
        );
        const next = l.labels[i + 1];
        if (next)
          assert.ok(
            next.x - next.width / 2 - (label.x + label.width / 2) >= 8,
            `${f.name} at ${width}px: "${label.text}" touches "${next.text}"`,
          );
      });
      // The count axis has room for its longest number at the label size.
      const longest = Math.max(...l.ticks.map((t) => String(t).length));
      assert.ok(l.pad.left >= longest * 7 + 6, `${f.name} at ${width}px: count labels fit`);
      assert.ok(l.plotW > 0 && l.plotH > 0);
    }
    assert.ok((counts[0] ?? 0) <= (counts.at(-1) ?? 0), `${f.name}: no more labels on a phone`);
  }
  const day = fixtures[1];
  assert.ok(day);
  assert.ok(
    chartLayout(day.data, 284, false).labels.length <
      chartLayout(day.data, 920, false).labels.length,
  );
});

test("a container narrower than the chart's minimum scrolls instead of shrinking the text", () => {
  containerWidth(180);
  const { container } = render(<RunsChart buckets={fixtures[1]?.data ?? []} wide={false} />);
  const svg = container.querySelector("svg[role=img]");
  assert.ok(svg);
  assert.equal(svg.getAttribute("width"), "240");
  for (const px of renderedSizes(svg)) assert.ok(px >= FLOOR_PX);
  assert.match(svg.parentElement?.getAttribute("class") ?? "", /overflow-x-auto/);
});
