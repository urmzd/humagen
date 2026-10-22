import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Run } from "./adapter.ts";
import type { Revision } from "./api.ts";
import { Chat } from "./Chat.tsx";
import { stubFetch } from "./dom.ts";

const at = "2026-10-01T09:00:00Z";
const lead: Revision = {
  rev: 1,
  spec: { name: "lead", description: "Answers.", provider: "offline", model: "offline", system_prompt: "Answer.", access: { spawn: [] } },
  created_at: at,
  created_by: "warp",
  hash: "h1",
};

/** A turn in conversation t1 that failed, raised by actor. */
const failed = (actor: Run["actor"]): Run => ({
  id: "t1",
  tag_id: "api-t1",
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

/** A stream that never says anything: the retried run is followed, and nothing here needs its events. */
class QuietStream {
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((m: MessageEvent) => void) | null = null;
  addEventListener() {}
  close() {}
}
(globalThis as { EventSource?: unknown }).EventSource ??= QuietStream;

let net: ReturnType<typeof stubFetch>;
afterEach(() => {
  cleanup();
  net.restore();
});

function open(turn: Run, me: string) {
  net = stubFetch({
    "GET /api/threads/t1": () => ({ turns: [turn] }),
    "POST /api/runs": (body) => ({ ...turn, id: "t2", text: (body as { text: string }).text, status: "accepted", actor: { id: me, user: me } }),
  });
  const noop = () => {};
  render(
    <Chat
      threadId="t1"
      me={me}
      summary={undefined}
      tick={0}
      agents={[lead]}
      settings={null}
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
  await waitFor(() => assert.ok(net.seen.some((r) => r.method === "POST" && r.path === "/api/runs")));
  const asked = net.seen.find((r) => r.method === "POST" && r.path === "/api/runs");
  assert.deepEqual(asked?.body, { agent: "lead", text: "file the flaky test" });
});

test("Retry is not offered on another person's turn, nor on one that acted for nobody, and nothing is asked", async () => {
  for (const [actor, says] of [
    [{ id: "ana", display: "Ana", user: "ana" }, "This turn acted for Ana, with their own credentials, so only they can retry it."],
    [{ id: "slack-app", bot: true }, "This turn acted for nobody, so it is not retried here, where it would act for you."],
    // A person who raised a run that acted for nobody, such as a retry of a machine's turn.
    [{ id: "ben" }, "This turn acted for nobody, so it is not retried here, where it would act for you."],
  ] as const) {
    open(failed(actor), "ben");
    await screen.findByText(says);
    assert.equal(screen.queryByRole("button", { name: "Retry" }), null);
    assert.ok(!net.seen.some((r) => r.method === "POST"));
    cleanup();
    net.restore();
  }
  net = stubFetch({});
});
