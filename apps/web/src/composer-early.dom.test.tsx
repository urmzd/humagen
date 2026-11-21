import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App } from "./App.tsx";
import type { ProjectAgentsView, Revision } from "./api.ts";
import { Chat } from "./Chat.tsx";
import { stubEventSource, stubFetch } from "./dom.ts";

// Home's chat bar is drawn with the page, before the agents are read. Only
// sending and mentions wait for them: a message sent early is held and sent
// once they arrive, and @ says they are loading.

const at = "2026-10-01T09:00:00Z";
const agent = (name: string): Revision => ({
  rev: 1,
  spec: { name, description: "", provider: "offline", model: "offline", system_prompt: "." },
  created_at: at,
  created_by: "ada",
  hash: name,
});
const view: ProjectAgentsView = {
  version: 2,
  enable: ["*"],
  available: { all: true, names: [] },
  offers: [{ agent: "*", from: "~" }],
  homes: [],
  default_resolved: null,
};
const started = (body: unknown) => ({
  id: "th-00000000000000ab",
  agent: "lead",
  parent: "console://test",
  project: "",
  title: (body as { text: string }).text,
  status: "done",
  updated_at: at,
  thread: {
    id: "th-00000000000000ab",
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
  messages: [
    {
      id: "m-1",
      session: "th-00000000000000ab",
      author: { kind: "person", name: "Ada" },
      status: "done",
      parts: [{ type: "text", text: (body as { text: string }).text }],
      at,
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
    },
  ],
});

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

const box = () => screen.getByLabelText("Message", { selector: "textarea" }) as HTMLTextAreaElement;
const posts = () => (net?.seen ?? []).filter((s) => s.method === "POST");

test("Home draws the chat bar while the agents are read, and reads the project's agents beside them", async () => {
  window.history.replaceState(null, "", "/");
  restoreEvents = stubEventSource();
  let reply!: (body: unknown) => void;
  const pending = new Promise((resolve) => {
    reply = resolve;
  });
  net = stubFetch({
    "GET /api/me": () => ({
      principal: { subject: "ada", tenant: "acme", kind: "user", name: "Ada", roles: ["member"] },
      permissions: ["agents:read", "runs:write"],
    }),
    "GET /api/branding": () => ({ name: "Humagen", title: "Humagen" }),
    "GET /api/agents": () => pending,
    "GET /api/projects/~/agents": () => view,
  });
  render(<App />);
  // Typing works before the agents arrive, and a screen reader hears they are loading.
  const input = (await screen.findByLabelText("Message", {
    selector: "textarea",
  })) as HTMLTextAreaElement;
  assert.ok(await screen.findByRole("status", { name: "Loading agents" }));
  fireEvent.change(input, { target: { value: "Plan the launch" } });
  assert.equal(input.value, "Plan the launch");
  // The project's agents are asked for while the workspace's are still on their way.
  await waitFor(() =>
    assert.ok(
      net?.seen.some((s) => s.path === "/api/projects/~/agents"),
      "project agents read",
    ),
  );
  assert.ok(net?.seen.some((s) => s.path === "/api/agents"));
  await act(async () => reply({ agents: [agent("lead")] }));
  await waitFor(() => assert.equal(screen.queryByRole("status", { name: "Loading agents" }), null));
  assert.equal(box().value, "Plan the launch");
});

const noHomes: Record<string, string> = {};

function home(props: Partial<Parameters<typeof Chat>[0]> = {}) {
  const opened: string[] = [];
  net = stubFetch({
    "GET /api/projects/~/agents": () => view,
    "GET /api/people": () => ({ people: [] }),
    "GET /api/skills": () => ({ skills: [] }),
    "GET /api/mentions": () => ({ kinds: [] }),
    "POST /api/sessions": (body) => started(body),
  });
  const noop = () => {};
  const base = {
    threadId: null,
    // Held across renders, as App holds it: completion recomputes on a new object.
    homes: noHomes,
    me: "ada",
    summary: undefined,
    tick: 0,
    projects: [],
    canRun: true,
    canCreateProject: false,
    greeting: "Good morning",
    onThread: (id: string) => opened.push(id),
    onChanged: noop,
    onProjectCreated: noop,
    agents: [] as Revision[],
    agentsRead: false,
  };
  const mounted = render(<Chat {...base} {...props} />);
  return {
    opened,
    rerender: (next: Partial<Parameters<typeof Chat>[0]>) =>
      mounted.rerender(<Chat {...base} {...props} {...next} />),
  };
}

test("Enter before the agents are read keeps the message on screen, read-only, and sends it once when they arrive", async () => {
  const chat = home();
  fireEvent.change(box(), { target: { value: "What changed this week?" } });
  fireEvent.keyDown(box(), { key: "Enter" });
  assert.ok(await screen.findByRole("status", { name: defaultCopy.chat.gettingReady }));
  // The held words stay in the box, which cannot be edited while they wait.
  await waitFor(() => assert.equal(box().value, "What changed this week?"));
  assert.equal(box().readOnly, true);
  fireEvent.keyDown(box(), { key: "Enter" });
  assert.equal(posts().length, 0, "nothing is sent before the agents are read");
  chat.rerender({ agents: [agent("lead")], agentsRead: true });
  await waitFor(() => assert.deepEqual(chat.opened, ["th-00000000000000ab"]));
  const sent = posts();
  assert.equal(sent.length, 1);
  assert.equal(sent[0]?.path, "/api/sessions");
  assert.equal((sent[0]?.body as { text: string }).text, "What changed this week?");
  assert.equal(screen.queryByRole("status", { name: defaultCopy.chat.gettingReady }), null);
  await waitFor(() => assert.equal(box().value, ""));
  assert.equal(box().readOnly, false);
});

test("a held message is given back to edit with Cancel or Escape, and is not sent", async () => {
  const chat = home();
  fireEvent.change(box(), { target: { value: "Second thoughts" } });
  fireEvent.keyDown(box(), { key: "Enter" });
  await screen.findByRole("status", { name: defaultCopy.chat.gettingReady });
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.cancel }));
  await waitFor(() => assert.equal(box().readOnly, false));
  assert.equal(box().value, "Second thoughts");
  assert.equal(screen.queryByRole("status", { name: defaultCopy.chat.gettingReady }), null);
  // Held again, Escape lets it go the same way.
  fireEvent.keyDown(box(), { key: "Enter" });
  await screen.findByRole("status", { name: defaultCopy.chat.gettingReady });
  await waitFor(() => assert.equal(box().readOnly, true));
  fireEvent.keyDown(box(), { key: "Escape" });
  await waitFor(() => assert.equal(box().readOnly, false));
  assert.equal(box().value, "Second thoughts");
  chat.rerender({ agents: [agent("lead")], agentsRead: true });
  await new Promise((done) => setTimeout(done, 50));
  assert.equal(posts().length, 0, "a message given back is not sent when the agents arrive");
});

test("agents that could not be read are said with Retry, and a held message goes back in the box", async () => {
  let retried = 0;
  const chat = home({ onRetryAgents: () => retried++ });
  fireEvent.change(box(), { target: { value: "Draft to keep" } });
  fireEvent.keyDown(box(), { key: "Enter" });
  await screen.findByRole("status", { name: defaultCopy.chat.gettingReady });
  chat.rerender({ agentsRead: true, agentsFailed: true });
  await screen.findByText(defaultCopy.chat.agentsFailed);
  await waitFor(() => assert.equal(box().value, "Draft to keep"));
  assert.equal(posts().length, 0);
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.retry }));
  assert.equal(retried, 1);
});

test("@ before the agents are read opens the list with a row saying they are loading", async () => {
  const chat = home();
  fireEvent.change(box(), { target: { value: "@" } });
  const row = await screen.findByRole("option", { name: defaultCopy.chat.loadingAgents });
  assert.equal(row.getAttribute("aria-disabled"), "true");
  // Once read, the row gives way to the agents.
  chat.rerender({ agents: [agent("lead")], agentsRead: true });
  fireEvent.change(box(), { target: { value: "@l" } });
  await waitFor(() => assert.ok(screen.getByRole("listbox").textContent?.includes("@lead")));
  assert.equal(screen.queryByRole("option", { name: defaultCopy.chat.loadingAgents }), null);
});

test("agents read with none to answer show Home's empty state, not the chat bar", async () => {
  const chat = home();
  assert.ok(box());
  chat.rerender({ agents: [], agentsRead: true });
  await screen.findByText(/agent needs to be configured/);
  assert.equal(screen.queryByLabelText("Message", { selector: "textarea" }), null);
});
