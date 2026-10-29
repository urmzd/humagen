import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Run } from "./adapter.ts";
import type { ProjectAgentsView, Revision, ThreadPage } from "./api.ts";
import { Chat } from "./Chat.tsx";
import { stubEventSource, stubFetch } from "./dom.ts";
import { globex } from "./projects.fixtures.ts";

const at = "2026-10-01T09:00:00Z";
const agent = (name: string, description = ""): Revision => ({
  rev: 1,
  spec: { name, description, provider: "offline", model: "offline", system_prompt: `${name}.` },
  created_at: at,
  created_by: "ada",
  hash: name,
});
// globex-helper lives in Globex, so it is not available at the workspace.
const agents = [
  agent("lead", "Coordinates work."),
  agent("helper"),
  agent("globex-helper", "Knows the Globex account."),
];
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

function openChat(view: ProjectAgentsView, props: Partial<Parameters<typeof Chat>[0]> = {}) {
  const opened: string[] = [];
  net = stubFetch({
    "GET /api/projects/~/agents": () => view,
    ...(props.personalProject
      ? { [`GET /api/projects/${encodeURIComponent(props.personalProject)}/agents`]: () => view }
      : {}),
    "GET /api/projects/Globex/agents": () => ({
      ...view,
      homes: ["globex-helper"],
      available: { all: true, names: ["globex-helper"] },
    }),
    "GET /api/people": () => ({
      people: [
        { subject: "ada", name: "Ada", handle: "ada", picture: null },
        { subject: "00u-ben", name: "Ben Ito", handle: "ben", picture: null },
      ],
    }),
    "GET /api/skills": () => ({
      skills: [{ name: "review", description: "Reviews a change", argument_hint: "", from: "" }],
    }),
    "GET /api/mentions": () => ({
      kinds: [{ kind: "sme", title: "Experts", resolves: "experts" }],
    }),
    "POST /api/threads": (body) => ({
      thread: {
        id: "th-00000000000000ab",
        project: (body as { project: string }).project,
        channel: "general",
        visibility: "private",
        participants: [],
        version: 1,
        created_by: "ada",
        created_at: at,
        updated_at: at,
      },
      message: {
        id: "m-1",
        thread: "th-00000000000000ab",
        by: "ada",
        name: "Ada",
        text: (body as { text: string }).text,
        mentions: [],
        via: "console",
        at,
      },
      runs: [],
      notified: [],
      refused: [],
      rule: "default",
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
      onThread={(id) => opened.push(id)}
      onChanged={noop}
      onProjectCreated={noop}
      {...props}
    />,
  );
  return { opened };
}

const box = () => screen.getByLabelText("Message", { selector: "textarea" }) as HTMLTextAreaElement;

test("Home's chat bar has no menu of who answers and no line saying who does", async () => {
  openChat(workspace("lead"));
  assert.ok(await screen.findByPlaceholderText(defaultCopy.chat.placeholder));
  assert.equal(screen.queryByRole("button", { name: /Choose who answers/ }), null);
  assert.equal(screen.queryByText("@lead answers"), null);
  assert.equal(screen.queryByText(/answers untagged messages/), null);
  assert.equal(screen.queryByText("Nobody answers; tag an agent"), null);
  assert.equal(screen.queryByRole("list", { name: "Agents" }), null);
});

test("@ completes the agents available here and people, never @auto, never the person typing, and Tab picks", async () => {
  openChat(workspace("lead"));
  await waitFor(() => assert.ok(net?.seen.some((s) => s.path === "/api/projects/~/agents")));
  fireEvent.change(box(), { target: { value: "@" } });
  const list = await screen.findByRole("listbox");
  await waitFor(() => assert.ok(list.textContent?.includes("@ben"), list.textContent ?? ""));
  const text = list.textContent ?? "";
  for (const want of ["@lead", "@helper", "@ben"])
    assert.ok(text.includes(want), `${want} in ${text}`);
  assert.ok(!text.includes("@auto"), "nobody goes by @auto, so it is not offered");
  assert.ok(
    !text.includes("@sme"),
    "retired SME routing is not offered, even from cached mention kinds",
  );
  assert.ok(
    !text.includes("globex-helper"),
    "an agent that lives in Globex is not offered at the workspace",
  );
  assert.ok(!text.includes("@ada"), "the person typing is not offered");
  fireEvent.change(box(), { target: { value: "ask @he" } });
  fireEvent.keyDown(box(), { key: "Tab" });
  await waitFor(() => assert.equal(box().value, "ask @helper "));
  // $ completes skills.
  fireEvent.change(box(), { target: { value: "ask @helper $r" } });
  const skills = await screen.findByRole("listbox");
  await waitFor(() => assert.ok(skills.textContent?.includes("$review"), skills.textContent ?? ""));
  fireEvent.keyDown(box(), { key: "Enter" });
  await waitFor(() => assert.equal(box().value, "ask @helper $review "));
  assert.ok(!net?.seen.some((s) => s.method === "POST"), "picking with Enter does not send");
});

test("Home's chat bar starts a private conversation in the picked project and opens it; nothing runs from an untagged message", async () => {
  const { opened } = openChat(workspace("lead"), { startIn: "Globex" });
  fireEvent.change(box(), { target: { value: "What changed this week?" } });
  fireEvent.keyDown(box(), { key: "Enter" });
  await waitFor(() => assert.deepEqual(opened, ["th-00000000000000ab"]));
  const started = net?.seen.find((s) => s.method === "POST");
  assert.equal(started?.path, "/api/threads");
  assert.deepEqual(started?.body, {
    visibility: "private",
    project: "Globex",
    channel: "general",
    text: "What changed this week?",
  });
  assert.ok(!net?.seen.some((s) => s.path.startsWith("/api/runs")), "no run is asked for");
});

const chat = defaultCopy.chat;
/** The names on the chips row, in order. */
const chips = () =>
  (screen.queryByRole("list", { name: chat.selected })?.querySelectorAll("li") ?? []).length === 0
    ? []
    : [...screen.getByRole("list", { name: chat.selected }).querySelectorAll("li")].map(
        (li) => li.textContent ?? "",
      );

test("a link or suggestion selects the agent without submitting, and suggestions remain while typing", async () => {
  openChat(workspace("lead"), { startAgent: "helper", startAsk: "Help with the launch." });
  await waitFor(() => assert.equal(box().value, "Help with the launch."));
  assert.deepEqual(chips(), ["helper"]);
  assert.ok(screen.getByPlaceholderText(chat.placeholderOne("helper")));
  close();
  const writer = [...agents, agent("humagen-writer", "Writes reports.")];
  openChat(workspace("lead"), { agents: writer });
  const suggestions = await screen.findByRole("list", { name: chat.suggestions });
  fireEvent.click(within(suggestions).getByRole("button", { name: /Write a report/ }));
  await waitFor(() => assert.equal(box().value, "Write a one-page report on "));
  assert.deepEqual(chips(), ["humagen-writer"]);
  assert.equal(screen.getByRole("list", { name: chat.suggestions }) === suggestions, true);
  fireEvent.change(box(), { target: { value: "Write a one-page report on launch notes" } });
  assert.equal(
    screen.getByRole("list", { name: chat.suggestions }) === suggestions,
    true,
    "typing keeps the suggestion list mounted",
  );
  assert.equal(
    net?.seen.some((request) => request.method === "POST"),
    false,
  );
  fireEvent.change(box(), { target: { value: "" } });
  assert.ok(screen.getByRole("list", { name: chat.suggestions }));
});

test("+ opens a menu where agents are checked with Space and the project with its channel is picked; the chips row routes Each Decides or All of Them", async () => {
  openChat(workspace("lead"), {
    projects: [{ ...globex, access: { role: "owner", from: "", via: "*" } }] as never,
  });
  net?.restore();
  const opened: string[] = [];
  net = stubFetch({
    "GET /api/projects/~/agents": () => workspace("lead"),
    "GET /api/projects/Globex/agents": () => ({
      ...workspace(),
      available: { all: true, names: [] },
    }),
    "GET /api/projects/Globex/channels": () => ({
      channels: ["general", "deals"].map((name) => ({
        project: "Globex",
        name,
        version: 1,
        created_at: at,
        updated_at: at,
        answers: { agent: "", from: "" },
      })),
    }),
    "POST /api/threads": (body) => {
      opened.push("started");
      return {
        thread: {
          id: "th-00000000000000ac",
          project: "Globex",
          channel: "deals",
          visibility: "private",
          participants: [],
          version: 1,
          created_by: "ada",
          created_at: at,
          updated_at: at,
        },
        message: {
          id: "m-1",
          thread: "th-00000000000000ac",
          by: "ada",
          name: "Ada",
          text: (body as { text: string }).text,
          mentions: [],
          via: "console",
          at,
        },
        runs: [],
        notified: [],
        refused: [],
        rule: "default",
      };
    },
  });
  // The shortcut opens it from the box.
  fireEvent.keyDown(box(), { key: "/", ctrlKey: true });
  let menu = await screen.findByRole("dialog", { name: chat.menu });
  assert.ok(within(menu).getByRole("button", { name: chat.attach }));
  fireEvent.click(within(menu).getByRole("button", { name: /^Agents\b/ }));
  const lead = await within(menu).findByRole("checkbox", { name: /^lead/ });
  // The arrows move between items; Space checks the one focused.
  lead.focus();
  fireEvent.keyDown(lead, { key: "ArrowDown" });
  const helperRow = within(menu).getByRole("checkbox", { name: /^helper/ });
  assert.equal(document.activeElement, helperRow);
  fireEvent.click(lead);
  fireEvent.click(helperRow);
  assert.equal(lead.getAttribute("aria-checked"), "true");
  assert.ok(
    !within(menu).queryByRole("checkbox", { name: /globex-helper/ }),
    "an agent homed elsewhere is not offered at the workspace",
  );
  // Search narrows the list.
  fireEvent.change(within(menu).getByRole("searchbox", { name: chat.findAgent }), {
    target: { value: "zzz" },
  });
  assert.ok(within(menu).getByText(chat.noAgentMatch));
  fireEvent.change(within(menu).getByRole("searchbox", { name: chat.findAgent }), {
    target: { value: "" },
  });
  // Enter closes it.
  fireEvent.keyDown(within(menu).getByRole("checkbox", { name: /^lead/ }), { key: "Enter" });
  assert.equal(screen.queryByRole("dialog", { name: chat.menu }), null);
  assert.deepEqual(chips().slice(0, 2), ["lead", "helper"]);

  // Two agents: the routing is chosen with text on the chips row.
  const all = screen.getByRole("button", { name: chat.all });
  assert.equal(
    screen.getByRole("button", { name: chat.join }).getAttribute("aria-pressed"),
    "true",
  );
  fireEvent.click(all);
  assert.equal(all.getAttribute("aria-pressed"), "true");
  assert.ok(screen.getByPlaceholderText(chat.placeholderAll(2)));

  // The project, then one of its channels, general first.
  fireEvent.click(screen.getByRole("button", { name: chat.plus }));
  menu = await screen.findByRole("dialog", { name: chat.menu });
  fireEvent.click(within(menu).getByRole("button", { name: /^Projects\b/ }));
  fireEvent.click(await within(menu).findByRole("button", { name: /Globex/ }));
  const channels = await within(menu).findByRole("radiogroup", {
    name: chat.picker.channelsIn("Globex"),
  });
  await waitFor(() =>
    assert.deepEqual(
      within(channels)
        .getAllByRole("radio")
        .map((r) => r.textContent),
      ["#general", "#deals"],
    ),
  );
  fireEvent.click(within(channels).getByRole("radio", { name: "#deals" }));
  assert.equal(screen.queryByRole("dialog", { name: chat.menu }), null);
  assert.equal(document.activeElement, box(), "closing gives the box its focus back");
  assert.ok(chips().includes("Globex / #deals"), JSON.stringify(chips()));

  // A chip is removed with its x.
  fireEvent.click(screen.getByRole("button", { name: chat.removeAgent("helper") }));
  assert.equal(
    screen.queryByRole("button", { name: chat.all }),
    null,
    "one agent has nothing to route between",
  );
  fireEvent.click(screen.getByRole("button", { name: chat.plus }));
  const again = await screen.findByRole("dialog", { name: chat.menu });
  fireEvent.click(within(again).getByRole("button", { name: /^Agents\b/ }));
  fireEvent.click(within(again).getByRole("checkbox", { name: /^helper/ }));
  fireEvent.click(screen.getByRole("button", { name: chat.plus }));

  fireEvent.change(box(), { target: { value: "Plan the renewal" } });
  fireEvent.keyDown(box(), { key: "Enter" });
  await waitFor(() => assert.deepEqual(opened, ["started"]));
  assert.deepEqual(net?.seen.find((s) => s.method === "POST")?.body, {
    visibility: "private",
    project: "Globex",
    channel: "deals",
    text: "Plan the renewal",
    agents: ["lead", "helper"],
    mode: "all",
  });
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

/** A retry humagen asked for nobody: it names whoever pressed it, and none of its words were theirs. */
const NOBODY_RETRY = "api-retry-3-ben-t0";

function open(turn: Run, me: string, turns: Run[] = [turn]) {
  // A stream that never says anything: the retried run is followed, and nothing here needs its events.
  restoreEvents ??= stubEventSource();
  net = stubFetch({
    "GET /api/threads/t1": () => ({
      thread: {
        id: "t1",
        project: turn.project ?? "",
        channel: "general",
        visibility: "shared",
        participants: [],
        version: 0,
        created_by: me,
        created_at: at,
        updated_at: at,
      },
      turns: turns.map((run) => ({ kind: "run", run })),
      next: null,
    }),
    "GET /api/projects/~/agents": () => workspace("lead"),
    "GET /api/projects/Globex/agents": () => ({ ...workspace(), homes: ["globex-helper"] }),
    "POST /api/runs": (body) => ({
      ...turn,
      id: "t9",
      text: (body as { text: string }).text,
      status: "accepted",
      actor: { id: me, user: me },
    }),
    "POST /api/threads/t1/messages": (body) => ({
      thread: {
        id: "t1",
        project: "",
        channel: "general",
        visibility: "shared",
        participants: [],
        version: 0,
        created_by: me,
        created_at: at,
        updated_at: at,
      },
      message: {
        id: "m-1",
        thread: "t1",
        by: me,
        name: me,
        text: (body as { text: string }).text,
        mentions: [],
        via: "console",
        at,
      },
      runs: [],
      notified: [],
      refused: [],
      rule: "tagged",
    }),
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
  await waitFor(() =>
    assert.ok(net?.seen.some((r) => r.method === "POST" && r.path === "/api/runs")),
  );
  const asked = net?.seen.find((r) => r.method === "POST" && r.path === "/api/runs");
  assert.deepEqual(asked?.body, { agent: "lead", text: "file the flaky test" });
});

test("Retry of the first turn of a project's conversation files it in that project again, and a later turn follows the turn before", async () => {
  const ana = { id: "ana", display: "Ana", user: "ana" };
  // globex-helper lives in Globex: asked anywhere else, it is refused as not available.
  const first: Run = { ...failed(ana), agent: "globex-helper", project: "Globex" };
  open(first, "ana");
  fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
  await waitFor(() =>
    assert.ok(net?.seen.some((r) => r.method === "POST" && r.path === "/api/runs")),
  );
  assert.deepEqual(net?.seen.find((r) => r.method === "POST" && r.path === "/api/runs")?.body, {
    agent: "globex-helper",
    text: "file the flaky test",
    project: "Globex",
  });
  close();

  // A later turn joins the conversation after the one before it, which carries the project.
  const done: Run = { ...first, status: "completed", error: undefined };
  const second: Run = {
    ...first,
    id: "t2",
    tag_id: "api-t2",
    text: "and the other one",
    prev: "t1",
  };
  open(second, "ana", [done, second]);
  fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
  await waitFor(() =>
    assert.ok(net?.seen.some((r) => r.method === "POST" && r.path === "/api/runs")),
  );
  assert.deepEqual(net?.seen.find((r) => r.method === "POST" && r.path === "/api/runs")?.body, {
    agent: "globex-helper",
    text: "and the other one",
    after: "t1",
  });
});

test("Retry is not offered on another person's turn, nor on one that acted for nobody, and nothing is asked", async () => {
  for (const [actor, tag, says] of [
    [
      { id: "ana", display: "Ana", user: "ana" },
      "api-t1",
      "This turn acted for Ana, so only they can retry it.",
    ],
    // A turn recorded with no name for its person never names them by subject.
    [
      { id: "tailscale:pat@example.com", user: "tailscale:pat@example.com" },
      "api-t1",
      "This turn acted for someone else, so only they can retry it.",
    ],
    [
      { id: "slack-app", bot: true },
      "api-t1",
      "This turn acted for nobody, so it is not retried here, where it would act for you.",
    ],
    // A person who pressed Retry on a turn that acted for nobody, such as a machine's.
    [
      { id: "ben", display: "Ben" },
      NOBODY_RETRY,
      "This turn acted for nobody, so it is not retried here, where it would act for you.",
    ],
  ] as const) {
    open(failed(actor, tag), "ben");
    await screen.findByText(says);
    assert.ok(!screen.queryByRole("button", { name: "Retry" }));
    // It can be retried, by someone else, so it does not say it cannot.
    assert.ok(!screen.queryByText(/can't be retried/));
    assert.ok(
      !screen.queryByText(/credentials/),
      "a turn's actor does not say whose credentials it used",
    );
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
  await waitFor(() =>
    assert.ok(net?.seen.some((r) => r.method === "POST" && r.path === "/api/runs")),
  );
});

test("a reader's own turn from before humagen said whom a run acts for may be asked again", async () => {
  // Recorded before actor.user existed: the person who asked, and no more.
  const before = { id: "ben", display: "Ben" };
  open(failed(before, "api-r-0123456789abcdefghijkl"), "ben");
  fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
  await waitFor(() =>
    assert.ok(net?.seen.some((r) => r.method === "POST" && r.path === "/api/runs")),
  );
  close();
  open(answered(before, "api-3-ben-k1"), "ben");
  fireEvent.click(await screen.findByRole("button", { name: "Regenerate" }));
  await waitFor(() =>
    assert.ok(net?.seen.some((r) => r.method === "POST" && r.path === "/api/runs")),
  );
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

const record = {
  id,
  project: "",
  channel: "general",
  visibility: "shared",
  participants: [],
  version: 0,
  created_by: "ana",
  created_at: at,
  updated_at: at,
};

test("a conversation longer than one page is read from its start, and the next message continues it", async () => {
  restoreEvents = stubEventSource();
  // 201 turns: the newest page holds 200, and the first turn is on the page before it.
  const all = Array.from({ length: 201 }, (_, n) => run(n));
  const pageOf = (turns: Run[], next: string | null): ThreadPage =>
    ({
      thread: record,
      turns: turns.map((r) => ({ kind: "run", run: r })),
      next,
      answers: { agent: "helper", from: "project:~" },
    }) as unknown as ThreadPage;
  net = stubFetch({
    [`GET /api/threads/${id}`]: (_, q) =>
      q.get("before") === "c1" ? pageOf(all.slice(0, 1), null) : pageOf(all.slice(1), "c1"),
    "GET /api/projects/~/agents": () => ({
      version: 1,
      available: { all: true, names: [] },
      homes: [],
      default_resolved: { agent: "helper", from: "project:~" },
    }),
    "GET /api/runs/r200/events": () => ({ events: [] }),
    "POST /api/threads/api-r-conversation/messages": (body) => ({
      thread: record,
      message: {
        id: "m-1",
        thread: id,
        by: "ana",
        name: "Ana",
        text: (body as { text: string }).text,
        mentions: [],
        via: "console",
        at,
      },
      runs: [],
      notified: [],
      refused: [],
      rule: "default",
    }),
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
  const input = screen.getByPlaceholderText(defaultCopy.threads.message) as HTMLTextAreaElement;
  fireEvent.change(input, { target: { value: "and then?" } });
  fireEvent.keyDown(input, { key: "Enter" });
  await waitFor(() => assert.ok(net?.seen.some((s) => s.method === "POST")));
  const sent = net?.seen.find((s) => s.method === "POST");
  // Posted to the conversation after the turn on screen. Untagged, it starts nothing.
  assert.equal(sent?.path, `/api/threads/${id}/messages`);
  assert.deepEqual(sent?.body, { text: "and then?", after: "r200" });
  // Posted, the conversation is a room: the message shows, as posted.
  assert.ok(await screen.findByRole("article", { name: "Message from Ana" }));
});

test("a turn from before humagen said whom a run acts for is not the reader's when its words were someone else's", async () => {
  // Before actor.user, the console's Retry and Regenerate asked an earlier
  // turn's words again at the same point, as an ordinary turn of whoever
  // pressed, and the Slack app asked for a person it maps under its own
  // key, whatever message the words came from.
  const anas: Run = {
    ...answered({ id: "ana", display: "Ana" }, "api-r-aaaaaaaaaaaaaaaaaaaaaa"),
    id: "t0",
    root: "t0",
  };
  const bens = { id: "ben", display: "Ben" };
  for (const turn of [
    answered(bens, "api-r-bbbbbbbbbbbbbbbbbbbbbb"),
    failed(bens, "api-r-bbbbbbbbbbbbbbbbbbbbbb"),
  ]) {
    open(turn, "ben", [anas, turn]);
    await screen.findByText(
      turn.status === "failed"
        ? "This turn acted for nobody, so it is not retried here, where it would act for you."
        : "Filed.",
    );
    assert.ok(
      !screen.queryByRole("button", { name: "Regenerate" }),
      "ben's Regenerate of ana's words is not his",
    );
    assert.ok(
      !screen.queryByRole("button", { name: "Retry" }),
      "ben's Retry of ana's words is not his",
    );
    close();
  }
  for (const turn of [
    answered(bens, "api-9-svc-slack-slack-T1-C1-1700000000-000100"),
    failed(bens, "api-9-svc-slack-slack-T1-C1-1700000000-000100"),
  ]) {
    open(turn, "ben");
    await screen.findByText(
      turn.status === "failed"
        ? "This turn acted for nobody, so it is not retried here, where it would act for you."
        : "Filed.",
    );
    assert.ok(
      !screen.queryByRole("button", { name: "Regenerate" }),
      "a turn the Slack app asked for ben is not his here",
    );
    assert.ok(!screen.queryByRole("button", { name: "Retry" }));
    close();
  }
  // His own words asked again by him are still his.
  const again = failed(bens, "api-r-bbbbbbbbbbbbbbbbbbbbbb");
  open(again, "ben", [{ ...anas, actor: bens, tag_id: "api-r-cccccccccccccccccccccc" }, again]);
  fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
  await waitFor(() =>
    assert.ok(net?.seen.some((r) => r.method === "POST" && r.path === "/api/runs")),
  );
});

test("a follow-up on a run's page is posted to the conversation, and runs only the agent it tags", async () => {
  const ana = { id: "ana", display: "Ana", user: "ana" };
  open({ ...answered(ana), agent: "helper" }, "ana");
  // No menu of who answers, and no line saying who does.
  const input = (await screen.findByPlaceholderText(
    defaultCopy.threads.message,
  )) as HTMLTextAreaElement;
  assert.equal(screen.queryByRole("button", { name: /Choose who answers/ }), null);
  fireEvent.change(input, { target: { value: "@helper and the week before?" } });
  fireEvent.keyDown(input, { key: "Enter" });
  await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "POST")));
  const sent = net?.seen.find((r) => r.method === "POST");
  assert.equal(sent?.path, "/api/threads/t1/messages");
  assert.deepEqual(sent?.body, { text: "@helper and the week before?", after: "t1" });
  assert.ok(!net?.seen.some((r) => r.method === "POST" && r.path === "/api/runs"));
});

test("a task the reader may not read reads as one that does not exist, never as Home with a composer", async () => {
  // The server answers a private thread the reader is not in as it does one
  // that is not there: 404, which stubFetch gives a route it was not told.
  net = stubFetch({ "GET /api/projects/~/agents": () => workspace("lead") });
  const noop = () => {};
  render(
    <Chat
      threadId="th-private"
      me="lee"
      summary={undefined}
      tick={0}
      agents={agents}
      homes={homes}
      projects={[]}
      canRun
      canCreateProject={false}
      greeting="Good morning, Lee"
      onThread={noop}
      onChanged={noop}
      onProjectCreated={noop}
    />,
  );
  // While it is read it is not Home either.
  assert.ok(!screen.queryByText("Good morning, Lee"));
  assert.ok(
    await screen.findByRole("heading", { level: 1, name: defaultCopy.threads.missingTask }),
  );
  assert.ok(screen.getByText(defaultCopy.projects.missingHint));
  assert.ok(!screen.queryByText("Good morning, Lee"));
  assert.ok(!screen.queryByRole("textbox"));
  assert.ok(!net?.seen.some((r) => r.method === "POST"));
});

test("completion and the + menu open toward the side of the chat bar with room, so on a phone neither runs off the screen", async () => {
  const real = HTMLFormElement.prototype.getBoundingClientRect;
  const height = window.innerHeight;
  // A phone's Home: the chat bar near the top of a tall screen.
  Object.defineProperty(window, "innerHeight", { configurable: true, value: 844 });
  HTMLFormElement.prototype.getBoundingClientRect = () =>
    ({
      top: 220,
      bottom: 330,
      left: 16,
      right: 374,
      width: 358,
      height: 110,
      x: 16,
      y: 220,
      toJSON() {},
    }) as DOMRect;
  try {
    openChat(workspace("lead"));
    fireEvent.change(box(), { target: { value: "@" } });
    const list = await screen.findByRole("listbox");
    assert.match(list.className, /(^|\s)top-full(\s|$)/, list.className);
    assert.ok(Number.parseInt(list.style.maxHeight, 10) <= 844 - 330 - 8, list.style.maxHeight);
    fireEvent.keyDown(box(), { key: "Escape" });
    fireEvent.click(screen.getByRole("button", { name: chat.plus }));
    const menu = await screen.findByRole("dialog", { name: chat.menu });
    assert.match(menu.className, /(^|\s)top-full(\s|$)/, menu.className);
    assert.ok(Number.parseInt(menu.style.maxHeight, 10) <= 844 - 330 - 8, menu.style.maxHeight);
  } finally {
    HTMLFormElement.prototype.getBoundingClientRect = real;
    Object.defineProperty(window, "innerHeight", { configurable: true, value: height });
  }
});

test("a link's agent that lives in a project opens that project; one that can't answer where it goes is taken off, and the page says so", async () => {
  // globex-helper lives in Globex: its link opens Globex, and keeps it.
  openChat(workspace("lead"), { startAgent: "globex-helper" });
  await waitFor(() => assert.ok(net?.seen.some((s) => s.path === "/api/projects/Globex/agents")));
  await waitFor(() => assert.deepEqual(chips(), ["globex-helper", "Globex"]));
  openChatClose();
  // Here only lead may answer, so helper is taken off, in words.
  openChat(
    { ...workspace("lead"), available: { all: false, names: ["lead"] } },
    { startAgent: "helper" },
  );
  await waitFor(() => assert.deepEqual(chips(), []));
  assert.ok(screen.getByText(defaultCopy.chat.notHere(["helper"])));
});

function openChatClose() {
  cleanup();
  net?.restore();
  net = undefined;
}

test("+ closes the menu it opened, though focus moves to it from the menu first", async () => {
  openChat(workspace("lead"));
  const plus = screen.getByRole("button", { name: chat.plus });
  fireEvent.click(plus);
  const menu = await screen.findByRole("dialog", { name: chat.menu });
  // A press on + moves focus there from the menu's search, then clicks it.
  fireEvent.focusOut(within(menu).getByRole("button", { name: chat.attach }), {
    relatedTarget: plus,
  });
  fireEvent.click(plus);
  assert.equal(screen.queryByRole("dialog", { name: chat.menu }), null);
  // Focus going anywhere else closes it.
  fireEvent.click(plus);
  const again = await screen.findByRole("dialog", { name: chat.menu });
  fireEvent.focusOut(within(again).getByRole("button", { name: chat.attach }), {
    relatedTarget: document.body,
  });
  assert.equal(screen.queryByRole("dialog", { name: chat.menu }), null);
});

test("all starter suggestions stay in one centered row that scrolls past its width, hiding none", async () => {
  const builtin = [
    ...agents,
    agent("humagen-analyst"),
    agent("humagen-builder"),
    agent("humagen-writer"),
  ];
  openChat(workspace("lead"), { agents: builtin });
  const list = await screen.findByRole("list", { name: chat.suggestions });
  assert.ok(list.className.split(/\s+/).includes("overflow-x-auto"));
  assert.equal(within(list).getAllByRole("button").length, 3);
  assert.ok(
    [...list.querySelectorAll("li")].every((row) => !row.className.split(/\s+/).includes("hidden")),
  );
});

test("a suggestion adds its agent as the + menu would: All of Them gives way past three, and a full selection says so", async () => {
  const builtin = [
    agent("a1"),
    agent("a2"),
    agent("a3"),
    agent("a4"),
    agent("a5"),
    agent("humagen-writer"),
  ];
  openChat(
    { ...workspace(), available: { all: true, names: [] } },
    { agents: builtin, startAgent: "a1" },
  );
  await waitFor(() => assert.deepEqual(chips(), ["a1"]));
  fireEvent.click(screen.getByRole("button", { name: chat.plus }));
  const menu = await screen.findByRole("dialog", { name: chat.menu });
  fireEvent.click(within(menu).getByRole("button", { name: /^Agents\b/ }));
  for (const n of ["a2", "a3"])
    fireEvent.click(await within(menu).findByRole("checkbox", { name: new RegExp(`^${n}`) }));
  fireEvent.keyDown(menu, { key: "Escape" });
  fireEvent.keyDown(menu, { key: "Escape" });
  fireEvent.click(screen.getByRole("button", { name: chat.all }));
  fireEvent.click(
    within(await screen.findByRole("list", { name: chat.suggestions })).getByRole("button", {
      name: /Write a report/,
    }),
  );
  await waitFor(() => assert.deepEqual(chips().slice(0, 4), ["a1", "a2", "a3", "humagen-writer"]));
  assert.equal(
    screen.getByRole("button", { name: chat.join }).getAttribute("aria-pressed"),
    "true",
  );
  // Five selected, a suggestion for another agent is refused in words.
  fireEvent.click(screen.getByRole("button", { name: chat.removeAgent("humagen-writer") }));
  fireEvent.click(screen.getByRole("button", { name: chat.plus }));
  const full = await screen.findByRole("dialog", { name: chat.menu });
  fireEvent.click(within(full).getByRole("button", { name: /^Agents\b/ }));
  for (const n of ["a4", "a5"])
    fireEvent.click(await within(full).findByRole("checkbox", { name: new RegExp(`^${n}`) }));
  fireEvent.keyDown(full, { key: "Escape" });
  fireEvent.keyDown(full, { key: "Escape" });
  assert.deepEqual(chips().slice(0, 5), ["a1", "a2", "a3", "a4", "a5"]);
  // The suggestions come back once the box is empty again.
  fireEvent.change(box(), { target: { value: "" } });
  fireEvent.click(
    within(await screen.findByRole("list", { name: chat.suggestions })).getByRole("button", {
      name: /Write a report/,
    }),
  );
  assert.ok(await screen.findByText(chat.maxAgents(5)));
  assert.ok(!chips().includes("humagen-writer"));
});

test("Home's first message says what it did in the conversation it opens", async () => {
  const { opened } = openChat(workspace("lead"));
  net?.restore();
  net = stubFetch({
    "GET /api/projects/~/agents": () => workspace("lead"),
    "POST /api/threads": (body) => ({
      thread: {
        id: "th-00000000000000ad",
        project: "",
        channel: "general",
        visibility: "private",
        participants: [],
        version: 1,
        created_by: "ada",
        created_at: at,
        updated_at: at,
      },
      message: {
        id: "m-1",
        thread: "th-00000000000000ad",
        by: "ada",
        name: "Ada",
        text: (body as { text: string }).text,
        mentions: [],
        via: "console",
        at,
      },
      runs: [],
      notified: [],
      refused: [{ kind: "agent", name: "nobody", refused: "no agent is called nobody" }],
      rule: "tagged",
    }),
  });
  fireEvent.change(box(), { target: { value: "@nobody hello" } });
  fireEvent.keyDown(box(), { key: "Enter" });
  await waitFor(() => assert.deepEqual(opened, ["th-00000000000000ad"]));
});

test("a conversation of runs has the + menu: its agents are saved on its thread so a follow-up goes to them, and its project is fixed, saying why", async () => {
  restoreEvents ??= stubEventSource();
  const ana = { id: "ana", display: "Ana", user: "ana" };
  const turn = { ...answered(ana), agent: "helper" };
  const record = {
    id: "t1",
    project: "",
    channel: "general",
    visibility: "shared",
    participants: [],
    version: 0,
    created_by: "ana",
    created_at: at,
    updated_at: at,
  };
  net = stubFetch({
    "GET /api/threads/t1": () => ({
      thread: record,
      turns: [{ kind: "run", run: turn }],
      next: null,
      answers: { agent: "", from: "" },
    }),
    "GET /api/projects/~/agents": () => workspace(),
    "GET /api/projects/~/channels": () => ({ channels: [] }),
    "PUT /api/threads/t1": (body) => ({
      ...record,
      version: 1,
      agents: (body as { agents: string[] }).agents,
    }),
  });
  render(
    <Chat
      threadId="t1"
      me="ana"
      summary={undefined}
      tick={0}
      agents={agents}
      homes={homes}
      projects={[]}
      canRun
      canCreateProject={false}
      findHelp={async () => {
        throw new Error("existing conversations never search outside their project");
      }}
      greeting=""
      onThread={() => {}}
      onChanged={() => {}}
      onProjectCreated={() => {}}
    />,
  );
  await screen.findByPlaceholderText(defaultCopy.threads.message);
  fireEvent.click(screen.getByRole("button", { name: chat.plus }));
  const menu = await screen.findByRole("dialog", { name: chat.menu });
  assert.ok(within(menu).getByText(defaultCopy.threads.shareProjectFixed));
  assert.equal(
    within(menu).queryByRole("button", { name: defaultCopy.home.findHelp.title }),
    null,
    "an existing conversation cannot offer global discovery",
  );
  fireEvent.click(within(menu).getByRole("button", { name: /^Agents\b/ }));
  fireEvent.click(await within(menu).findByRole("checkbox", { name: /^helper/ }));
  await waitFor(() => assert.ok(net?.seen.some((s) => s.method === "PUT")));
  assert.deepEqual(net?.seen.find((s) => s.method === "PUT")?.body, {
    version: 0,
    title: "",
    visibility: "shared",
    default: "",
    agents: ["helper"],
    mode: "join",
  });
  fireEvent.keyDown(menu, { key: "Escape" });
  fireEvent.keyDown(menu, { key: "Escape" });
  await waitFor(() => assert.ok(chips().includes("helper"), JSON.stringify(chips())));
  assert.ok(screen.getByPlaceholderText(chat.placeholderOne("helper")));
  // Its project shows as a chip that can't be removed.
  assert.equal(
    screen.queryByRole("button", { name: /^Remove (Acme|Workspace|Projectless)/ }),
    null,
  );
});

test("a late personal-home address never becomes visible while project titles load", async () => {
  const personal = "~personal-opaque-address";
  net = stubFetch({
    "GET /api/projects/~/agents": () => workspace(),
    [`GET /api/projects/${encodeURIComponent(personal)}/agents`]: () => workspace(),
    "GET /api/skills": () => ({ skills: [] }),
    "GET /api/mentions": () => ({ kinds: [] }),
    "GET /api/people": () => ({ people: [] }),
    "POST /api/threads": () => ({
      thread: { id: "new-personal", project: personal },
      runs: [],
      notified: [],
      refused: [],
      message: { id: "message", text: "Keep this internal" },
    }),
  });
  const props: Parameters<typeof Chat>[0] = {
    threadId: null,
    me: "ada",
    summary: undefined,
    tick: 0,
    agents,
    homes: {},
    projects: [],
    canRun: true,
    canCreateProject: false,
    greeting: "Good afternoon",
    onThread: () => {},
    onChanged: () => {},
    onProjectCreated: () => {},
  };
  const mounted = render(<Chat {...props} />);
  mounted.rerender(<Chat {...props} personalProject={personal} projects={null} />);
  const selected = await screen.findByRole("list", { name: defaultCopy.chat.selected });
  assert.ok(within(selected).getByText(defaultCopy.projects.personalTitle));
  assert.ok(!document.body.textContent?.includes(personal));
  assert.ok(!screen.queryByRole("button", { name: new RegExp(personal) }));
  assert.ok(
    !within(selected).queryByRole("button", {
      name: defaultCopy.chat.removeProject(defaultCopy.projects.personalTitle),
    }),
  );
  mounted.rerender(<Chat {...props} personalProject={personal} projects={[]} />);
  assert.ok(within(selected).getByText(defaultCopy.projects.personalTitle));
});

test("the composer opens discovery in its picker and preserves the draft and attachments when choosing a match", async () => {
  const c = defaultCopy.home.findHelp;
  const queries: string[] = [];
  const { opened } = openChat(workspace("lead"), {
    projects: [{ ...globex, access: { role: "owner", from: "", via: "*" } }] as never,
    findHelp: async (query) => {
      queries.push(query);
      return [
        {
          project: "Globex",
          title: "Globex renewal",
          agent: "globex-helper",
          reason: "Matches the renewal work",
        },
      ];
    },
  });
  fireEvent.change(box(), { target: { value: "Keep my detailed request." } });
  const picker = document.querySelector<HTMLInputElement>('input[type="file"]');
  assert.ok(picker);
  fireEvent.change(picker, {
    target: { files: [new File(["Renewal evidence"], "evidence.txt", { type: "text/plain" })] },
  });
  await screen.findByRole("list", { name: "Attached files" });
  assert.equal(
    screen.queryByRole("region", { name: c.title }),
    null,
    "discovery is absent from the resting Home page",
  );
  assert.equal(screen.queryByRole("button", { name: c.title }), null);

  fireEvent.click(screen.getByRole("button", { name: chat.plus }));
  const menu = await screen.findByRole("dialog", { name: chat.menu });
  fireEvent.click(within(menu).getByRole("button", { name: /^Agents\b/ }));
  fireEvent.click(within(menu).getByRole("button", { name: c.title }));
  const query = await within(menu).findByRole("textbox", { name: c.label });
  assert.equal((query as HTMLInputElement).value, "Keep my detailed request.");
  assert.equal(
    menu.querySelector("form"),
    null,
    "search must not nest a form inside the message form",
  );
  fireEvent.change(query, { target: { value: "renewal" } });
  fireEvent.keyDown(query, { key: "Enter" });
  await within(menu).findByText("Globex renewal");
  assert.deepEqual(queries, ["renewal"]);
  assert.equal(box().value, "Keep my detailed request.");
  assert.ok(
    !net?.seen.some((request) => request.method === "POST"),
    "search never sends the conversation",
  );
  fireEvent.click(within(menu).getByRole("button", { name: c.choose }));
  await waitFor(() => {
    assert.ok(net?.seen.some((request) => request.path === "/api/projects/Globex/agents"));
    assert.ok(chips().includes("globex-helper"), JSON.stringify(chips()));
    assert.ok(
      chips().some((chip) => chip.includes("Globex")),
      JSON.stringify(chips()),
    );
  });
  assert.equal(screen.queryByRole("dialog", { name: chat.menu }), null);
  assert.equal(box().value, "Keep my detailed request.");
  assert.ok(within(screen.getByRole("list", { name: "Attached files" })).getByText("evidence.txt"));
  assert.equal(document.activeElement, box());
  assert.ok(
    !net?.seen.some((request) => request.method === "POST"),
    "choosing prepares work without starting it",
  );

  fireEvent.keyDown(box(), { key: "Enter" });
  await waitFor(() => assert.deepEqual(opened, ["th-00000000000000ab"]));
  const started = net?.seen.find(
    (request) => request.method === "POST" && request.path === "/api/threads",
  )?.body as { project: string; channel: string; agents: string[]; text: string };
  assert.equal(started.project, "Globex");
  assert.equal(started.channel, "general");
  assert.deepEqual(started.agents, ["globex-helper"]);
  assert.equal(
    started.text,
    "Keep my detailed request.\n\nAttached file: evidence.txt\n```\nRenewal evidence\n```",
  );
});

test("discovery seeds an empty composer only after an explicit match choice", async () => {
  const c = defaultCopy.home.findHelp;
  openChat(workspace("lead"), {
    projects: [{ ...globex, access: { role: "owner", from: "", via: "*" } }] as never,
    findHelp: async () => [
      {
        project: "Globex",
        title: "Globex renewal",
        agent: "globex-helper",
        reason: "Matches your work",
      },
    ],
  });
  fireEvent.click(screen.getByRole("button", { name: chat.plus }));
  const menu = await screen.findByRole("dialog", { name: chat.menu });
  fireEvent.click(within(menu).getByRole("button", { name: /^Agents\b/ }));
  fireEvent.click(within(menu).getByRole("button", { name: c.title }));
  fireEvent.change(await within(menu).findByRole("textbox", { name: c.label }), {
    target: { value: "Plan a customer renewal" },
  });
  fireEvent.click(within(menu).getByRole("button", { name: c.search }));
  await within(menu).findByText("Globex renewal");
  assert.equal(box().value, "", "search alone does not change the draft");
  fireEvent.click(within(menu).getByRole("button", { name: c.choose }));
  await waitFor(() => assert.equal(box().value, "Plan a customer renewal"));
  assert.ok(chips().includes("globex-helper"), JSON.stringify(chips()));
  assert.ok(!net?.seen.some((request) => request.method === "POST"));
});

test("Escape returns from discovery to its picker trigger before closing the picker", async () => {
  const c = defaultCopy.home.findHelp;
  openChat(workspace("lead"), { findHelp: async () => [] });
  fireEvent.click(screen.getByRole("button", { name: chat.plus }));
  const menu = await screen.findByRole("dialog", { name: chat.menu });
  fireEvent.click(within(menu).getByRole("button", { name: /^Agents\b/ }));
  fireEvent.click(within(menu).getByRole("button", { name: c.title }));
  const query = await within(menu).findByRole("textbox", { name: c.label });
  await waitFor(() => assert.equal(document.activeElement, query));
  fireEvent.keyDown(query, { key: "Escape" });
  assert.ok(screen.getByRole("dialog", { name: chat.menu }));
  assert.equal(within(menu).queryByRole("textbox", { name: c.label }), null);
  const trigger = within(menu).getByRole("button", { name: c.title });
  await waitFor(() => assert.equal(document.activeElement, trigger));
  fireEvent.keyDown(trigger, { key: "Escape" });
  const agents = within(menu).getByRole("button", { name: /^Agents\b/ });
  await waitFor(() => assert.equal(document.activeElement, agents));
  fireEvent.keyDown(agents, { key: "Escape" });
  assert.equal(!!screen.queryByRole("dialog", { name: chat.menu }), false);
  assert.equal(document.activeElement, box());
});

test("the Personal project control opens a searchable chooser and keeps an unsent draft when switching", async () => {
  openChat(workspace("lead"), { personalProject: "Personal", projects: [globex] as never });
  fireEvent.change(box(), { target: { value: "Preserve this draft" } });
  fireEvent.click(screen.getByRole("button", { name: "Select project: Personal" }));
  const drawer = await screen.findByRole("dialog", { name: chat.chooseProject });
  assert.ok(within(drawer).getByRole("button", { name: "Personal" }));
  fireEvent.change(within(drawer).getByRole("searchbox", { name: defaultCopy.projects.search }), {
    target: { value: "Globex" },
  });
  fireEvent.click(within(drawer).getByRole("button", { name: /^Globex/ }));
  await waitFor(() => assert.ok(screen.getByRole("button", { name: "Select project: Globex" })));
  assert.equal(box().value, "Preserve this draft");
  assert.ok(!net?.seen.some((request) => request.method === "POST"));
});

test("a starter suggestion keeps the selected project and waits for explicit submission", async () => {
  const builtin = [...agents, agent("humagen-writer")];
  openChat(workspace("lead"), { startIn: "Globex", projects: [globex] as never, agents: builtin });
  const list = await screen.findByRole("list", { name: chat.suggestions });
  fireEvent.click(within(list).getByRole("button", { name: /Write a report/ }));
  await waitFor(() => assert.equal(box().value, "Write a one-page report on "));
  assert.ok(screen.getByRole("button", { name: "Select project: Globex" }));
  assert.ok(!net?.seen.some((request) => request.method === "POST"));
});

test("choosing a project skill keeps the draft and attached file and waits for explicit Send", async () => {
  const { opened } = openChat(workspace("lead"));
  fireEvent.change(box(), { target: { value: "Review this launch change." } });
  const picker = document.querySelector<HTMLInputElement>('input[type="file"]');
  assert.ok(picker);
  fireEvent.change(picker, {
    target: { files: [new File(["Launch evidence"], "launch.txt", { type: "text/plain" })] },
  });
  await screen.findByRole("list", { name: "Attached files" });
  fireEvent.click(screen.getByRole("button", { name: chat.plus }));
  const menu = await screen.findByRole("dialog", { name: chat.menu });
  fireEvent.click(within(menu).getByRole("button", { name: /^Skills\b/ }));
  fireEvent.click(await within(menu).findByRole("button", { name: /^\$?review(?:\s|$)/i }));
  await waitFor(() => assert.equal(box().value, "$review Review this launch change."));
  assert.equal(!!screen.queryByRole("dialog", { name: chat.menu }), false);
  assert.ok(within(screen.getByRole("list", { name: "Attached files" })).getByText("launch.txt"));
  assert.equal(document.activeElement === box(), true);
  assert.equal(
    net?.seen.filter((request) => request.method === "POST").length,
    0,
    "choosing a skill never starts work",
  );
  fireEvent.click(screen.getByRole("button", { name: "Send message" }));
  await waitFor(() => assert.deepEqual(opened, ["th-00000000000000ab"]));
  const started = net?.seen.find(
    (request) => request.method === "POST" && request.path === "/api/threads",
  )?.body as { text: string };
  assert.equal(
    started.text,
    "$review Review this launch change.\n\nAttached file: launch.txt\n```\nLaunch evidence\n```",
  );
});
