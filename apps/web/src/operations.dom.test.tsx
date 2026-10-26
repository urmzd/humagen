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

test("local resume never reports accepting work beneath a workspace pause", async () => {
  net = stubFetch({
    "GET /api/agents/helper/control": () => ({
      ...control,
      agent: "helper",
      effective_paused: true,
      pause_reason: "workspace",
    }),
  });
  render(<AgentControls agent="helper" canWrite />);
  await screen.findByText(
    "Workspace pause is in effect. Resuming this agent does not resume the workspace.",
  );
  assert.ok(!screen.queryByText("Accepting new work"));
});

test("policy editor shows the inherited effective Never and locks weaker choices", async () => {
  net = stubFetch({
    "GET /api/agents/helper/action-policy": () => ({
      scope: { kind: "agent", name: "helper" },
      rules: { external_messages: "allow" },
      version: 1,
      effective: {
        external_messages: {
          level: "never",
          inherited: "never",
          locked: true,
          sources: [{ kind: "workspace", label: "", level: "never" }],
        },
      },
    }),
  });
  render(<ActionPolicies scope="agent" name="helper" canWrite />);
  await screen.findByText("Restricted by", { exact: false });
  const select = screen.getByLabelText("Send external messages") as HTMLSelectElement;
  assert.equal(select.value, "never");
  assert.equal(select.disabled, true);
});

test("reminder controls save bounded native timer values in seconds", async () => {
  net = stubFetch({
    "GET /api/agent-controls": () => control,
    "PUT /api/agent-controls": (body) => ({ ...control, ...(body as object), version: 3 }),
  });
  render(<AgentControls canWrite />);
  const unanswered = await screen.findByLabelText(
    "Remind about unanswered questions after (minutes)",
  );
  fireEvent.change(unanswered, { target: { value: "5" } });
  fireEvent.change(screen.getByLabelText("Remind about blocked work after (minutes)"), {
    target: { value: "10" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save Changes" }));
  await waitFor(() =>
    assert.equal(
      (
        net?.seen.find((request) => request.method === "PUT")?.body as {
          reminder_after_seconds?: number;
        }
      )?.reminder_after_seconds,
      300,
    ),
  );
  assert.equal(
    (
      net?.seen.find((request) => request.method === "PUT")?.body as {
        blocked_reminder_after_seconds: number;
      }
    ).blocked_reminder_after_seconds,
    600,
  );
});

test("an expired deadline is corrected with its recorded version before retry", async () => {
  const assignment = {
    id: "late",
    agent: "helper",
    owner: "ada",
    project: "",
    goal: "Review",
    done_when: "Report",
    state: "needs_person",
    deadline_missed: true,
    due_at: "2020-01-01T00:00:00Z",
    version: 3,
  };
  net = stubFetch({
    "GET /api/assignments": () => [assignment],
    "PUT /api/assignments/late": (body) => ({ ...assignment, ...(body as object), version: 4 }),
  });
  render(<Assignments agents={["helper"]} projects={[]} subject="ada" canWrite onRun={() => {}} />);
  await screen.findByText("This deadline has passed. Review it before starting or resuming work.");
  assert.equal(
    (screen.getByRole("button", { name: "Run Assignment" }) as HTMLButtonElement).disabled,
    true,
  );
  fireEvent.click(screen.getByText("Review Deadline"));
  fireEvent.input(screen.getByLabelText("Due date"), { target: { value: "2099-01-02T12:00" } });
  assert.equal((screen.getByLabelText("Due date") as HTMLInputElement).value, "2099-01-02T12:00");
  fireEvent.click(screen.getByRole("button", { name: "Save Deadline" }));
  await waitFor(() => assert.ok(net?.seen.find((request) => request.method === "PUT")));
  const body = net?.seen.find((request) => request.method === "PUT")?.body as {
    version: number;
    due_at: string;
  };
  assert.equal(body.version, 3);
  assert.equal(body.due_at, new Date("2099-01-02T12:00").toISOString());
});

test("handoff review exposes its owner and immutable input references", async () => {
  net = stubFetch({
    "GET /api/agent-network": () => ({
      edges: [{ from: "lead", to: "helper", handoffs: 1, waiting: 1, blocked: 0 }],
      truncated: false,
    }),
    "GET /api/handoffs": () => [
      {
        id: "h",
        from: "lead",
        to: "helper",
        goal: "Review",
        done_when: "Saved",
        state: "accepted",
        owner: "private-subject",
        owner_name: "Mira",
        due_at: "2099-01-01T12:00:00Z",
        inputs: [
          { type: "text", reference: "Review the immutable draft" },
          { type: "run", reference: "original-run" },
          { type: "thread", reference: "room" },
          { type: "artifact", reference: "revision-id@3" },
        ],
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
  await screen.findByText("Owner: Mira");
  assert.ok(screen.getByText("Review the immutable draft"));
  assert.equal(
    screen.getByRole("link", { name: "Open Input Artifact (Version 3)" }).getAttribute("href"),
    "/artifacts/revision-id",
  );
  assert.equal(
    screen.getByRole("link", { name: "Open Input Conversation" }).getAttribute("href"),
    "/conversations/room",
  );
  fireEvent.click(screen.getByRole("button", { name: "Open Input Run" }));
  assert.equal(opened, "original-run");
  assert.ok(!screen.queryByText("private-subject"));
});

test("Agent Detail assignments filter on the server and preselect the same agent", async () => {
  net = stubFetch({
    "GET /api/assignments": (_body, query) => {
      assert.equal(query.get("agent"), "helper");
      return [];
    },
  });
  render(
    <Assignments
      agent="helper"
      agents={["lead", "helper"]}
      projects={[]}
      subject="ada"
      canWrite
      onRun={() => {}}
    />,
  );
  await screen.findByText("No assignments are visible here yet.");
  assert.ok(screen.getByRole("button", { name: "Cancel New Assignment" }));
  assert.equal((screen.getByLabelText("Agent") as HTMLSelectElement).value, "helper");
});

test("controls refresh effective pause without overwriting a reviewed draft", async () => {
  let current = {
    ...control,
    agent: "helper",
    effective_paused: false,
    pause_reason: undefined as string | undefined,
  };
  net = stubFetch({ "GET /api/agents/helper/control": () => current });
  const original = window.setInterval;
  let refresh: (() => void) | undefined;
  window.setInterval = ((callback: () => void, delay: number) => {
    if (delay === 5000) {
      refresh = callback;
      return 1;
    }
    return original.call(window, callback, delay);
  }) as typeof window.setInterval;
  try {
    render(<AgentControls agent="helper" canWrite />);
    const stopRules = await screen.findByLabelText("Stop rules and reminders");
    fireEvent.change(stopRules, { target: { value: "Keep my reviewed draft" } });
    current = { ...current, version: 3, effective_paused: true, pause_reason: "workspace" };
    refresh?.();
    await screen.findByText(
      "Workspace pause is in effect. Resuming this agent does not resume the workspace.",
    );
    assert.equal((stopRules as HTMLTextAreaElement).value, "Keep my reviewed draft");
    assert.equal(
      (screen.getByRole("button", { name: "Save Changes" }) as HTMLButtonElement).disabled,
      true,
    );
    fireEvent.click(screen.getByRole("button", { name: "Review Latest Record" }));
    assert.equal((stopRules as HTMLTextAreaElement).value, "");
    assert.equal(
      (screen.getByRole("button", { name: "Save Changes" }) as HTMLButtonElement).disabled,
      false,
    );
  } finally {
    cleanup();
    window.setInterval = original;
  }
});

test("stale default bindings expose a review link without granting a resume or deleting bindings", async () => {
  net = stubFetch({
    "GET /api/agents/reviewer/control": () => ({
      ...control,
      state: "needs_review",
      needs_skill_review: true,
    }),
  });
  render(<AgentControls agent="reviewer" canWrite />);
  const link = await screen.findByRole("link", { name: "Review Default Skills" });
  assert.equal(link.getAttribute("href"), "/agents/reviewer/context");
  assert.equal(
    net.seen.some((request) => request.method !== "GET"),
    false,
  );
});
