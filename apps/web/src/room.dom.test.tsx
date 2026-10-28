import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import type { Turn } from "@humagen/core";
import { WebSurface } from "@humagen/render-web";
import { useRunStop } from "./RunStop.tsx";
import { AgentMessage } from "@humagen/web";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useState } from "react";
import type { Revision, ThreadPage, ThreadTurn } from "./api.ts";
import { liveEventSource, stubEventSource, stubFetch } from "./dom.ts";
import { PlusMenu, type Selection, SelectionChips } from "./Plus.tsx";
import { ChannelPage, ChannelsTab, postedLine, ThreadRoom } from "./Room.tsx";
import { ShareDialog } from "./Share.tsx";
import { globex, tree } from "./projects.fixtures.ts";

const words = defaultCopy.threads;
const at = "2026-10-01T09:00:00Z";
const helper = { spec: { name: "helper", description: "Renewals and billing", model: "offline", provider: "offline", system_prompt: "Help." }, rev: 1 } as unknown as Revision;

test("a channel's Plus picker offers explicit Company-home agents and excludes unavailable scoped homes", async () => {
  net = stubFetch({
    "GET /api/projects/Globex/channels": () => ({ channels: [{ project: "Globex", name: "renewals", version: 1, created_at: at, updated_at: at }] }),
    "GET /api/threads": () => ({ threads: [], next: null }),
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
  });
  const scoped = { ...helper, spec: { ...helper.spec, name: "scoped" } };
  render(<ChannelPage project="Globex" channel="renewals" tick={0} agents={[helper, scoped]} homes={{ helper: "~", scoped: "Vault" }} projects={tree} root={null} workspaceName="Acme" canRun onOpenProject={() => {}} onOpenProjects={() => {}} onOpenThread={() => {}} />);
  await waitFor(() => assert.ok(net?.seen.some((request) => request.path === "/api/projects/Globex/agents")));
  fireEvent.click(await screen.findByRole("button", { name: defaultCopy.chat.plus }));
  const menu = await screen.findByRole("dialog", { name: defaultCopy.chat.menu });
  fireEvent.click(within(menu).getByRole("button", { name: /^Agents\b/ }));
  const option = await within(menu).findByRole("checkbox", { name: /^helper/ });
  fireEvent.click(option);
  await waitFor(() => assert.equal(option.getAttribute("aria-checked"), "true"));
  assert.equal(within(menu).queryByRole("checkbox", { name: /^scoped/ }), null);
});

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

function room(routes: Record<string, (body: unknown) => unknown> = {}, initial: ThreadPage = page) {
  net = stubFetch({
    "GET /api/threads/th-0123456789abcdef": () => initial,
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
      initial={initial}
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
  const where = screen.getByRole("button",{name:words.sharedIn(globex.title ?? "Globex", "renewals")});
  // Nobody answers untagged messages in the console, so nothing says who does.
  assert.ok(!screen.queryByText(words.answers("helper")));
  assert.ok(!screen.queryByText(words.nobody));
  // A shared conversation is shared already.
  assert.ok(!screen.queryByRole("button", { name: words.share }));
  fireEvent.click(where);
  assert.deepEqual(opened, ["Globex/renewals"]);
});

test("message dates reveal the exact local date, time and timezone on hover, focus and tap", async () => {
  room();
  const message = await screen.findByRole("article", { name: "Message from Ana" });
  const exact = new Date(at).toLocaleString(undefined, { dateStyle: "full", timeStyle: "long" });
  const trigger = within(message).getByRole("button", { name: `Posted ${exact}` });
  assert.equal(trigger.querySelector("time")?.getAttribute("datetime"), at);
  assert.ok(!within(message).queryByRole("tooltip"));

  fireEvent.mouseEnter(trigger.parentElement!);
  assert.equal(within(message).getByRole("tooltip").textContent, exact);
  fireEvent.mouseLeave(trigger.parentElement!);
  assert.ok(!within(message).queryByRole("tooltip"));

  fireEvent.focus(trigger);
  const tooltip = within(message).getByRole("tooltip");
  assert.equal(trigger.getAttribute("aria-describedby"), tooltip.id);
  fireEvent.keyDown(trigger, { key: "Escape" });
  assert.ok(!within(message).queryByRole("tooltip"));

  fireEvent.click(trigger);
  assert.equal(within(message).getByRole("tooltip").textContent, exact);
  fireEvent.blur(trigger);
  assert.ok(!within(message).queryByRole("tooltip"));
});

test("message date details stay visible while focused after the pointer leaves and Escape keeps them dismissed", async () => {
  room();
  const message = await screen.findByRole("article", { name: "Message from Ana" });
  const exact = new Date(at).toLocaleString(undefined, { dateStyle: "full", timeStyle: "long" });
  const trigger = within(message).getByRole("button", { name: `Posted ${exact}` });

  fireEvent.mouseEnter(trigger.parentElement!);
  fireEvent.focus(trigger);
  fireEvent.mouseLeave(trigger.parentElement!);
  assert.equal(within(message).getByRole("tooltip").textContent, exact);

  fireEvent.mouseEnter(trigger.parentElement!);
  fireEvent.keyDown(trigger, { key: "Escape" });
  assert.ok(!within(message).queryByRole("tooltip"));
  fireEvent.mouseLeave(trigger.parentElement!);
  assert.ok(!within(message).queryByRole("tooltip"));
  assert.equal(trigger.getAttribute("aria-describedby"), null);

  fireEvent.blur(trigger);
  fireEvent.focus(trigger);
  assert.equal(within(message).getByRole("tooltip").textContent, exact);
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
  assert.ok(!screen.queryByRole("button",{name:words.sharedIn(globex.title ?? "Globex", "renewals")}));
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
  assert.ok(await screen.findByText(words.private));
  assert.ok(screen.getByPlaceholderText(words.startThread));
  assert.ok(!screen.queryByText(words.startPrivate, { selector: "button" }));
  assert.ok(!screen.queryByText(words.postOnly, { selector: "button" }));
});

test("a project's channels open their pages, and a new one is made with a name that cannot change", async () => {
  const made: unknown[] = [];
  net = stubFetch({
    "GET /api/projects/Globex/channels": () => ({
      channels: [
        { project: "Globex", name: "general", version: 0, created_at: at, updated_at: at, answers: { agent: "*", from: "project:~" }, slack: { client: "slack-acme", channel: "C0123GLOBEX" } },
      ],
    }),
    "POST /api/projects/Globex/channels": (body) => {
      made.push(body);
      return { project: "Globex", name: "renewals", version: 1, created_at: at, updated_at: at, answers: { agent: "*", from: "project:~" } };
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

test("release notes seed a thread from humagen itself, with no person's or agent's mark", async () => {
  seededRoom(seeded({ id: "m-10", kind: "system", by: "humagen", subject: "What's new in Humagen 0.16.0", text: "- Slack catch-up", at }));
  const notes = await screen.findByRole("article", { name: `${defaultCopy.template.announcement} from Humagen` });
  assert.ok(notes.textContent?.includes("Slack catch-up"));
  assert.equal(notes.querySelector("[aria-hidden]")?.textContent, "", "no letter in a circle reads as a person called H");
  assert.ok(screen.getByRole("heading", { name: "What's new in Humagen 0.16.0" }));
});

test("release notes posted before the rename, by warp, are from Humagen too", async () => {
  seededRoom(seeded({ id: "m-11", kind: "system", by: "warp", subject: "What's new in warp 0.15.0", text: "- Inbox", at }));
  assert.ok(await screen.findByRole("article", { name: `${defaultCopy.template.announcement} from Humagen` }));
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
      live.send({ run: "r1", agent: "ledger", type: "run.accepted.v1" });
      live.send({ run: "r1", agent: "ledger", type: "run.parked.v1", data: { question: q1 } });
      live.send({ run: "r1", agent: "ledger", type: "run.resumed.v1", data: { answer: { question_id: "q1", text: "Only the duplicated line.", by: "priya", at } } });
      live.send({ run: "r1", agent: "ledger", type: "run.parked.v1", data: { question: q2 } });
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
      live.send({ run: "r1", agent: "ledger", type: "run.accepted.v1" });
      live.send({ run: "r1", agent: "ledger", type: "run.parked.v1", data: { question: q1 } });
      live.send({ run: "r1", agent: "ledger", type: "run.resumed.v1", data: { answer: { question_id: "q1", text: "Only the duplicated line.", by: "olu", at } } });
      live.send({ run: "r1", agent: "ledger", type: "run.completed.v1", data: { text: "I drafted credit note CN-0187." } });
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

test("a settled question precedes its answer, work follows the outcome, and typed answers remain distinct from choices", async () => {
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
  assert.ok(question >= 0 && question < answer && answer < step, `step ${step}, question ${question}, answer ${answer}`);
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
    assert.equal(screen.getAllByRole("heading", { level: 1 }).length, 1, "the conversation has one page title");
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
    "POST /api/threads/th-00000000000000aa/share": () => ({ ...shared, not_added: [] }),
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
  assert.ok(await screen.findByRole("button",{name:words.sharedIn("Initech", "general")}));
  assert.equal(screen.queryByRole("dialog"), null);
  assert.equal(screen.queryByRole("button", { name: words.share }), null);
  assert.deepEqual(opened, [true, false]);
  assert.ok(screen.getByText(words.sharedTold(["dana"])));
});

test("completed work requires destination review, and an unseen run refusal stays actionable", async () => {
  const ran: ThreadPage = { ...mine, turns: [...mine.turns, page.turns[1] as ThreadTurn] };
  net = stubFetch({
    "GET /api/threads/th-00000000000000aa": () => ran,
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/projects/Globex/channels": () => ({ channels: [] }),
    "GET /api/projects/Initech/channels": () => ({ channels: [] }),
    "GET /api/runs/r1/events": () => ({ events: [] }),
    "GET /api/threads/th-00000000000000aa/placement": () => ({thread:mine.thread.id,project:"Globex",channel:"renewals",audience:"Globex members",runs:1,token:"review-token",exclusions:[]}),
    "POST /api/threads/th-00000000000000aa/share": () => ({...ran,thread:{...ran.thread,visibility:"shared"},not_added:[]}),
  });
  render(<Sharing initial={ran} />);
  fireEvent.click(await screen.findByRole("button", { name: words.share }));
  let dialog = await screen.findByRole("dialog", { name: words.shareTitle });
  const fixed = within(dialog).getByLabelText(new RegExp(`^${words.shareProject}`)) as HTMLSelectElement;
  assert.equal(fixed.disabled, false);
  const publish = within(dialog).getByRole("button", {name:words.share}) as HTMLButtonElement;
  assert.equal(publish.disabled,true);
  fireEvent.click(await within(dialog).findByRole("checkbox", {name:words.placementReview}));
  assert.equal(publish.disabled,false);
  fireEvent.click(publish);
  await waitFor(() => assert.ok(net?.seen.some((request) => request.method === "POST")));
  assert.equal((net?.seen.find((request) => request.method === "POST")?.body as {placement_token:string}).placement_token,"review-token");
  cleanup();
  net?.restore();

  // A run the page has not read yet: the server refuses the move (409), and the dialog says why.
  net = stubFetch({
    "GET /api/threads/th-00000000000000aa": () => mine,
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/projects/Globex/channels": () => ({ channels: [] }),
    "GET /api/projects/Initech/channels": () => ({ channels: [] }),
    "POST /api/threads/th-00000000000000aa/share": () =>
      new Response(JSON.stringify({ error: "a conversation that has runs stays in its project" }), { status: 409 }),
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
    const routedRun = { id: "r3", agent: "helper", status: "completed", answer: "Renewal is on track.", text: "how is it going?", message: "m-1", prev: "m-1", thread: "th-00000000000000aa", calls: 0, routed: "all", created_at: at, updated_at: at } as never;
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
    fireEvent.click(within(menu).getByRole("button", { name: /^Agents\b/ }));
    fireEvent.click(await within(menu).findByRole("checkbox", { name: /^helper/ }));
    await waitFor(() => assert.ok(net?.seen.some((s) => s.method === "PUT")));
    assert.deepEqual(net?.seen.find((s) => s.method === "PUT")?.body, { version: 1, title: "", visibility: "private", default: "", agents: ["helper"], mode: "join" });
    cleanup();

    // Read back, the selection is the thread's, for everyone in it; a run its message did not tag was routed.
    const leadRun = { ...(routedRun as object), id: "r3b", agent: "lead", answer: "On track." } as never;
    const routed: ThreadPage = { ...mine, thread: saved, turns: [...mine.turns, { kind: "run", run: routedRun }, { kind: "run", run: leadRun }] };
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
      const answer = { ...mine.thread, agents: b.agents, mode: "join", version: b.version + 1 };
      // The first save is slow, so the second change is made while it is in flight.
      return puts.length === 1 ? new Promise((resolve) => (release = () => resolve(answer))) : answer;
    },
  });
  render(
    <ThreadRoom id={mine.thread.id} me="ana" initial={mine} tick={0} agents={[helper, lead]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />,
  );
  fireEvent.click(await screen.findByRole("button", { name: defaultCopy.chat.plus }));
  const menu = await screen.findByRole("dialog", { name: defaultCopy.chat.menu });
  fireEvent.click(within(menu).getByRole("button", { name: /^Agents\b/ }));
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
  const projects = within(menu).getByRole("button", { name: /^Projects\b/ }) as HTMLButtonElement;
  assert.equal(projects.disabled, true, "an existing conversation cannot change project");
  const agents = within(menu).getByRole("button", { name: /^Agents\b/ });
  agents.focus();
  fireEvent.keyDown(agents, { key: "ArrowDown" });
  assert.match(document.activeElement?.textContent ?? "", /^Skills/);
  assert.notEqual(document.activeElement, projects, "keyboard navigation skips the locked project row");
});

test("All of Them is offered for three agents at most, and a conversation checks five at most, as the platform takes them", async () => {
  const names = ["a1", "a2", "a3", "a4", "a5", "a6"];
  let sel: Selection = { agents: ["a1", "a2", "a3"], mode: "all", project: "Globex", channel: "general" };
  const { rerender } = render(<PlusMenu agents={names.map((name) => ({ name }))} selection={sel} onChange={(n) => (sel = n)} onAttach={() => {}} onClose={() => {}} />);
  fireEvent.click(screen.getByRole("button", { name: /^Agents\b/ }));
  // A fourth agent turns All of Them back to Each Decides.
  fireEvent.click(screen.getByRole("checkbox", { name: /^a4/ }));
  assert.deepEqual(sel, { agents: ["a1", "a2", "a3", "a4"], mode: "join", project: "Globex", channel: "general" });
  sel = { ...sel, agents: ["a1", "a2", "a3", "a4", "a5"] };
  rerender(<PlusMenu agents={names.map((name) => ({ name }))} selection={sel} onChange={(n) => (sel = n)} onAttach={() => {}} onClose={() => {}} />);
  const before = sel;
  fireEvent.click(screen.getByRole("checkbox", { name: /^a6/ }));
  assert.equal(sel, before, "a sixth is not checked");
  assert.ok(screen.getByText(defaultCopy.chat.maxAgents(5)));
  cleanup();
  render(<SelectionChips selection={{ ...sel, agents: ["a1", "a2", "a3", "a4"] }} onChange={() => {}} />);
  const all = screen.getByRole("button", { name: defaultCopy.chat.all });
  assert.equal(all.getAttribute("aria-disabled"), "true");
  // Why is said to a screen reader at once, and in words on the page once it is pressed.
  const why = document.getElementById(all.getAttribute("aria-describedby") ?? "");
  assert.equal(why?.textContent, defaultCopy.chat.allMax(3));
  assert.match(why?.className ?? "", /sr-only/);
  fireEvent.click(all);
  assert.doesNotMatch(document.getElementById(all.getAttribute("aria-describedby") ?? "")?.className ?? "", /sr-only/);
  cleanup();
  render(<SelectionChips selection={{ ...sel, agents: ["a1", "a2", "a3"] }} onChange={() => {}} />);
  assert.equal(screen.getByRole("button", { name: defaultCopy.chat.all }).getAttribute("aria-disabled"), null);
});

test("a shared conversation's project remains locked in the + menu with its explanation", async () => {
  net = stubFetch({
    "GET /api/threads/th-0123456789abcdef": () => ({ ...page, turns: [page.turns[0]] }),
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/projects/Globex/channels": () => ({ channels: [] }),
  });
  const shared: ThreadPage = { ...page, turns: [page.turns[0] as ThreadTurn] };
  render(<ThreadRoom id={shared.thread.id} me="ana" initial={shared} tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />);
  fireEvent.click(await screen.findByRole("button", { name: defaultCopy.chat.plus }));
  const menu = await screen.findByRole("dialog", { name: defaultCopy.chat.menu });
  assert.ok(within(menu).getByText(defaultCopy.chat.projectShared));
  assert.equal((within(menu).getByRole("button", { name: /^Projects\b/ }) as HTMLButtonElement).disabled, true);
});

test("someone added to a private conversation may change its agents but not move it, and the + menu says who may", async () => {
  const added: ThreadPage = { ...mine, thread: { ...mine.thread, created_by: "ben" } };
  const member = tree.map((p) => (p.name === "Globex" ? { ...p, access: { role: "member" as const, from: "Globex", via: "*" } } : p));
  net = stubFetch({
    "GET /api/threads/th-00000000000000aa": () => added,
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/projects/Globex/channels": () => ({ channels: [] }),
  });
  render(<ThreadRoom id={added.thread.id} me="ana" initial={added} tick={0} agents={[helper]} projects={member} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />);
  fireEvent.click(await screen.findByRole("button", { name: defaultCopy.chat.plus }));
  const menu = await screen.findByRole("dialog", { name: defaultCopy.chat.menu });
  assert.ok(within(menu).getByText(defaultCopy.chat.projectStarter));
  assert.equal((within(menu).getByRole("button", { name: /^Projects\b/ }) as HTMLButtonElement).disabled, true);
  fireEvent.click(within(menu).getByRole("button", { name: /^Agents\b/ }));
  assert.equal((await within(menu).findByRole("checkbox", { name: /^helper/ }) as HTMLButtonElement).disabled, false);
});

test("an answer says it was routed as its run recorded it, whatever the conversation's selection is now", async () => {
  const restore = stubEventSource();
  try {
    const run = (id: string, agent: string, extra: object) =>
      ({ id, agent, status: "completed", answer: `${agent} answered.`, text: "q", message: "m-1", prev: "m-1", thread: "th-00000000000000aa", calls: 0, created_at: at, updated_at: at, ...extra }) as never;
    // Nothing is selected now; one run was routed as one that joined, one as one of all, and one was tagged.
    const read: ThreadPage = {
      ...mine,
      turns: [
        ...mine.turns,
        { kind: "run", run: run("r4", "helper", { routed: "join", picked: "Joined on its own: matched renewals." }) },
        { kind: "run", run: run("r5", "lead", { routed: "all" }) },
        { kind: "run", run: run("r6", "helper", {}) },
      ],
    };
    net = stubFetch({
      "GET /api/threads/th-00000000000000aa": () => read,
      "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    });
    render(<ThreadRoom id={read.thread.id} me="ana" initial={read} tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />);
    assert.ok(await screen.findByText(`${defaultCopy.chat.routed("helper")}. Joined on its own: matched renewals.`));
    assert.ok(screen.getByText(defaultCopy.chat.routedAll("lead")));
    assert.equal(screen.getAllByText(/^Routed to /).length, 2, "the tagged answer says nothing of routing");
    cleanup();
  } finally {
    restore();
  }
});

test("what a post did names a refused tag as written, with its kind, and says why the selected agents did not all answer", () => {
  const message = { id: "m-9", thread: "t", by: "ana", name: "Ana", text: "@team:finance and $triage, please", mentions: [], via: "console", at };
  const posted = (over: object) => ({ thread: mine.thread, message, runs: [], notified: [], refused: [], rule: "tagged", ...over }) as never;
  assert.equal(postedLine(posted({ refused: [{ kind: "role", name: "finance", refused: "nobody holds it" }] })), "@team:finance: nobody holds it");
  assert.equal(postedLine(posted({ refused: [{ kind: "skill", name: "triage", refused: "there is no skill called triage" }] })), "$triage: there is no skill called triage");
  assert.equal(postedLine(posted({ rule: "routed", reason: "No selected agent could answer: lead is not available here." })), "No selected agent could answer: lead is not available here.");
  // An untagged message where nothing is selected says nothing.
  assert.equal(postedLine(posted({ rule: "untagged", reason: "Nothing runs until you tag an agent." })), "");
});

test("a channel's first message says what it did in the conversation it opens", async () => {
  const opened: string[] = [];
  const first = { id: "m-1", thread: "th-00000000000000cc", by: "ana", name: "Ana", text: "@dana can you look?", mentions: [], via: "console", at };
  net = stubFetch({
    "GET /api/projects/Globex/channels": () => ({ channels: [{ project: "Globex", name: "renewals", version: 1, created_at: at, updated_at: at, answers: { agent: "", from: "" } }] }),
    "GET /api/threads": () => ({ threads: [], next: null }),
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "POST /api/threads": () => ({ thread: { ...page.thread, id: "th-00000000000000cc" }, message: first, runs: [], notified: ["dana"], refused: [], rule: "people" }),
    "GET /api/threads/th-00000000000000cc": () => ({ ...page, thread: { ...page.thread, id: "th-00000000000000cc" }, turns: [{ kind: "message", message: first }] }),
  });
  render(<ChannelPage project="Globex" channel="renewals" tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun onOpenProject={() => {}} onOpenProjects={() => {}} onOpenThread={(id) => opened.push(id)} />);
  const box = await screen.findByPlaceholderText(words.startThread);
  fireEvent.change(box, { target: { value: first.text } });
  fireEvent.keyDown(box, { key: "Enter" });
  await waitFor(() => assert.deepEqual(opened, ["th-00000000000000cc"]));
  cleanup();
  const room: ThreadPage = { ...page, thread: { ...page.thread, id: "th-00000000000000cc" }, turns: [{ kind: "message", message: first }] };
  render(<ThreadRoom id={room.thread.id} me="ana" initial={room} tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />);
  assert.ok(await screen.findByText(words.notified(["dana"])));
});

test("Share preserves the selected channel and invalidates consent after a stale placement", async () => {
  const ran: ThreadPage = { ...mine, thread: { ...mine.thread, channel: "deals" }, turns: [...mine.turns, page.turns[1] as ThreadTurn] };
  net = stubFetch({
    "GET /api/threads/th-00000000000000aa": () => ran,
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/projects/Globex/channels": () => ({ channels: ["general", "deals"].map((name) => ({ project: "Globex", name, version: 1, created_at: at, updated_at: at, answers: { agent: "", from: "" } })) }),
    "GET /api/runs/r1/events": () => ({ events: [] }),
    "GET /api/threads/th-00000000000000aa/placement": () => ({thread:mine.thread.id,project:"Globex",channel:"deals",audience:"Globex members",runs:1,token:"channel-token",exclusions:[]}),
    "POST /api/threads/th-00000000000000aa/share": () => new Response(JSON.stringify({ error: "a conversation that has runs stays in its channel" }), { status: 409 }),
  });
  render(<Sharing initial={ran} />);
  fireEvent.click(await screen.findByRole("button", { name: words.share }));
  const dialog = await screen.findByRole("dialog", { name: words.shareTitle });
  const channel = within(dialog).getByLabelText(words.shareChannel) as HTMLSelectElement;
  await waitFor(() => assert.equal(channel.value, "deals"));
  assert.equal(channel.disabled, false);
  fireEvent.click(await within(dialog).findByRole("checkbox",{name:words.placementReview}));
  fireEvent.click(within(dialog).getByRole("button", { name: words.share }));
  await waitFor(() => assert.ok(net?.seen.some((s) => s.method === "POST")));
  assert.deepEqual(net?.seen.find((s) => s.method === "POST")?.body, { channel: "deals", placement_token:"channel-token" });
  assert.ok(await within(dialog).findByText(words.placementChanged));
  assert.ok(!within(dialog).queryByText(/stays in its channel$/));
});

test("a change to the + selection someone else beat says so in words and reads the conversation again", async () => {
  let reads = 0;
  net = stubFetch({
    "GET /api/threads/th-00000000000000aa": () => {
      reads++;
      return { ...mine, thread: { ...mine.thread, version: 3, agents: ["helper"] } };
    },
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/projects/Globex/channels": () => ({ channels: [] }),
    "PUT /api/threads/th-00000000000000aa": () =>
      new Response(JSON.stringify({ error: 'changed since it was read: "th-00000000000000aa" is at version 3, not 1; reload and try again' }), { status: 409 }),
  });
  render(<ThreadRoom id={mine.thread.id} me="ana" initial={mine} tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />);
  await waitFor(() => assert.ok(reads >= 1));
  const before = reads;
  fireEvent.click(await screen.findByRole("button", { name: defaultCopy.chat.plus }));
  const menu = await screen.findByRole("dialog", { name: defaultCopy.chat.menu });
  fireEvent.click(within(menu).getByRole("button", { name: /^Agents\b/ }));
  fireEvent.click(await within(menu).findByRole("checkbox", { name: /^helper/ }));
  assert.ok(await screen.findByText(words.changedMeanwhile));
  assert.ok(!screen.queryByText(/version 3/));
  await waitFor(() => assert.ok(reads > before, "the conversation is read again"));
});

test("an answer routed to the one agent a message went to says only where it went", async () => {
  const restore = stubEventSource();
  try {
    const lone: ThreadPage = {
      ...mine,
      turns: [...mine.turns, { kind: "run", run: { id: "r7", agent: "helper", status: "completed", answer: "Done.", text: "q", message: "m-1", prev: "m-1", thread: "th-00000000000000aa", calls: 0, routed: "all", created_at: at, updated_at: at } as never }],
    };
    net = stubFetch({
      "GET /api/threads/th-00000000000000aa": () => lone,
      "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    });
    render(<ThreadRoom id={lone.thread.id} me="ana" initial={lone} tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />);
    assert.ok(await screen.findByText(defaultCopy.chat.routed("helper")));
    assert.ok(!screen.queryByText(defaultCopy.chat.routedAll("helper")));
    cleanup();
  } finally {
    restore();
  }
});

test("the + menu returns focus to its category when leaving Agents and Projects", async () => {
  net = stubFetch({
    "GET /api/projects/Globex/channels": () => ({ channels: [] }),
  });
  const sel: Selection = { agents: [], mode: "join", project: "Globex", channel: "general" };
  render(<PlusMenu agents={[{ name: "helper" }, { name: "lead" }]} skills={[]} onSkill={() => {}} selection={sel} onChange={() => {}} onAttach={() => {}} onClose={() => {}} projects={tree} />);
  const menu = screen.getByRole("dialog", { name: defaultCopy.chat.menu });
  const agents = within(menu).getByRole("button", { name: /^Agents\b/ });
  fireEvent.click(agents);
  const search = within(menu).getByRole("searchbox", { name: defaultCopy.chat.findAgent });
  search.focus();
  fireEvent.keyDown(search, { key: "ArrowDown" });
  assert.equal(document.activeElement?.textContent, "helper");
  fireEvent.keyDown(document.activeElement as Element, { key: "Escape" });
  await waitFor(() => assert.equal(document.activeElement?.textContent?.startsWith("Agents"), true));
  fireEvent.click(within(menu).getByRole("button", { name: /^Projects\b/ }));
  await within(menu).findByRole("searchbox", { name: defaultCopy.projects.search });
  fireEvent.keyDown(document.activeElement as Element, { key: "Escape" });
  await waitFor(() => assert.equal(document.activeElement?.textContent?.startsWith("Projects"), true));
  assert.ok(screen.getByRole("dialog", { name: defaultCopy.chat.menu }), "the root menu remains open");
});

test("a mention kind that resolves to people never offers the person typing", async () => {
  net = stubFetch({
    "GET /api/threads/th-00000000000000aa": () => mine,
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/mentions": (_, q) =>
      q.get("kind") === "who" ? { items: [{ name: "ana-s", description: "Ana" }, { name: "dana", description: "Dana Scully" }] } : { kinds: [{ kind: "who", title: "People", resolves: "people" }] },
    "GET /api/people": () => ({ people: [{ subject: "ana", name: "Ana", handle: "ana-s", picture: null }] }),
  });
  render(<ThreadRoom id={mine.thread.id} me="ana" initial={mine} tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />);
  const box = (await screen.findByPlaceholderText(words.message)) as HTMLTextAreaElement;
  await waitFor(() => assert.ok(net?.seen.some((s) => s.path.startsWith("/api/mentions?project="))));
  fireEvent.change(box, { target: { value: "@who:" } });
  const list = await screen.findByRole("listbox");
  await waitFor(() => assert.ok(list.textContent?.includes("@who:dana"), list.textContent ?? ""));
  assert.ok(!list.textContent?.includes("@who:ana-s"), list.textContent ?? "");
});

test("a starter whose subject holds an address is known by their handle: Share and the move are theirs, and completion leaves them out", async () => {
  const pats: ThreadPage = { ...mine, thread: { ...mine.thread, created_by: "pat", participants: [{ kind: "person", name: "pat", owner: true, at }] } };
  const member = tree.map((p) => (p.name === "Globex" ? { ...p, access: { role: "member" as const, from: "Globex", via: "*" } } : p));
  net = stubFetch({
    "GET /api/threads/th-00000000000000aa": () => pats,
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/projects/Globex/channels": () => ({ channels: [] }),
    // The directory leaves out a subject that holds an address.
    "GET /api/people": () => ({ people: [{ handle: "pat", name: "Pat", picture: null }, { subject: "00u-dana", handle: "dana", name: "Dana", picture: null }] }),
  });
  render(
    <ThreadRoom id={pats.thread.id} me="tailscale:pat@example.com" handle="pat" initial={pats} tick={0} agents={[helper]} projects={member} root={null} workspaceName="Acme" canRun sharing={false} onSharing={() => {}} onOpenChannel={() => {}} onChanged={() => {}} />,
  );
  assert.ok(await screen.findByRole("button", { name: words.share }), "the starter may share it");
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.chat.plus }));
  const menu = await screen.findByRole("dialog", { name: defaultCopy.chat.menu });
  fireEvent.click(within(menu).getByRole("button", { name: /^Agents\b/ }));
  assert.ok(!within(menu).queryByText(defaultCopy.chat.projectStarter), "the starter may move it");
  fireEvent.keyDown(menu, { key: "Escape" });
  const box = screen.getByPlaceholderText(words.message) as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: "@" } });
  const list = await screen.findByRole("listbox");
  await waitFor(() => assert.ok(list.textContent?.includes("@dana"), list.textContent ?? ""));
  assert.ok(!list.textContent?.includes("@pat"), list.textContent ?? "");
});

test("Share says who the platform did not add, and why, beside who was told", async () => {
  const people2 = { people: [{ subject: "00u-dana", name: "Dana Scully", handle: "dana", picture: null }, { subject: "00u-eve", name: "Eve", handle: "eve", picture: null }] };
  const shared: ThreadPage = { ...mine, thread: { ...mine.thread, visibility: "shared" } };
  net = stubFetch({
    "GET /api/threads/th-00000000000000aa": () => mine,
    "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
    "GET /api/projects/Globex/channels": () => ({ channels: [] }),
    "GET /api/people": () => people2,
    "POST /api/threads/th-00000000000000aa/share": () => ({ ...shared, not_added: [{ handle: "eve", reason: "they cannot read this project, so they were not added" }] }),
  });
  render(<Sharing initial={mine} />);
  fireEvent.click(await screen.findByRole("button", { name: words.share }));
  const dialog = await screen.findByRole("dialog", { name: words.shareTitle });
  assert.ok(within(dialog).getByText(words.shareLede));
  const who = within(dialog).getByRole("combobox", { name: words.shareAdd }) as HTMLInputElement;
  for (const h of ["dana", "eve"]) {
    fireEvent.change(who, { target: { value: h } });
    const found = await within(dialog).findByRole("listbox");
    await waitFor(() => assert.ok(found.textContent?.includes(`@${h}`), found.textContent ?? ""));
    fireEvent.keyDown(who, { key: "Enter" });
    await within(dialog).findByRole("button", { name: words.shareRemove(h) });
  }
  fireEvent.click(within(dialog).getByRole("button", { name: words.share }));
  const note = await screen.findByText(`${words.sharedTold(["dana"])} ${words.notAdded(["eve"], [])}`);
  assert.equal(note.textContent, "Shared. @dana was told in their inbox. @eve can't see this project, so wasn't added.");
  assert.ok(!screen.queryByText(/they were not added/), "the server's words are not shown");
});

test("human and agent replies keep Copy and Reply in the same footer", async () => {
  room();
  for (const label of ["Message from Ana", "helper message"]) {
    const message = await screen.findByRole("article", { name: label });
    const toolbar = within(message).getByRole("toolbar", { name: "Message actions" });
    assert.ok(within(toolbar).getByRole("button", { name: /Copy (message|answer)/ }));
    assert.ok(within(toolbar).getByRole("button", { name: "Reply" }));
    assert.equal(message.querySelectorAll('[role="toolbar"]').length, 1);
  }
});

test("agent replies expose their recorded instant with the same accessible timestamp", async () => {
  room();
  const message = await screen.findByRole("article", { name: "helper message" });
  const exact = new Date(at).toLocaleString(undefined, { dateStyle: "full", timeStyle: "long" });
  const time = within(message).getByRole("button", { name: words.posted(exact) });
  assert.equal(time.querySelector("time")?.dateTime, at);
  fireEvent.focus(time);
  assert.equal(within(message).getByRole("tooltip").textContent, exact);
  fireEvent.keyDown(time, { key: "Escape" });
  assert.equal(within(message).queryByRole("tooltip"), null);
});

function StopFixture() {
  const run = { ...page.turns[1]?.run, status: "running" } as NonNullable<ThreadTurn["run"]>;
  const stopped = useRunStop(run, true);
  return <div><output aria-label="Recorded status">{stopped.run.status}</output>{stopped.control}</div>;
}

test("Stop Turn reports a request and waits for the ledger, rather than inventing a stopped turn", async () => {
  let terminal = false;
  net = stubFetch({
    "POST /api/runs/r1/stop": () => ({ ...page.turns[1]?.run, status: "running" }),
    "GET /api/runs/r1": () => ({ ...page.turns[1]?.run, status: terminal ? "stopped" : "running" }),
  });
  render(<StopFixture />);
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.conversation.stopTurn }));
  await screen.findByText(defaultCopy.conversation.stopPending);
  assert.equal(screen.getByLabelText("Recorded status").textContent, "running");
  terminal = true;
  await waitFor(() => assert.equal(screen.getByLabelText("Recorded status").textContent, "stopped"), { timeout: 2000 });
  assert.equal(screen.queryByText(defaultCopy.conversation.stopPending), null);
});

test("recorded outcomes lead with recommendations and Redirect prepares human intent without executing it", async () => {
  const initial: ThreadPage = {...page,turns:page.turns.map(turn => turn.run ? {...turn,run:{...turn.run,outcome:{kind:"recommendation",summary:"Renew the agreement.",decisions:["Keep the current term"],next_action:"Confirm the owner"},citations:[{id:"[1]",title:"Agreement",url:"https://example.test/agreement",snippet:"The source passage"}]}} : turn)};
  room({},initial);
  const message=await screen.findByRole("article",{name:"helper message"});
  assert.ok(within(message).getByText("Recommendation"));
  assert.ok(within(message).getByText("Keep the current term"));
  assert.ok(within(message).getByText("Confirm the owner"));
  fireEvent.click(within(message).getByRole("button",{name:"Redirect"}));
  const composer=screen.getByPlaceholderText(words.channel("renewals")) as HTMLTextAreaElement;
  assert.ok(composer.value.includes("@helper redirecting [this outcome]"));
  assert.ok(composer.value.endsWith("Please change direction: "));
  assert.equal(net?.seen.filter(call=>call.method==="POST").length,0);

});

test("answer copy is an action menu and never exposes an empty-selection sentinel",async()=>{
  room();
  const answer=await screen.findByRole("article",{name:"helper message"});
  const copy=within(answer).getByRole("button",{name:"Copy answer as"});
  assert.equal(copy.getAttribute("aria-label"),"Copy answer as");
  fireEvent.click(copy);
  assert.ok(await screen.findByRole("listbox"));
  assert.ok(!copy.getAttribute("aria-label")?.includes("none"));
});


test("private source history publishes only a reviewed summary and stale consent preserves the draft", async () => {
  let previews = 0;
  let published = "";
  let posts = 0;
  net = stubFetch({
    "GET /api/projects/Globex/channels": () => ({channels:[]}),
    "GET /api/threads/th-00000000000000aa/placement": () => ({thread:mine.thread.id,project:"Globex",channel:"general",audience:"Globex members",runs:1,token:`review-${++previews}`,exclusions:["Private source history stays private"],summary_required:true}),
    "POST /api/threads/th-00000000000000aa/publication": () => ++posts === 1 ? new Response(JSON.stringify({error:"placement changed"}),{status:409}) : {...mine.thread,id:"published-summary",visibility:"shared"},
  });
  render(<ShareDialog thread={mine.thread} hasRuns projects={tree} workspaceName="Acme" me="ana" onClose={()=>{}} onShared={()=>assert.fail("raw history must not publish")} onPublished={(thread)=>{published=thread.id;}}/>);
  const dialog = screen.getByRole("dialog");
  const input = await within(dialog).findByLabelText(new RegExp(`^${words.publicationSummary}`));
  fireEvent.change(input,{target:{value:"Only the approved delivery date."}});
  fireEvent.click(within(dialog).getByRole("checkbox",{name:words.placementReview}));
  fireEvent.click(within(dialog).getByRole("button",{name:words.publishSummary}));
  assert.ok(await within(dialog).findByText(words.placementChanged));
  assert.equal((input as HTMLTextAreaElement).value,"Only the approved delivery date.");
  await waitFor(()=>assert.ok(previews >= 2));
  assert.equal((within(dialog).getByRole("button",{name:words.publishSummary}) as HTMLButtonElement).disabled,true);
  assert.equal(published,"");
  assert.equal(net.seen.some((request)=>request.path.endsWith("/share")),false);
  assert.deepEqual(net.seen.find((request)=>request.method === "POST")?.body,{project:"Globex",channel:"general",summary:"Only the approved delivery date.",placement_token:"review-1"});
  fireEvent.click(await within(dialog).findByRole("checkbox",{name:words.placementReview}));
  fireEvent.click(within(dialog).getByRole("button",{name:words.publishSummary}));
  await waitFor(()=>assert.equal(published,"published-summary"));
  assert.equal(net.seen.filter((request)=>request.method === "POST").length,2);
});


test("human and completed-agent fork actions share the Copy and Reply footer", async()=>{
 const points:string[]=[];
 net=stubFetch({["GET /api/threads/"+page.thread.id]:()=>page,"GET /api/runs/r1/events":()=>({events:[]}),"GET /api/projects/Globex/agents":()=>({version:1,available:{all:true,names:[]},homes:[],default_resolved:null})});
 render(<ThreadRoom id={page.thread.id} me="ana" initial={page} tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun forkActions={{onFork:point=>points.push(point)}} onOpenChannel={()=>{}} onChanged={()=>{}}/>);
 const human=screen.getByRole("article",{name:"Message from Ana"});
 const actions=within(human).getByRole("toolbar",{name:defaultCopy.conversation.messageActions});
 assert.ok(within(actions).getByRole("button",{name:defaultCopy.conversation.copyMessage}));
 fireEvent.click(within(actions).getByRole("button",{name:defaultCopy.forking.fromHere}));
 const agent=screen.getByRole("article",{name:"Answered by helper"});
 fireEvent.click(within(agent).getByRole("button",{name:defaultCopy.forking.fromHere}));
 assert.deepEqual(points,["m-1","r1"]);
});

test("a read-only conversation archive presents saved context and exposes no composer or run action",async()=>{
 const archive:ThreadPage={...page,thread:{...page.thread,read_only:true,fork:{source_thread:"source-thread",through:"source-last",by:"ana",at,context:[{kind:"person",by:"ana",name:"Ana",text:"Retained source decision",at}]}},turns:[]};
 net=stubFetch({["GET /api/threads/"+archive.thread.id]:()=>archive,"GET /api/projects/Globex/agents":()=>({version:1,available:{all:true,names:[]},homes:[],default_resolved:null})});
 render(<ThreadRoom id={archive.thread.id} me="ana" initial={archive} tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun forkActions={{onFork:()=>assert.fail("archive cannot fork")}} onOpenChannel={()=>{}} onChanged={()=>{}}/>);
 assert.ok(await screen.findByText(defaultCopy.forking.archive));
 assert.ok(screen.getByText("Retained source decision"));
 assert.ok(!screen.queryByRole("textbox"));
 assert.ok(!screen.queryByRole("button",{name:defaultCopy.forking.fromHere}));
 assert.ok(!screen.queryByRole("button",{name:words.share}));
 assert.equal(net.seen.some(request=>request.method!=="GET"),false);
});

test("multiple replies by the same agent each retain one accessible Copy and Reply footer", async () => {
  const followup = {
    kind: "message",
    message: { ...page.turns[0]!.message!, id: "m-2", text: "Who owns the next step?" },
  } as ThreadTurn;
  const secondRun = {
    kind: "run",
    run: { ...page.turns[1]!.run!, id: "r2", message: "m-2", prev: "m-2", answer: "Ana owns the next step." },
  } as ThreadTurn;
  room({"GET /api/runs/r2/events":()=>({events:[]})}, {...page,turns:[...page.turns,followup,secondRun]});
  const agents = await screen.findAllByRole("article",{name:"helper message"});
  assert.equal(agents.length,2);
  const humans=screen.getAllByRole("article",{name:"Message from Ana"});
  assert.equal(humans.length,2);
  for(const message of [...humans,...agents]) {
    const actions=within(message).getAllByRole("toolbar",{name:defaultCopy.conversation.messageActions});
    assert.equal(actions.length,1);
    assert.equal(within(actions[0]!).getAllByRole("button",{name:/^Copy (message|answer)/}).length,1);
    assert.equal(within(actions[0]!).getAllByRole("button",{name:"Reply"}).length,1);
  }
  fireEvent.click(within(agents[1]!).getByRole("button",{name:"Reply"}));
  await waitFor(()=>assert.ok((document.querySelector('form[aria-label="Message"] textarea') as HTMLTextAreaElement | null)?.value.includes("activity/r2")));
});

for (const kind of ["conversation", "channel"] as const) {
  test(`the ${kind} composer inserts a project skill without submitting the draft`, async () => {
    net = stubFetch({
      "GET /api/threads/th-00000000000000aa": () => mine,
      "GET /api/threads": () => ({ threads: [], next: null }),
      "GET /api/projects/Globex/agents": () => ({ version: 1, available: { all: true, names: [] }, homes: [], default_resolved: null }),
      "GET /api/projects/Globex/channels": () => ({ channels: [{ name: "renewals", project: "Globex", version: 1, created_at: at, updated_at: at }] }),
      "GET /api/skills": () => ({ skills: [{ name: "review", description: "Review the change", argument_hint: "", from: "Globex" }] }),
      "GET /api/mentions": () => ({ kinds: [] }),
      "POST /api/threads/th-00000000000000aa/messages": () => ({ thread: mine.thread, message: { id: "skill-message", thread: mine.thread.id, by: "ana", text: "$review Keep this request.", mentions: [], via: "console", at }, runs: [], notified: [], refused: [], rule: "people" }),
      "POST /api/threads": () => ({ thread: { ...mine.thread, id: "skill-thread" }, message: { id: "skill-message", thread: "skill-thread", by: "ana", text: "$review Keep this request.", mentions: [], via: "console", at }, runs: [], notified: [], refused: [], rule: "people" }),
    });
    if (kind === "conversation") render(<ThreadRoom id={mine.thread.id} me="ana" initial={mine} tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />);
    else render(<ChannelPage project="Globex" channel="renewals" tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun onOpenProject={() => {}} onOpenProjects={() => {}} onOpenThread={() => {}} />);
    const box = await screen.findByPlaceholderText(kind === "conversation" ? words.message : words.startThread) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: "Keep this request." } });
    fireEvent.click(screen.getByRole("button", { name: defaultCopy.chat.plus }));
    const menu = await screen.findByRole("dialog", { name: defaultCopy.chat.menu });
    fireEvent.click(within(menu).getByRole("button", { name: /^Skills\b/ }));
    fireEvent.click(await within(menu).findByRole("button", { name: /^\$?review(?:\s|$)/i }));
    await waitFor(() => assert.equal(box.value, "$review Keep this request."));
    assert.equal(!!screen.queryByRole("dialog", { name: defaultCopy.chat.menu }), false);
    assert.equal(net.seen.filter(request => request.method === "POST").length, 0);
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => assert.equal(net?.seen.filter(request => request.method === "POST").length, 1));
    const posted = net.seen.find(request => request.method === "POST");
    assert.equal(posted?.path, kind === "conversation" ? `/api/threads/${mine.thread.id}/messages` : "/api/threads");
    assert.equal((posted?.body as { text: string }).text, "$review Keep this request.");
  });
}
