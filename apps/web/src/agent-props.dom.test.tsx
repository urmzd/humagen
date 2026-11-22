import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import {
  AgentInitials,
  GLANCE,
  type GlanceTimers,
  glanceOffset,
  type OrbState,
  PROP_FADE_MS,
  useIdleGlance,
} from "@humagen/web";
import { act, cleanup, render, waitFor } from "@testing-library/react";
import { useRef } from "react";

afterEach(cleanup);

const propOf = (el: ParentNode) => el.querySelector(".aui-prop");

test("a mark holds the prop that says what its agent is doing", () => {
  const cases: [OrbState, string][] = [
    ["searching", "magnifier"],
    ["thinking", "thought"],
    ["composing", "pencil"],
    ["delegating", "bubbles"],
    ["waiting", "hourglass"],
  ];
  for (const [state, prop] of cases) {
    const { container, unmount } = render(<AgentInitials name="lead" state={state} size={28} />);
    assert.equal(propOf(container)?.getAttribute("data-prop"), prop, state);
    assert.ok(!container.querySelector(".aui-orb"), `${state}: the prop, never the orb as well`);
    unmount();
  }
});

test("a tool call keeps the orb, and an idle or settled mark holds nothing", () => {
  const calling = render(<AgentInitials name="lead" state="calling" size={28} />);
  assert.ok(!propOf(calling.container));
  assert.ok(calling.container.querySelector(".aui-orb"), "the orb says it runs a tool");
  calling.unmount();
  for (const state of ["idle", "done", "error", "stopped"] as const) {
    const { container, unmount } = render(<AgentInitials name="lead" state={state} size={28} />);
    assert.ok(!propOf(container), state);
    unmount();
  }
});

test("a mark under 24px keeps just the bot", () => {
  for (const state of ["searching", "thinking", "composing", "waiting"] as const) {
    const { container, unmount } = render(<AgentInitials name="lead" state={state} size={20} />);
    assert.ok(!propOf(container), state);
    unmount();
  }
  const { container } = render(<AgentInitials name="lead" state="searching" size={24} />);
  assert.equal(propOf(container)?.getAttribute("data-prop"), "magnifier");
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
