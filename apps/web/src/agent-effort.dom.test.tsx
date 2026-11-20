import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AgentDetail } from "./AgentDetail.tsx";
import type { AgentSpec, Revision } from "./api.ts";
import { stubFetch } from "./dom.ts";

const revision: Revision = {
  spec: {
    name: "thinker",
    provider: "anthropic",
    model: "claude-sonnet-5-5",
    system_prompt: "Think it through.",
  },
  rev: 2,
  hash: "revision2",
  created_by: "ada",
  created_at: "2026-11-07T00:00:00Z",
};
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

function open(canWrite: boolean) {
  const saved: { spec: AgentSpec; expected_rev: number }[] = [];
  net = stubFetch({
    "GET /api/agents/thinker/history": () => ({ revisions: [revision] }),
    "GET /api/model-providers": () => ({
      providers: [{ name: "anthropic", declared: true, kind: "anthropic", source: "deployment" }],
      kinds: ["anthropic", "google", "openai"],
    }),
    "PUT /api/agents/thinker": (body) => {
      saved.push(body as (typeof saved)[number]);
      return { ...revision, rev: 3, spec: saved.at(-1)?.spec };
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
      builtin={false}
      canEval={false}
      canAlert={false}
      onBack={noop}
      onChanged={noop}
      onRulesChanged={noop}
      onSuitesChanged={noop}
      onOpenRun={noop}
    />,
  );
  return saved;
}

// The agent's definition sets its reasoning effort beside its models; people pick agents.
test("the Model tab sets the agent's reasoning effort, saved with its revision", async () => {
  const saved = open(true);
  const effort = screen.getByLabelText("Reasoning effort") as HTMLSelectElement;
  assert.equal(effort.value, "", "an agent with none runs at the model's default");
  fireEvent.change(effort, { target: { value: "high" } });
  fireEvent.click(screen.getByRole("button", { name: "Save as Revision 3" }));
  await waitFor(() => assert.equal(saved.length, 1));
  assert.equal(saved[0]?.spec.effort, "high");
});

test("without the right to change the agent, its effort is shown and cannot change", () => {
  open(false);
  assert.ok((screen.getByLabelText("Reasoning effort") as HTMLSelectElement).disabled);
});
