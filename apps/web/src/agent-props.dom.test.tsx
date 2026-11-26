import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import {
  AgentInitials,
  eyesOpen,
  GLANCE,
  type GlanceTimers,
  glanceOffset,
  type OrbState,
  PROP_FADE_MS,
  SETTLE_MS,
  useIdleGlance,
  useSettle,
} from "@humagen/web";
import { act, cleanup, render, waitFor } from "@testing-library/react";
import { useRef } from "react";

afterEach(cleanup);

const propOf = (el: ParentNode) => el.querySelector(".aui-prop");

test("a mark holds the prop that says what its agent is doing", () => {
  const cases: [OrbState, string][] = [
    ["searching", "microscope"],
    ["thinking", "thought"],
    ["calling", "wrench"],
    ["composing", "pencil"],
    ["delegating", "bubbles"],
    ["waiting", "hourglass"],
  ];
  for (const [state, prop] of cases) {
    const { container, unmount } = render(<AgentInitials name="lead" state={state} size={28} />);
    assert.equal(propOf(container)?.getAttribute("data-prop"), prop, state);
    assert.ok(!container.querySelector(".aui-orb"), `${state}: a mascot never carries the orb`);
    unmount();
  }
});

test("an idle or settled mark holds nothing, and a mascot never carries the orb", () => {
  for (const state of ["idle", "done", "error", "stopped"] as const) {
    const { container, unmount } = render(<AgentInitials name="lead" state={state} size={28} />);
    assert.ok(!propOf(container), state);
    unmount();
  }
  for (const size of [16, 20, 24, 32]) {
    const { container, unmount } = render(
      <AgentInitials name="lead" state="calling" size={size} />,
    );
    assert.ok(!container.querySelector(".aui-orb, .aui-orb-drawn"), `${size}px`);
    unmount();
  }
});

test("a mark under 24px keeps just the bot", () => {
  for (const state of ["searching", "thinking", "calling", "composing", "waiting"] as const) {
    const { container, unmount } = render(<AgentInitials name="lead" state={state} size={20} />);
    assert.ok(!propOf(container), state);
    unmount();
  }
  const { container } = render(<AgentInitials name="lead" state="searching" size={24} />);
  assert.equal(propOf(container)?.getAttribute("data-prop"), "microscope");
});

test("the hourglass holds still, and the other props loop", () => {
  const waiting = render(<AgentInitials name="lead" state="waiting" size={28} />);
  assert.equal(propOf(waiting.container)?.getAttribute("data-motion"), "still");
  waiting.unmount();
  const searching = render(<AgentInitials name="lead" state="searching" size={28} />);
  assert.equal(propOf(searching.container)?.getAttribute("data-motion"), "loop");
});

test("a prop fades out when the work stops, then goes", async () => {
  const { container, rerender } = render(<AgentInitials name="lead" state="searching" size={28} />);
  assert.ok(!propOf(container)?.hasAttribute("data-leaving"));
  rerender(<AgentInitials name="lead" state="idle" size={28} />);
  assert.ok(propOf(container)?.hasAttribute("data-leaving"), "held for its fade");
  await waitFor(() => assert.ok(!propOf(container)), { timeout: PROP_FADE_MS * 10 });
});

/** Timers a test runs by hand: each pending one is fired in turn. */
function handTimers(random = () => 0.5) {
  let next = 1;
  const pending = new Map<number, { fn: () => void; ms: number }>();
  const timers: GlanceTimers = {
    set(fn, ms) {
      const id = next++;
      pending.set(id, { fn, ms });
      return id;
    },
    clear(id) {
      pending.delete(id as number);
    },
    random,
  };
  const fire = () => {
    const first = [...pending][0];
    assert.ok(first, "a timer is pending");
    const [id, t] = first;
    pending.delete(id);
    act(() => t.fn());
    return t.ms;
  };
  return { timers, pending, fire };
}

function Probe({ on, name, timers }: { on: boolean; name: string; timers: GlanceTimers }) {
  const ref = useRef<HTMLSpanElement>(null);
  const glancing = useIdleGlance(ref, on, name, timers);
  return <span ref={ref} data-testid="probe" data-glancing={glancing ? "yes" : "no"} />;
}

test("an idle mark moves now and then for a moment, then holds still again", () => {
  const { timers, pending, fire } = handTimers();
  const { getByTestId, unmount } = render(<Probe on name="lead" timers={timers} />);
  const glancing = () => getByTestId("probe").getAttribute("data-glancing");
  assert.equal(glancing(), "no");
  assert.equal(pending.size, 1);

  const firstWait = fire();
  assert.equal(firstWait, GLANCE.minGap + glanceOffset("lead") * (GLANCE.maxGap - GLANCE.minGap));
  assert.equal(glancing(), "yes");

  const moveFor = fire();
  assert.ok(moveFor >= GLANCE.minFor && moveFor <= GLANCE.maxFor, `moved for ${moveFor}ms`);
  assert.equal(glancing(), "no");

  const nextWait = fire();
  assert.ok(nextWait >= GLANCE.minGap && nextWait <= GLANCE.maxGap, `waited ${nextWait}ms`);
  assert.equal(glancing(), "yes");

  unmount();
  assert.equal(pending.size, 0, "every timer is cleared when the mark goes");
});

test("agents in a list first move at different times", () => {
  const names = ["lead", "helper", "researcher", "writer"];
  const offsets = new Set(names.map((n) => glanceOffset(n).toFixed(3)));
  assert.equal(offsets.size, names.length);
  for (const n of names) assert.ok(glanceOffset(n) >= 0 && glanceOffset(n) < 1);
});

test("nothing is scheduled for a mark that should not move, or under reduced motion", () => {
  const off = handTimers();
  render(<Probe on={false} name="lead" timers={off.timers} />);
  assert.equal(off.pending.size, 0, "a mark that works, waits or is hovered schedules nothing");
  cleanup();

  const match = window.matchMedia;
  window.matchMedia = ((query: string) => ({
    matches: query.includes("reduce"),
    media: query,
    addEventListener() {},
    removeEventListener() {},
  })) as unknown as typeof window.matchMedia;
  try {
    const reduced = handTimers();
    const { getByTestId } = render(<Probe on name="lead" timers={reduced.timers} />);
    assert.equal(reduced.pending.size, 0, "reduced motion schedules nothing");
    assert.equal(getByTestId("probe").getAttribute("data-glancing"), "no");
  } finally {
    window.matchMedia = match;
  }
});

function Settle({ moving, timers }: { moving: boolean; timers: GlanceTimers }) {
  const stirring = useSettle(moving, timers);
  return <span data-testid="settle" data-stirring={stirring ? "yes" : "no"} />;
}

test("a mark the pointer leaves finishes its move, then holds still", () => {
  const { timers, pending, fire } = handTimers();
  const { getByTestId, rerender, unmount } = render(<Settle moving={false} timers={timers} />);
  const stirring = () => getByTestId("settle").getAttribute("data-stirring");
  assert.equal(stirring(), "no", "a still mark stays still");
  assert.equal(pending.size, 0);

  rerender(<Settle moving timers={timers} />);
  assert.equal(stirring(), "yes");
  rerender(<Settle moving={false} timers={timers} />);
  assert.equal(stirring(), "yes", "it keeps moving after the pointer leaves");
  assert.equal(fire(), SETTLE_MS);
  assert.equal(stirring(), "no", "then it holds still");
  assert.equal(pending.size, 0);

  rerender(<Settle moving timers={timers} />);
  rerender(<Settle moving={false} timers={timers} />);
  rerender(<Settle moving timers={timers} />);
  assert.equal(stirring(), "yes", "a pointer that comes back keeps it moving");
  assert.equal(pending.size, 0, "and the pending stop is cleared");

  rerender(<Settle moving={false} timers={timers} />);
  assert.equal(pending.size, 1);
  unmount();
  assert.equal(pending.size, 0, "the timer is cleared when the mark goes");
});

test("under reduced motion a mark stops when the pointer leaves", () => {
  const match = window.matchMedia;
  window.matchMedia = ((query: string) => ({
    matches: query.includes("reduce"),
    media: query,
    addEventListener() {},
    removeEventListener() {},
  })) as unknown as typeof window.matchMedia;
  try {
    const { timers, pending } = handTimers();
    const { getByTestId, rerender } = render(<Settle moving timers={timers} />);
    rerender(<Settle moving={false} timers={timers} />);
    assert.equal(getByTestId("settle").getAttribute("data-stirring"), "no");
    assert.equal(pending.size, 0);
  } finally {
    window.matchMedia = match;
  }
});

test("a mark stops on open eyes, never mid-blink", () => {
  const open = [80, 82, 81, 83, 82, 81, 82];
  assert.ok(eyesOpen(open), "eyes as wide as they have been");
  assert.ok(!eyesOpen([...open, 60, 30]), "closing in a blink");
  assert.ok(!eyesOpen([...open, 60, 30, 8, 30]), "opening, still part closed");
  assert.ok(eyesOpen([...open, 60, 30, 8, 30, 70, 79]), "open again");
  assert.ok(!eyesOpen([80, 82]), "too few frames to tell");
  assert.ok(!eyesOpen([0, 0, 0, 0, 0, 0, 0]), "no face to read");
});
