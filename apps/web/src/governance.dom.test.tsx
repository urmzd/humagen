import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { QueuedApproval } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { ApprovalQueue } from "./Governance.tsx";

// Every DOM file's afterEach runs after every DOM test, so this one leaves
// alone what it did not stub.
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

type Question = NonNullable<QueuedApproval["parts"][number]["question"]>;

const waiting = (question: Partial<Question>): QueuedApproval => ({
  id: "m1",
  subject: "boss wants to run wiki__search",
  state: "open",
  source: { run: "run-1", agent: "boss" },
  parts: [
    { id: "p1", question: { run: "run-1", id: "q1", prompt: "Search the wiki?", ...question } },
  ],
});

function open(message: QueuedApproval, viewer: string) {
  net = stubFetch({
    "GET /api/inbox": () => ({ messages: [message], next: null }),
    "POST /api/runs/run-1/questions/q1/answer": () => ({}),
  });
  render(<ApprovalQueue agent="boss" viewer={viewer} />);
}

/** Denies the one approval on screen and returns what was sent. */
async function deny(): Promise<unknown> {
  fireEvent.click(screen.getByRole("button", { name: "Deny" }));
  const posted = () => (net?.seen ?? []).find((s) => s.method === "POST");
  await waitFor(() => assert.ok(posted()));
  return posted()?.body;
}

test("on a run that takes words only from its person, anyone else is offered no note", async () => {
  open(waiting({ steered_for: "ana", steered_for_name: "Ana" }), "ben");
  await screen.findByText("Search the wiki?");
  assert.ok(!screen.queryByLabelText("Note with the answer"));
  assert.ok(screen.getByText(/Only Ana may add a note/));
  assert.deepEqual(await deny(), { action: "deny" });
});

test("the person whose credentials the run uses is named by what they are called, never by subject", async () => {
  open(waiting({ steered_for: "tailscale:pat@example.com", steered_for_name: "pat" }), "ben");
  await screen.findByText(/Only pat may add a note/);
  assert.ok(!screen.queryByText(/example\.com/));
  cleanup();
  net?.restore();
  // A server that sends no name still names nobody by subject.
  open(waiting({ steered_for: "tailscale:pat@example.com" }), "ben");
  await screen.findByText(/Only the person it acts for may add a note/);
  assert.ok(!screen.queryByText(/example\.com/));
});

test("that person, and anyone on any other run, may add one", async () => {
  for (const [message, viewer] of [
    [waiting({ steered_for: "ana" }), "ana"],
    [waiting({}), "ben"],
  ] as const) {
    open(message, viewer);
    const note = await screen.findByLabelText("Note with the answer");
    assert.ok(!screen.queryByText(/may add a note/));
    fireEvent.change(note, { target: { value: "Not today" } });
    assert.deepEqual(await deny(), { action: "deny", note: "Not today" });
    cleanup();
    net?.restore();
    net = undefined;
  }
});

test("a call that acts as the person says so", async () => {
  open(
    waiting({ steered_for: "ana", tool: { name: "issues__search", acts_for_person: true } }),
    "ben",
  );
  await screen.findByText(defaultCopy.actsForPerson);
});

test("the approval agent's view shows with the workspace's own fields, and nothing shows without one", async () => {
  open(
    waiting({
      judgement: {
        decision: "ask",
        reason: "Nobody in the session asked to search the wiki.",
        references: ["Policy: searches need a person"],
        fields: [
          { name: "risk", label: "Risk", value: "high" },
          { name: "conditions", label: "Conditions", value: "only on main" },
          { name: "expires_in", label: "Expires in", value: "3600" },
        ],
      },
    }),
    "ben",
  );
  await screen.findByText("Search the wiki?");
  assert.ok(screen.getByText(/Asked a person: "Nobody in the session asked to search the wiki\."/));
  for (const [label, value] of [
    ["Risk", "high"],
    ["Conditions", "only on main"],
    ["Expires in", "3600"],
  ] as const) {
    const shown = screen.getByText(`${label}:`).parentElement?.textContent;
    assert.equal(shown, `${label}: ${value}`);
  }
  assert.ok(screen.getByText("Based on: Policy: searches need a person"));
  cleanup();
  net?.restore();

  open(waiting({}), "ben");
  await screen.findByText("Search the wiki?");
  assert.ok(!screen.queryByText(/Approval agent/));
  assert.ok(!screen.queryByText(/Risk/));
});
