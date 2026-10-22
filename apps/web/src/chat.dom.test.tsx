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


/** A turn in conversation t1 that failed, raised by actor, asked under tag. */
const failed = (actor: Run["actor"], tag = "api-t1"): Run => ({
  id: "t1",
  tag_id: tag,
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

/** The same turn, answered. */
const answered = (actor: Run["actor"], tag = "api-t1"): Run => {
  const { error: _, ...turn } = failed(actor, tag);
  return { ...turn, status: "completed", answer: "Filed." };
};

/** A retry warp asked for nobody: it names whoever pressed it, and none of its words were theirs. */
const NOBODY_RETRY = "api-retry-3-ben-t0";

/** A stream that never says anything: the retried run is followed, and nothing here needs its events. */
class QuietStream {
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((m: MessageEvent) => void) | null = null;
  addEventListener() {}
  close() {}
}
(globalThis as { EventSource?: unknown }).EventSource ??= QuietStream;

function open(turn: Run, me: string, turns: Run[] = [turn]) {
  net = stubFetch({
    "GET /api/threads/t1": () => ({ turns }),
    "GET /api/projects/~/agents": () => workspace("lead"),
    "GET /api/projects/Globex/agents": () => ({ ...workspace(), homes: ["globex-helper"] }),
    "POST /api/runs": (body) => ({ ...turn, id: "t9", text: (body as { text: string }).text, status: "accepted", actor: { id: me, user: me } }),
  });
  const noop = () => {};
  render(
    <Chat
      threadId="t1"
      me={me}
      summary={undefined}
      tick={0}
      agents={agents}
      homes={homes}
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

test("Retry of the first turn of a project's conversation files it in that project again, and a later turn follows the turn before", async () => {
  const ana = { id: "ana", display: "Ana", user: "ana" };
  // globex-helper lives in Globex: asked anywhere else, it is refused as not available.
  const first: Run = { ...failed(ana), agent: "globex-helper", project: "Globex" };
  open(first, "ana");
  fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
  await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "POST" && r.path === "/api/runs")));
  assert.deepEqual(net?.seen.find((r) => r.method === "POST" && r.path === "/api/runs")?.body, {
    agent: "globex-helper",
    text: "file the flaky test",
    project: "Globex",
  });
  cleanup();
  net?.restore();

  // A later turn joins the conversation after the one before it, which carries the project.
  const answered: Run = { ...first, status: "completed", error: undefined };
  const second: Run = { ...first, id: "t2", tag_id: "api-t2", text: "and the other one", prev: "t1" };
  open(second, "ana", [answered, second]);
  fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
  await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "POST" && r.path === "/api/runs")));
  assert.deepEqual(net?.seen.find((r) => r.method === "POST" && r.path === "/api/runs")?.body, {
    agent: "globex-helper",
    text: "and the other one",
    after: "t1",
  });
});

test("Retry is not offered on another person's turn, nor on one that acted for nobody, and nothing is asked", async () => {
  for (const [actor, tag, says] of [
    [{ id: "ana", display: "Ana", user: "ana" }, "api-t1", "This turn acted for Ana, so only they can retry it."],
    [{ id: "slack-app", bot: true }, "api-t1", "This turn acted for nobody, so it is not retried here, where it would act for you."],
    // A person who pressed Retry on a turn that acted for nobody, such as a machine's.
    [{ id: "ben", display: "Ben" }, NOBODY_RETRY, "This turn acted for nobody, so it is not retried here, where it would act for you."],
  ] as const) {
    open(failed(actor, tag), "ben");
    await screen.findByText(says);
    assert.ok(!screen.queryByRole("button", { name: "Retry" }));
    // It can be retried, by someone else, so it does not say it cannot.
    assert.ok(!screen.queryByText(/can't be retried/));
    assert.ok(!screen.queryByText(/credentials/), "a turn's actor does not say whose credentials it used");
    assert.ok(!net?.seen.some((r) => r.method === "POST"));
    cleanup();
    net?.restore();
    net = undefined;
  }
});

// Ends one stub mid-test so the next open() starts its own, without leaving
// it for afterEach (a function call, so type narrowing does not stick).
function resetNet() {
  net?.restore();
  net = undefined;
}

test("Regenerate is offered only on an answered turn that acted for the reader", async () => {
  for (const [actor, tag] of [
    [{ id: "ana", display: "Ana", user: "ana" }, "api-t1"],
    [{ id: "slack-app", bot: true }, "api-t1"],
    [{ id: "ben", display: "Ben" }, NOBODY_RETRY],
  ] as const) {
    open(answered(actor, tag), "ben");
    await screen.findByText("Filed.");
    assert.ok(!screen.queryByRole("button", { name: "Regenerate" }), JSON.stringify(actor));
    cleanup();
    resetNet();
  }
  open(answered({ id: "ben", display: "Ben", user: "ben" }), "ben");
  fireEvent.click(await screen.findByRole("button", { name: "Regenerate" }));
  await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "POST" && r.path === "/api/runs")));
});

test("a reader's own turn from before warp said whom a run acts for may be asked again", async () => {
  // Recorded before actor.user existed: the person who asked, and no more.
  const before = { id: "ben", display: "Ben" };
  open(failed(before, "api-r-0123456789abcdefghijkl"), "ben");
  fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
  await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "POST" && r.path === "/api/runs")));
  cleanup();
  resetNet();
  open(answered(before, "api-3-ben-k1"), "ben");
  fireEvent.click(await screen.findByRole("button", { name: "Regenerate" }));
  await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "POST" && r.path === "/api/runs")));
  cleanup();
  resetNet();
  // Someone else's is still not the reader's.
  open(failed(before, "api-r-0123456789abcdefghijkl"), "ana");
  await screen.findByText("This turn acted for Ben, so only they can retry it.");
  assert.ok(!screen.queryByRole("button", { name: "Retry" }));
});
