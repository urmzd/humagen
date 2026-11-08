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

type Call = { session: string; agent: string; status: string; text?: string };

/** A root session whose entry agent picked for Ana's message. */
function rootSession(
  reason: string,
  calls: Call[],
  more: { display?: string; messages?: unknown[] } = {},
): AgentSession {
  return {
    id: root,
    agent: "humagen",
    parent: "",
    project: "",
    title: "Globex renewal",
    status: calls.some((c) => c.status === "working") ? "working" : "done",
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
    messages: [
      asked,
      {
        id: "pick-m-1",
        session: root,
        author: {
          kind: "agent",
          name: "humagen",
          ...(more.display ? { display: more.display } : {}),
        },
        status: calls.some((c) => c.status === "working") ? "working" : "done",
        parts: [{ type: "pick", text: reason }, ...calls.map((c) => ({ type: "call", ...c }))],
        at,
      },
      ...(more.messages ?? []),
    ],
    children: calls.map((c) => ({
      id: c.session,
      agent: c.agent,
      title: "Globex renewal",
      status: c.status,
    })),
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

function open(read: () => AgentSession, tick = 0) {
  restoreEvents ??= stubEventSource();
  net ??= stubFetch({
    [`GET /api/sessions/${root}`]: () => read(),
    "GET /api/runs/r-entry/events": () => ({ events: [] }),
  });
  const opened: string[] = [];
  const props = {
    id: root,
    me: "ana",
    initial: sessionPage(read()),
    agents: [],
    projects: tree,
    root: null,
    workspaceName: "Acme",
    canRun: false,
    onOpenChannel: () => {},
    onOpenThread: (id: string) => opened.push(id),
    onChanged: () => {},
  };
  const view = render(<ThreadRoom {...props} tick={tick} />);
  return {
    opened,
    refresh: (next: number) => view.rerender(<ThreadRoom {...props} tick={next} />),
  };
}

test("a root shows why its entry agent picked, then each delegated answer inline with a link to its session", async () => {
  setEntryAgent("humagen", "Acme");
  const session = rootSession(
    "Billing owns renewals and contracts know the dates (matched renew, owe).",
    [
      { session: "s_billing", agent: "billing", status: "done", text: "Globex owes **$4,200**." },
      { session: "s_contracts", agent: "contracts", status: "done", text: "It renews on May 3." },
    ],
    { display: "Acme" },
  );
  const { opened } = open(() => session);
  const pick = await screen.findByRole("region", { name: words.pickedBy("Acme") });
  // One quiet line says why, under the workspace's name.
  const line = within(pick).getByRole("note");
  assert.equal(
    line.textContent,
    "Acme: Billing owns renewals and contracts know the dates (matched renew, owe).",
  );
  // Each answer is read in place, under its agent's name, as Markdown.
  const billing = within(pick).getByRole("article", { name: words.answeredBy("billing") });
  assert.ok(within(billing).getByText("billing"));
  assert.equal(within(billing).getByText("$4,200").tagName, "STRONG");
  const contracts = within(pick).getByRole("article", { name: words.answeredBy("contracts") });
  assert.ok(within(contracts).getByText("It renews on May 3."));
  // And opens its own session at its address.
  const links = within(pick).getAllByRole("link", { name: words.openSession });
  assert.deepEqual(
    links.map((a) => a.getAttribute("href")),
    ["/sessions/s_billing", "/sessions/s_contracts"],
  );
  links[1]?.click();
  assert.deepEqual(opened, ["s_contracts"]);
});

test("a delegated agent still working says so and shows no answer until the session reads it", async () => {
  setEntryAgent("humagen", "Acme");
  let session = rootSession(
    "Billing owns renewals.",
    [{ session: "s_billing", agent: "billing", status: "working" }],
    { display: "Acme" },
  );
  const { refresh } = open(() => session);
  const answer = await screen.findByRole("article", { name: words.answeredBy("billing") });
  assert.equal(answer.getAttribute("aria-busy"), "true");
  assert.ok(within(answer).getByText(defaultCopy.status.working));
  assert.ok(!screen.queryByText("Globex owes $4,200."));
  // The child answers; the root reads it on its next refresh.
  session = rootSession(
    "Billing owns renewals.",
    [{ session: "s_billing", agent: "billing", status: "done", text: "Globex owes $4,200." }],
    { display: "Acme" },
  );
  refresh(1);
  await screen.findByText("Globex owes $4,200.");
  const done = screen.getByRole("article", { name: words.answeredBy("billing") });
  assert.equal(done.getAttribute("aria-busy"), null);
  assert.ok(!within(done).queryByText(defaultCopy.status.working));
});

test("the entry agent answering itself shows under the workspace's name, never its own, and says why once", async () => {
  setEntryAgent("humagen", "Acme");
  const reason = "A quick question I can answer myself.";
  const session = rootSession(reason, [], {
    messages: [
      {
        id: "r-entry",
        session: root,
        author: { kind: "agent", name: "humagen" },
        status: "done",
        parts: [{ type: "text", text: "Hello, Ana." }],
        at,
        run: {
          id: "r-entry",
          agent: "humagen",
          status: "completed",
          answer: "Hello, Ana.",
          text: "When does Globex renew, and what do we owe?",
          message: "m-1",
          prev: "m-1",
          thread: root,
          picked: reason,
          created_at: at,
          updated_at: at,
        },
      },
    ],
  });
  open(() => session);
  const answer = await screen.findByRole("article", { name: words.answeredBy("Acme") });
  assert.ok(within(answer).getByText("Hello, Ana."));
  assert.ok(within(answer).getByText("Acme"));
  // The reason shows once, above the answer, not again as a pick line.
  await waitFor(() => assert.equal(screen.getAllByText(reason).length, 1));
  assert.equal(screen.queryByRole("region", { name: /^Picked by/ }), null);
  assert.doesNotMatch(document.body.textContent ?? "", /humagen/i);
});

test("a pick with no display name still shows the entry agent under the workspace's name", async () => {
  setEntryAgent("humagen", "Acme");
  open(() =>
    rootSession("Billing owns renewals.", [
      { session: "s_billing", agent: "billing", status: "done", text: "Globex owes $4,200." },
    ]),
  );
  const pick = await screen.findByRole("region", { name: words.pickedBy("Acme") });
  assert.equal(within(pick).getByRole("note").textContent, "Acme: Billing owns renewals.");
  assert.doesNotMatch(document.body.textContent ?? "", /humagen/i);
});
