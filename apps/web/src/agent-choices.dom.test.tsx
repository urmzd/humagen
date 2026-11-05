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
const models = () => screen.getByLabelText("Allowed models") as HTMLTextAreaElement;
const efforts = () => screen.getByLabelText("Allowed reasoning efforts") as HTMLTextAreaElement;
const save = () => screen.getByRole("button", { name: "Save as Revision 5" }) as HTMLButtonElement;

test("agent message choices load, edit and save one canonical versioned revision", async () => {
  const result = open();
  assert.equal(models().value, "offline/alternate");
  assert.equal(efforts().value, "low");
  assert.ok(save().disabled);
  fireEvent.change(models(), {
    target: { value: " openai/org/model \noffline/alternate\nopenai/org/model\n" },
  });
  fireEvent.change(efforts(), { target: { value: " HIGH \nlow\nhigh\n" } });
  assert.ok(!save().disabled);
  fireEvent.click(save());
  await waitFor(() => assert.equal(result.changed(), 1));
  assert.deepEqual(result.saved, [
    {
      expected_rev: 4,
      spec: {
        ...revision.spec,
        choices: { models: ["offline/alternate", "openai/org/model"], efforts: ["high", "low"] },
      },
    },
  ]);
});

test("invalid choices block saving across tabs; discard restores saved choices", () => {
  open();
  fireEvent.change(models(), { target: { value: "missing-provider" } });
  assert.equal(models().getAttribute("aria-invalid"), "true");
  assert.match(screen.getByRole("alert").textContent ?? "", /provider\/model/);
  assert.ok(save().disabled);
  fireEvent.click(screen.getByRole("tab", { name: /^Prompt/ }));
  assert.ok(save().disabled);
  fireEvent.click(screen.getByRole("button", { name: "Discard" }));
  fireEvent.click(screen.getByRole("tab", { name: /^Model/ }));
  assert.equal(models().value, "offline/alternate");
  for (const value of ["very high", "high-1", "a".repeat(33)]) {
    fireEvent.change(efforts(), { target: { value } });
    assert.ok(save().disabled);
    assert.equal(efforts().getAttribute("aria-invalid"), "true");
  }
});

test("clearing both choices removes overrides without changing the default model", async () => {
  const result = open();
  fireEvent.change(models(), { target: { value: "\n " } });
  fireEvent.change(efforts(), { target: { value: "" } });
  fireEvent.click(save());
  await waitFor(() => assert.equal(result.changed(), 1));
  const { choices: _choices, ...withoutChoices } = revision.spec;
  assert.deepEqual(result.saved[0]?.spec, withoutChoices);
});

test("a failed choice save preserves the draft and does not report success", async () => {
  const result = open({ fail: true });
  fireEvent.change(efforts(), { target: { value: "high" } });
  fireEvent.click(save());
  await screen.findByText("Provider is unavailable.");
  assert.equal(result.changed(), 0);
  assert.equal(efforts().value, "high");
  assert.ok(!save().disabled);
});

for (const permission of [{ canWrite: false }, { builtin: true }]) {
  test(`message choices are read only for ${JSON.stringify(permission)}`, () => {
    open(permission);
    assert.ok(models().disabled);
    assert.ok(efforts().disabled);
    assert.ok(!screen.queryByRole("button", { name: /Save as Revision/ }));
  });
}
