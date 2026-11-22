import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Revision, ThreadMessage, ThreadPage } from "./api.ts";
import {
  bangCommand,
  CommandMessage,
  commandHint,
  commandHold,
  commandsHere,
} from "./BangCommand.tsx";
import { Chat } from "./Chat.tsx";
import { stubEventSource, stubFetch } from "./dom.ts";
import { ThreadRoom } from "./Room.tsx";

// A composer message that starts with "!" runs its command in the session's
// sandbox: the bar says so while it is typed, the post carries command:
// true, and the conversation shows the command with its output. Where no
// command may run, the bar says why and nothing is sent to an agent.

const words = defaultCopy.bangCommand;
const at = "2026-11-22T09:00:00Z";

let net: ReturnType<typeof stubFetch> | undefined;
let restoreEvents: (() => void) | undefined;
afterEach(() => {
  cleanup();
  restoreEvents?.();
  restoreEvents = undefined;
  net?.restore();
  net = undefined;
});

const box = () => screen.getByLabelText("Message", { selector: "textarea" }) as HTMLTextAreaElement;
const posts = () => (net?.seen ?? []).filter((s) => s.method === "POST");
const note = () => document.querySelector('[data-layer="note"]')?.textContent ?? null;

const options = {
  images: [],
  imageRegistries: [],
  sandbox: "docker/runsc",
  isolated: true,
  terminal: true,
  repos: [],
};

const lead: Revision = {
  rev: 1,
  spec: {
    name: "lead",
    description: "",
    provider: "offline",
    model: "offline",
    system_prompt: ".",
  },
  created_at: at,
  created_by: "ada",
  hash: "lead",
};

function commandMessage(id: string, text: string, output: string, exit = 0): ThreadMessage {
  return {
    id,
    thread: "s_bang",
    by: "ada",
    name: "Ada",
    text,
    mentions: [],
    via: "console",
    command: { command: text.slice(1), exit_code: exit, output, duration_ms: 120 },
    at,
  };
}

/** A session's answer whose messages are the ones given, as the server sends it. */
function sessionOf(messages: ThreadMessage[]) {
  return {
    id: "s_bang",
    agent: "lead",
    parent: "console://test",
    project: "",
    title: messages[0]?.text ?? "",
    status: "done",
    updated_at: at,
    thread: {
      id: "s_bang",
      project: "",
      channel: "general",
      visibility: "private",
      participants: [],
      version: 1,
      created_by: "ada",
      created_at: at,
      updated_at: at,
    },
    children: [],
    links: [],
    people: [],
    cursor: "cursor",
    messages: messages.map((message) => ({
      id: message.id,
      session: "s_bang",
      author: { kind: "person", name: "Ada" },
      status: "done",
      parts: [{ type: "text", text: message.text }],
      at,
      message,
    })),
  };
}

test("a draft is a command when it starts with !, and the hint says what it does or why it cannot", () => {
  assert.equal(bangCommand("!pytest -q"), "pytest -q");
  assert.equal(bangCommand("  ! ls "), "ls");
  assert.equal(bangCommand("!"), "");
  assert.equal(bangCommand("run !this"), null);
  assert.equal(commandHint("hello", null), null);
  assert.equal(commandHint("!", null), words.empty);
  assert.equal(commandHint("!ls", null), words.hint);
  assert.equal(commandHint("!ls", null, true), words.hintHome);
  assert.equal(commandHint("!ls", commandsHere(false, true)), words.noSandbox);
  // Outside a session "!" is plain text, so nothing refuses it.
  assert.equal(commandsHere(true, false), undefined);
  // Until the server's options are read, the server decides.
  assert.equal(commandsHere(undefined, true), null);
});

test("a command draft is held while it has files attached or nothing after the !", () => {
  // A file attached to a command would be run as part of it.
  assert.equal(commandHold("!ls", 1, null), words.withFiles);
  assert.equal(commandHold("!", 0, null), words.empty);
  assert.equal(commandHold("!ls", 0, null), null);
  assert.equal(commandHold("hello", 2, null), null);
  // Where "!" is plain text, nothing is held.
  assert.equal(commandHold("!", 1, undefined), null);
});

test("on Home, a ! first message starts the session with its setup and runs the command, and nothing is asked of an agent", async () => {
  net = stubFetch({
    "GET /api/projects/~/agents": () => ({
      version: 2,
      enable: ["*"],
      available: { all: true, names: [] },
      offers: [{ agent: "*", from: "~" }],
      homes: [],
      default_resolved: null,
    }),
    "GET /api/people": () => ({ people: [] }),
    "GET /api/skills": () => ({ skills: [] }),
    "GET /api/mentions": () => ({ kinds: [] }),
    "GET /api/sessions/options": () => options,
    "POST /api/sessions": (body) =>
      sessionOf([commandMessage("m-1", (body as { text: string }).text, "hello\n")]),
  });
  const opened: string[] = [];
  render(
    <Chat
      threadId={null}
      homes={{}}
      me="ada"
      summary={undefined}
      tick={0}
      projects={[]}
      canRun
      canCreateProject={false}
      greeting="Good morning"
      onThread={(id) => opened.push(id)}
      onChanged={() => {}}
      onProjectCreated={() => {}}
      agents={[lead]}
      agentsRead
    />,
  );
  await waitFor(() => assert.ok(net?.seen.some((s) => s.path === "/api/sessions/options")));
  fireEvent.change(box(), { target: { value: "!" } });
  await waitFor(() => assert.equal(note(), words.empty));
  fireEvent.change(box(), { target: { value: "!echo hello" } });
  await waitFor(() => assert.equal(note(), words.hintHome));
  fireEvent.keyDown(box(), { key: "Enter" });
  await waitFor(() => assert.deepEqual(opened, ["s_bang"]));
  const sent = posts();
  assert.equal(sent.length, 1);
  assert.equal(sent[0]?.path, "/api/sessions");
  assert.deepEqual(
    {
      text: (sent[0]?.body as { text: string }).text,
      command: (sent[0]?.body as { command?: boolean }).command,
    },
    { text: "!echo hello", command: true },
  );
});

const sessionPage: ThreadPage = {
  thread: {
    id: "s_bang",
    project: "",
    channel: "general",
    visibility: "private",
    participants: [],
    version: 1,
    created_by: "ada",
    created_at: at,
    updated_at: at,
  },
  turns: [{ kind: "message", message: { ...commandMessage("m-1", "!echo hello", "hello\n") } }],
  next: null,
  answers: { agent: "lead", from: "session" },
} as unknown as ThreadPage;

function sessionRoom(page: ThreadPage, routes: Record<string, (body: unknown) => unknown> = {}) {
  restoreEvents = stubEventSource();
  net = stubFetch({
    [`GET /api/sessions/${page.thread.id}`]: () => sessionOf([]),
    [`GET /api/threads/${page.thread.id}`]: () => page,
    "GET /api/sessions/options": () => options,
    "GET /api/people": () => ({ people: [], next: null }),
    ...routes,
  });
  render(
    <ThreadRoom
      id={page.thread.id}
      me="ada"
      initial={page}
      tick={0}
      agents={[lead]}
      projects={[]}
      root={null}
      workspaceName="Acme"
      canRun
      onOpenChannel={() => {}}
      onChanged={() => {}}
    />,
  );
}

test("in a session, a ! message posts with command: true and shows as a command block with its output", async () => {
  sessionRoom(sessionPage, {
    "POST /api/sessions/s_bang/messages": (body) =>
      sessionOf([
        commandMessage("m-1", "!echo hello", "hello\n"),
        commandMessage("m-2", (body as { text: string }).text, "a.txt\nb.txt\n", 2),
      ]),
  });
  // A command read with the session is drawn as one, with its output.
  const first = await screen.findByRole("article", { name: words.label(words.you, "echo hello") });
  assert.equal(within(first).getByTestId("command-output").textContent, "hello");
  assert.ok(within(first).getByText(words.exited(0)));
  fireEvent.change(box(), { target: { value: "!ls; exit 2" } });
  await waitFor(() => assert.equal(note(), words.hint));
  fireEvent.keyDown(box(), { key: "Enter" });
  await waitFor(() => assert.equal(posts().length, 1));
  const sent = posts()[0];
  assert.equal(sent?.path, "/api/sessions/s_bang/messages");
  assert.deepEqual(sent?.body, { text: "!ls; exit 2", command: true });
  const second = await screen.findByRole("article", { name: words.label(words.you, "ls; exit 2") });
  assert.equal(within(second).getByTestId("command-output").textContent, "a.txt\nb.txt");
  assert.ok(within(second).getByText(words.exited(2)));
  await waitFor(() => assert.equal(box().value, ""));
});

test("in a shared thread a ! message is plain text: it is posted as words, with no hint", async () => {
  const shared = {
    ...sessionPage,
    thread: { ...sessionPage.thread, id: "th-00000000000000cd", visibility: "shared" },
    turns: [
      {
        kind: "message",
        message: {
          ...commandMessage("m-1", "hello", ""),
          command: undefined,
          thread: "th-00000000000000cd",
        },
      },
    ],
  } as unknown as ThreadPage;
  sessionRoom(shared, { "POST /api/threads/th-00000000000000cd/messages": () => ({ runs: [] }) });
  await screen.findByText("hello");
  fireEvent.change(box(), { target: { value: "!!! deploy done" } });
  await new Promise((r) => setTimeout(r, 50));
  assert.equal(note(), null);
  fireEvent.keyDown(box(), { key: "Enter" });
  await waitFor(() => assert.equal(posts().length, 1));
  const body = posts()[0]?.body as { text?: string; command?: boolean };
  assert.equal(body.text, "!!! deploy done");
  assert.equal(body.command, undefined);
});

test("in a session a lone ! is held in the box, saying it needs a command", async () => {
  sessionRoom(sessionPage);
  await screen
    .findByRole("article", { name: words.label(words.you, "echo hello") })
    .catch(() => {});
  fireEvent.change(box(), { target: { value: "!" } });
  await waitFor(() => assert.equal(note(), words.empty));
  fireEvent.keyDown(box(), { key: "Enter" });
  await new Promise((r) => setTimeout(r, 50));
  assert.equal(posts().length, 0);
  assert.equal(box().value, "!");
});

test("a command's block folds long output, says when output was cut, and says how a timed-out command ended", () => {
  const long = Array.from({ length: 60 }, (_, i) => `line ${i + 1}`).join("\n");
  const m = {
    ...commandMessage("m-3", "!seq 60", long),
    command: {
      command: "seq 60",
      exit_code: -1,
      timed_out: true,
      output: long,
      truncated: true,
      duration_ms: 15000,
    },
  };
  render(<CommandMessage m={m} mine={false} focused={false} />);
  const block = screen.getByRole("article", { name: words.label("Ada", "seq 60") });
  assert.ok(within(block).getByText(words.timedOut));
  assert.ok(within(block).getByText(words.cut));
  const out = within(block).getByTestId("command-output");
  assert.equal(out.textContent?.split("\n").length, 24);
  assert.ok(out.textContent?.endsWith("line 60"));
  fireEvent.click(within(block).getByRole("button", { name: words.showAll(60) }));
  assert.equal(within(block).getByTestId("command-output").textContent?.split("\n").length, 60);
});
