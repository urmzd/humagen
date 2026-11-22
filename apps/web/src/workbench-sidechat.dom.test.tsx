import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import {
  createHumanActivityStore,
  foldHumanActivity,
  humanActivity,
  MAX_HUMAN_DETAIL,
} from "@humagen/core";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Run } from "./adapter.ts";
import type { Thread, ThreadMessage } from "./api.ts";
import { stubEventSource, stubFetch } from "./dom.ts";
import { HumanActivityList } from "./humanActivity.tsx";
import {
  conclusionRecords,
  forkPoint,
  inParts,
  isSideChatOf,
  MAX_CONCLUSION_PARTS,
  sideChatRouting,
  sideChatTitle,
} from "./sidechat.ts";
import { WorkbenchSidechat } from "./WorkbenchSidechat.tsx";
import { WORKBENCH_TABS, type WorkbenchTabProps } from "./workbench.ts";

const words = defaultCopy.workbenchSidechat;
const at = "2026-11-19T09:00:00Z";
const session = "s_main";
const side = "th-00000000000000aa";
const run = (id: string, status: Run["status"], more: Partial<Run> = {}): Run =>
  ({
    id,
    tenant: "t",
    agent: "helper",
    rev: 1,
    origin: "api",
    status,
    text: "",
    thread: session,
    root: id,
    depth: 0,
    lease: 0,
    created_at: at,
    updated_at: at,
    ...more,
  }) as Run;
const message = (id: string, text: string): ThreadMessage => ({
  id,
  thread: side,
  by: "ana",
  name: "ana",
  text,
  mentions: [],
  via: "console",
  at,
});
const sideThread: Thread = {
  id: side,
  project: "",
  channel: "general",
  title: "Side chat: Why did the test fail?",
  visibility: "private",
  participants: [{ kind: "person", name: "ana", owner: true, at }],
  agents: ["helper"],
  fork: { source_thread: session, through: "r1", by: "ana", at, context: [] },
  version: 1,
  created_by: "ana",
  created_at: at,
  updated_at: at,
} as Thread;

let net: ReturnType<typeof stubFetch> | undefined;
let quiet: (() => void) | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
  quiet?.();
  quiet = undefined;
  humanActivity.clear(session);
});

/** The tab as the pane mounts it, with the list the composer of the session draws. */
function tab(props: Partial<WorkbenchTabProps> = {}) {
  const selected: (string | undefined)[] = [];
  const base: WorkbenchTabProps = {
    thread: session,
    runs: [run("r1", "completed", { answer: "Done." })],
    responses: [],
    summary: undefined,
    canPost: true,
    onSelection: (ref) => selected.push(ref),
    openTab: () => {},
    record: (input) => humanActivity.add(session, input),
    expanded: false,
  };
  const draw = (more: Partial<WorkbenchTabProps>) => (
    <>
      <WorkbenchSidechat {...base} {...props} {...more} />
      <HumanActivityList conversation={session} draft="" />
    </>
  );
  const out = render(draw({}));
  return { selected, select: (selection?: string) => out.rerender(draw({ selection })) };
}

test("the registry builds Side Chat for a reader who may post, filling the pane", () => {
  const entry = WORKBENCH_TABS.find((t) => t.id === "sidechat");
  assert.ok(entry, "the tab is built");
  assert.equal(entry.fill, true);
  const of = (canPost: boolean) =>
    entry.available?.({ thread: session, runs: [], responses: [], summary: undefined, canPost });
  assert.equal(of(true), true);
  assert.equal(of(false), false);
});

test("with nothing asked the tab is a title and the box, and lists earlier side chats", async () => {
  quiet = stubEventSource();
  net = stubFetch({
    [`GET /api/threads/${session}/forks`]: () => ({
      forks: [{ id: side, title: "Side chat: Earlier", through: "r1", at, updated_at: at }],
    }),
  });
  const { selected } = tab();
  assert.ok(screen.getByRole("heading", { name: words.empty }).textContent);
  assert.ok(screen.getByRole("textbox"));
  fireEvent.click(await screen.findByRole("button", { name: /Side chat: Earlier/ }));
  assert.deepEqual(selected, [side]);
});

test("a session with no saved turn, or a reader who may not post, gets a title and no box", () => {
  quiet = stubEventSource();
  net = stubFetch({ [`GET /api/threads/${session}/forks`]: () => ({ forks: [] }) });
  tab({ runs: [] });
  assert.ok(screen.getByRole("heading", { name: words.nothing }));
  assert.ok(!screen.queryByRole("textbox"));
  cleanup();
  tab({ canPost: false });
  assert.ok(screen.getByRole("heading", { name: words.readOnly }));
  assert.ok(!screen.queryByRole("textbox"));
});

test("the first question forks the session privately, asks there and takes the fork's address", async () => {
  quiet = stubEventSource();
  const answered = run("r9", "completed", {
    thread: side,
    message: "m-1",
    answer: "The fixture is stale.",
  });
  net = stubFetch({
    [`GET /api/threads/${session}/forks`]: () => ({ forks: [] }),
    // The session's own record: its agent is lead, though helper's run is the newest.
    [`GET /api/sessions/${session}`]: () => ({
      id: session,
      agent: "lead",
      thread: { ...sideThread, id: session, fork: undefined, agents: ["lead"] },
      messages: [],
      children: [],
      links: [],
      people: [],
    }),
    [`POST /api/threads/${session}/fork`]: () => sideThread,
    [`POST /api/threads/${side}/messages`]: (body) => ({
      thread: sideThread,
      message: message("m-1", (body as { text: string }).text),
      runs: [answered],
      notified: [],
      refused: [],
      rule: "default",
    }),
    [`GET /api/threads/${side}`]: () => ({
      thread: sideThread,
      turns: [
        { kind: "message", message: message("m-1", "Why did the test fail?") },
        { kind: "run", run: answered },
      ],
      next: null,
    }),
  });
  const { selected, select } = tab({
    runs: [run("r1", "completed"), run("r2", "running", { message: "m-7" })],
  });
  const box = screen.getByRole("textbox") as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: "Why did the test fail?" } });
  fireEvent.keyDown(box, { key: "Enter" });
  await waitFor(() => assert.deepEqual(selected, [side]));
  // The fork runs through the message the working agent is answering, to the session's own agent.
  assert.deepEqual(net.seen.find((s) => s.path === `/api/threads/${session}/fork`)?.body, {
    through: "m-7",
    title: "Side chat: Why did the test fail?",
    agents: ["lead"],
  });
  assert.equal(
    (net.seen.find((s) => s.path === `/api/threads/${side}/messages`)?.body as { text: string })
      .text,
    "Why did the test fail?",
  );
  // Nothing was posted in the session itself.
  assert.equal(
    net.seen.some((s) => s.method === "POST" && s.path.startsWith(`/api/sessions/${session}`)),
    false,
  );
  select(side);
  await screen.findByRole("article", { name: words.answeredBy("Helper") });
  assert.ok(screen.getByRole("article", { name: words.saidBy("ana") }));
  assert.equal(
    screen.getByRole("link", { name: words.openFull }).getAttribute("href"),
    `/messages/${side}`,
  );

  // Add to Message records the answer for the session's next message, and sends nothing.
  const sent = net.seen.length;
  fireEvent.click(screen.getByRole("button", { name: words.add }));
  const records = humanActivity.list(session);
  assert.equal(records.length, 1);
  assert.equal(records[0]?.source, "other");
  assert.equal(records[0]?.summary, 'Side chat answer from Helper to "Why did the test fail?"');
  assert.equal(records[0]?.detail, "The fixture is stale.");
  assert.equal(net.seen.length, sent);
  const added = screen.getByRole("button", { name: words.added });
  assert.equal(added.getAttribute("aria-pressed"), "true");
  // Removing it from the list above the composer puts the button back.
  fireEvent.click(
    screen.getByRole("button", { name: defaultCopy.workbench.byHand.remove(records[0]!.summary) }),
  );
  assert.equal(humanActivity.list(session).length, 0);
  assert.ok(screen.getByRole("button", { name: words.add }));
  // Pressed twice, it is taken back.
  fireEvent.click(screen.getByRole("button", { name: words.add }));
  fireEvent.click(screen.getByRole("button", { name: words.added }));
  assert.equal(humanActivity.list(session).length, 0);
});

test("a side chat opens again from its address, and one that is not this session's does not", async () => {
  quiet = stubEventSource();
  const other = "th-00000000000000bb";
  net = stubFetch({
    [`GET /api/threads/${session}/forks`]: () => ({ forks: [] }),
    [`GET /api/threads/${side}`]: () => ({
      thread: sideThread,
      turns: [{ kind: "message", message: message("m-1", "Why did the test fail?") }],
      next: null,
    }),
    [`GET /api/threads/${other}`]: () => ({
      thread: { ...sideThread, id: other, fork: { ...sideThread.fork, source_thread: "s_else" } },
      turns: [{ kind: "message", message: message("m-2", "Not yours to see here") }],
      next: null,
    }),
  });
  const { selected, select } = tab({ selection: side });
  await screen.findByText("Why did the test fail?");
  assert.ok(screen.getByRole("heading", { name: sideThread.title }));
  fireEvent.click(screen.getByRole("button", { name: words.fresh }));
  assert.deepEqual(selected, [undefined]);
  select(other);
  await screen.findByRole("heading", { name: words.missing });
  assert.ok(!screen.queryByText("Not yours to see here"));
  assert.ok(!screen.queryByRole("textbox"));
});

test("a fork the platform refuses says why and leaves the question in the box's queue", async () => {
  quiet = stubEventSource();
  net = stubFetch({
    [`GET /api/threads/${session}/forks`]: () => ({ forks: [] }),
    [`POST /api/threads/${session}/fork`]: () =>
      new Response(JSON.stringify({ error: "fork from a completed turn" }), { status: 400 }),
  });
  const { selected } = tab();
  const box = screen.getByRole("textbox") as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: "Why?" } });
  fireEvent.keyDown(box, { key: "Enter" });
  const alert = await screen.findByRole("alert");
  assert.ok(alert.textContent);
  assert.deepEqual(selected, []);
});

test("a side chat starts from the newest saved turn and leaves a working agent alone", () => {
  assert.equal(forkPoint([]), undefined);
  assert.equal(forkPoint([run("r1", "completed"), run("r2", "failed")]), "r2");
  // An answer still being written is not saved: the message it answers is.
  assert.equal(
    forkPoint([run("r1", "completed"), run("r2", "running", { message: "m-3" })]),
    "m-3",
  );
  // A run from before threads names no message, so the turn before it stands.
  assert.equal(forkPoint([run("r1", "stopped"), run("r2", "parked")]), "r1");
  assert.equal(forkPoint([run("r1", "accepted")]), undefined);
});

test("a side chat is answered by the session's own agents, whoever answered last", () => {
  const runs = [run("r1", "completed"), run("r2", "running", { agent: "rev" })];
  const page = (thread: Partial<Thread>, agent?: string) => ({
    thread: thread as Thread,
    ...(agent ? { answers: { agent, from: "session" } } : {}),
  });
  assert.deepEqual(sideChatRouting(page({ agents: ["lead"] }, "other"), runs), {
    agents: ["lead"],
  });
  assert.deepEqual(sideChatRouting(page({ agents: ["a", "b"], mode: "all" }), runs), {
    agents: ["a", "b"],
    mode: "all",
  });
  assert.deepEqual(sideChatRouting(page({}, "lead"), runs), { agents: ["lead"] });
  // The session could not be read: the agent that answered last stands in.
  assert.deepEqual(sideChatRouting(undefined, runs), { agents: ["rev"] });
  assert.deepEqual(sideChatRouting(undefined, []), {});
});

test("a side chat is titled from its first question, within the platform's limit", () => {
  assert.equal(sideChatTitle("  Why did\nthe test fail? "), "Side chat: Why did the test fail?");
  assert.equal(sideChatTitle(" "), words.untitled);
  const long = sideChatTitle("x".repeat(400));
  assert.equal(long.length, 200);
  assert.ok(long.endsWith("…"));
});

test("only a live fork of this session is its side chat", () => {
  const fork = { source_thread: "s_main", through: "r1", by: "ana", at: "", context: [] };
  const thread = (more: Partial<Thread>) => ({ id: "th-1", ...more }) as Thread;
  assert.equal(isSideChatOf(thread({ fork }), "s_main"), true);
  assert.equal(isSideChatOf(thread({ fork }), "s_other"), false);
  assert.equal(isSideChatOf(thread({ fork, read_only: true }), "s_main"), false);
  assert.equal(isSideChatOf(thread({}), "s_main"), false);
});

test("text is cut into parts at a paragraph, a line, a sentence or a word", () => {
  assert.deepEqual(inParts("  short  ", 20, 3), ["short"]);
  assert.deepEqual(inParts("aaaa bbbb cccc. dddd eeee", 18, 3), ["aaaa bbbb cccc.", "dddd eeee"]);
  assert.deepEqual(inParts("aaaaaaaaaaaaaaaa\n\nbbbb", 19, 3), ["aaaaaaaaaaaaaaaa", "bbbb"]);
  // No place to break: the cut is at the limit.
  assert.deepEqual(inParts("x".repeat(25), 10, 3), ["x".repeat(10), "x".repeat(10), "x".repeat(5)]);
  // Past the last part, the rest stays on it for the store to cut and mark.
  const parts = inParts("word ".repeat(100), 50, 2);
  assert.equal(parts.length, 2);
  assert.ok(parts[0]!.length <= 50);
  assert.ok(parts[1]!.length > 50);
});

test("an answer is carried back as records that fold into the next message", () => {
  assert.deepEqual(conclusionRecords({ id: "r1", agent: "helper", answer: " " }, "Why?"), []);
  const [one, ...rest] = conclusionRecords(
    { id: "r1", agent: "helper", answer: "The fixture is stale." },
    "Why did\nthe test fail?",
    "Humagen",
  );
  assert.equal(rest.length, 0);
  assert.deepEqual(one, {
    source: "other",
    summary: 'Side chat answer from Humagen to "Why did the test fail?"',
    detail: "The fixture is stale.",
    key: "sidechat:r1:1",
  });

  const long = conclusionRecords(
    { id: "r2", agent: "helper", answer: "A sentence of the answer. ".repeat(200) },
    "q".repeat(300),
  );
  assert.equal(long.length, MAX_CONCLUSION_PARTS);
  assert.ok(long[0]!.summary.endsWith("(part 1 of 3)"));
  assert.ok(long[0]!.summary.length <= 200, "the summary fits the record's limit");
  assert.ok(long[0]!.detail!.length <= MAX_HUMAN_DETAIL);
  assert.ok(long[0]!.detail!.endsWith("."));

  // Adding the same answer twice keeps one copy, and it reaches the message.
  const store = createHumanActivityStore();
  for (const input of [...long, ...long]) store.add("s_main", input);
  assert.equal(store.list("s_main").length, MAX_CONCLUSION_PARTS);
  const folded = foldHumanActivity("So what now?", store.list("s_main"));
  assert.ok(folded.text.startsWith("So what now?"));
  assert.ok(folded.text.includes("Side chat answer from helper"));
  assert.ok(folded.text.includes("A sentence of the answer."));
});
