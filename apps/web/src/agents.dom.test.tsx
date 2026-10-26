import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AgentDetail } from "./AgentDetail.tsx";
import type { Note, ProjectView, Revision } from "./api.ts";
import { stubFetch } from "./dom.ts";

const writer: Revision = {
  rev: 2,
  spec: { name: "humagen-writer", description: "Writes a document.", provider: "offline", model: "offline", system_prompt: "Write it.", access: { spawn: [] } },
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
const older: Revision = { ...writer, rev: 1, hash: "h1", spec: { ...writer.spec, system_prompt: "Write." } };
const notes: Note[] = [{ id: "n1", text: "Releases go out on Tuesdays.", source: "person", status: "approved", by: "ada", at: new Date().toISOString() }];

let net: ReturnType<typeof stubFetch>;
afterEach(() => {
  cleanup();
  net.restore();
});

function open(builtin: boolean, onHomed: () => void = () => {}) {
  net = stubFetch({
    "PUT /api/agents/humagen-writer/home": () => ({ agent: "humagen-writer", home: "Field", enabled_in: [] }),
    "GET /api/agents/humagen-writer/history": () => ({ revisions: [writer, older] }),
    "GET /api/agents/humagen-writer/memory": () => ({ notes }),
    "POST /api/agents/humagen-writer/memory": () => ({ id: "n2" }),
    "DELETE /api/agents/humagen-writer/memory/n1": () => undefined,
  });
  const noop = () => {};
  render(
    <AgentDetail
      revision={writer}
      agents={[writer]}
      sources={[]}
      suites={[]}
      rules={[]}
      alerts={[]}
      stats={undefined}
      tree={[field]}
      place={{ enabled_in: ["~"] }}
      workspaceName="Acme"
      canWrite
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

const tab = (name: string) => fireEvent.click(screen.getByRole("tab", { name: new RegExp(`^${name}`) }));

test("a built-in agent: the spec cannot be changed or saved, nor moved into a project; its memory can", async () => {
  open(true);
  assert.ok(screen.getByTestId("agent-locked"));
  assert.ok(!screen.queryByRole("button", { name: /Save as Revision/ }));
  const prompt = screen.getByLabelText("System prompt") as HTMLTextAreaElement;
  assert.ok(prompt.disabled);

  tab("Access");
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
  fireEvent.change(screen.getByLabelText("Something to remember"), { target: { value: "Ship on Fridays." } });
  fireEvent.click(screen.getByRole("button", { name: "Remember" }));
  await waitFor(() => assert.ok(net.seen.some((r) => r.method === "POST" && r.path === "/api/agents/humagen-writer/memory")));
  fireEvent.click(screen.getByRole("button", { name: "Forget" }));
  await waitFor(() => assert.ok(net.seen.some((r) => r.method === "DELETE" && r.path === "/api/agents/humagen-writer/memory/n1")));
  assert.ok(!net.seen.some((r) => r.method === "PUT"), "nothing revised the agent");
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
  await waitFor(() => assert.ok(net.seen.some((r) => r.method === "PUT" && r.path === "/api/agents/humagen-writer/home")));
  await waitFor(() => assert.equal(homed, 1));
});
