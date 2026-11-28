import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import type { ResponseTool } from "@humagen/core";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { RunEvent } from "./adapter.ts";
import { type FollowRuns, useCallLive } from "./callLive.ts";

afterEach(cleanup);

const RUN = `r${"7".repeat(32)}`;
const event = (seq: number, type: string, data: unknown = {}, run = RUN): RunEvent =>
  ({
    id: `e${seq}`,
    source: "humagen:acme",
    type,
    channel: "humagen://acme",
    time: new Date(Date.UTC(2026, 10, 7, 12, 0, seq)).toISOString(),
    hops: 0,
    tenant: "acme",
    run,
    seq,
    agent: "writer",
    version: "1.0.0",
    data,
  }) as RunEvent;

const call = (status: ResponseTool["status"]): ResponseTool =>
  ({
    type: "tool",
    id: "c1",
    name: "call_agent",
    kind: "agent",
    agent: "writer",
    runId: RUN,
    input: {},
    depth: 1,
    status,
    content: [],
  }) as ResponseTool;

/** A session stream a test speaks for. */
function stream() {
  const open = new Set<(event: RunEvent) => void>();
  const follow: FollowRuns = (_session, run) => {
    open.add(run);
    return () => open.delete(run);
  };
  return {
    follow,
    send: (e: RunEvent) => {
      for (const f of open) f(e);
    },
    open: () => open.size,
  };
}

test("a called agent's words are shown as it writes them: what the run said so far, then each event once", async () => {
  const s = stream();
  let release: (v: { events: RunEvent[] }) => void = () => {};
  const load = () => new Promise<{ events: RunEvent[] }>((r) => (release = r));
  const { result, rerender } = renderHook(
    ({ status }: { status: ResponseTool["status"] }) =>
      useCallLive(call(status), "s_1", load, s.follow),
    { initialProps: { status: "running" as ResponseTool["status"] } },
  );
  // Read through a function: an assert on the value would narrow it for the rest of the test.
  const said = (): string | undefined => result.current?.text;
  assert.equal(s.open(), 1);
  // An event that arrives before the run's past is read waits for it.
  act(() => s.send(event(4, "run.delta.text.v1", { text: " to find" })));
  assert.equal(said(), undefined);
  await act(async () => {
    release({
      events: [
        event(1, "run.accepted.v1"),
        event(2, "run.started.v1"),
        event(3, "run.delta.text.v1", { text: "I am going" }),
        // The read overlaps what the stream already brought.
        event(4, "run.delta.text.v1", { text: " to find" }),
      ],
    });
  });
  await waitFor(() => assert.equal(said(), "I am going to find"));
  act(() => {
    // A repeat, as a stream that connects again sends, and another run's words.
    s.send(event(3, "run.delta.text.v1", { text: "I am going" }));
    s.send(event(9, "run.delta.text.v1", { text: " elsewhere" }, `r${"8".repeat(32)}`));
    s.send(event(5, "run.delta.text.v1", { text: " the file." }));
  });
  assert.equal(said(), "I am going to find the file.");
  // Once the call has ended its record speaks, and the stream is let go.
  rerender({ status: "done" });
  assert.equal(said(), undefined);
  assert.equal(s.open(), 0);
});

test("a call outside a session, or one that is not running, follows nothing", () => {
  const s = stream();
  const load = async () => ({ events: [] });
  const outside = renderHook(() => useCallLive(call("running"), "th-1", load, s.follow));
  assert.equal(outside.result.current, undefined);
  const ended = renderHook(() => useCallLive(call("done"), "s_1", load, s.follow));
  assert.equal(ended.result.current, undefined);
  assert.equal(s.open(), 0);
});
