import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Run, RunEvent } from "./adapter.ts";
import { stubFetch } from "./dom.ts";
import { SessionPlace } from "./SessionPlace.tsx";
import { TurnTrace } from "./TurnTrace.tsx";
import { placeOf } from "./trace.ts";

const words = defaultCopy.trace;
let restore: (() => void) | undefined;
afterEach(() => {
  cleanup();
  restore?.();
  restore = undefined;
});

const at = (s: number) => new Date(Date.UTC(2026, 10, 7, 12, 0, s)).toISOString();
const run = (id: string, extra: Partial<Run> = {}): Run => ({
  id,
  tag_id: id,
  tenant: "acme",
  agent: "humagen",
  rev: 1,
  origin: "slack",
  text: "why did the deploy fail",
  root: "r1",
  depth: 0,
  thread: "t1",
  lease: 0,
  granted: 0,
  spent: 12000,
  calls: 0,
  status: "completed",
  created_at: at(0),
  updated_at: at(14),
  citations: [{ id: "c1", title: "Deploy runbook", url: "https://docs.example/runbook" }],
  ...extra,
});
const ev = (type: string, data: unknown, s: number) => ({
  id: `${type}-${s}`,
  source: "test",
  type,
  channel: "c",
  time: at(s),
  hops: 0,
  data,
});

const root = run("r1", { picked: "It asks about a deploy, which ops owns" });
const child = run("r2", {
  agent: "ops",
  parent: "r1",
  depth: 1,
  spent: 0,
  citations: [],
  created_at: at(2),
  updated_at: at(9),
});

function stub() {
  const net = stubFetch({
    "GET /api/runs/r1/trace": () => ({ runs: [root, child], spans: [] }),
    "GET /api/runs/r1/events": () => ({
      complete: true,
      events: [
        ev(
          "run.delta.tool.call.v1",
          {
            id: "g",
            name: "get_context",
            phase: "requested",
            arguments: { request: "what did people say" },
          },
          1,
        ),
        ev(
          "run.delta.tool.done.v1",
          {
            id: "g",
            result: JSON.stringify({
              text: "Ana: the deploy failed at 02:10.\nBo: the migration timed out.",
              sources: [
                { title: "#eng thread", url: "https://app.slack.com/client/T1/C1/thread/C1-1.2" },
              ],
              reference: "slack://T1/C1/1.2",
            }),
          },
          2,
        ),
      ],
    }),
    "GET /api/runs/r2/events": () => ({
      complete: true,
      events: [
        ev(
          "run.delta.tool.call.v1",
          { id: "a", name: "deploy_logs", phase: "requested", arguments: { job: "nightly" } },
          3,
        ),
        ev("run.delta.tool.done.v1", { id: "a", result: "migration 0042 timed out" }, 4),
        ev(
          "run.delta.tool.call.v1",
          { id: "b", name: "hr_lookup", phase: "requested", arguments_bytes: 40 },
          5,
        ),
        ev(
          "run.parked.v1",
          {
            question: {
              id: "q1",
              kind: "approval",
              prompt: "Retry deploy?",
              tool: { name: "retry_deploy", call_id: "x" },
            },
          },
          6,
        ),
        ev(
          "run.resumed.v1",
          { answer: { question_id: "q1", decision: "approve", by_name: "Ana" } },
          7,
        ),
      ] as unknown as RunEvent[],
    }),
  });
  restore = net.restore;
  return net;
}

test("closed, the trace is one quiet line, and nothing is fetched", () => {
  const net = stub();
  render(<TurnTrace run={root} />);
  const button = screen.getByRole("button", { name: /Trace/ });
  assert.equal(button.getAttribute("aria-expanded"), "false");
  assert.match(button.textContent ?? "", /Trace · 1 source · 14s · \$0\.01/);
  assert.equal(net.seen.length, 0);
});

test("open, it shows the pick, nested agents, context reads, tools, sources, approvals, time and cost", async () => {
  const net = stub();
  render(<TurnTrace run={root} origin="slack://T1/C1/1.2" />);
  fireEvent.click(screen.getByRole("button", { name: /Trace/ }));
  await screen.findByText(words.picked("It asks about a deploy, which ops owns"));
  const nodes = screen.getAllByTestId("trace-node").map((n) => n.getAttribute("data-run"));
  assert.deepEqual(nodes, ["r1", "r2"]);
  // get_context: the request, the origin it read (linked), what came back, its sources.
  const read = screen.getByTestId("trace-context");
  assert.match(read.textContent ?? "", /Read context “what did people say” from Slack thread/);
  assert.match(
    read.textContent ?? "",
    /Ana: the deploy failed at 02:10\. Bo: the migration timed out\./,
  );
  assert.equal(
    screen.getByRole("link", { name: "Slack thread" }).getAttribute("href"),
    "https://app.slack.com/client/T1/C1/thread/C1-1.2",
  );
  assert.ok(screen.getByRole("link", { name: "#eng thread" }));
  assert.ok(screen.getByRole("link", { name: "Deploy runbook" }));
  // Tools with their input and output; one whose input was kept back says so once.
  assert.ok(screen.getByText("deploy_logs"));
  assert.ok(screen.getByText("migration 0042 timed out"));
  assert.ok(screen.getByText(words.hiddenHere));
  assert.equal(screen.getAllByText(words.withheld).length, 1);
  // Approvals and who decided.
  assert.match(
    screen.getByText("retry_deploy").parentElement?.textContent ?? "",
    /Approved by Ana/,
  );
  const line = screen.getByRole("button", { name: /Trace/ });
  assert.equal(line.getAttribute("aria-expanded"), "true");
  assert.match(
    line.textContent ?? "",
    /2 agents · 1 context read · 2 tools · 1 source · 1 approval · 14s · \$0\.01/,
  );
  assert.deepEqual(net.seen.map((r) => r.path).sort(), [
    "/api/runs/r1/events",
    "/api/runs/r1/trace",
    "/api/runs/r2/events",
  ]);
});

test("a trace that cannot be read says so and can be tried again", async () => {
  const net = stubFetch({});
  restore = net.restore;
  render(<TurnTrace run={root} />);
  fireEvent.click(screen.getByRole("button", { name: /Trace/ }));
  await screen.findByText(words.failed);
  restore();
  stub();
  fireEvent.click(screen.getByRole("button", { name: words.retry }));
  await waitFor(() => assert.ok(screen.getAllByTestId("trace-node").length > 0));
});

test("the header names where the session started, links back, and says who can read it", () => {
  render(
    <SessionPlace
      place={placeOf(
        { surface: { client: "acme", channel: "C1", ts: "1.2" }, visibility: "shared" },
        "T1",
      )}
    />,
  );
  const link = screen.getByRole("link", { name: "Slack thread" });
  assert.equal(link.getAttribute("href"), "https://app.slack.com/client/T1/C1/thread/C1-1.2");
  assert.match(screen.getByTestId("session-place").textContent ?? "", /Slack channel/);
  cleanup();
  render(<SessionPlace place={placeOf({ visibility: "private" })} />);
  assert.match(screen.getByTestId("session-place").textContent ?? "", /Console.*Private/);
});
