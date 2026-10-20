import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AgentDetail } from "./AgentDetail.tsx";
import type { Note, Revision } from "./api.ts";
import { stubFetch } from "./dom.ts";

const writer: Revision = {
  rev: 2,
  spec: { name: "warp-writer", description: "Writes a document.", provider: "offline", model: "offline", system_prompt: "Write it.", access: { spawn: [] } },
  created_at: new Date().toISOString(),
  created_by: "warp",
  hash: "h2",
};
const older: Revision = { ...writer, rev: 1, hash: "h1", spec: { ...writer.spec, system_prompt: "Write." } };
const notes: Note[] = [{ id: "n1", text: "Releases go out on Tuesdays.", source: "person", status: "approved", by: "ada", at: new Date().toISOString() }];

let net: ReturnType<typeof stubFetch>;
afterEach(() => {
  cleanup();
  net.restore();
});

function open(builtin: boolean, onBaseline: (name: string) => void = () => {}) {
  net = stubFetch({
    "GET /api/agents/warp-writer/history": () => ({ revisions: [writer, older] }),
    "GET /api/agents/warp-writer/memory": () => ({ notes }),
    "POST /api/agents/warp-writer/memory": () => ({ id: "n2" }),
    "DELETE /api/agents/warp-writer/memory/n1": () => undefined,
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
      baseline=""
      canWrite
      builtin={builtin}
      canEval={false}
      canAlert={false}
      onBack={noop}
      onChanged={noop}
      onRulesChanged={noop}
      onSuitesChanged={noop}
      onOpenRun={noop}
      onBaseline={onBaseline}
    />,
  );
}

const tab = (name: string) => fireEvent.click(screen.getByRole("tab", { name: new RegExp(`^${name}`) }));

test("a built-in agent: the spec cannot be changed or saved, the baseline and memory can", async () => {
  const chose: string[] = [];
  open(true, (n) => chose.push(n));
  assert.ok(screen.getByTestId("agent-locked"));
  assert.ok(!screen.queryByRole("button", { name: /Save as Revision/ }));
  const prompt = screen.getByLabelText("System prompt") as HTMLTextAreaElement;
  assert.ok(prompt.disabled);

  const baseline = screen.getByRole("checkbox", { name: /Baseline agent/ }) as HTMLInputElement;
  assert.ok(!baseline.disabled);
  fireEvent.click(baseline);
  assert.deepEqual(chose, ["warp-writer"]);

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
  await waitFor(() => assert.ok(net.seen.some((r) => r.method === "POST" && r.path === "/api/agents/warp-writer/memory")));
  fireEvent.click(screen.getByRole("button", { name: "Forget" }));
  await waitFor(() => assert.ok(net.seen.some((r) => r.method === "DELETE" && r.path === "/api/agents/warp-writer/memory/n1")));
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
