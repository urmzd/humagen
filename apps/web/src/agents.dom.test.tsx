import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AgentDetail } from "./AgentDetail.tsx";
import type { Note, ProjectView, Revision } from "./api.ts";
import { stubFetch } from "./dom.ts";

const writer: Revision = {
  rev: 2,
  spec: {
    name: "humagen-writer",
    description: "Writes a document.",
    provider: "offline",
    model: "offline",
    system_prompt: "Write it.",
    access: { spawn: [] },
  },
  created_at: new Date().toISOString(),
  created_by: "humagen",
  hash: "h2",
};
const field: ProjectView = {
  name: "Field",
  kind: "team",
  members: [{ who: "*", role: "member" }],
  experts: [],
  agents: { enable: [] },
  access: { role: "owner", from: "", via: "projects:govern" },
  actions: [],
  version: 1,
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
  depth: 1,
  path: [{ name: "Field", kind: "team" }],
  children: 0,
  conversations: 0,
  active: 0,
  schedules: 0,
  notes: 0,
  proposed: 0,
  unread: 0,
  row_agents: [],
  mine: false,
};
const older: Revision = {
  ...writer,
  rev: 1,
  hash: "h1",
  spec: { ...writer.spec, system_prompt: "Write." },
};
const notes: Note[] = [
  {
    id: "n1",
    text: "Releases go out on Tuesdays.",
    source: "person",
    status: "approved",
    by: "ada",
    at: new Date().toISOString(),
  },
];

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

function open(
  builtin: boolean,
  onHomed: () => void = () => {},
  revision = writer,
  canWrite = true,
) {
  net = stubFetch({
    "PUT /api/agents/humagen-writer": (body) => ({
      ...revision,
      rev: revision.rev + 1,
      spec: (body as { spec: Revision["spec"] }).spec,
    }),
    "PUT /api/agents/humagen-writer/home": () => ({
      agent: "humagen-writer",
      home: "Field",
      enabled_in: [],
    }),
    "GET /api/agents/humagen-writer/history": () => ({ revisions: [writer, older] }),
    "GET /api/agents/humagen-writer/memory": () => ({ notes }),
    "POST /api/agents/humagen-writer/memory": () => ({ id: "n2" }),
    "DELETE /api/agents/humagen-writer/memory/n1": () => undefined,
  });
  const noop = () => {};
  render(
    <AgentDetail
      revision={revision}
      agents={[revision]}
      sources={[]}
      suites={[]}
      rules={[]}
      alerts={[]}
      stats={undefined}
      tree={[field]}
      place={{ enabled_in: ["~"] }}
      workspaceName="Acme"
      canWrite={canWrite}
      builtin={builtin}
      canEval={false}
      canAlert={false}
      onBack={noop}
      onChanged={noop}
      onRulesChanged={noop}
      onSuitesChanged={noop}
      onOpenRun={noop}
      onHomed={onHomed}
    />,
  );
}

const tab = (name: string) =>
  fireEvent.click(screen.getByRole("tab", { name: new RegExp(`^${name}`) }));

test("a built-in agent: the spec cannot be changed or saved, nor moved into a project; its memory can", async () => {
  open(true);
  assert.ok(screen.getByTestId("agent-locked"));
  assert.ok(!screen.queryByRole("button", { name: /Save as Revision/ }));
  const prompt = screen.getByLabelText("System prompt") as HTMLTextAreaElement;
  assert.ok(prompt.disabled);

  tab("Access");
  assert.equal(
    (
      screen.getByRole("checkbox", {
        name: /^Use the requester's approved connection tools/,
      }) as HTMLInputElement
    ).disabled,
    true,
  );
  const lives = screen.getByRole("button", { name: /^Lives in:/ }) as HTMLButtonElement;
  assert.ok(lives.disabled, "the platform's own agents live nowhere");

  tab("Model");
  const model = screen.getByDisplayValue("offline") as HTMLInputElement;
  assert.ok(model.disabled);

  tab("Revisions");
  await screen.findByText("Revision 1");
  assert.ok(!screen.queryByRole("button", { name: /Restore as Revision/ }));

  tab("Memory");
  await screen.findByText("Releases go out on Tuesdays.");
  fireEvent.change(screen.getByLabelText("Something to remember"), {
    target: { value: "Ship on Fridays." },
  });
  fireEvent.click(screen.getByRole("button", { name: "Remember" }));
  await waitFor(() =>
    assert.ok(
      net!.seen.some((r) => r.method === "POST" && r.path === "/api/agents/humagen-writer/memory"),
    ),
  );
  fireEvent.click(screen.getByRole("button", { name: "Forget" }));
  await waitFor(() =>
    assert.ok(
      net!.seen.some(
        (r) => r.method === "DELETE" && r.path === "/api/agents/humagen-writer/memory/n1",
      ),
    ),
  );
  assert.ok(!net!.seen.some((r) => r.method === "PUT"), "nothing revised the agent");
});

test("personal connection access stays off until the agent revision is explicitly saved", async () => {
  open(false, () => {}, { ...writer, spec: { ...writer.spec, tools: ["file_read"] } });
  tab("Access");
  const grant = screen.getByRole("checkbox", {
    name: /^Use the requester's approved connection tools/,
  }) as HTMLInputElement;
  assert.equal(grant.checked, false);
  fireEvent.click(grant);
  assert.equal(
    net!.seen.some((request) => request.method === "PUT"),
    false,
  );
  assert.equal(screen.queryByText("Approve person_tools first"), null);
  fireEvent.click(screen.getByRole("button", { name: "Save as Revision 3" }));
  await waitFor(() => assert.ok(net!.seen.some((request) => request.method === "PUT")));
  const body = net!.seen.find((request) => request.method === "PUT")?.body as {
    expected_rev: number;
    spec: Revision["spec"];
  };
  assert.equal(body.expected_rev, 2);
  assert.deepEqual(body.spec.tools, ["file_read", "person_tools"]);
  assert.equal(
    screen.getByRole("link", { name: "Review Connections" }).getAttribute("href"),
    "/connections",
  );
});

test("readers cannot grant personal connection access", () => {
  open(false, () => {}, writer, false);
  tab("Access");
  assert.equal(
    (
      screen.getByRole("checkbox", {
        name: /^Use the requester's approved connection tools/,
      }) as HTMLInputElement
    ).disabled,
    true,
  );
  assert.equal(screen.queryByRole("button", { name: /Save as Revision/ }), null);
});

test("the same agent, not built in: editing makes a save, and an older revision can be restored", async () => {
  open(false);
  assert.ok(!screen.queryByTestId("agent-locked"));
  const save = screen.getByRole("button", { name: "Save as Revision 3" }) as HTMLButtonElement;
  assert.ok(save.disabled, "nothing to save yet");
  fireEvent.change(screen.getByLabelText("System prompt"), { target: { value: "Write it well." } });
  assert.ok(!save.disabled);

  tab("Model");
  assert.ok(!(screen.getByDisplayValue("offline") as HTMLInputElement).disabled);
  tab("Revisions");
  assert.ok(await screen.findByRole("button", { name: "Restore as Revision 3" }));
});

test("an agent of the workspace's own comes to live in a project from its Access tab", async () => {
  let homed = 0;
  open(false, () => homed++);
  tab("Access");
  fireEvent.click(screen.getByRole("button", { name: /^Lives in:/ }));
  fireEvent.click(await screen.findByRole("option", { name: /Field/ }));
  await waitFor(() =>
    assert.ok(
      net!.seen.some((r) => r.method === "PUT" && r.path === "/api/agents/humagen-writer/home"),
    ),
  );
  await waitFor(() => assert.equal(homed, 1));
});

test("the agent directory filters operational states and offers agent changes through the builder", async () => {
  const { Agents } = await import("./Agents.tsx");
  const { defaultCopy } = await import("@humagen/components");
  net = stubFetch({
    "GET /api/agent-roster": () => ({
      controls: { "humagen-writer": { paused: false, version: 3, state: "watching" } },
      window_days: 31,
    }),
    "GET /api/agents/humagen-writer/health": () => ({
      agent: "humagen-writer",
      status: "untested",
      samples: 0,
      passed: 0,
      pass_rate: 0,
      results: [],
    }),
  });
  let building = false;
  render(
    <Agents
      operational
      agents={[writer]}
      stats={new Map()}
      canWrite
      onOpen={() => {}}
      onNew={() => {}}
      onBuild={() => {
        building = true;
      }}
    />,
  );
  await screen.findByText("Not evaluated");
  fireEvent.change(screen.getByLabelText(defaultCopy.agentOperations.filter), {
    target: { value: "watching" },
  });
  assert.ok(await screen.findByRole("button", { name: /humagen-writer/ }));
  assert.equal(screen.queryByRole("button", { name: defaultCopy.agentOperations.pauseAll }), null);
  assert.equal(screen.queryByRole("button", { name: defaultCopy.agentOperations.build }), null);
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.agentOperations.newAgent }));
  assert.equal(building, true);
  assert.equal(
    net.seen.some((request) => request.method !== "GET"),
    false,
  );
});

test("the roster filters Needs Review without presenting stale skill bindings as ready", async () => {
  const { Agents } = await import("./Agents.tsx");
  const { defaultCopy } = await import("@humagen/components");
  const state = {
    paused: false,
    version: 1,
    updated_at: "2026-10-01T00:00:00Z",
    updated_by: "ada",
  };
  const ready = { ...writer, spec: { ...writer.spec, name: "ready-agent" } };
  net = stubFetch({
    "GET /api/agent-roster": () => ({
      controls: {
        "humagen-writer": {
          ...state,
          state: "needs_review",
          needs_skill_review: true,
          effective_paused: false,
        },
        "ready-agent": { ...state, state: "ready", effective_paused: false },
      },
      window_days: 31,
    }),
    "GET /api/agent-controls": () => state,
  });
  render(
    <Agents
      operational
      agents={[writer, ready]}
      stats={new Map()}
      canWrite
      onOpen={() => {}}
      onNew={() => {}}
    />,
  );
  await screen.findByRole("option", { name: "Needs review" });
  await waitFor(() =>
    assert.match(
      screen.getByRole("button", { name: /humagen-writer/ }).textContent ?? "",
      /Needs review/,
    ),
  );
  assert.doesNotMatch(
    screen.getByRole("button", { name: /ready-agent/ }).textContent ?? "",
    /Ready/,
  );
  fireEvent.change(screen.getByLabelText(defaultCopy.agentOperations.filter), {
    target: { value: "needs_review" },
  });
  assert.ok(await screen.findByRole("button", { name: /humagen-writer/ }));
  assert.ok(!screen.queryByRole("button", { name: /ready-agent/ }));
});

test("agent detail offers Context review for native skill audience review even with current revision", async () => {
  const { AgentOperations } = await import("./AgentOperations.tsx");
  net = stubFetch({
    "GET /api/agent-roster": () => ({
      controls: {
        "humagen-writer": {
          paused: false,
          version: 1,
          state: "needs_review",
          needs_skill_review: true,
          effective_paused: false,
        },
      },
      window_days: 31,
    }),
    "GET /api/agents/humagen-writer/skills": () => ({
      agent: "humagen-writer",
      agent_revision: 2,
      version: 1,
      skills: [{ id: "published-exact", version: 1 }],
    }),
    "GET /api/agent-setups/humagen-writer": () => new Response(null, { status: 404 }),
  });
  render(
    <AgentOperations name="humagen-writer" revision={2} canReadRuns canWrite onRun={() => {}} />,
  );
  const review = await screen.findByRole("link", { name: "Review Default Skills" });
  assert.equal(review.getAttribute("href"), "/agents/humagen-writer/context");
  assert.ok(screen.getByText("Needs Review"));
});

test("directory search and the single builder action preserve opening and creation permissions", async () => {
  const { Agents } = await import("./Agents.tsx");
  const agents = Array.from({ length: 7 }, (_, i) => ({
    ...writer,
    spec: { ...writer.spec, name: `writer-${i}`, description: `Document ${i}` },
  }));
  let opened = "";
  let built = 0;
  let created = 0;
  const props = {
    agents,
    stats: new Map(),
    onOpen: (name: string) => {
      opened = name;
    },
    onBuild: () => {
      built++;
    },
    onNew: () => {
      created++;
    },
  };
  const view = render(<Agents {...props} canWrite />);
  fireEvent.change(screen.getByRole("searchbox", { name: "Find an agent" }), {
    target: { value: "Document 3" },
  });
  assert.ok(!screen.queryByRole("button", { name: /writer-2/ }));
  fireEvent.click(screen.getByRole("button", { name: /writer-3/ }));
  assert.equal(opened, "writer-3");
  fireEvent.click(screen.getByRole("button", { name: "New Agent" }));
  assert.equal(built, 1);
  assert.equal(created, 0);
  fireEvent.click(screen.getByRole("button", { name: "Clear search" }));
  assert.ok(screen.getByRole("button", { name: /writer-2/ }));
  view.rerender(<Agents {...props} canWrite={false} />);
  assert.ok(screen.getByRole("button", { name: "New Agent" }));
  assert.ok(!screen.queryByRole("button", { name: "Agent actions" }));
  assert.ok(!screen.queryByRole("button", { name: "Build With the Builder" }));
  view.rerender(<Agents {...props} onBuild={undefined} canWrite={false} />);
  assert.equal(screen.queryByRole("button", { name: "New Agent" }), null);
});
