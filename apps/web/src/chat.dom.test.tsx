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

/** The same turn, answered. */
const answered = (actor: Run["actor"], tag = "api-t1"): Run => {
  const { error: _, ...turn } = failed(actor, tag);
  return { ...turn, status: "completed", answer: "Filed." };
};

/** A retry warp asked for nobody: it names whoever pressed it, and none of its words were theirs. */
const NOBODY_RETRY = "api-retry-3-ben-t0";

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

/** Open conversation t1 as me. Of turns at one point, the newest is on screen. */
function open(turn: Run | Run[], me: string) {
  const turns = Array.isArray(turn) ? turn : [turn];
  const last = turns[turns.length - 1] as Run;
  net = stubFetch({
    "GET /api/threads/t1": () => ({ turns }),
    "POST /api/runs": (body) => ({ ...last, id: "t2", text: (body as { text: string }).text, status: "accepted", actor: { id: me, user: me } }),
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
  for (const [actor, tag, says] of [
    [{ id: "ana", display: "Ana", user: "ana" }, "api-t1", "This turn acted for Ana, so only they can retry it."],
    [{ id: "slack-app", bot: true }, "api-t1", "This turn acted for nobody, so it is not retried here, where it would act for you."],
    // A person who pressed Retry on a turn that acted for nobody, such as a machine's.
    [{ id: "ben", display: "Ben" }, NOBODY_RETRY, "This turn acted for nobody, so it is not retried here, where it would act for you."],
  ] as const) {
    open(failed(actor, tag), "ben");
    await screen.findByText(says);
    assert.ok(!screen.queryByRole("button", { name: "Retry" }));
    // It can be retried, by someone else, so it does not say it cannot.
    assert.ok(!screen.queryByText(/can't be retried/));
    assert.ok(!screen.queryByText(/credentials/), "a turn's actor does not say whose credentials it used");
    assert.ok(!net.seen.some((r) => r.method === "POST"));
    cleanup();
    net.restore();
  }
  net = stubFetch({});
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
    cleanup();
    net.restore();
  }
  open(answered({ id: "ben", display: "Ben", user: "ben" }), "ben");
  fireEvent.click(await screen.findByRole("button", { name: "Regenerate" }));
  await waitFor(() => assert.ok(net.seen.some((r) => r.method === "POST" && r.path === "/api/runs")));
});

test("a reader's own turn from before warp said whom a run acts for may be asked again", async () => {
  // Recorded before actor.user existed: the person who asked, and no more.
  const before = { id: "ben", display: "Ben" };
  open(failed(before, "api-r-0123456789abcdefghijkl"), "ben");
  fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
  await waitFor(() => assert.ok(net.seen.some((r) => r.method === "POST" && r.path === "/api/runs")));
  cleanup();
  net.restore();
  open(answered(before, "api-3-ben-k1"), "ben");
  fireEvent.click(await screen.findByRole("button", { name: "Regenerate" }));
  await waitFor(() => assert.ok(net.seen.some((r) => r.method === "POST" && r.path === "/api/runs")));
  cleanup();
  net.restore();
  // Someone else's is still not the reader's.
  open(failed(before, "api-r-0123456789abcdefghijkl"), "ana");
  await screen.findByText("This turn acted for Ben, so only they can retry it.");
  assert.ok(!screen.queryByRole("button", { name: "Retry" }));
});

test("a turn from before warp said whom a run acts for is not the reader's when its words were someone else's", async () => {
  // Before actor.user, the console's Retry and Regenerate asked an earlier
  // turn's words again at the same point, as an ordinary turn of whoever
  // pressed, and the Slack app asked for a person it maps under its own
  // key, whatever message the words came from.
  const anas: Run = { ...answered({ id: "ana", display: "Ana" }, "api-r-aaaaaaaaaaaaaaaaaaaaaa"), id: "t0", root: "t0" };
  const bens = { id: "ben", display: "Ben" };
  for (const turn of [
    answered(bens, "api-r-bbbbbbbbbbbbbbbbbbbbbb"),
    failed(bens, "api-r-bbbbbbbbbbbbbbbbbbbbbb"),
  ]) {
    open([anas, turn], "ben");
    await screen.findByText(turn.status === "failed" ? "This turn acted for nobody, so it is not retried here, where it would act for you." : "Filed.");
    assert.ok(!screen.queryByRole("button", { name: "Regenerate" }), "ben's Regenerate of ana's words is not his");
    assert.ok(!screen.queryByRole("button", { name: "Retry" }), "ben's Retry of ana's words is not his");
    cleanup();
    net.restore();
  }
  for (const turn of [answered(bens, "api-9-svc-slack-slack-T1-C1-1700000000-000100"), failed(bens, "api-9-svc-slack-slack-T1-C1-1700000000-000100")]) {
    open(turn, "ben");
    await screen.findByText(turn.status === "failed" ? "This turn acted for nobody, so it is not retried here, where it would act for you." : "Filed.");
    assert.ok(!screen.queryByRole("button", { name: "Regenerate" }), "a turn the Slack app asked for ben is not his here");
    assert.ok(!screen.queryByRole("button", { name: "Retry" }));
    cleanup();
    net.restore();
  }
  // His own words asked again by him are still his.
  open([{ ...anas, actor: bens, tag_id: "api-r-cccccccccccccccccccccc" }, failed(bens, "api-r-bbbbbbbbbbbbbbbbbbbbbb")], "ben");
  fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
  await waitFor(() => assert.ok(net.seen.some((r) => r.method === "POST" && r.path === "/api/runs")));
});
