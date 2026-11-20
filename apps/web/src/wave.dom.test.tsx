import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { RAIL_MIN_TURNS, slotAt, Thread, type TurnRailEntry } from "@humagen/web";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

afterEach(cleanup);

const filler = (id: string): TurnRailEntry => ({
  id,
  agent: "helper",
  preview: `Step ${id}`,
  status: "done",
  weight: 60,
});
// The rail shows from RAIL_MIN_TURNS turns: a short conversation needs no way around it.
const turns: TurnRailEntry[] = [
  { id: "a", agent: "lead", preview: "Summarize the launch plan", status: "done", weight: 400 },
  { id: "b", agent: "helper", preview: "When does packaging arrive?", status: "done", weight: 90 },
  filler("d"),
  filler("e"),
  filler("f"),
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
  assert.equal(wave.getAttribute("aria-valuemax"), "6");
  assert.equal(wave.getAttribute("aria-valuenow"), "6");
  assert.match(wave.getAttribute("aria-valuetext") ?? "", /6 of 6, lead: Draft the checklist/);
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

test("a short conversation has no wave", () => {
  render(
    <Thread turns={turns.slice(0, RAIL_MIN_TURNS - 1)}>
      <article data-turn-id="a" />
    </Thread>,
  );
  assert.equal(screen.queryByRole("slider"), null);
});

test("a turn's preview carries its author's mark: an agent's initials, a person's initial", () => {
  const mixed: TurnRailEntry[] = [
    {
      id: "p",
      agent: "Mira",
      kind: "person",
      preview: "Can we ship Friday?",
      status: "done",
      weight: 60,
    },
    filler("w"),
    filler("x"),
    filler("y"),
    filler("z"),
    { id: "a", agent: "lead", preview: "Yes, once QA signs off.", status: "done", weight: 80 },
  ];
  render(
    <Thread turns={mixed}>
      {mixed.map((t) => (
        <article key={t.id} data-turn-id={t.id} />
      ))}
    </Thread>,
  );
  const wave = screen.getByRole("slider", { name: "Turns in this conversation" });
  fireEvent.keyDown(wave, { key: "Home" });
  const person = screen.getByRole("tooltip");
  assert.equal(person.querySelector("[data-mark='person']")?.textContent, "M");
  assert.match(person.textContent ?? "", /Can we ship Friday\?/);
  fireEvent.keyDown(wave, { key: "End" });
  const agent = screen.getByRole("tooltip");
  assert.equal(agent.querySelector(".aui-initials")?.textContent, "LE");
});

test("an addressed message scrolls only its transcript once, including a later loaded page", () => {
  const moves: Element[] = [];
  const old = HTMLElement.prototype.scrollTo;
  HTMLElement.prototype.scrollTo = function () {
    moves.push(this);
  };
  try {
    const view = render(
      <Thread focus="b" turns={turns.slice(0, 1)}>
        <article data-turn-id="a" />
      </Thread>,
    );
    assert.equal(moves.length, 0);
    view.rerender(
      <Thread focus="b" turns={turns}>
        <article data-turn-id="a" />
        <article data-turn-id="b" />
      </Thread>,
    );
    assert.deepEqual(
      moves.map((element) => element.getAttribute("role")),
      ["log"],
    );
    view.rerender(
      <Thread focus="b" turns={[...turns]}>
        <article data-turn-id="a" />
        <article data-turn-id="b" />
      </Thread>,
    );
    assert.equal(moves.length, 1, "new stream content does not pull the reader back to the anchor");
  } finally {
    HTMLElement.prototype.scrollTo = old;
  }
});
