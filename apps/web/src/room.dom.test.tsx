import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@warp/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Revision, ThreadPage, ThreadTurn } from "./api.ts";
import { stubEventSource, stubFetch } from "./dom.ts";
import { ChannelPage, ChannelsTab, ThreadRoom } from "./Room.tsx";
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
      { kind: "person", name: "00u-ana", owner: true, at },
      { kind: "agent", name: "helper", at },
    ],
    version: 1,
    created_by: "00u-ana",
    created_at: at,
    updated_at: at,
  },
  turns: [
    {
      kind: "message",
      message: { id: "m-1", thread: "th-0123456789abcdef", by: "00u-ana", name: "Ana", text: "@helper when does Globex renew?", mentions: [{ kind: "agent", name: "helper", run: "r1" }], via: "console", at },
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
      threads: [{ id: "th-0123456789abcdef", title: "Globex renewal", named: true, origin: "api", project: "Globex", channel: "renewals", agents: ["helper"], people: ["00u-ana"], visibility: "shared", messages: 2, turns: 1, branches: 0, active: 0, failed: 0, created_at: at, updated_at: at }],
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

test("a project's channels open their pages, and a new one is made with a name that cannot change", async () => {
  const made: unknown[] = [];
  net = stubFetch({
    "GET /api/projects/Globex/channels": () => ({
      channels: [{ project: "Globex", name: "general", version: 0, created_at: at, updated_at: at, answers: { agent: "auto", from: "project:~" } }],
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
        <ThreadRoom id="th-0123456789abcdef" initial={waiting} tick={0} agents={[helper]} projects={tree} root={null} workspaceName="Acme" canRun onOpenChannel={() => {}} onChanged={() => {}} />,
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

test("turns read earlier stay when the newest page moves on", async () => {
  const message = (n: number): ThreadTurn => ({
    kind: "message",
    message: { id: `m-${n}`, thread: "th-0123456789abcdef", by: "00u-ana", name: "Ana", text: `turn ${n}`, mentions: [], via: "console", at },
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
  const props = { id: "th-0123456789abcdef", initial: newest, agents: [helper], projects: tree, root: null, workspaceName: "Acme", canRun: true, onOpenChannel: () => {}, onChanged: () => {} };
  const { rerender } = render(<ThreadRoom {...props} tick={0} />);
  await waitFor(() => assert.equal(reads, 1));
  fireEvent.click(await screen.findByText(words.olderTurns));
  assert.ok(await screen.findByText("turn 1", { selector: "p" }));
  rerender(<ThreadRoom {...props} tick={1} />);
  assert.ok(await screen.findByText("turn 4", { selector: "p" }));
  for (const n of [1, 2, 3, 4]) assert.equal(screen.getAllByText(`turn ${n}`, { selector: "p" }).length, 1, `turn ${n} is shown once`);
  assert.equal(screen.queryByText(words.olderTurns), null);
});
