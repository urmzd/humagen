import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App } from "./App.tsx";
import { stubFetch } from "./dom.ts";

// Home's first load reads the list of runs once. The console reads it on
// arrival, the workspace's stream then connects and replays the run events
// the bus still holds, and neither the connection nor the replay reads the
// list again, since it already shows them.

type Source = {
  url: string;
  onopen: (() => void) | null;
  onmessage: ((m: { data: string }) => void) | null;
};

/** An EventSource a test opens and delivers to by hand. */
function handSources(): { open: Source[]; restore: () => void } {
  const g = globalThis as { EventSource?: unknown };
  const real = g.EventSource;
  const open: Source[] = [];
  g.EventSource = class {
    url: string;
    onopen: (() => void) | null = null;
    onerror: (() => void) | null = null;
    onmessage: ((m: { data: string }) => void) | null = null;
    constructor(url: string) {
      this.url = url;
      open.push(this);
    }
    addEventListener() {}
    close() {
      const at = open.indexOf(this);
      if (at >= 0) open.splice(at, 1);
    }
  };
  return {
    open,
    restore: () => {
      g.EventSource = real;
    },
  };
}

const run = {
  citations: [],
  id: "r1",
  tag_id: "t1",
  tenant: "acme",
  agent: "lead",
  rev: 1,
  origin: "api",
  text: "Summarize the launch plan",
  root: "r1",
  depth: 0,
  thread: "r1",
  lease: 0,
  granted: 0,
  spent: 0,
  calls: 0,
  status: "completed",
  answer: "Done.",
  created_at: "2026-10-01T09:00:00Z",
  updated_at: "2026-10-01T09:00:05Z",
};

const event = (id: string, type: string, time: string) => ({
  data: JSON.stringify({
    id: `${id}:${type}`,
    source: `humagen://run/${id}`,
    type,
    channel: "humagen://acme",
    time,
    hops: 1,
    tenant: "acme",
    run: id,
    agent: "lead",
    data: {},
  }),
});

let net: ReturnType<typeof stubFetch> | undefined;
let sources: ReturnType<typeof handSources> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  sources?.restore();
  net = undefined;
  sources = undefined;
});

const settle = (ms: number) => act(() => new Promise<void>((done) => setTimeout(done, ms)));
const runsRead = () => (net?.seen ?? []).filter((s) => s.path === "/api/runs").length;
const workspaceStream = () =>
  sources?.open.find((s) => s.url.includes("run.accepted.v1") && !s.url.includes("thread="));

async function openHome() {
  window.history.replaceState(null, "", "/");
  sources = handSources();
  net = stubFetch({
    "GET /api/me": () => ({
      principal: { subject: "ada", tenant: "acme", kind: "user", name: "Ada", roles: ["member"] },
      permissions: ["runs:read", "runs:write"],
    }),
    "GET /api/branding": () => ({ name: "Humagen", title: "Humagen" }),
    "GET /api/runs": () => ({ runs: [run] }),
  });
  render(<App />);
  await waitFor(() => assert.equal(runsRead(), 1));
  await waitFor(() => assert.ok(workspaceStream()));
  return workspaceStream() as Source;
}

test("Home's first load reads the list of runs once, through the stream's connection and replay", async () => {
  const stream = await openHome();
  await act(async () => {
    stream.onopen?.();
    // The replay: the events of the run the list already shows.
    stream.onmessage?.(event("r1", "run.accepted.v1", "2026-10-01T09:00:00Z"));
    stream.onmessage?.(event("r1", "run.completed.v1", "2026-10-01T09:00:05Z"));
  });
  // Longer than the wait an event's read is coalesced over.
  await settle(400);
  assert.equal(runsRead(), 1);
});

test("a run event the list does not show yet, and a reconnect, each read the list again", async () => {
  const stream = await openHome();
  await act(async () => {
    stream.onopen?.();
  });
  await act(async () => {
    stream.onmessage?.(event("r2", "run.accepted.v1", "2026-10-01T09:01:00Z"));
    stream.onmessage?.(event("r1", "run.started.v1", "2026-10-01T09:02:00Z"));
  });
  await waitFor(() => assert.equal(runsRead(), 2));
  await settle(400);
  assert.equal(runsRead(), 2);
  // The source reconnected by itself: what was sent meanwhile was missed.
  await act(async () => {
    stream.onopen?.();
  });
  await waitFor(() => assert.equal(runsRead(), 3));
});

test("Home retries the agent directory and links a manager with an empty list to Agents", async () => {
  window.history.replaceState(null, "", "/");
  sources = handSources();
  let reply!: (response: Response) => void;
  const firstRead = new Promise<Response>((resolve) => {
    reply = resolve;
  });
  let reads = 0;
  net = stubFetch({
    "GET /api/me": () => ({
      principal: { subject: "ada", tenant: "acme", kind: "user", name: "Ada", roles: ["manager"] },
      permissions: ["agents:read", "agents:write", "runs:write"],
    }),
    "GET /api/branding": () => ({ name: "Humagen", title: "Humagen" }),
    "GET /api/agents": () => (++reads === 1 ? firstRead : { agents: [] }),
  });
  render(<App />);
  await screen.findByRole("status", { name: "Loading agents" });
  await act(async () => reply(new Response("Unavailable", { status: 503 })));
  await screen.findByText("Agents could not be loaded.");
  assert.equal(screen.queryByRole("link", { name: "Manage Agents" }), null);
  const settingsReads = net.seen.filter((r) => r.path === "/api/settings").length;
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  const manage = await screen.findByRole("link", { name: "Manage Agents" });
  assert.equal(manage.getAttribute("href"), "/agents");
  assert.equal(reads, 2);
  assert.equal(net.seen.filter((r) => r.path === "/api/settings").length, settingsReads);
});
