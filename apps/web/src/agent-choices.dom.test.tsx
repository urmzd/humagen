import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AgentDetail } from "./AgentDetail.tsx";
import type { AgentSpec, Revision } from "./api.ts";
import { stubFetch } from "./dom.ts";

const revision: Revision = {
  spec: {
    name: "reviewer",
    provider: "offline",
    model: "offline",
    system_prompt: "Review it.",
    choices: { models: ["offline/alternate"], efforts: ["low"] },
  },
  rev: 4,
  hash: "revision4",
  created_by: "ada",
  created_at: "2026-11-05T00:00:00Z",
};
let net: ReturnType<typeof stubFetch>;
afterEach(() => {
  cleanup();
  net?.restore();
});

function open({ canWrite = true, builtin = false, fail = false } = {}) {
  const saved: { spec: AgentSpec; expected_rev: number }[] = [];
  let changed = 0;
  net = stubFetch({
    "GET /api/agents/reviewer/history": () => ({ revisions: [revision] }),
    "PUT /api/agents/reviewer": (body) => {
      saved.push(body as (typeof saved)[number]);
      return fail
        ? new Response(JSON.stringify({ error: "Provider is unavailable." }), { status: 400 })
        : { ...revision, rev: 5, spec: saved.at(-1)!.spec };
    },
  });
  const noop = () => {};
  render(
    <AgentDetail
      revision={revision}
      tab="model"
      agents={[revision]}
      sources={[]}
      suites={[]}
      rules={[]}
      alerts={[]}
      stats={undefined}
      canWrite={canWrite}
      builtin={builtin}
      canEval={false}
      canAlert={false}
      onBack={noop}
      onChanged={() => changed++}
      onRulesChanged={noop}
      onSuitesChanged={noop}
      onOpenRun={noop}
    />,
  );
  return { saved, changed: () => changed };
}
// Message choices are the workspace's now, set on Settings › Model providers. The agent's
// Model tab has none, and a save keeps the choices an agent had from before, which apply
// for one release where the workspace offers none.
test("the agent's Model tab has no message choices, and a save keeps the agent's old ones", async () => {
  const result = open();
  assert.equal(screen.queryByLabelText("Allowed models"), null);
  assert.equal(screen.queryByLabelText("Allowed reasoning efforts"), null);
  assert.equal(screen.queryByText("Message choices"), null);
  fireEvent.click(screen.getByRole("tab", { name: "Prompt" }));
  fireEvent.change(screen.getByLabelText("System prompt"), {
    target: { value: "Review it well." },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save as Revision 5" }));
  await waitFor(() => assert.equal(result.changed(), 1));
  assert.deepEqual(result.saved[0]?.spec.choices, revision.spec.choices);
});
