import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import type { Turn } from "@humagen/core";
import { WebSurface } from "@humagen/render-web";
import { AgentMessage } from "@humagen/web";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { BuilderButton } from "./AgentBuilder.tsx";
import { AgentHealth } from "./AgentHealth.tsx";
import { Agents } from "./Agents.tsx";
import type { Revision } from "./api.ts";
import { stubFetch } from "./dom.ts";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

test("New agent starts one builder session and opens it only after acceptance", async () => {
  let finish: ((value: unknown) => void) | undefined;
  const opened: string[] = [];
  net = stubFetch({
    "POST /api/sessions": (body) => {
      assert.equal((body as { agent: string }).agent, "humagen-builder");
      assert.match((body as { text: string }).text, /before asking for approval/);
      return new Promise((resolve) => {
        finish = resolve;
      });
    },
  });
  render(<BuilderButton onOpen={(id) => opened.push(id)} />);
  fireEvent.click(screen.getByRole("button", { name: "New agent" }));
  assert.equal(
    (screen.getByRole("button", { name: "Opening builder…" }) as HTMLButtonElement).disabled,
    true,
  );
  assert.deepEqual(opened, []);
  finish?.({ id: "s_newbuilder" });
  await waitFor(() => assert.deepEqual(opened, ["s_newbuilder"]));
  assert.equal(net.seen.length, 1);
});

test("managing an agent resumes its labeled builder session without sending another prompt", async () => {
  net = stubFetch({
    "GET /api/sessions": (_body, query) => {
      assert.equal(query.get("agent"), "humagen-builder");
      assert.equal(query.get("mine"), "true");
      return { sessions: [{ id: "s_manage", label: "manage-writer" }] };
    },
  });
  const opened: string[] = [];
  render(<BuilderButton name="writer" onOpen={(id) => opened.push(id)} />);
  fireEvent.click(screen.getByRole("button", { name: "Manage with builder" }));
  await waitFor(() => assert.deepEqual(opened, ["s_manage"]));
  assert.equal(
    net.seen.some((request) => request.method === "POST"),
    false,
  );
});

test("management-session lookup follows the server cursor without posting a duplicate", async () => {
  net = stubFetch({
    "GET /api/sessions": (_body, query) =>
      query.get("after") === "s_page"
        ? { sessions: [{ id: "s_older", label: "manage-writer" }] }
        : { sessions: [{ id: "s_page", label: "different" }], next: "s_page" },
  });
  const opened: string[] = [];
  render(<BuilderButton name="writer" onOpen={(id) => opened.push(id)} />);
  fireEvent.click(screen.getByRole("button", { name: "Manage with builder" }));
  await waitFor(() => assert.deepEqual(opened, ["s_older"]));
  assert.equal(net.seen.length, 2);
  assert.equal(
    net.seen.some((request) => request.method === "POST"),
    false,
  );
});

test("agent health shows actual failed checks and before-after scores, then refreshes a refused read", async () => {
  let refused = true;
  net = stubFetch({
    "GET /api/agents/writer/health": () =>
      refused
        ? new Response(JSON.stringify({ error: "Health temporarily unavailable" }), { status: 503 })
        : {
            status: "needs_review",
            samples: 2,
            passed: 1,
            pass_rate: 50,
            results: [
              {
                run: "r1",
                version: 2,
                at: new Date().toISOString(),
                passed: false,
                verdicts: [{ check: "evidence", passed: false, reason: "No evidence linked" }],
              },
            ],
          },
    "GET /api/agents/writer/changes": () => [
      {
        id: "c1",
        evaluated: true,
        passed: false,
        at: new Date().toISOString(),
        scores: [{ suite: "review", before: 100, after: 50, passed: false }],
      },
    ],
  });
  render(<AgentHealth name="writer" />);
  await screen.findByText("Health temporarily unavailable");
  assert.equal(screen.queryByText(/Healthy/), null);
  refused = false;
  fireEvent.click(screen.getByRole("button", { name: "Refresh health" }));
  await screen.findByText(/1 of 2 recent evaluations passed \(50%\)/);
  assert.ok(screen.getByText("No evidence linked"));
  assert.ok(screen.getByText("review: 100% → 50% · Regression"));
});

test("the Needs review agent filter includes failed runs and eval regressions and preserves the chosen address state", async () => {
  const revisions = ["failed-agent", "eval-agent", "ready-agent"].map(
    (name) =>
      ({
        rev: 1,
        spec: { name, provider: "offline", model: "offline", system_prompt: "Help" },
      }) as Revision,
  );
  net = stubFetch({
    "GET /api/agent-roster": () => ({
      controls: {
        "failed-agent": { state: "failed" },
        "eval-agent": { state: "ready" },
        "ready-agent": { state: "ready" },
      },
    }),
    ...Object.fromEntries(
      revisions.map((revision) => [
        `GET /api/agents/${revision.spec.name}/health`,
        () => ({
          status: revision.spec.name === "eval-agent" ? "needs_review" : "healthy",
          samples: 1,
          passed: 1,
          pass_rate: 100,
          results: [],
        }),
      ]),
    ),
  });
  const states: string[] = [];
  render(
    <Agents
      operational
      agents={revisions}
      stats={new Map()}
      state="needs_review"
      onState={(state) => states.push(state)}
      canWrite
      onOpen={() => {}}
      onNew={() => {}}
    />,
  );
  await screen.findByRole("button", { name: /failed-agent/ });
  await screen.findByRole("button", { name: /eval-agent/ });
  assert.equal(screen.queryByRole("button", { name: /ready-agent/ }), null);
  fireEvent.change(screen.getByRole("combobox"), { target: { value: "draft" } });
  assert.deepEqual(states, ["draft"]);
});

test("a recorded builder preview renders a prompt diff and eval regression without an unapproved apply action", () => {
  const output = JSON.stringify({
    change: {
      id: "c1",
      agent: "writer",
      evaluated: true,
      passed: false,
      scores: [{ suite: "review", before: 100, after: 50, passed: false }],
    },
    before_prompt: "Cite evidence.",
    after_prompt: "Skip citations.",
    tools: ["file_read"],
    connections: [
      { name: "Team Slack", service: "slack", scope: "workspace", status: "configured" },
    ],
    listens: [{ connection: "Team Slack", place: "#reviews", project: "Launch", enabled: false }],
  });
  const turn = {
    id: "builder-run",
    agent: { name: "humagen-builder" },
    status: "done",
    text: "Review the proposed change.",
    tasks: [
      {
        id: "preview",
        tool: "preview_agent_change",
        title: "Preview agent change",
        status: "complete",
        output,
      },
    ],
    citations: [],
    interrupts: [],
    notices: [],
  } as Turn;
  render(
    <WebSurface>
      <AgentMessage turn={turn} />
    </WebSurface>,
  );
  const card = screen.getByRole("region", { name: "Proposed change to writer" });
  assert.ok(within(card).getByText(/An evaluation regressed/));
  assert.ok(within(card).getByText("file_read"));
  assert.ok(within(card).getByText("Team Slack · slack · workspace · configured"));
  assert.ok(within(card).getByText("Team Slack · #reviews · Launch · Disabled"));
  assert.equal(within(card).queryByText("Connected"), null);
  assert.ok(
    within(card).getByRole("table", {
      name: "Evaluation scores before and after the proposed change",
    }),
  );
  assert.ok(within(card).getByText("100%"));
  assert.ok(within(card).getByText("50%"));
  assert.equal(within(card).queryByRole("button", { name: /Apply|Approve/ }), null);
});

test("an incomplete builder evaluation with no tools remains visibly blocked", () => {
  const output = JSON.stringify({
    change: { id: "c2", agent: "writer", evaluated: false, passed: false, scores: null },
    before_prompt: "Before",
    after_prompt: "After",
    tools: null,
  });
  const turn = {
    id: "incomplete-builder",
    agent: { name: "humagen-builder" },
    status: "done",
    text: "Evaluation could not finish.",
    tasks: [
      {
        id: "preview",
        tool: "preview_agent_change",
        title: "Preview agent change",
        status: "complete",
        output,
      },
    ],
    citations: [],
    interrupts: [],
    notices: [],
  } as Turn;
  render(
    <WebSurface>
      <AgentMessage turn={turn} />
    </WebSurface>,
  );
  const card = screen.getByRole("region", { name: "Proposed change to writer" });
  assert.ok(within(card).getByText("Evaluation incomplete. This change cannot be applied."));
  assert.ok(within(card).getByText("No tools"));
  assert.equal(within(card).queryByRole("button", { name: /Apply|Approve/ }), null);
});

test("workspace prompt previews use the same scorecard and stay behind native approval", () => {
  const output = JSON.stringify({
    change: {
      id: "workspace-change",
      kind: "workspace_prompt",
      agent: "humagen-builder",
      evaluated: true,
      passed: true,
      scores: [],
    },
    before_prompt: "Be brief.",
    after_prompt: "Be brief and link evidence.",
    tools: [],
  });
  const turn = {
    id: "workspace-builder",
    agent: { name: "humagen-builder" },
    status: "done",
    text: "Review workspace instructions.",
    tasks: [
      {
        id: "preview",
        tool: "preview_workspace_prompt",
        title: "Preview workspace prompt",
        status: "complete",
        output,
      },
    ],
    citations: [],
    interrupts: [],
    notices: [],
  } as Turn;
  render(
    <WebSurface>
      <AgentMessage turn={turn} />
    </WebSurface>,
  );
  const card = screen.getByRole("region", { name: "Proposed change to workspace instructions" });
  assert.ok(
    within(card).getByText("Evaluations passed. Approval is required to apply this change."),
  );
  assert.equal(within(card).queryByRole("button", { name: /Apply|Approve/ }), null);
});
