import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@warp/components";
import type { Turn } from "@warp/core";
import { WebSurface } from "@warp/render-web";
import { AgentMessage } from "@warp/web";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Revision, ThreadPage, ThreadTurn } from "./api.ts";
import { liveEventSource, stubEventSource, stubFetch } from "./dom.ts";
import { ChannelPage, ChannelsTab, postedLine, ThreadRoom } from "./Room.tsx";
import { globex, tree } from "./projects.fixtures.ts";

const words = defaultCopy.threads;
const at = "2026-10-01T09:00:00Z";
const helper = { spec: { name: "helper", description: "Renewals and billing", model: "offline", provider: "offline", system_prompt: "Help." }, rev: 1 } as unknown as Revision;

const page: ThreadPage = {
  thread: {
    id: "th-0123456789abcdef",
    project: "Globex",
    channel: "renewals",
    visibility: "shared",
    participants: [
      { kind: "person", name: "ana", owner: true, at },
      { kind: "agent", name: "helper", at },
    ],
    version: 1,
    created_by: "ana",
    created_at: at,
    updated_at: at,
  },
  turns: [
    {
      kind: "message",
      message: { id: "m-1", thread: "th-0123456789abcdef", by: "ana", name: "Ana", text: "@helper when does Globex renew?", mentions: [{ kind: "agent", name: "helper", run: "r1" }], via: "console", at },
    },
    {
      kind: "run",
      run: { id: "r1", agent: "helper", status: "completed", answer: "It renews on May 3.", text: "@helper when does Globex renew?", message: "m-1", prev: "m-1", thread: "th-0123456789abcdef", created_at: at, updated_at: at } as never,
    },
  ],
  next: null,
  answers: { agent: "helper", from: "channel" },
};

// Hooks at the top level run after every DOM file's tests, so this one
// restores only a fetch it stubbed.
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

function room(routes: Record<string, (body: unknown) => unknown> = {}) {
  net = stubFetch({
    "GET /api/threads/th-0123456789abcdef": () => page,
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/people": () => ({ people: [{ subject: "00u-dana", name: "Dana Scully", handle: "dana", roles: [], picture: null }], next: null }),
    "GET /api/runs/r1/events": () => ({ events: [] }),
    ...routes,
  });
  const opened: string[] = [];
  render(
    <ThreadRoom
      id="th-0123456789abcdef"
      me="ana"
      initial={page}
      tick={0}
      agents={[helper]}
      projects={tree}
      root={null}
      workspaceName="Acme"
      canRun
      onOpenChannel={(p, c) => opened.push(`${p}/${c}`)}
      onChanged={() => {}}
    />,
  );
  return { opened };
}

test("a shared thread shows people by name and agents by theirs, where it is shared, and who answers", async () => {
  const { opened } = room();
  assert.ok(await screen.findByText("Ana"));
  // The thread is untitled, so its first message is its title as well.
  assert.equal(screen.getAllByText("@helper when does Globex renew?").length, 2);
  assert.ok(screen.getByText("It renews on May 3."));
  const where = screen.getByText(words.sharedIn(globex.title ?? "Globex", "renewals"));
  assert.ok(screen.getByText(words.answers("helper")));
  fireEvent.click(where);
  assert.deepEqual(opened, ["Globex/renewals"]);
});

test("what a post did names people by handle and experts a role names by the role, never by a subject", () => {
  const posted = { ...page, message: page.turns[0]?.message, runs: [], notified: ["ada", "role:finance"], refused: [], rule: "people" } as never;
  assert.equal(postedLine(posted), "Sent to @ada, role:finance.");
});

test("under *, the typeahead never offers an agent that lives in another project, which * does not reach", async () => {
  const initechBot = { spec: { ...helper.spec, name: "initech-bot", description: "Initech's own" }, rev: 1 } as unknown as Revision;
  const globexBot = { spec: { ...helper.spec, name: "globex-bot", description: "Lives here" }, rev: 1 } as unknown as Revision;
  net = stubFetch({
    "GET /api/threads/th-0123456789abcdef": () => page,
    // The platform lists the project agents on Globex's chain: globex-bot.
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: ["globex-bot"] }, homes: ["globex-bot"], default_resolved: null }),
    "GET /api/people": () => ({ people: [], next: null }),
    "GET /api/runs/r1/events": () => ({ events: [] }),
  });
  render(
    <ThreadRoom
      id="th-0123456789abcdef"
      me="ana"
      initial={page}
      tick={0}
      agents={[helper, initechBot, globexBot]}
      homes={{ "initech-bot": "Initech", "globex-bot": "Globex" }}
      projects={tree}
      root={null}
      workspaceName="Acme"
      canRun
      onOpenChannel={() => {}}
      onChanged={() => {}}
    />,
  );
  const box = (await screen.findByPlaceholderText(words.channel("renewals"))) as HTMLTextAreaElement;
  await waitFor(() => assert.ok(net?.seen.some((s) => s.path === "/api/projects/Globex/agents")));
  fireEvent.change(box, { target: { value: "ask @" } });
  const list = await screen.findByRole("listbox");
  await waitFor(() => assert.ok(list.textContent?.includes("@globex-bot"), list.textContent ?? ""));
  assert.ok(list.textContent?.includes("@helper"), list.textContent ?? "");
  assert.ok(!list.textContent?.includes("initech-bot"), list.textContent ?? "");
});

test("@ opens the typeahead, a pick writes the tag, and Post Only posts as the person and nothing more", async () => {
  room({ "POST /api/threads/th-0123456789abcdef/messages": () => ({ ...page, message: page.turns[0]?.message, runs: [], notified: [], refused: [], rule: "post_only" }) });
  const box = (await screen.findByPlaceholderText(words.channel("renewals"))) as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: "ask @he" } });
  const list = await screen.findByRole("listbox");
  assert.ok(list.textContent?.includes("@helper"));
  fireEvent.keyDown(box, { key: "Enter" });
  await waitFor(() => assert.equal(box.value, "ask @helper "));
  fireEvent.click(screen.getByText(words.postOnly));
  assert.ok(screen.getByText(words.postOnlyHint, { selector: "p" }));
  fireEvent.change(box, { target: { value: "noted for the record" } });
  fireEvent.keyDown(box, { key: "Enter" });
  await waitFor(() => assert.ok(net?.seen.some((s) => s.method === "POST")));
  const sent = net?.seen.find((s) => s.method === "POST");
  assert.deepEqual(sent?.body, { text: "noted for the record", post_only: true });
});

test("a channel lists its threads, and its composer starts a shared thread there", async () => {
  net = stubFetch({
    "GET /api/projects/Globex/channels": () => ({
      channels: [{ project: "Globex", name: "renewals", purpose: "Renewal work", version: 1, created_at: at, updated_at: at, answers: { agent: "", from: "" } }],
    }),
    "GET /api/threads": () => ({
      threads: [{ id: "th-0123456789abcdef", title: "Globex renewal", named: true, origin: "api", project: "Globex", channel: "renewals", agents: ["helper"], people: ["ana"], visibility: "shared", messages: 2, turns: 1, branches: 0, active: 0, failed: 0, created_at: at, updated_at: at }],
      next: null,
    }),
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "POST /api/threads": () => ({ thread: { ...page.thread, id: "th-00000000000000ff" }, message: page.turns[0]?.message, runs: [], notified: [], refused: [], rule: "people" }),
  });
  const opened: string[] = [];
  render(
    <ChannelPage
      project="Globex"
      channel="renewals"
      tick={0}
      agents={[helper]}
      projects={tree}
      root={null}
      workspaceName="Acme"
      canRun
      onOpenProject={() => {}}
      onOpenProjects={() => {}}
      onOpenThread={(id) => opened.push(id)}
    />,
  );
  assert.ok(await screen.findByText("Globex renewal"));
  assert.ok(screen.getByText("Renewal work"));
  assert.ok(screen.getByText(words.nobody));
  fireEvent.click(screen.getByText("Globex renewal"));
  const box = screen.getByPlaceholderText(words.startThread);
  fireEvent.change(box, { target: { value: "who knows the PO?" } });
  fireEvent.keyDown(box, { key: "Enter" });
  await waitFor(() => assert.equal(opened.length, 2));
  assert.deepEqual(opened, ["th-0123456789abcdef", "th-00000000000000ff"]);
  const started = net?.seen.find((s) => s.method === "POST" && s.path === "/api/threads");
  assert.deepEqual(started?.body, { project: "Globex", channel: "renewals", text: "who knows the PO?", post_only: false, visibility: "shared" });
});

test("a viewer of a project reads its channel and its threads, and is offered no composer in either", async () => {
  // Globex, as a viewer sees it: the server refuses their posts (403).
  const viewing = tree.map((p) => (p.name === "Globex" ? { ...p, access: { role: "viewer" as const, from: "Globex", via: "*" }, actions: [] } : p));
  net = stubFetch({
    "GET /api/projects/Globex/channels": () => ({
      channels: [{ project: "Globex", name: "renewals", purpose: "Renewal work", version: 1, created_at: at, updated_at: at, answers: { agent: "", from: "" } }],
    }),
    "GET /api/threads": () => ({ threads: [], next: null }),
    "GET /api/threads/th-0123456789abcdef": () => page,
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/people": () => ({ people: [], next: null }),
    "GET /api/runs/r1/events": () => ({ events: [] }),
  });
  render(
    <ChannelPage
      project="Globex"
      channel="renewals"
      tick={0}
      agents={[helper]}
      projects={viewing}
      root={null}
      workspaceName="Acme"
      canRun
      onOpenProject={() => {}}
      onOpenProjects={() => {}}
      onOpenThread={() => {}}
    />,
  );
  assert.ok(await screen.findByText("Renewal work"));
  assert.ok(screen.getByText(words.viewerOnly));
  assert.equal(screen.queryByPlaceholderText(words.startThread), null);
  cleanup();
  render(
    <ThreadRoom id="th-0123456789abcdef" me="cy" initial={page} tick={0} agents={[helper]} projects={viewing} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />,
  );
  assert.ok(await screen.findByText("It renews on May 3."));
  assert.ok(screen.getByText(words.viewerOnly));
  assert.equal(screen.queryByPlaceholderText(words.channel("renewals")), null);
  assert.ok(!net?.seen.some((s) => s.method === "POST"));
});

test("a private thread says so where a shared one says where it is shared", async () => {
  net = stubFetch({
    "GET /api/threads/th-0123456789abcdef": () => ({ ...page, thread: { ...page.thread, visibility: "private" } }),
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/people": () => ({ people: [], next: null }),
    "GET /api/runs/r1/events": () => ({ events: [] }),
  });
  render(
    <ThreadRoom
      id="th-0123456789abcdef"
      me="ana"
      initial={{ ...page, thread: { ...page.thread, visibility: "private" } }}
      tick={0}
      agents={[helper]}
      projects={tree}
      root={null}
      workspaceName="Acme"
      canRun
      onOpenChannel={() => {}}
      onChanged={() => {}}
    />,
  );
  assert.ok(await screen.findByText(words.private));
  assert.ok(!screen.queryByText(words.sharedIn(globex.title ?? "Globex", "renewals")));
  // A thread's own composer starts nothing, so it offers no Private.
  assert.ok(!screen.queryByText(words.startPrivate, { selector: "button" }));
});

test("Private in a channel starts a thread only the people in it read, and its row says so", async () => {
  net = stubFetch({
    "GET /api/projects/Globex/channels": () => ({
      channels: [{ project: "Globex", name: "renewals", version: 1, created_at: at, updated_at: at, answers: { agent: "", from: "" } }],
    }),
    "GET /api/threads": () => ({
      threads: [{ id: "th-0123456789abcdef", title: "Pricing", named: true, origin: "api", project: "Globex", channel: "renewals", agents: [], people: ["00u-ana"], visibility: "private", messages: 1, turns: 0, branches: 0, active: 0, failed: 0, created_at: at, updated_at: at }],
      next: null,
    }),
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "POST /api/threads": () => ({ thread: { ...page.thread, id: "th-00000000000000ff", visibility: "private" }, message: page.turns[0]?.message, runs: [], notified: [], refused: [], rule: "people" }),
  });
  render(
    <ChannelPage
      project="Globex"
      channel="renewals"
      tick={0}
      agents={[helper]}
      projects={tree}
      root={null}
      workspaceName="Acme"
      canRun
      onOpenProject={() => {}}
      onOpenProjects={() => {}}
      onOpenThread={() => {}}
    />,
  );
  assert.ok(await screen.findByText(new RegExp(`^${words.private} · `)));
  fireEvent.click(screen.getByText(words.startPrivate, { selector: "button" }));
  assert.ok(screen.getByText(words.privateHint, { selector: "p" }));
  const box = screen.getByPlaceholderText(words.startThread);
  fireEvent.change(box, { target: { value: "the discount we can offer" } });
  fireEvent.keyDown(box, { key: "Enter" });
  await waitFor(() => assert.ok(net?.seen.some((s) => s.method === "POST" && s.path === "/api/threads")));
  const started = net?.seen.find((s) => s.method === "POST" && s.path === "/api/threads");
  assert.deepEqual(started?.body, { project: "Globex", channel: "renewals", text: "the discount we can offer", post_only: false, visibility: "private" });
});

test("a project's channels open their pages, and a new one is made with a name that cannot change", async () => {
  const made: unknown[] = [];
  net = stubFetch({
    "GET /api/projects/Globex/channels": () => ({
      channels: [
        { project: "Globex", name: "general", version: 0, created_at: at, updated_at: at, answers: { agent: "auto", from: "project:~" }, slack: { client: "slack-acme", channel: "C0123GLOBEX" } },
      ],
    }),
    "POST /api/projects/Globex/channels": (body) => {
      made.push(body);
      return { project: "Globex", name: "renewals", version: 1, created_at: at, updated_at: at, answers: { agent: "auto", from: "project:~" } };
    },
  });
  const opened: string[] = [];
  render(<ChannelsTab project="Globex" canWrite onOpenChannel={(c) => opened.push(c)} />);
  fireEvent.click(await screen.findByText(words.channel("general")));
  assert.deepEqual(opened, ["general"]);
  assert.ok(screen.getByText(words.boundTo("slack-acme", "C0123GLOBEX")), "a bound channel says where it is in Slack");
  assert.ok(!screen.queryByText(words.archiveChannel));
  fireEvent.click(screen.getByText(words.newChannel));
  assert.ok(screen.getByText(words.channelForm.nameHint));
  fireEvent.change(screen.getByLabelText(words.channelForm.name, { exact: false }), { target: { value: "renewals" } });
  fireEvent.click(screen.getByText(words.createChannel));
  await waitFor(() => assert.equal(made.length, 1));
  assert.deepEqual(made[0], { name: "renewals" });
});

test("a run waiting on a person, such as an expert's answer, is shown still", async () => {
  const restore = stubEventSource();
  try {
    const run = (status: string) =>
      ({
        ...page.turns[1]?.run,
        status,
        answer: "",
        question: { id: "q1", kind: "reply", question: "What is the PO number?", asked_of: "sme:billing" },
      }) as never;
    for (const [status, bars] of [
      ["parked", 0],
      ["running", 1],
    ] as const) {
      const waiting: ThreadPage = { ...page, turns: [page.turns[0] as ThreadTurn, { kind: "run", run: run(status) }] };
      net = stubFetch({
        "GET /api/threads/th-0123456789abcdef": () => waiting,
        "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
        "GET /api/runs/r1/events": () => ({ events: [] }),
      });
      render(
        <ThreadRoom id="th-0123456789abcdef" me="ana" initial={waiting} tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />,
      );
      await screen.findByText("Ana");
      assert.equal(document.querySelectorAll(".aui-busy-bar").length, bars, `a ${status} run`);
      cleanup();
      net.restore();
      net = undefined;
    }
  } finally {
    restore();
  }
});

test("a room offers no Retry or Regenerate: a failed answer is retried from the inbox, by the person it acted for", async () => {
  const restore = stubEventSource();
  try {
    const ana = { id: "ana", display: "Ana", user: "ana" };
    const failed = { ...page.turns[1]?.run, status: "failed", answer: "", error: "provider: 529 overloaded", actor: ana } as never;
    const done = { ...page.turns[1]?.run, actor: ana } as never;
    for (const [me, run, says] of [
      ["ana", failed, words.retryFromInbox],
      ["ben", failed, defaultCopy.retryOnlyTheirs],
      ["ana", done, "It renews on May 3."],
    ] as const) {
      const shown: ThreadPage = { ...page, turns: [page.turns[0] as ThreadTurn, { kind: "run", run }] };
      net = stubFetch({
        "GET /api/threads/th-0123456789abcdef": () => shown,
        "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
        "GET /api/runs/r1/events": () => ({ events: [] }),
      });
      render(
        <ThreadRoom id="th-0123456789abcdef" me={me} initial={shown} tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />,
      );
      await screen.findByText(says);
      assert.ok(!screen.queryByRole("button", { name: "Retry" }), `Retry for ${me}`);
      assert.ok(!screen.queryByRole("button", { name: "Regenerate" }), `Regenerate for ${me}`);
      assert.ok(!screen.queryByText(/can't be retried/));
      cleanup();
      net.restore();
      net = undefined;
    }
  } finally {
    restore();
  }
});

test("a failed turn that acted for nobody says anyone who may start runs retries it from the inbox, not only its person", async () => {
  const restore = stubEventSource();
  try {
    // A turn from before threads, which a room still shows: a machine's, and the Slack app's own.
    const before = { ...page.turns[1]?.run, message: undefined, prev: undefined, status: "failed", answer: "", error: "provider: 529 overloaded" };
    for (const actor of [{ id: "svc-ci", bot: true }, { id: "svc-slack", display: "Slack app", bot: true, user: "ana" }, { id: "U0STRANGER", display: "Stranger" }]) {
      for (const me of ["ana", "ben"]) {
        const shown: ThreadPage = { ...page, turns: [{ kind: "run", run: { ...before, actor } as never }] };
        net = stubFetch({
          "GET /api/threads/th-0123456789abcdef": () => shown,
          "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
          "GET /api/runs/r1/events": () => ({ events: [] }),
        });
        render(
          <ThreadRoom id="th-0123456789abcdef" me={me} initial={shown} tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />,
        );
        await screen.findByText(words.retryNobodyFromInbox);
        assert.ok(!screen.queryByText(defaultCopy.retryOnlyTheirs), `${me} is told only its person retries a turn of ${actor.id}`);
        assert.ok(!screen.queryByRole("button", { name: "Retry" }), `Retry for ${me}`);
        cleanup();
        net.restore();
        net = undefined;
      }
    }
  } finally {
    restore();
  }
});

test("turns read earlier stay when the newest page moves on", async () => {
  const message = (n: number): ThreadTurn => ({
    kind: "message",
    message: { id: `m-${n}`, thread: "th-0123456789abcdef", by: "ana", name: "Ana", text: `turn ${n}`, mentions: [], via: "console", at },
  });
  const newest: ThreadPage = { ...page, turns: [message(2), message(3)], next: "c2" };
  const moved: ThreadPage = { ...page, turns: [message(3), message(4)], next: "c3" };
  let reads = 0;
  net = stubFetch({
    "GET /api/threads/th-0123456789abcdef": (_, q) => {
      if (q.get("before") === "c2") return { ...page, turns: [message(1)], next: null };
      return reads++ === 0 ? newest : moved;
    },
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
  });
  const props = { id: "th-0123456789abcdef", me: "ana", initial: newest, agents: [helper], projects: tree, root: null, workspaceName: "Acme", canRun: true, onOpenChannel: () => {}, onChanged: () => {} };
  const { rerender } = render(<ThreadRoom {...props} tick={0} />);
  await waitFor(() => assert.equal(reads, 1));
  fireEvent.click(await screen.findByText(words.olderTurns));
  assert.ok(await screen.findByText("turn 1", { selector: "p" }));
  rerender(<ThreadRoom {...props} tick={1} />);
  assert.ok(await screen.findByText("turn 4", { selector: "p" }));
  for (const n of [1, 2, 3, 4]) assert.equal(screen.getAllByText(`turn ${n}`, { selector: "p" }).length, 1, `turn ${n} is shown once`);
  assert.equal(screen.queryByText(words.olderTurns), null);
});

/** A thread a Slack reply to an announcement started, with the announcement as it stands. */
function seeded(announcement: NonNullable<ThreadTurn["announcement"]>): ThreadPage {
  const reply: ThreadTurn = {
    kind: "message",
    message: { id: "m-1", thread: "th-0123456789abcdef", by: "ana", name: "Ana", text: "what does it mean for renewals?", mentions: [], via: "app", at },
  };
  return {
    ...page,
    thread: { ...page.thread, channel: "general", announcement: announcement.id },
    turns: [{ kind: "announcement", announcement }, reply],
  };
}

function seededRoom(p: ThreadPage) {
  net = stubFetch({
    "GET /api/threads/th-0123456789abcdef": () => p,
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/people": () => ({ people: [], next: null }),
  });
  render(
    <ThreadRoom
      id="th-0123456789abcdef"
      me="ana"
      initial={p}
      tick={0}
      agents={[helper]}
      projects={tree}
      root={null}
      workspaceName="Acme"
      canRun
      onOpenChannel={() => {}}
      onChanged={() => {}}
    />,
  );
}

test("a thread a reply to an announcement started opens with the announcement, from whoever posted it", async () => {
  seededRoom(seeded({ id: "m-7", kind: "person", by: "dana", name: "Dana Scully", subject: "Globex renews in May", text: "Three more years.", at }));
  const first = await screen.findByRole("article", { name: `${defaultCopy.template.announcement} from Dana Scully` });
  assert.ok(first.textContent?.includes("Three more years."));
  // Untitled, the thread is called by what was announced, not by the reply.
  assert.ok(screen.getByRole("heading", { name: "Globex renews in May" }));
  const articles = screen.getAllByRole("article");
  assert.equal(articles[0], first, "the announcement comes first");
  assert.equal(articles[1]?.getAttribute("aria-label"), "Message from Ana");
});

test("a recap's announcement is the agent's, and a withdrawn one says only that", async () => {
  seededRoom(seeded({ id: "m-8", kind: "agent", by: "helper", subject: "Weekly recap", text: "Two renewals closed.", at }));
  assert.ok(await screen.findByRole("article", { name: `${defaultCopy.template.announcement} from helper` }));
  cleanup();
  net?.restore();
  seededRoom(seeded({ id: "m-9", kind: "person", by: "dana", name: "Dana Scully", withdrawn: true, at }));
  const gone = await screen.findByRole("article", { name: `${defaultCopy.template.announcement} from Dana Scully` });
  assert.ok(gone.textContent?.includes(defaultCopy.announcements.seedWithdrawn));
  assert.ok(screen.getByRole("heading", { name: "what does it mean for renewals?" }), "a withdrawn subject is not the title");
});

test("release notes seed a thread from warp itself, with no person's or agent's mark", async () => {
  seededRoom(seeded({ id: "m-10", kind: "system", by: "warp", subject: "What's new in warp 0.16.0", text: "- Slack catch-up", at }));
  const notes = await screen.findByRole("article", { name: `${defaultCopy.template.announcement} from warp` });
  assert.ok(notes.textContent?.includes("Slack catch-up"));
  assert.equal(notes.querySelector("[aria-hidden]")?.textContent, "", "no letter in a circle reads as a person called W");
  assert.ok(screen.getByRole("heading", { name: "What's new in warp 0.16.0" }));
});

/** The room with ledger parked on a question for the billing experts, as the platform says it to `ask`. */
function questionRoom(ask: ThreadTurn["ask"], me = "priya", routes: Record<string, (body: unknown) => unknown> = {}) {
  const parked = {
    ...page.turns[1]?.run,
    agent: "ledger",
    status: "parked",
    answer: "",
    question: { id: "q1", kind: "reply", prompt: "Should the credit note cover only the duplicated line?", asked_of: "sme:billing" },
  } as never;
  const waiting: ThreadPage = { ...page, turns: [page.turns[0] as ThreadTurn, { kind: "run", run: parked, ask }] };
  net = stubFetch({
    "GET /api/threads/th-0123456789abcdef": () => waiting,
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/runs/r1/events": () => ({ events: [] }),
    ...routes,
  });
  render(
    <ThreadRoom id="th-0123456789abcdef" me={me} initial={waiting} tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />,
  );
}

const forBilling = { question: "q1", yours: true, for: "experts", topic: "billing" } as const;

test("an expert's typed answer is posted in the room as their own message, and recorded as the answer", async () => {
  const restore = stubEventSource();
  try {
    const posted = { ...page, message: { id: "m-2", thread: "th-0123456789abcdef", by: "priya", name: "Priya", text: "Only the duplicated line.", mentions: [], via: "console", at }, runs: [], notified: [], refused: [], rule: "reply" };
    questionRoom(forBilling, "priya", { "POST /api/threads/th-0123456789abcdef/messages": () => posted });
    fireEvent.click(await screen.findByRole("button", { name: defaultCopy.somethingElse }));
    const box = screen.getByRole("textbox", { name: "Tell me what you need" }) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: "Only the duplicated line." } });
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "POST")));
    const sent = net?.seen.filter((r) => r.method === "POST") ?? [];
    assert.deepEqual(sent.map((r) => r.path), ["/api/threads/th-0123456789abcdef/messages"], "the answer is a message in the room, not a click on the run");
    assert.deepEqual(sent[0]?.body, { text: "Only the duplicated line.", answers: { run: "r1", question: "q1" } });
  } finally {
    restore();
  }
});

test("a question for the billing experts is not offered to anyone else, who is told whom it is for", async () => {
  const restore = stubEventSource();
  try {
    questionRoom({ ...forBilling, yours: false }, "sam");
    assert.ok(await screen.findByText(words.questionFor.experts("billing")));
    assert.ok(screen.getAllByText(words.waitingOnOthers).length > 0, "the run waits on someone else, not on the reader");
    assert.equal(screen.queryByText(defaultCopy.status.waiting), null);
    assert.ok(screen.getByText("Should the credit note cover only the duplicated line?"));
    assert.equal(screen.queryByRole("button", { name: defaultCopy.somethingElse }), null);
    assert.equal(screen.queryByRole("textbox", { name: "Tell me what you need" }), null);
    cleanup();
    net?.restore();
    // One for the person a run acts for names them by handle.
    questionRoom({ question: "q1", yours: false, for: "person", person: "ana" }, "ben");
    assert.ok(await screen.findByText(words.questionFor.person("ana")));
    assert.equal(screen.queryByRole("button", { name: defaultCopy.somethingElse }), null);
    cleanup();
    net?.restore();
    // One for a project's owners names the project when the platform does,
    // and is not offered to anyone else.
    questionRoom({ question: "q1", yours: false, for: "owners", owners_of: "Field engineering" }, "ben");
    assert.ok(await screen.findByText("This question is for the owners of Field engineering."));
    assert.equal(screen.queryByRole("button", { name: defaultCopy.somethingElse }), null);
    cleanup();
    net?.restore();
    // A project the reader may not see is never named.
    questionRoom({ question: "q1", yours: false, for: "owners" }, "ben");
    assert.ok(await screen.findByText(words.questionFor.owners("")));
    assert.equal(screen.queryByText(/question is for the owners of /), null, "no project is named");
  } finally {
    restore();
  }
});

test("an answer the platform refuses says why under the question, and keeps the words typed for it", async () => {
  const restore = stubEventSource();
  try {
    questionRoom(forBilling, "priya", {
      "POST /api/threads/th-0123456789abcdef/messages": () => new Response(JSON.stringify({ error: "forbidden" }), { status: 403 }),
    });
    fireEvent.click(await screen.findByRole("button", { name: defaultCopy.somethingElse }));
    const box = screen.getByRole("textbox", { name: "Tell me what you need" }) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: "Waive it in full." } });
    fireEvent.keyDown(box, { key: "Enter" });
    const turn = document.getElementById("turn-r1") as HTMLElement;
    assert.ok(await within(turn).findByText(defaultCopy.noPermission), "the refusal is said with the question");
    fireEvent.click(screen.getByRole("button", { name: defaultCopy.somethingElse }));
    assert.equal((screen.getByRole("textbox", { name: "Tell me what you need" }) as HTMLTextAreaElement).value, "Waive it in full.");
  } finally {
    restore();
  }
});

const q1 = { id: "q1", kind: "reply", prompt: "Should the credit note cover only the duplicated line?", asked_of: "sme:billing" };
const q2 = { id: "q2", kind: "reply", prompt: "Which date should the credit note carry?", asked_of: "sme:billing" };

/** Type words into the open question's box and send them. */
function typeAnswer(words: string) {
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.somethingElse }));
  const box = screen.getByRole("textbox", { name: "Tell me what you need" }) as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: words } });
  fireEvent.keyDown(box, { key: "Enter" });
}

test("a typed answer answers the question on the card it was typed in, though the room's record of the run still holds the one before", async () => {
  const live = liveEventSource();
  try {
    const posted = { ...page, message: { id: "m-3", thread: "th-0123456789abcdef", by: "priya", name: "Priya", text: "The first of April.", mentions: [], via: "console", at }, runs: [], notified: [], refused: [], rule: "reply" };
    questionRoom(forBilling, "priya", { "POST /api/threads/th-0123456789abcdef/messages": () => posted });
    await screen.findByRole("button", { name: defaultCopy.somethingElse });
    // q1 is answered and the run parks on q2, on its stream, before the
    // room reads the run again.
    act(() => {
      live.send({ run_id: "r1", agent: "ledger", kind: "lifecycle.accepted" });
      live.send({ run_id: "r1", agent: "ledger", kind: "lifecycle.parked", body: { question: q1 } });
      live.send({ run_id: "r1", agent: "ledger", kind: "lifecycle.resumed", body: { answer: { question_id: "q1", text: "Only the duplicated line.", by: "priya", at } } });
      live.send({ run_id: "r1", agent: "ledger", kind: "lifecycle.parked", body: { question: q2 } });
    });
    await screen.findByText(q2.prompt);
    typeAnswer("The first of April.");
    await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "POST")));
    const sent = net?.seen.filter((r) => r.method === "POST") ?? [];
    assert.deepEqual(sent.map((r) => r.path), ["/api/threads/th-0123456789abcdef/messages"], "it is said in the room");
    assert.deepEqual(sent[0]?.body, { text: "The first of April.", answers: { run: "r1", question: "q2" } });
  } finally {
    live.restore();
  }
});

test("a refusal said under a question goes once someone else settles it", async () => {
  const live = liveEventSource();
  try {
    questionRoom(forBilling, "priya", {
      "POST /api/threads/th-0123456789abcdef/messages": () => new Response(JSON.stringify({ error: "forbidden" }), { status: 403 }),
    });
    await screen.findByRole("button", { name: defaultCopy.somethingElse });
    typeAnswer("Waive it in full.");
    const turn = document.getElementById("turn-r1") as HTMLElement;
    assert.ok(await within(turn).findByText(defaultCopy.noPermission));
    // Another billing expert answers, and ledger finishes.
    act(() => {
      live.send({ run_id: "r1", agent: "ledger", kind: "lifecycle.accepted" });
      live.send({ run_id: "r1", agent: "ledger", kind: "lifecycle.parked", body: { question: q1 } });
      live.send({ run_id: "r1", agent: "ledger", kind: "lifecycle.resumed", body: { answer: { question_id: "q1", text: "Only the duplicated line.", by: "olu", at } } });
      live.send({ run_id: "r1", agent: "ledger", kind: "lifecycle.completed", body: { text: "I drafted credit note CN-0187." } });
    });
    await within(turn).findByText("I drafted credit note CN-0187.");
    assert.ok(!within(turn).queryByText(defaultCopy.noPermission), "the refusal is not left under the settled question");
  } finally {
    live.restore();
  }
});

test("the box for a typed answer grows with its words up to a few lines, then scrolls", async () => {
  const restore = stubEventSource();
  // happy-dom lays nothing out: a line of the box is 20px of its height.
  const proto = HTMLTextAreaElement.prototype;
  Object.defineProperty(proto, "scrollHeight", { configurable: true, get(this: HTMLTextAreaElement) { return 20 * this.value.split("\n").length + 4; } });
  try {
    questionRoom(forBilling);
    fireEvent.click(await screen.findByRole("button", { name: defaultCopy.somethingElse }));
    const box = screen.getByRole("textbox", { name: "Tell me what you need" }) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: "one\ntwo\nthree" } });
    await waitFor(() => assert.equal(box.style.height, "64px"));
    assert.equal(box.style.overflowY, "hidden");
    fireEvent.change(box, { target: { value: Array.from({ length: 9 }, (_, i) => `line ${i}`).join("\n") } });
    await waitFor(() => assert.equal(box.style.height, "112px"));
    assert.equal(box.style.overflowY, "auto");
  } finally {
    delete (proto as { scrollHeight?: number }).scrollHeight;
    restore();
  }
});

test("a question's buttons are one card with the question: the row carries no utility that would undo the card's own box", async () => {
  const restore = stubEventSource();
  try {
    questionRoom(forBilling);
    await screen.findByRole("button", { name: defaultCopy.somethingElse });
    const row = document.querySelector('fieldset[data-aui-block="clarification"]');
    assert.ok(row, "the question's row of actions");
    // A utility sits in a later layer than the card's rules, and would beat
    // the margin, border and padding that join the row to the question.
    assert.ok(!/(^|\s)(m|mt|p|pt|border|border-t)-0(\s|$)/.test(row.className), row.className);
  } finally {
    restore();
  }
});

test("a settled question sits between its step and the answer, and a typed answer reads as an answer, not a choice", async () => {
  const turn: Turn = {
    id: "r1",
    agent: { name: "ledger" },
    status: "done",
    text: "I drafted credit note CN-0187.",
    tasks: [{ id: "t1", title: "Ask person", status: "complete", tool: "ask_person" }],
    citations: [],
    notices: [],
    interrupts: [
      {
        type: "clarification",
        id: "q1",
        question: "Should the credit note cover only the duplicated line?",
        options: [],
        allowOther: true,
        answer: { kind: "answered", by: "priya", at, optionIds: [], other: "Only the duplicated line." },
      },
    ],
  };
  render(
    <WebSurface onAction={() => {}}>
      <AgentMessage turn={turn} />
    </WebSurface>,
  );
  const text = document.body.textContent ?? "";
  const step = text.indexOf("Ask person");
  const question = text.indexOf("Should the credit note cover only the duplicated line?");
  const answer = text.indexOf("I drafted credit note CN-0187.");
  assert.ok(step >= 0 && step < question && question < answer, `step ${step}, question ${question}, answer ${answer}`);
  assert.ok(/priya answered "Only the duplicated line\."/.test(text), text);
  assert.ok(!/priya chose/.test(text), text);
});

test("the typeahead never offers the person writing", async () => {
  room({ "GET /api/people": () => ({ people: [{ subject: "ana", name: "Ana", handle: "ana", roles: [], picture: null }, { subject: "00u-dana", name: "Dana Scully", handle: "dana", roles: [], picture: null }], next: null }) });
  const box = (await screen.findByPlaceholderText(words.channel("renewals"))) as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: "@" } });
  const list = await screen.findByRole("listbox");
  await waitFor(() => assert.ok(list.textContent?.includes("@dana"), list.textContent ?? ""));
  assert.ok(!list.textContent?.includes("@ana"), list.textContent ?? "");
});

test("a thread started in Slack says so, and so does each message posted there; the title has a line of its own on a phone", async () => {
  const restore = stubEventSource();
  try {
    const slack: ThreadPage = {
      ...page,
      thread: { ...page.thread, surface: { client: "slack-acme", channel: "C0123GLOBEX", ts: "1700000000.000100" } },
      turns: [
        { kind: "message", message: { ...(page.turns[0]?.message as NonNullable<ThreadTurn["message"]>), surface_id: "1700000000.000100", via: "slack-acme" } },
        { kind: "message", message: { id: "m-2", thread: "th-0123456789abcdef", by: "ben", name: "Ben", text: "Asked here.", mentions: [], via: "console", at } },
      ],
    };
    net = stubFetch({
      "GET /api/threads/th-0123456789abcdef": () => slack,
      "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    });
    render(
      <ThreadRoom id="th-0123456789abcdef" me="ana" initial={slack} tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />,
    );
    assert.ok(await screen.findByText(words.inSlack));
    assert.equal(screen.getAllByText(words.fromSlack).length, 1, "only the message posted in Slack says it came from there");
    const fromAna = screen.getByRole("article", { name: "Message from Ana" });
    assert.ok(within(fromAna).getByText(words.fromSlack));
    // Below the small breakpoint the title takes the whole first line.
    const title = screen.getByRole("heading", { level: 1 });
    assert.ok(/(^|\s)w-full(\s|$)/.test(title.className) && /(^|\s)sm:flex-1(\s|$)/.test(title.className), title.className);
  } finally {
    restore();
  }
});

test("a run a message in the room answered is read from its journal, so its question shows though turns came after it", async () => {
  const restore = stubEventSource();
  try {
    const answeredBy: ThreadTurn = {
      kind: "message",
      message: { id: "m-2", thread: "th-0123456789abcdef", by: "priya", name: "Priya", text: "Only the duplicated line.", mentions: [], via: "console", answers: { run: "r1", question: "q1" }, at },
    };
    const later: ThreadPage = { ...page, turns: [...page.turns, answeredBy] };
    net = stubFetch({
      "GET /api/threads/th-0123456789abcdef": () => later,
      "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
      "GET /api/runs/r1/events": () => ({ events: [] }),
    });
    render(
      <ThreadRoom id="th-0123456789abcdef" me="ana" initial={later} tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />,
    );
    await screen.findByText("Only the duplicated line.");
    await waitFor(() => assert.ok(net?.seen.some((r) => r.path === "/api/runs/r1/events"), "the answered run's journal is read"));
  } finally {
    restore();
  }
});

test("a channel of a project the reader may not see reads as the project's own page does: no channel, no threads, no composer", async () => {
  // Initech is members only and the reader is not one: every read of it
  // answers 404, which stubFetch gives a route it was not told.
  net = stubFetch({ "GET /api/people": () => ({ people: [], next: null }) });
  let back = 0;
  render(
    <ChannelPage
      project="Initech"
      channel="general"
      tick={0}
      agents={[helper]}
      projects={tree}
      root={null}
      workspaceName="Acme"
      canRun
      onOpenProject={() => {}}
      onOpenProjects={() => back++}
      onOpenThread={() => {}}
    />,
  );
  assert.ok(await screen.findByRole("heading", { level: 1, name: defaultCopy.projects.missing("Initech") }));
  assert.ok(screen.getByText(defaultCopy.projects.missingHint));
  assert.ok(!screen.queryByText("not found"));
  assert.ok(!screen.queryByText(words.channel("general")));
  assert.ok(!screen.queryByPlaceholderText(words.startThread));
  assert.ok(!screen.queryByText(words.startPrivate));
  assert.ok(!screen.queryByText(words.nobody));
  fireEvent.click(screen.getByRole("button", { name: `‹ ${defaultCopy.projects.allProjects}` }));
  assert.equal(back, 1);
});

test("a channel its project does not hold says so, and offers no composer", async () => {
  net = stubFetch({
    "GET /api/projects/Globex/channels": () => ({
      channels: [{ project: "Globex", name: "general", version: 1, created_at: at, updated_at: at, answers: { agent: "", from: "" } }],
    }),
    "GET /api/threads": () => ({ threads: [], next: null }),
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/people": () => ({ people: [], next: null }),
  });
  let back = 0;
  render(
    <ChannelPage
      project="Globex"
      channel="renewals"
      tick={0}
      agents={[helper]}
      projects={tree}
      root={null}
      workspaceName="Acme"
      canRun
      onOpenProject={() => back++}
      onOpenProjects={() => {}}
      onOpenThread={() => {}}
    />,
  );
  assert.ok(await screen.findByRole("heading", { level: 1, name: words.missingChannel }));
  assert.ok(screen.getByText("This project has no channel named #renewals. Its Channels tab lists the ones it has."));
  assert.ok(!screen.queryByPlaceholderText(words.startThread));
  fireEvent.click(screen.getByRole("button", { name: "‹ Globex" }));
  assert.equal(back, 1);
});
