import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import type { AgentSession } from "../../../packages/client/src/generated/types.ts";
import { setEntryAgent } from "./branding.ts";
import { stubEventSource, stubFetch } from "./dom.ts";
import { tree } from "./projects.fixtures.ts";
import { ThreadRoom } from "./Room.tsx";
import { sessionPage } from "./sessionAdapter.ts";

const words = defaultCopy.threads;
const at = "2026-11-07T09:00:00Z";
const root = "s_root";

const asked = {
  id: "m-1",
  session: root,
  author: { kind: "person", name: "Ana" },
  status: "done",
  parts: [{ type: "text", text: "When does Globex renew, and what do we owe?" }],
  at,
  message: {
    id: "m-1",
    thread: root,
    by: "ana",
    name: "Ana",
    text: "When does Globex renew, and what do we owe?",
    mentions: [],
    via: "console",
    at,
  },
};

/** An agent's answer to Ana's message, as its run in the root. */
function answer(agent: string, text: string, more: Record<string, unknown> = {}) {
  const id = `r-${agent}`;
  return {
    id,
    session: root,
    author: { kind: "agent", name: agent },
    status: "done",
    parts: [{ type: "text", text }],
    at,
    run: {
      id,
      agent,
      status: "completed",
      answer: text,
      text: "When does Globex renew, and what do we owe?",
      message: "m-1",
      prev: "m-1",
      thread: root,
      created_at: at,
      updated_at: at,
      ...more,
    },
  };
}

/** A root session of the entry agent, with what followed Ana's message. */
function rootSession(messages: unknown[], children: unknown[] = []): AgentSession {
  return {
    id: root,
    agent: "humagen",
    parent: "",
    project: "",
    title: "Globex renewal",
    status: "done",
    updated_at: at,
    thread: {
      id: root,
      project: "",
      channel: "general",
      visibility: "private",
      participants: [
        { kind: "person", name: "ana", owner: true, at },
        { kind: "agent", name: "humagen", at },
      ],
      version: 1,
      created_by: "ana",
      created_at: at,
      updated_at: at,
    },
    messages: [asked, ...messages],
    children,
    links: [],
    people: [],
    cursor: "c",
  } as unknown as AgentSession;
}

let net: ReturnType<typeof stubFetch> | undefined;
let restoreEvents: (() => void) | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
  restoreEvents?.();
  restoreEvents = undefined;
  setEntryAgent("", "");
});

function open(read: () => AgentSession) {
  restoreEvents ??= stubEventSource();
  net ??= stubFetch({
    [`GET /api/sessions/${root}`]: () => read(),
    "GET /api/runs/r-humagen/events": () => ({ events: [] }),
    "GET /api/runs/r-billing/events": () => ({ events: [] }),
  });
  render(
    <ThreadRoom
      id={root}
      me="ana"
      initial={sessionPage(read())}
      agents={[]}
      projects={tree}
      root={null}
      workspaceName="Acme"
      canRun={false}
      onOpenChannel={() => {}}
      onOpenThread={() => {}}
      onChanged={() => {}}
      tick={0}
    />,
  );
}

test("a message gets one answer in its root: the entry agent's, under the workspace's name", async () => {
  setEntryAgent("humagen", "Acme");
  open(() => rootSession([answer("humagen", "Globex renews on May 3 and owes **$4,200**.")]));
  const said = await screen.findByRole("article", { name: words.answeredBy("Acme") });
  assert.equal(within(said).getByText("$4,200").tagName, "STRONG");
  assert.equal(screen.getAllByRole("article", { name: /^Answered by/ }).length, 1);
  assert.doesNotMatch(document.body.textContent ?? "", /humagen/i);
});

test("the one agent a message mentions answers in the root itself, with no link to a session of its own", async () => {
  setEntryAgent("humagen", "Acme");
  open(() => rootSession([answer("billing", "Globex owes $4,200.")]));
  const said = await screen.findByRole("article", { name: words.answeredBy("billing") });
  assert.ok(within(said).getByText("Globex owes $4,200."));
  assert.equal(screen.queryByRole("link", { name: /open session/i }), null);
});

test("why an answer came from where it did is never a line of the conversation", async () => {
  setEntryAgent("humagen", "Acme");
  // A root read from before answers were one run still carries a pick
  // message with its calls. It is no turn: the answers are.
  const session = rootSession(
    [
      {
        id: "pick-m-1",
        session: root,
        author: { kind: "agent", name: "humagen", display: "Acme" },
        status: "done",
        parts: [
          { type: "pick", text: "Billing owns renewals (matched renew, owe)." },
          { type: "call", session: "s_billing", agent: "billing", status: "done", text: "Old." },
        ],
        at,
      },
      answer("humagen", "Globex renews on May 3."),
    ],
    [{ id: "s_billing", agent: "billing", title: "Globex renewal", status: "done" }],
  );
  assert.deepEqual(
    sessionPage(session).turns.map((t) => t.kind),
    ["message", "run"],
  );
  open(() => session);
  await screen.findByRole("article", { name: words.answeredBy("Acme") });
  await waitFor(() => assert.equal(screen.queryByText(/Billing owns renewals/), null));
  assert.equal(screen.queryByRole("region", { name: /^Picked by/ }), null);
  assert.equal(screen.queryByRole("link", { name: /open session/i }), null);
});
