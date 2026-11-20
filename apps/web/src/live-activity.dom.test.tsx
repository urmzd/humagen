import assert from "node:assert/strict";
import { test } from "node:test";
import type { Run } from "./adapter.ts";
import type { MessageTrace, TraceNode } from "./api.ts";
import {
  type Follow,
  followTraces,
  runsOf,
  type TraceLoader,
  traceSource,
} from "./responseSource.ts";

const tick = (ms: number) => new Promise((r) => setTimeout(r, ms));

/** A session stream a test speaks for: send(run, type) is one run event. */
function fakeFollow() {
  const open = new Set<(run: string, type: string) => void>();
  let stopped = 0;
  const follow: Follow = (_thread, onRun) => {
    open.add(onRun);
    return () => {
      open.delete(onRun);
      stopped++;
    };
  };
  return {
    follow,
    send: (run: string, type = "run.delta.tool.call.v1") => {
      for (const f of open) f(run, type);
    },
    open: () => open.size,
    stopped: () => stopped,
  };
}

test("a running answer is read again on its runs' work, at most once a wait, and not for others' runs", async () => {
  const s = fakeFollow();
  let reads = 0;
  const stop = followTraces(
    "s_1",
    (r) => r === "r_mine",
    async () => void reads++,
    s.follow,
    30,
  );
  s.send("r_other");
  await tick(50);
  assert.equal(reads, 0, "another answer's work is not this one's");
  s.send("r_mine");
  s.send("r_mine");
  s.send("r_mine");
  await tick(50);
  assert.equal(reads, 1, "a burst is one read");
  s.send("r_new", "run.started.v1");
  await tick(50);
  assert.equal(reads, 2, "a run starting may be one it called");
  stop();
  assert.equal(s.open(), 0);
  s.send("r_mine");
  await tick(50);
  assert.equal(reads, 2, "stopped means stopped");
});

test("a read still going holds the next until it ends", async () => {
  const s = fakeFollow();
  let reads = 0;
  let release = () => {};
  const changed = () => {
    reads++;
    return new Promise<void>((r) => {
      release = r;
    });
  };
  const stop = followTraces("s_1", () => true, changed, s.follow, 0);
  s.send("r");
  await tick(10);
  s.send("r");
  await tick(10);
  assert.equal(reads, 1, "no second read while the first reads");
  release();
  await tick(10);
  assert.equal(reads, 2, "the held one follows");
  release();
  stop();
});

const at = (s: number) => new Date(Date.UTC(2026, 10, 9, 12, 0, s)).toISOString();
const node = (
  status: TraceNode["status"],
  tools: TraceNode["tools"],
  children: TraceNode[] = [],
): TraceNode => ({
  run: "r_root",
  agent: "lead",
  status,
  started_at: at(0),
  ...(status === "completed" ? { ended_at: at(9), answer: "Done." } : {}),
  ms: 1000,
  tools,
  context: [],
  sources: [],
  approvals: [],
  children,
});
const tool = (n: number, ms?: number) => ({
  id: `htu_r_root_${n}`,
  name: `step_${n}`,
  at: at(n),
  ...(ms === undefined ? {} : { ms }),
  input: {},
  withheld: false,
});
const trace = (n: TraceNode): MessageTrace => ({
  message: "s_1:r_root",
  nodes: [n],
  hidden: 0,
  truncated: false,
});

test("a running answer's source sends its trace, then each new read whole, and stops once it ends", async () => {
  const s = fakeFollow();
  const reads = [
    trace(node("running", [tool(1, 5)])),
    trace(node("running", [tool(1, 5), tool(2)])),
    trace(node("completed", [tool(1, 5), tool(2, 5)])),
  ];
  let read = 0;
  const loader: TraceLoader = {
    trace: async (id) => {
      assert.equal(id, "s_1:r_root");
      return reads[Math.min(read++, reads.length - 1)] as MessageTrace;
    },
    follow: s.follow,
  };
  const run = { id: "r_root", thread: "s_1", status: "running" } as Run;
  const first: string[] = [];
  const snapshots: number[] = [];
  let ended = 0;
  const close = traceSource(run, loader, 10).open(
    (e) => first.push(e.type),
    () => ended++,
    (events) => snapshots.push(events.filter((e) => e.type === "content_block_start").length),
  );
  await tick(20);
  assert.equal(ended, 1, "the first read is the response");
  assert.equal(first[0], "message_start");
  assert.equal(s.open(), 1, "it follows the session while it runs");
  s.send("r_root");
  await tick(30);
  s.send("r_root");
  await tick(30);
  assert.deepEqual(snapshots.length, 2, "each read is sent whole");
  assert.equal(s.open(), 0, "it stops following once the trace says it ended");
  assert.ok(runsOf(reads[2] as MessageTrace).has("r_root"));
  close();
});

test("an ended answer is read once and never followed", async () => {
  const s = fakeFollow();
  const loader: TraceLoader = {
    trace: async () => trace(node("completed", [tool(1, 5)])),
    follow: s.follow,
  };
  let ended = 0;
  traceSource({ id: "r_root", thread: "s_1", status: "completed" } as Run, loader).open(
    () => {},
    () => ended++,
    () => assert.fail("no second read"),
  );
  await tick(10);
  assert.equal(ended, 1);
  assert.equal(s.open(), 0);
});

test("Activity on a running answer shows new work as it is read, and says when a call waits", async () => {
  const { cleanup, render, screen, waitFor } = await import("@testing-library/react");
  const { ResponseActivityPanel } = await import("./ResponseActivity.tsx");
  const s = fakeFollow();
  const waitingNode = {
    ...node("parked", [tool(1, 5), tool(2)]),
    approvals: [{ id: "q", tool: "htu_r_root_2", action: "step_2", decision: "ask" as const }],
  };
  const reads = [trace(node("running", [tool(1, 5)])), trace(waitingNode)];
  let read = 0;
  const loader: TraceLoader = {
    trace: async () => reads[Math.min(read++, reads.length - 1)] as MessageTrace,
    follow: s.follow,
  };
  const run = { id: "r_root", thread: "s_1", status: "running" } as Run;
  try {
    render(<ResponseActivityPanel source={traceSource(run, loader, 10)} onClose={() => {}} />);
    await waitFor(() => assert.equal(screen.queryAllByTestId("activity-part").length, 1));
    s.send("r_root");
    await waitFor(() => assert.equal(screen.queryAllByTestId("activity-part").length, 2));
    const parts = screen.getAllByTestId("activity-part");
    assert.equal(parts[1]?.getAttribute("data-status"), "awaiting_approval");
    await waitFor(
      () =>
        assert.match(
          document.querySelector("[data-testid=response-activity] output")?.textContent ?? "",
          /waiting for you/,
        ),
      { timeout: 6000 },
    );
  } finally {
    cleanup();
  }
});
