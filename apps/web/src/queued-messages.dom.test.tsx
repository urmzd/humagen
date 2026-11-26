import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { useQueue } from "@humagen/web";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { useState } from "react";
import { Composer } from "./Composer.tsx";

// A message sent while an agent works waits at the end of the conversation,
// as the person's own message, greyed, with Send now (DES-087). The box
// holds only what is being typed.

afterEach(cleanup);

type Seen = { started: string[]; interrupts: number };

function Page({ seen, inChat = true }: { seen: Seen; inChat?: boolean }) {
  const queue = useQueue({
    onStart: (m) => seen.started.push(m.text),
    onInterrupt: () => seen.interrupts++,
  });
  const [place, setPlace] = useState<HTMLElement | null>(null);
  return (
    <>
      <main data-testid="conversation">
        <div ref={setPlace} data-layer="queued-place" />
      </main>
      <Composer placeholder="Reply" queue={queue} queuePlace={inChat ? place : undefined} />
      <button type="button" onClick={() => queue.turnEnded("done")}>
        end the turn
      </button>
    </>
  );
}

const type = (text: string, keys: Record<string, unknown> = {}) => {
  const box = screen.getByRole("textbox", { name: /^(?!Edit)/ }) as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: text } });
  fireEvent.keyDown(box, { key: "Enter", ...keys });
};

test("a message sent while an agent works waits in the conversation, not in the box", () => {
  const seen: Seen = { started: [], interrupts: 0 };
  render(<Page seen={seen} />);
  type("check my repos");
  assert.deepEqual(seen.started, ["check my repos"], "nothing running: it starts");
  type("then rebase");
  type("and on Windows");
  assert.deepEqual(seen.started, ["check my repos"], "Enter queues behind what runs");
  const conversation = screen.getByTestId("conversation");
  const queued = within(conversation).getByRole("region", { name: "Queued messages" });
  assert.deepEqual(
    [...queued.querySelectorAll('[data-aui="queued-message"] p')].map((p) => p.textContent),
    ["then rebase", "and on Windows"],
  );
  // The box holds nothing of the queue.
  const form = screen.getByRole("form", { name: "Message" });
  assert.equal(within(form).queryByText("then rebase"), null);
  assert.equal(document.querySelectorAll('[aria-label="Queued messages"]').length, 1);
  // When the turn ends, the next one goes and leaves the queue.
  act(() => screen.getByRole("button", { name: "end the turn" }).click());
  assert.deepEqual(seen.started, ["check my repos", "then rebase"]);
  assert.deepEqual(
    [...queued.querySelectorAll('[data-aui="queued-message"] p')].map((p) => p.textContent),
    ["and on Windows"],
  );
});

test("a queued message can be sent now, edited and removed where it waits", () => {
  const seen: Seen = { started: [], interrupts: 0 };
  render(<Page seen={seen} />);
  type("check my repos");
  type("then rebase");
  type("and on Windows");
  fireEvent.click(screen.getByRole("button", { name: 'Edit "then rebase"' }));
  const edit = screen.getByRole("textbox", { name: "Edit queued message" });
  fireEvent.change(edit, { target: { value: "then rebase on main" } });
  fireEvent.keyDown(edit, { key: "Enter" });
  assert.ok(screen.getByText("then rebase on main"));
  fireEvent.click(screen.getByRole("button", { name: 'Remove "then rebase on main"' }));
  assert.equal(screen.queryByText("then rebase on main"), null);
  // Send now stops the work in flight; the message goes when that turn ends.
  fireEvent.click(screen.getByRole("button", { name: "Send now: and on Windows" }));
  assert.equal(seen.interrupts, 1);
  assert.ok(screen.getByText("Sending next"));
  act(() => screen.getByRole("button", { name: "end the turn" }).click());
  assert.deepEqual(seen.started, ["check my repos", "and on Windows"]);
  assert.equal(screen.queryByRole("region", { name: "Queued messages" }), null);
});

test("Cmd+Enter sends now, and more than one waiting message can be cleared together", () => {
  const seen: Seen = { started: [], interrupts: 0 };
  render(<Page seen={seen} />);
  type("check my repos");
  type("one");
  assert.equal(
    screen.queryByRole("button", { name: "Clear queue" }),
    null,
    "one message: no Clear",
  );
  type("two");
  fireEvent.click(screen.getByRole("button", { name: "Clear queue" }));
  assert.equal(screen.queryByRole("region", { name: "Queued messages" }), null);
  type("use the staging branch", { metaKey: true });
  assert.equal(seen.interrupts, 1);
});

test("a composer with no conversation to show them in keeps the strip in the box", () => {
  const seen: Seen = { started: [], interrupts: 0 };
  render(<Page seen={seen} inChat={false} />);
  type("check my repos");
  type("then rebase");
  const form = screen.getByRole("form", { name: "Message" });
  assert.ok(within(form).getByRole("region", { name: "Queued messages" }));
  assert.equal(within(screen.getByTestId("conversation")).queryByText("then rebase"), null);
});
