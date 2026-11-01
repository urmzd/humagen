import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { slotAt, Thread, type TurnRailEntry } from "@humagen/web";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

afterEach(cleanup);

const turns: TurnRailEntry[] = [
  { id: "a", agent: "lead", preview: "Summarize the launch plan", status: "done", weight: 400 },
  { id: "b", agent: "helper", preview: "When does packaging arrive?", status: "done", weight: 90 },
  { id: "c", agent: "lead", preview: "Draft the checklist", status: "working", weight: 40 },
];

function conversation() {
  return render(
    <Thread turns={turns}>
      {turns.map((t) => (
        <article key={t.id} data-turn-id={t.id}>
          {t.preview}
        </article>
      ))}
    </Thread>,
  );
}

test("slotAt: a pointer down the rail lands on the turn whose slot it is over, never past either end", () => {
  assert.equal(slotAt(100, 100, 90, 3), 0);
  assert.equal(slotAt(145, 100, 90, 3), 1);
  assert.equal(slotAt(189, 100, 90, 3), 2);
  assert.equal(slotAt(40, 100, 90, 3), 0, "above the rail is the first turn");
  assert.equal(slotAt(400, 100, 90, 3), 2, "below it, the last");
});

test("the wave is one slider over the turns, at the latest, previewing its prompt", () => {
  conversation();
  const wave = screen.getByRole("slider", { name: "Turns in this conversation" });
  assert.equal(wave.getAttribute("aria-valuemax"), "3");
  assert.equal(wave.getAttribute("aria-valuenow"), "3");
  assert.match(wave.getAttribute("aria-valuetext") ?? "", /3 of 3, lead: Draft the checklist/);
});

test("the keyboard steps through the wave and shows the prompt of the turn it reaches", () => {
  conversation();
  const wave = screen.getByRole("slider", { name: "Turns in this conversation" });
  fireEvent.keyDown(wave, { key: "Home" });
  assert.equal(wave.getAttribute("aria-valuenow"), "1");
  assert.match(screen.getByRole("tooltip").textContent ?? "", /Summarize the launch plan/);
  fireEvent.keyDown(wave, { key: "ArrowDown" });
  assert.equal(wave.getAttribute("aria-valuenow"), "2");
  assert.match(screen.getByRole("tooltip").textContent ?? "", /packaging/);
});

test("a conversation of one turn has no wave", () => {
  render(
    <Thread turns={turns.slice(0, 1)}>
      <article data-turn-id="a" />
    </Thread>,
  );
  assert.equal(screen.queryByRole("slider"), null);
});
