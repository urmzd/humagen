import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Run } from "./adapter.ts";
import type { Revision, ThreadPage } from "./api.ts";
import { Chat } from "./Chat.tsx";
import { stubEventSource, stubFetch } from "./dom.ts";

const at = "2026-10-01T09:00:00Z";
const helper = { spec: { name: "helper", description: "Renewals and billing", model: "offline", provider: "offline", system_prompt: "Help." }, rev: 1 } as unknown as Revision;
const id = "api-r-conversation";

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

const record = { id, project: "", channel: "general", visibility: "shared", participants: [], version: 0, created_by: "00u-ana", created_at: at, updated_at: at };

let net: ReturnType<typeof stubFetch> | undefined;
let restoreEvents: (() => void) | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  restoreEvents?.();
  net = undefined;
  restoreEvents = undefined;
});

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
  assert.deepEqual(sent?.body, { agent: "", text: "and then?", after: "r200" });
});
