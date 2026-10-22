import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Run } from "./adapter.ts";
import type { ProjectAgentsView, Revision, ThreadPage } from "./api.ts";
import { Chat } from "./Chat.tsx";
import { stubEventSource, stubFetch } from "./dom.ts";

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
let restoreEvents: (() => void) | undefined;
afterEach(() => {
  restoreEvents?.();
  restoreEvents = undefined;
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

/** Takes down what open set up, so the next open starts afresh. */
function close() {
  cleanup();
  net?.restore();
  net = undefined;
}

/** The same turn, answered. */
const answered = (actor: Run["actor"], tag = "api-t1"): Run => {
  const { error: _, ...turn } = failed(actor, tag);
  return { ...turn, status: "completed", answer: "Filed." };
};

/** A retry warp asked for nobody: it names whoever pressed it, and none of its words were theirs. */
const NOBODY_RETRY = "api-retry-3-ben-t0";

function open(turn: Run, me: string, turns: Run[] = [turn]) {
  // A stream that never says anything: the retried run is followed, and nothing here needs its events.
  restoreEvents ??= stubEventSource();
  net = stubFetch({
    "GET /api/threads/t1": () => ({
      thread: { id: "t1", project: turn.project ?? "", channel: "general", visibility: "shared", participants: [], version: 0, created_by: me, created_at: at, updated_at: at },
      turns: turns.map((run) => ({ kind: "run", run })),
      next: null,
    }),
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
  close();

  // A later turn joins the conversation after the one before it, which carries the project.
  const done: Run = { ...first, status: "completed", error: undefined };
  const second: Run = { ...first, id: "t2", tag_id: "api-t2", text: "and the other one", prev: "t1" };
  open(second, "ana", [done, second]);
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
    close();
  }
});

test("Regenerate is offered only on an answered turn that acted for the reader", async () => {
  for (const [actor, tag] of [
    [{ id: "ana", display: "Ana", user: "ana" }, "api-t1"],
    [{ id: "slack-app", bot: true }, "api-t1"],
    [{ id: "ben", display: "Ben" }, NOBODY_RETRY],
  ] as const) {
    open(answered(actor, tag), "ben");
    await screen.findByText("Filed.");
    assert.ok(!screen.queryByRole("button", { name: "Regenerate" }), JSON.stringify(actor));
    close();
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
  close();
  open(answered(before, "api-3-ben-k1"), "ben");
  fireEvent.click(await screen.findByRole("button", { name: "Regenerate" }));
  await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "POST" && r.path === "/api/runs")));
  close();
  // Someone else's is still not the reader's.
  open(failed(before, "api-r-0123456789abcdefghijkl"), "ana");
  await screen.findByText("This turn acted for Ben, so only they can retry it.");
  assert.ok(!screen.queryByRole("button", { name: "Retry" }));
});

const id = "api-r-conversation";
const helper = agent("helper", "Renewals and billing");

/** The nth turn of one long branch, each following the one before. */
const run = (n: number) =>
  ({
    id: `r${n}`,
    agent: "helper",
    status: "completed",
    text: `question ${n}`,
    answer: `answer ${n}`,
    thread: id,
    prev: n === 0 ? undefined : `r${n - 1}`,
    calls: 0,
    created_at: at,
    updated_at: at,
  }) as unknown as Run;

const record = { id, project: "", channel: "general", visibility: "shared", participants: [], version: 0, created_by: "ana", created_at: at, updated_at: at };

test("a conversation longer than one page is read from its start, and the next message continues it", async () => {
  restoreEvents = stubEventSource();
  // 201 turns: the newest page holds 200, and the first turn is on the page before it.
  const all = Array.from({ length: 201 }, (_, n) => run(n));
  const pageOf = (turns: Run[], next: string | null): ThreadPage =>
    ({ thread: record, turns: turns.map((r) => ({ kind: "run", run: r })), next, answers: { agent: "helper", from: "project:~" } }) as unknown as ThreadPage;
  net = stubFetch({
    [`GET /api/threads/${id}`]: (_, q) => (q.get("before") === "c1" ? pageOf(all.slice(0, 1), null) : pageOf(all.slice(1), "c1")),
    "GET /api/projects/~/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: { agent: "helper", from: "project:~" } }),
    "GET /api/runs/r200/events": () => ({ events: [] }),
    "POST /api/runs": () => ({ ...run(201), prev: "r200" }),
  });
  render(
    <Chat
      threadId={id}
      me="ana"
      summary={undefined}
      tick={0}
      agents={[helper]}
      projects={[]}
      canRun
      canCreateProject={false}
      greeting="Hello"
      onThread={() => {}}
      onChanged={() => {}}
      onProjectCreated={() => {}}
    />,
  );
  assert.ok(await screen.findByText("question 200"));
  assert.ok(screen.getAllByText("question 0").length > 0, "the first turn is shown");
  assert.equal(screen.queryByText("Hello"), null, "the conversation is not taken for a new one");
  const box = screen.getByPlaceholderText("Ask helper") as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: "and then?" } });
  fireEvent.keyDown(box, { key: "Enter" });
  await waitFor(() => assert.ok(net?.seen.some((s) => s.method === "POST")));
  const sent = net?.seen.find((s) => s.method === "POST");
  // Untagged, it names no agent, so the default the conversation inherits answers.
  assert.deepEqual(sent?.body, { text: "and then?", after: "r200" });
});

test("a turn from before warp said whom a run acts for is not the reader's when its words were someone else's", async () => {
  // Before actor.user, the console's Retry and Regenerate asked an earlier
  // turn's words again at the same point, as an ordinary turn of whoever
  // pressed, and the Slack app asked for a person it maps under its own
  // key, whatever message the words came from.
  const anas: Run = { ...answered({ id: "ana", display: "Ana" }, "api-r-aaaaaaaaaaaaaaaaaaaaaa"), id: "t0", root: "t0" };
  const bens = { id: "ben", display: "Ben" };
  for (const turn of [
    answered(bens, "api-r-bbbbbbbbbbbbbbbbbbbbbb"),
    failed(bens, "api-r-bbbbbbbbbbbbbbbbbbbbbb"),
  ]) {
    open(turn, "ben", [anas, turn]);
    await screen.findByText(turn.status === "failed" ? "This turn acted for nobody, so it is not retried here, where it would act for you." : "Filed.");
    assert.ok(!screen.queryByRole("button", { name: "Regenerate" }), "ben's Regenerate of ana's words is not his");
    assert.ok(!screen.queryByRole("button", { name: "Retry" }), "ben's Retry of ana's words is not his");
    close();
  }
  for (const turn of [answered(bens, "api-9-svc-slack-slack-T1-C1-1700000000-000100"), failed(bens, "api-9-svc-slack-slack-T1-C1-1700000000-000100")]) {
    open(turn, "ben");
    await screen.findByText(turn.status === "failed" ? "This turn acted for nobody, so it is not retried here, where it would act for you." : "Filed.");
    assert.ok(!screen.queryByRole("button", { name: "Regenerate" }), "a turn the Slack app asked for ben is not his here");
    assert.ok(!screen.queryByRole("button", { name: "Retry" }));
    close();
  }
  // His own words asked again by him are still his.
  const again = failed(bens, "api-r-bbbbbbbbbbbbbbbbbbbbbb");
  open(again, "ben", [{ ...anas, actor: bens, tag_id: "api-r-cccccccccccccccccccccc" }, again]);
  fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
  await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "POST" && r.path === "/api/runs")));
});
