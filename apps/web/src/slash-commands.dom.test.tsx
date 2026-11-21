import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { useQueue } from "@humagen/web";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { Composer } from "./Composer.tsx";
import { tagOptions } from "./tags.ts";
import { Mentioned } from "./ui.tsx";

afterEach(() => cleanup());

const words = defaultCopy.threads;
const options = tagOptions(
  [{ name: "helper" }],
  [],
  [],
  { experts: (t) => t },
  {
    skills: [
      { name: "review", description: "Review changes in the current session." },
      { name: "security-review", description: "Find vulnerabilities in a change" },
      { name: "triage", description: "Sort incoming requests" },
    ],
  },
);

function setup() {
  const sent: string[] = [];
  function Compose() {
    const queue = useQueue({ onStart: (m) => sent.push(m.text), onInterrupt: () => {} });
    return (
      <Composer placeholder="Message" queue={queue} tags={{ label: words.typeahead, options }} />
    );
  }
  render(<Compose />);
  const box = screen.getByRole("textbox", { name: "Message" }) as HTMLTextAreaElement;
  const type = (value: string) => fireEvent.change(box, { target: { value } });
  const menu = () => screen.queryByRole("listbox", { name: words.commands });
  return { box, type, menu, sent };
}

test("while an agent works Send is Stop, for everything running, and the bar has no working line or beam", async () => {
  const { type, box, sent } = setup();
  type("Draft the plan");
  fireEvent.submit(box.closest("form") as HTMLFormElement);
  await waitFor(() => assert.deepEqual(sent, ["Draft the plan"]));
  const form = box.closest("form") as HTMLFormElement;
  // Asserted by name and by presence only: printing a happy-dom node does not finish.
  const buttons = screen.getAllByRole("button").map((b) => b.getAttribute("aria-label"));
  assert.ok(buttons.includes(defaultCopy.dock.stopAllLabel), "Stop is in Send's place");
  assert.ok(!buttons.includes("Send message"), "Send is not shown while work runs");
  assert.equal(form.hasAttribute("data-working"), false);
  assert.ok(!form.querySelector("[data-working-beam]"), "no working beam");
  assert.ok(!screen.queryByTestId("working-thread"), "no working line");
});

test("/ opens the command menu of skills, each with its description and kind, the match highlighted", () => {
  const { type, menu } = setup();
  type("/rev");
  const list = menu();
  assert.ok(list, "the command menu opens");
  const rows = within(list).getAllByRole("option");
  assert.deepEqual(
    rows.map((r) => r.textContent),
    [
      `/reviewReview changes in the current session.${words.commandKind}`,
      `/security-reviewFind vulnerabilities in a change${words.commandKind}`,
    ],
  );
  assert.equal(rows[0]?.getAttribute("aria-selected"), "true");
  assert.deepEqual(
    Array.from(rows[1]?.querySelectorAll("mark") ?? []).map((m) => m.textContent),
    ["rev"],
  );
  // Agents are `@` completion's, never commands.
  type("/hel");
  assert.equal(menu(), null);
  // A slash inside a word or a path opens nothing.
  type("and/rev");
  assert.equal(menu(), null);
});

test("the composer is a multiline text box named Message that completes from a list", () => {
  const { box, type, menu } = setup();
  assert.equal(box.tagName, "TEXTAREA", "a textarea is multiline");
  // A textarea may not be a combobox, and aria-expanded is not a text box's.
  assert.equal(box.hasAttribute("role"), false);
  assert.equal(box.hasAttribute("aria-expanded"), false);
  assert.equal(screen.queryByRole("combobox"), null);
  assert.equal(box.getAttribute("aria-autocomplete"), "list");
  assert.equal(box.getAttribute("aria-haspopup"), "listbox");
  assert.equal(box.hasAttribute("aria-controls"), false, "nothing is controlled while closed");
  assert.equal(box.hasAttribute("aria-activedescendant"), false);
  type("/rev");
  const list = menu();
  assert.ok(list, "the command menu opens");
  assert.equal(box.getAttribute("aria-controls"), list.id);
  const active = box.getAttribute("aria-activedescendant");
  assert.equal(active, within(list).getAllByRole("option")[0]?.id);
  fireEvent.keyDown(box, { key: "ArrowDown" });
  assert.equal(
    box.getAttribute("aria-activedescendant"),
    within(list).getAllByRole("option")[1]?.id,
    "the arrow keys move the active option",
  );
  fireEvent.keyDown(box, { key: "Escape" });
  assert.equal(menu(), null);
  assert.equal(box.hasAttribute("aria-controls"), false);
  // Shift+Enter is a new line, never a send: the text box keeps its text.
  type("one");
  fireEvent.keyDown(box, { key: "Enter", shiftKey: true });
  assert.equal(box.value, "one");
});

test("arrow keys and Enter pick a command, written /name as a token, and sending posts the text", async () => {
  const { box, type, menu, sent } = setup();
  type("/rev");
  fireEvent.keyDown(box, { key: "ArrowDown" });
  assert.equal(
    within(menu() as HTMLElement)
      .getAllByRole("option")[1]
      ?.getAttribute("aria-selected"),
    "true",
  );
  fireEvent.keyDown(box, { key: "ArrowUp" });
  fireEvent.keyDown(box, { key: "Enter" });
  assert.equal(box.value, "/review ");
  assert.equal(menu(), null);
  assert.equal(sent.length, 0, "picking with Enter does not send");
  const token = document.querySelector("[data-tokens] mark");
  assert.equal(token?.textContent, "/review");
  type("/review the launch");
  fireEvent.keyDown(box, { key: "Enter" });
  await waitFor(() => assert.deepEqual(sent, ["/review the launch"]));
});

test("Tab picks, Esc closes, and a tap picks on a phone", () => {
  const { box, type, menu } = setup();
  type("please /tri");
  fireEvent.keyDown(box, { key: "Tab" });
  assert.equal(box.value, "please /triage ");
  type("/s");
  assert.ok(menu());
  fireEvent.keyDown(box, { key: "Escape" });
  assert.equal(menu(), null);
  assert.equal(box.value, "/s");
  type("/sec");
  const row = within(menu() as HTMLElement)
    .getAllByRole("option")
    .find((r) => r.textContent?.startsWith("/security-review"));
  assert.ok(row);
  fireEvent.mouseDown(row);
  assert.equal(box.value, "/security-review ");
});

test("a skill the message already names is greyed with the reason, and is skipped and cannot be picked", () => {
  const { box, type, menu } = setup();
  type("$review /");
  const rows = within(menu() as HTMLElement).getAllByRole("option");
  const review = rows.find((r) => r.textContent?.startsWith("/review"));
  assert.equal(review?.getAttribute("aria-disabled"), "true");
  assert.ok(review?.textContent?.includes(words.commandNamed));
  assert.notEqual(review?.getAttribute("aria-selected"), "true", "the highlight skips it");
  if (review) fireEvent.mouseDown(review);
  assert.equal(box.value, "$review /");
  fireEvent.keyDown(box, { key: "Enter" });
  assert.equal(box.value, "$review /security-review ");
});

test("$ opens no menu, and @ offers no skill", () => {
  const { type, menu } = setup();
  type("$rev");
  assert.equal(screen.queryByRole("listbox"), null);
  type("@");
  const list = screen.getByRole("listbox");
  assert.ok(!list.textContent?.includes("review"), list.textContent ?? "");
  assert.equal(menu(), null);
});

test("a posted message shows each skill it names as a /name chip, the aliases included", () => {
  render(
    <p>
      <Mentioned
        text="$review then @skill:triage and /security-review, not /tmp/x or $5"
        skills={["review", "triage", "security-review"]}
      />
    </p>,
  );
  assert.deepEqual(
    Array.from(document.querySelectorAll("[data-skill]")).map((c) => c.textContent),
    ["/review", "/triage", "/security-review"],
  );
  assert.ok(document.body.textContent?.includes("not /tmp/x or $5"));
});
