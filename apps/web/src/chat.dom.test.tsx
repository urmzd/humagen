import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Run } from "./adapter.ts";
import type { ProjectAgentsView, Revision } from "./api.ts";
import { Chat } from "./Chat.tsx";
import { stubFetch } from "./dom.ts";

const at = "2026-10-01T09:00:00Z";
const agent = (name: string, description = ""): Revision => ({
  rev: 1,
  spec: { name, description, provider: "offline", model: "offline", system_prompt: `${name}.` },
  created_at: at,
  created_by: "ada",
  hash: name,
});
// globex-helper lives in Globex, so it is not available at the workspace.
const agents = [agent("lead", "Coordinates work."), agent("helper"), agent("globex-helper", "Knows the Globex account.")];
const homes = { "globex-helper": "Globex" };

const workspace = (defaultResolved?: string): ProjectAgentsView => ({
  version: 2,
  enable: ["*"],
  ...(defaultResolved ? { default: defaultResolved } : {}),
  available: { all: true, names: [] },
  offers: [{ agent: "*", from: "~" }],
  homes: [],
  default_resolved: defaultResolved ? { agent: defaultResolved, from: "project:~" } : null,
});

// The loader imports every DOM file into one process, where a top-level hook
// runs after other files' tests too, so it acts only on what this file set up.
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

function openChat(view: ProjectAgentsView) {
  net = stubFetch({
    "GET /api/projects/~/agents": () => view,
    "GET /api/pick": () => ({ agent: "lead", reason: "matched work" }),
    "POST /api/runs": (body) => ({
      id: "r1",
      tag_id: "api-r-1",
      tenant: "acme",
      agent: (body as { agent?: string }).agent ?? "lead",
      rev: 1,
      origin: "api",
      text: (body as { text: string }).text,
      root: "r1",
      depth: 0,
      thread: "r1",
      status: "accepted",
      calls: 0,
      created_at: at,
      updated_at: at,
    }),
  });
  const noop = () => {};
  render(
    <Chat
      threadId={null}
      me="ada"
      summary={undefined}
      tick={0}
      agents={agents}
      homes={homes}
      projects={[]}
      canRun
      canCreateProject={false}
      greeting="Good morning"
      onThread={noop}
      onChanged={noop}
      onProjectCreated={noop}
    />,
  );
  return net;
}

/** The rows of the composer's menu, by what each says first. */
async function menuRows(): Promise<string[]> {
  fireEvent.click(screen.getByRole("button", { name: /^Choose who answers/ }));
  const list = await screen.findByRole("list", { name: "Agents" });
  return within(list)
    .getAllByRole("button")
    .map((b) => b.textContent ?? "");
}

async function send(text: string) {
  fireEvent.change(screen.getByLabelText("Message", { selector: "textarea" }), { target: { value: text } });
  fireEvent.submit(screen.getByRole("form", { name: "Message" }));
  await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "POST" && r.path === "/api/runs")));
  return net?.seen.find((r) => r.method === "POST" && r.path === "/api/runs")?.body as Record<string, unknown>;
}

test("the composer's menu offers only the agents available where the message goes, with @auto first when it is the default there", async () => {
  openChat(workspace("auto"));
  // Under the composer, in words, who answers a message that tags nobody.
  await screen.findByText("@auto picks who answers");
  const rows = await menuRows();
  assert.equal(rows.length, 3);
  assert.match(rows[0] ?? "", /^@autoDefault/);
  assert.match(rows[1] ?? "", /^lead/);
  assert.match(rows[2] ?? "", /^helper/);
  assert.ok(!rows.some((r) => r.includes("globex-helper")));
});

test("with no default where the message goes, the composer says nobody answers and @auto comes last", async () => {
  openChat(workspace());
  await screen.findByText("Nobody answers; tag an agent");
  const rows = await menuRows();
  assert.match(rows[0] ?? "", /^lead/);
  assert.match(rows[rows.length - 1] ?? "", /^@auto/);
  assert.ok(!rows.some((r) => r.includes("Default")));
});

test("a message the person leaves untagged is sent with no agent, so the default where it goes answers", async () => {
  openChat(workspace("lead"));
  await screen.findByText("@lead answers");
  const body = await send("What changed this week?");
  assert.ok(!("agent" in body), `the untagged message named an agent: ${JSON.stringify(body)}`);
  assert.equal(body.text, "What changed this week?");
});

test("an agent the person chooses is sent by name, and the composer says it answers", async () => {
  openChat(workspace("lead"));
  await screen.findByText("@lead answers");
  await menuRows();
  fireEvent.click(screen.getByRole("button", { name: /^helper/ }));
  await screen.findByText("@helper answers");
  const body = await send("Help with the launch.");
  assert.equal(body.agent, "helper");
});

test("@auto chosen in the menu is sent as auto, not left to the default", async () => {
  openChat(workspace("lead"));
  await screen.findByText("@lead answers");
  await menuRows();
  fireEvent.click(screen.getByRole("button", { name: /^@auto/ }));
  await screen.findByText("@auto picks who answers");
  const body = await send("Who knows billing?");
  assert.equal(body.agent, "auto");
});


/** A turn in conversation t1 that failed, raised by actor. */
const failed = (actor: Run["actor"]): Run => ({
  id: "t1",
  tag_id: "api-t1",
  tenant: "acme",
  agent: "lead",
  rev: 1,
  origin: "api",
  text: "file the flaky test",
  actor,
  root: "t1",
  depth: 0,
  thread: "t1",
  lease: 0,
  granted: 0,
  calls: 0,
  status: "failed",
  error: "provider: 529 overloaded",
  created_at: at,
  updated_at: at,
});

/** A stream that never says anything: the retried run is followed, and nothing here needs its events. */
class QuietStream {
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((m: MessageEvent) => void) | null = null;
  addEventListener() {}
  close() {}
}
(globalThis as { EventSource?: unknown }).EventSource ??= QuietStream;

function open(turn: Run, me: string) {
  net = stubFetch({
    "GET /api/threads/t1": () => ({ turns: [turn] }),
    "GET /api/projects/~/agents": () => workspace("lead"),
    "POST /api/runs": (body) => ({ ...turn, id: "t2", text: (body as { text: string }).text, status: "accepted", actor: { id: me, user: me } }),
  });
  const noop = () => {};
  render(
    <Chat
      threadId="t1"
      me={me}
      summary={undefined}
      tick={0}
      agents={agents}
      projects={[]}
      canRun
      canCreateProject={false}
      greeting=""
      onThread={noop}
      onChanged={noop}
      onProjectCreated={noop}
    />,
  );
}

test("Retry in a conversation asks the turn again only for the person it acted for, as them", async () => {
  open(failed({ id: "ana", display: "Ana", user: "ana" }), "ana");
  const retry = await screen.findByRole("button", { name: "Retry" });
  fireEvent.click(retry);
  await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "POST" && r.path === "/api/runs")));
  const asked = net?.seen.find((r) => r.method === "POST" && r.path === "/api/runs");
  assert.deepEqual(asked?.body, { agent: "lead", text: "file the flaky test" });
});

test("Retry is not offered on another person's turn, nor on one that acted for nobody, and nothing is asked", async () => {
  for (const [actor, says] of [
    [{ id: "ana", display: "Ana", user: "ana" }, "This turn acted for Ana, with their own credentials, so only they can retry it."],
    [{ id: "slack-app", bot: true }, "This turn acted for nobody, so it is not retried here, where it would act for you."],
    // A person who raised a run that acted for nobody, such as a retry of a machine's turn.
    [{ id: "ben" }, "This turn acted for nobody, so it is not retried here, where it would act for you."],
  ] as const) {
    open(failed(actor), "ben");
    await screen.findByText(says);
    assert.equal(screen.queryByRole("button", { name: "Retry" }), null);
    assert.ok(!net?.seen.some((r) => r.method === "POST"));
    cleanup();
    net?.restore();
    net = undefined;
  }
});
