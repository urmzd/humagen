import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@warp/components";
import type { Turn } from "@warp/core";
import { WebSurface } from "@warp/render-web";
import { AgentMessage } from "@warp/web";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useState } from "react";
import type { Revision, ThreadPage, ThreadTurn } from "./api.ts";
import { liveEventSource, stubEventSource, stubFetch } from "./dom.ts";
import { PlusMenu, type Selection, SelectionChips } from "./Plus.tsx";
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

test("a shared thread shows people by name and agents by theirs and where it is shared, with no line saying who answers and no Share", async () => {
  const { opened } = room();
  assert.ok(await screen.findByText("Ana"));
  // The thread is untitled, so its first message is its title as well.
  assert.equal(screen.getAllByText("@helper when does Globex renew?").length, 2);
  assert.ok(screen.getByText("It renews on May 3."));
  const where = screen.getByText(words.sharedIn(globex.title ?? "Globex", "renewals"));
  // Nobody answers untagged messages in the console, so nothing says who does.
  assert.ok(!screen.queryByText(words.answers("helper")));
  assert.ok(!screen.queryByText(words.nobody));
  // A shared conversation is shared already.
  assert.ok(!screen.queryByRole("button", { name: words.share }));
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

test("@ opens completion, Enter picks the highlighted, and an untagged message posts as the person with no Post Only and no run", async () => {
  const noted = { id: "m-2", thread: "th-0123456789abcdef", by: "ana", name: "Ana", text: "noted for the record", mentions: [], via: "console", at };
  room({ "POST /api/threads/th-0123456789abcdef/messages": () => ({ thread: page.thread, message: noted, runs: [], notified: [], refused: [], rule: "default", reason: "Nobody answers untagged messages here." }) });
  const box = (await screen.findByPlaceholderText(words.channel("renewals"))) as HTMLTextAreaElement;
  assert.ok(!screen.queryByText(words.postOnly), "there is no Post Only");
  fireEvent.change(box, { target: { value: "ask @he" } });
  const list = await screen.findByRole("listbox");
  assert.ok(list.textContent?.includes("@helper"));
  fireEvent.keyDown(box, { key: "Enter" });
  await waitFor(() => assert.equal(box.value, "ask @helper "));
  fireEvent.change(box, { target: { value: "noted for the record" } });
  fireEvent.keyDown(box, { key: "Enter" });
  await waitFor(() => assert.ok(net?.seen.some((s) => s.method === "POST")));
  const sent = net?.seen.find((s) => s.method === "POST");
  assert.deepEqual(sent?.body, { text: "noted for the record" });
  // It shows as posted at once, and says nothing about who answers.
  assert.ok(await screen.findByText("noted for the record"));
  assert.ok(!screen.queryByText("Nobody answers untagged messages here."));
  assert.equal(screen.queryAllByRole("article", { name: /^Answered by/ }).length, 1);
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
  assert.ok(!screen.queryByText(words.nobody));
  fireEvent.click(screen.getByText("Globex renewal"));
  const box = screen.getByPlaceholderText(words.startThread);
  fireEvent.change(box, { target: { value: "who knows the PO?" } });
  fireEvent.keyDown(box, { key: "Enter" });
  await waitFor(() => assert.equal(opened.length, 2));
  assert.deepEqual(opened, ["th-0123456789abcdef", "th-00000000000000ff"]);
  const started = net?.seen.find((s) => s.method === "POST" && s.path === "/api/threads");
  assert.deepEqual(started?.body, { project: "Globex", channel: "renewals", text: "who knows the PO?", visibility: "shared" });
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

test("a channel's chat bar offers no Private and no Post Only, and a private thread's row says it is private", async () => {
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
  assert.ok(screen.getByPlaceholderText(words.startThread));
  assert.ok(!screen.queryByText(words.startPrivate, { selector: "button" }));
  assert.ok(!screen.queryByText(words.postOnly, { selector: "button" }));
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

/** A private conversation of one untagged message: Ana's, in Globex, with no run yet. */
const mine: ThreadPage = {
  thread: { ...page.thread, id: "th-00000000000000aa", channel: "general", visibility: "private", participants: [{ kind: "person", name: "ana", owner: true, at }] },
  turns: [{ kind: "message", message: { id: "m-1", thread: "th-00000000000000aa", by: "ana", name: "Ana", text: "thinking out loud about the renewal", mentions: [], via: "console", at } }],
  next: null,
  answers: { agent: "", from: "" },
};

/** A private conversation whose Share dialog opens and closes at its address, as App keeps it. */
function Sharing({ initial, onShared }: { initial: ThreadPage; onShared?: (open: boolean) => void }) {
  const [open, setOpen] = useState(false);
  return (
    <ThreadRoom
      id={initial.thread.id}
      me="ana"
      initial={initial}
      tick={0}
      agents={[helper]}
      projects={tree}
      root={null}
      workspaceName="Acme"
      canRun
      sharing={open}
      onSharing={(next) => {
        setOpen(next);
        onShared?.(next);
      }}
      onOpenChannel={() => {}}
      onChanged={() => {}}
    />
  );
}

const people = { people: [{ subject: "00u-dana", name: "Dana Scully", handle: "dana", picture: null }, { subject: "ana", name: "Ana", handle: "ana", picture: null }] };

test("$ completes the project's skills and @kind: completes a mention kind's items; Tab picks and Escape closes", async () => {
  net = stubFetch({
    "GET /api/threads/th-00000000000000aa": () => mine,
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/people": () => ({ people: [], next: null }),
    "GET /api/skills": () => ({
      skills: [
        { name: "review", description: "Reviews a change", argument_hint: "<pr>", from: "" },
        { name: "triage", description: "Sorts issues", argument_hint: "", from: "Globex" },
      ],
    }),
    "GET /api/mentions": (_, q) =>
      q.get("kind") === "skill"
        ? { items: [{ name: "review", description: "Reviews a change" }, { name: "release-notes" }].filter((i) => i.name.startsWith(q.get("q") ?? "")) }
        : {
            kinds: [
              { kind: "sme", title: "Experts", resolves: "experts" },
              { kind: "skill", title: "Skills", resolves: "skills" },
            ],
          },
  });
  render(<Sharing initial={mine} />);
  const box = (await screen.findByPlaceholderText(words.message)) as HTMLTextAreaElement;
  await waitFor(() => assert.ok(net?.seen.some((s) => s.path.startsWith("/api/skills?project=Globex"))));
  await waitFor(() => assert.ok(net?.seen.some((s) => s.path.startsWith("/api/mentions?project=Globex"))));

  // $ offers skills alone.
  fireEvent.change(box, { target: { value: "@helper $" } });
  let list = await screen.findByRole("listbox");
  await waitFor(() => assert.ok(list.textContent?.includes("$review") && list.textContent.includes("$triage"), list.textContent ?? ""));
  assert.ok(!list.textContent?.includes("@helper"), list.textContent ?? "");
  fireEvent.change(box, { target: { value: "@helper $tr" } });
  fireEvent.keyDown(box, { key: "Tab" });
  await waitFor(() => assert.equal(box.value, "@helper $triage "));
  assert.equal(screen.queryByRole("listbox"), null);

  // @ offers the mention kinds as kind:, and a kind picked opens its items.
  fireEvent.change(box, { target: { value: "@sk" } });
  list = await screen.findByRole("listbox");
  assert.ok(list.textContent?.includes("@skill:"), list.textContent ?? "");
  fireEvent.keyDown(box, { key: "Tab" });
  await waitFor(() => assert.equal(box.value, "@skill:"));
  await waitFor(() => assert.ok(net?.seen.some((s) => s.path.includes("kind=skill")), JSON.stringify(net?.seen.map((s) => s.path))));
  list = await screen.findByRole("listbox");
  await waitFor(() => assert.ok(list.textContent?.includes("@skill:release-notes"), list.textContent ?? ""));
  fireEvent.keyDown(box, { key: "ArrowDown" });
  fireEvent.keyDown(box, { key: "Tab" });
  await waitFor(() => assert.equal(box.value, "@skill:release-notes "));

  // Escape closes the list and leaves the words.
  fireEvent.change(box, { target: { value: "@he" } });
  await screen.findByRole("listbox");
  fireEvent.keyDown(box, { key: "Escape" });
  assert.equal(screen.queryByRole("listbox"), null);
  assert.equal(box.value, "@he");
});

test("a message that tags an agent starts its run, shown with the agent's answer, after messages that started nothing", async () => {
  const restore = stubEventSource();
  try {
    const asked = { id: "m-2", thread: "th-00000000000000aa", by: "ana", name: "Ana", text: "@helper what do you make of it?", mentions: [{ kind: "agent" as const, name: "helper", run: "r2" }], via: "console", at };
    const run = { id: "r2", agent: "helper", status: "running", text: asked.text, message: "m-2", prev: "m-2", thread: "th-00000000000000aa", calls: 0, created_at: at, updated_at: at } as never;
    net = stubFetch({
      "GET /api/threads/th-00000000000000aa": () => mine,
      "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
      "POST /api/threads/th-00000000000000aa/messages": () => ({ thread: mine.thread, message: asked, runs: [run], notified: [], refused: [], rule: "tagged" }),
      "GET /api/runs/r2/events": () => ({ events: [] }),
    });
    render(<Sharing initial={mine} />);
    const box = (await screen.findByPlaceholderText(words.message)) as HTMLTextAreaElement;
    assert.equal(screen.queryAllByRole("article", { name: /^Answered by/ }).length, 0, "the untagged message started nothing");
    fireEvent.change(box, { target: { value: asked.text } });
    fireEvent.keyDown(box, { key: "Enter" });
    assert.ok(await screen.findByRole("article", { name: "Answered by helper" }));
    // Both messages are there: the one that started nothing, then the one that tagged helper.
    assert.equal(screen.getAllByRole("article", { name: "Message from Ana" }).length, 2);
  } finally {
    // Unmounted before the stream goes, so no live run opens one after it.
    cleanup();
    restore();
  }
});

test("Share at the top of a private conversation moves it into a project's channel and adds people, then shows it shared", async () => {
  const opened: boolean[] = [];
  const shared: ThreadPage = {
    ...mine,
    thread: { ...mine.thread, project: "Initech", channel: "general", visibility: "shared", participants: [...mine.thread.participants, { kind: "person", name: "dana", at }] },
  };
  net = stubFetch({
    "GET /api/threads/th-00000000000000aa": () => mine,
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/projects/Initech/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/projects/Globex/channels": () => ({ channels: [{ project: "Globex", name: "general", version: 1, created_at: at, updated_at: at, answers: { agent: "", from: "" } }] }),
    "GET /api/projects/Initech/channels": () => ({
      channels: ["general", "deals"].map((name) => ({ project: "Initech", name, version: 1, created_at: at, updated_at: at, answers: { agent: "", from: "" } })),
    }),
    "GET /api/people": () => people,
    "POST /api/threads/th-00000000000000aa/share": () => shared,
  });
  render(<Sharing initial={mine} onShared={(o) => opened.push(o)} />);
  assert.ok(await screen.findByText(words.private));
  // Share sits at the top, not under the chat bar.
  const share = screen.getByRole("button", { name: words.share });
  assert.ok(share.closest("header"), "Share is in the conversation's header");
  fireEvent.click(share);
  const dialog = await screen.findByRole("dialog", { name: words.shareTitle });
  const project = within(dialog).getByLabelText(words.shareProject) as HTMLSelectElement;
  assert.equal(project.value, "Globex");
  assert.equal(project.disabled, false, "no agent has run here, so the project may change");
  fireEvent.change(project, { target: { value: "Initech" } });
  const channel = within(dialog).getByLabelText(words.shareChannel) as HTMLSelectElement;
  await waitFor(() => assert.ok([...channel.options].some((o) => o.value === "deals")));
  assert.equal(channel.value, "general", "general is the default");

  // People are completed as they are typed, never the person sharing.
  const who = within(dialog).getByRole("combobox", { name: words.shareAdd }) as HTMLInputElement;
  fireEvent.change(who, { target: { value: "a" } });
  const found = await within(dialog).findByRole("listbox");
  await waitFor(() => assert.ok(found.textContent?.includes("@dana"), found.textContent ?? ""));
  assert.ok(!found.textContent?.includes("@ana"), found.textContent ?? "");
  fireEvent.keyDown(who, { key: "Tab" });
  assert.ok(await within(dialog).findByRole("button", { name: words.shareRemove("dana") }));

  fireEvent.click(within(dialog).getByRole("button", { name: words.share }));
  await waitFor(() => assert.ok(net?.seen.some((s) => s.method === "POST")));
  const sent = net?.seen.find((s) => s.method === "POST");
  assert.equal(sent?.path, "/api/threads/th-00000000000000aa/share");
  assert.deepEqual(sent?.body, { project: "Initech", channel: "general", add: ["dana"] });
  // Shared, it says where, and offers no Share.
  assert.ok(await screen.findByText(words.sharedIn("Initech", "general")));
  assert.equal(screen.queryByRole("dialog"), null);
  assert.equal(screen.queryByRole("button", { name: words.share }), null);
  assert.deepEqual(opened, [true, false]);
});

test("the project is fixed once an agent has run, and a project the server will not move it to says why", async () => {
  const ran: ThreadPage = { ...mine, turns: [...mine.turns, page.turns[1] as ThreadTurn] };
  net = stubFetch({
    "GET /api/threads/th-00000000000000aa": () => ran,
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/projects/Globex/channels": () => ({ channels: [] }),
    "GET /api/projects/Initech/channels": () => ({ channels: [] }),
    "GET /api/runs/r1/events": () => ({ events: [] }),
    "POST /api/threads/th-00000000000000aa/share": () =>
      new Response(JSON.stringify({ error: "this thread has runs, so its project can't change" }), { status: 409 }),
  });
  render(<Sharing initial={ran} />);
  fireEvent.click(await screen.findByRole("button", { name: words.share }));
  let dialog = await screen.findByRole("dialog", { name: words.shareTitle });
  const fixed = within(dialog).getByLabelText(new RegExp(`^${words.shareProject}`)) as HTMLSelectElement;
  assert.equal(fixed.disabled, true);
  assert.ok(within(dialog).getByText(words.shareProjectFixed));
  cleanup();
  net?.restore();

  // A run the page has not read yet: the server refuses the move (409), and the dialog says why.
  net = stubFetch({
    "GET /api/threads/th-00000000000000aa": () => mine,
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/projects/Globex/channels": () => ({ channels: [] }),
    "GET /api/projects/Initech/channels": () => ({ channels: [] }),
    "POST /api/threads/th-00000000000000aa/share": () =>
      new Response(JSON.stringify({ error: "this thread has runs, so its project can't change" }), { status: 409 }),
  });
  render(<Sharing initial={mine} />);
  fireEvent.click(await screen.findByRole("button", { name: words.share }));
  dialog = await screen.findByRole("dialog", { name: words.shareTitle });
  fireEvent.change(within(dialog).getByLabelText(words.shareProject), { target: { value: "Initech" } });
  fireEvent.click(within(dialog).getByRole("button", { name: words.share }));
  assert.ok(await within(dialog).findByText(words.shareConflict));
  // The refused project goes back to where the conversation is, and stays.
  const moved = within(dialog).getByLabelText(new RegExp(`^${words.shareProject}`)) as HTMLSelectElement;
  assert.equal(moved.value, "Globex");
  assert.equal(moved.disabled, true);
  // Still private, and the dialog stays open to share it where it is.
  assert.ok(screen.getByText(words.private));
});

test("agents checked in a conversation's + menu are saved on the thread, and an answer they were routed to says so", async () => {
  const restore = stubEventSource();
  try {
    const routedRun = { id: "r3", agent: "helper", status: "completed", answer: "Renewal is on track.", text: "how is it going?", message: "m-1", prev: "m-1", thread: "th-00000000000000aa", calls: 0, created_at: at, updated_at: at } as never;
    const saved = { ...mine.thread, agents: ["helper", "lead"], mode: "all" as const, version: 2 };
    const lead = { spec: { ...helper.spec, name: "lead", description: "Coordinates" }, rev: 1 } as unknown as Revision;
    let current = mine;
    net = stubFetch({
      "GET /api/threads/th-00000000000000aa": () => current,
      "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
      "GET /api/projects/Globex/channels": () => ({ channels: [] }),
      "PUT /api/threads/th-00000000000000aa": () => saved,
      "GET /api/runs/r3/events": () => ({ events: [] }),
    });
    render(
      <ThreadRoom id={mine.thread.id} me="ana" initial={mine} tick={0} agents={[helper, lead]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />,
    );
    fireEvent.click(await screen.findByRole("button", { name: defaultCopy.chat.plus }));
    const menu = await screen.findByRole("dialog", { name: defaultCopy.chat.menu });
    fireEvent.click(await within(menu).findByRole("checkbox", { name: /^helper/ }));
    await waitFor(() => assert.ok(net?.seen.some((s) => s.method === "PUT")));
    assert.deepEqual(net?.seen.find((s) => s.method === "PUT")?.body, { version: 1, title: "", visibility: "private", default: "", agents: ["helper"], mode: "best_fit" });
    cleanup();

    // Read back, the selection is the thread's, for everyone in it; a run its message did not tag was routed.
    const routed: ThreadPage = { ...mine, thread: saved, turns: [...mine.turns, { kind: "run", run: routedRun }] };
    current = routed;
    render(
      <ThreadRoom id={mine.thread.id} me="ana" initial={routed} tick={0} agents={[helper, lead]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />,
    );
    const row = await screen.findByRole("list", { name: defaultCopy.chat.selected });
    assert.ok(within(row).getByText("helper") && within(row).getByText("lead"));
    assert.equal(within(row).getByRole("button", { name: defaultCopy.chat.all }).getAttribute("aria-pressed"), "true");
    assert.ok(screen.getByText(defaultCopy.chat.routedAll("helper")));
    // The project of a conversation is not removed from its chip.
    assert.equal(within(row).queryByRole("button", { name: /^Remove Globex/ }), null);
    cleanup();
  } finally {
    restore();
  }
});

test("someone added to a private conversation is not offered Share: its starter, or an owner of its project, publishes it", async () => {
  const added: ThreadPage = { ...mine, thread: { ...mine.thread, created_by: "ben" } };
  const member = tree.map((p) => (p.name === "Globex" ? { ...p, access: { role: "member" as const, from: "Globex", via: "*" } } : p));
  net = stubFetch({
    "GET /api/threads/th-00000000000000aa": () => added,
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
  });
  render(
    <ThreadRoom id={added.thread.id} me="ana" initial={added} tick={0} agents={[helper]} projects={member} root={null} workspaceName="Acme" canRun sharing={false} onSharing={() => {}} onOpenChannel={() => {}} onChanged={() => {}} />,
  );
  assert.ok(await screen.findByText(words.private));
  assert.equal(screen.queryByRole("button", { name: words.share }), null);
  cleanup();
  // An owner of the project may.
  render(
    <ThreadRoom id={added.thread.id} me="ana" initial={added} tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun sharing={false} onSharing={() => {}} onOpenChannel={() => {}} onChanged={() => {}} />,
  );
  assert.ok(await screen.findByRole("button", { name: words.share }));
});

test("two quick changes in a conversation's + menu are saved one after the other, the second from the version the first answered", async () => {
  const lead = { spec: { ...helper.spec, name: "lead", description: "Coordinates" }, rev: 1 } as unknown as Revision;
  let release: (() => void) | undefined;
  const puts: unknown[] = [];
  net = stubFetch({
    "GET /api/threads/th-00000000000000aa": () => mine,
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/projects/Globex/channels": () => ({ channels: [] }),
    "PUT /api/threads/th-00000000000000aa": (body) => {
      puts.push(body);
      const b = body as { version: number; agents: string[] };
      const answer = { ...mine.thread, agents: b.agents, mode: "best_fit", version: b.version + 1 };
      // The first save is slow, so the second change is made while it is in flight.
      return puts.length === 1 ? new Promise((resolve) => (release = () => resolve(answer))) : answer;
    },
  });
  render(
    <ThreadRoom id={mine.thread.id} me="ana" initial={mine} tick={0} agents={[helper, lead]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />,
  );
  fireEvent.click(await screen.findByRole("button", { name: defaultCopy.chat.plus }));
  const menu = await screen.findByRole("dialog", { name: defaultCopy.chat.menu });
  fireEvent.click(await within(menu).findByRole("checkbox", { name: /^helper/ }));
  fireEvent.click(within(menu).getByRole("checkbox", { name: /^lead/ }));
  await waitFor(() => assert.equal(puts.length, 1));
  release?.();
  await waitFor(() => assert.equal(puts.length, 2));
  assert.deepEqual(
    puts.map((p) => [(p as { version: number }).version, (p as { agents: string[] }).agents]),
    [
      [1, ["helper"]],
      [2, ["helper", "lead"]],
    ],
  );
});

test("in a conversation an agent has run in, the arrow keys skip the project, which can't change", async () => {
  const ran: ThreadPage = { ...mine, turns: [...mine.turns, page.turns[1] as ThreadTurn] };
  net = stubFetch({
    "GET /api/threads/th-00000000000000aa": () => ran,
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/projects/Globex/channels": () => ({ channels: [] }),
    "GET /api/runs/r1/events": () => ({ events: [] }),
  });
  render(<ThreadRoom id={ran.thread.id} me="ana" initial={ran} tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />);
  fireEvent.click(await screen.findByRole("button", { name: defaultCopy.chat.plus }));
  const menu = await screen.findByRole("dialog", { name: defaultCopy.chat.menu });
  const last = await within(menu).findByRole("checkbox", { name: /^helper/ });
  last.focus();
  fireEvent.keyDown(last, { key: "ArrowDown" });
  // Past the last agent, the project is disabled, so focus wraps to the top.
  assert.equal(document.activeElement?.textContent, defaultCopy.chat.attach);
});

test("All of Them is offered for three agents at most, and a conversation checks five at most, as the platform takes them", async () => {
  const names = ["a1", "a2", "a3", "a4", "a5", "a6"];
  let sel: Selection = { agents: ["a1", "a2", "a3"], mode: "all", project: "Globex", channel: "general" };
  const { rerender } = render(<PlusMenu agents={names.map((name) => ({ name }))} selection={sel} onChange={(n) => (sel = n)} onAttach={() => {}} onClose={() => {}} />);
  // A fourth agent turns All of Them back to the best fit.
  fireEvent.click(screen.getByRole("checkbox", { name: /^a4/ }));
  assert.deepEqual(sel, { agents: ["a1", "a2", "a3", "a4"], mode: "best_fit", project: "Globex", channel: "general" });
  sel = { ...sel, agents: ["a1", "a2", "a3", "a4", "a5"] };
  rerender(<PlusMenu agents={names.map((name) => ({ name }))} selection={sel} onChange={(n) => (sel = n)} onAttach={() => {}} onClose={() => {}} />);
  const before = sel;
  fireEvent.click(screen.getByRole("checkbox", { name: /^a6/ }));
  assert.equal(sel, before, "a sixth is not checked");
  assert.ok(screen.getByText(defaultCopy.chat.maxAgents(5)));
  cleanup();
  render(<SelectionChips selection={{ ...sel, agents: ["a1", "a2", "a3", "a4"] }} onChange={() => {}} />);
  assert.equal((screen.getByRole("button", { name: defaultCopy.chat.all }) as HTMLButtonElement).disabled, true);
  cleanup();
  render(<SelectionChips selection={{ ...sel, agents: ["a1", "a2", "a3"] }} onChange={() => {}} />);
  assert.equal((screen.getByRole("button", { name: defaultCopy.chat.all }) as HTMLButtonElement).disabled, false);
});
