import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ActionPolicies } from "./ActionPolicies.tsx";
import { AgentControls } from "./AgentControls.tsx";
import { AgentNetwork } from "./AgentNetwork.tsx";
import { Assignments } from "./Assignments.tsx";
import { stubFetch } from "./dom.ts";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});
const control = {
  paused: false,
  version: 2,
  updated_by: "ada",
  updated_at: "2026-10-25T12:00:00Z",
};

test("pausing waits for the recorded control and sends its current version", async () => {
  net = stubFetch({
    "GET /api/agent-controls": () => control,
    "PUT /api/agent-controls": (body) => ({ ...control, ...(body as object), version: 3 }),
  });
  render(<AgentControls canWrite />);
  fireEvent.click(await screen.findByRole("button", { name: "Pause All Agents" }));
  await screen.findByRole("button", { name: "Resume All Agents" });
  assert.deepEqual(net.seen.find((r) => r.method === "PUT")?.body, {
    paused: true,
    manager: "",
    stop_rules: "",
    version: 2,
  });
});

test("a refused pause keeps accepting-work state and shows the server refusal", async () => {
  net = stubFetch({
    "GET /api/agent-controls": () => control,
    "PUT /api/agent-controls": () =>
      new Response(JSON.stringify({ error: "reload the version" }), { status: 409 }),
  });
  render(<AgentControls canWrite />);
  fireEvent.click(await screen.findByRole("button", { name: "Pause All Agents" }));
  await screen.findByText("reload the version");
  assert.ok(screen.getByRole("button", { name: "Pause All Agents" }));
  assert.ok(!screen.queryByRole("button", { name: "Resume All Agents" }));
});

test("action policies save explicit tightening rules, never an invented tool grant", async () => {
  const policy = {
    scope: { kind: "workspace" },
    rules: { call_agents: "ask" },
    version: 4,
    updated_by: "ada",
    updated_at: control.updated_at,
  };
  net = stubFetch({
    "GET /api/action-policy": () => policy,
    "PUT /api/action-policy": (body) => ({ ...policy, ...(body as object), version: 5 }),
  });
  render(<ActionPolicies canWrite />);
  const select = await screen.findByRole("combobox", { name: "Call agents" });
  fireEvent.change(select, { target: { value: "never" } });
  fireEvent.click(screen.getByRole("button", { name: "Save Changes" }));
  await waitFor(() => assert.equal(net?.seen.filter((r) => r.method === "PUT").length, 1));
  assert.deepEqual(net.seen.find((r) => r.method === "PUT")?.body, {
    rules: { call_agents: "never" },
    version: 4,
  });
});

test("an assignment opens the actual returned run and keeps its recorded state", async () => {
  const assignment = {
    id: "a",
    agent: "helper",
    project: "",
    owner: "ada",
    goal: "Review queue",
    done_when: "Report recorded",
    state: "requested",
    version: 1,
    updated_by: "ada",
    updated_at: control.updated_at,
    created_at: control.updated_at,
  };
  net = stubFetch({
    "GET /api/assignments": () => [assignment],
    "POST /api/assignments/a/run": () => ({ id: "real-run" }),
  });
  let opened = "";
  render(
    <Assignments
      agents={["helper"]}
      projects={[]}
      subject="ada"
      canWrite
      onRun={(id) => {
        opened = id;
      }}
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: "Run Assignment" }));
  await waitFor(() => assert.equal(opened, "real-run"));
  assert.ok(screen.getByText("Requested"));
  assert.ok(!screen.queryByText("Result recorded"));
});

test("the network uses recorded visible edges and links to the exact handoff run", async () => {
  net = stubFetch({
    "GET /api/agent-network": () => ({
      edges: [{ from: "lead", to: "helper", handoffs: 2, waiting: 1, blocked: 1 }],
      truncated: false,
    }),
    "GET /api/handoffs": () => [
      {
        id: "h",
        from: "lead",
        to: "helper",
        goal: "Review draft",
        done_when: "Review saved",
        state: "blocked",
        blocker: "Missing source",
        run_id: "child-run",
      },
    ],
  });
  let opened = "";
  render(
    <AgentNetwork
      onAgent={() => {}}
      onRun={(id) => {
        opened = id;
      }}
    />,
  );
  await screen.findByText("Missing source", { exact: false });
  fireEvent.click(screen.getByRole("button", { name: "Open Recorded Run" }));
  assert.equal(opened, "child-run");
  assert.ok(screen.getByText("Handoffs: 2"));
});
