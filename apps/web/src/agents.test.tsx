import assert from "node:assert/strict";
import { test } from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import { AgentDetail, BUILTIN_LOCKED, type Tab } from "./AgentDetail.tsx";
import type { Note, Revision } from "./api.ts";

const writer: Revision = {
  rev: 2,
  spec: { name: "warp-writer", description: "Writes a document.", provider: "offline", model: "offline", system_prompt: "Write it.", access: { spawn: [] } },
  created_at: new Date().toISOString(),
  created_by: "warp",
  hash: "h2",
};

const older: Revision = { ...writer, rev: 1, hash: "h1", spec: { ...writer.spec, system_prompt: "Write." } };
const notes: Note[] = [
  { id: "n1", text: "Releases go out on Tuesdays.", source: "person", status: "approved", by: "ada", at: new Date().toISOString() },
  { id: "n2", text: "Ada prefers short plans.", source: "consolidated", status: "pending", at: new Date().toISOString() },
];

const noop = () => {};
const render = (builtin: boolean, canWrite = true, startOn: Tab = "prompt") =>
  renderToStaticMarkup(
    <AgentDetail
      revision={writer}
      agents={[writer]}
      sources={[]}
      suites={[]}
      rules={[]}
      alerts={[]}
      stats={undefined}
      baseline=""
      canWrite={canWrite}
      builtin={builtin}
      canEval={false}
      canAlert={false}
      onBack={noop}
      onChanged={noop}
      onRulesChanged={noop}
      onSuitesChanged={noop}
      onOpenRun={noop}
      onBaseline={noop}
      tab={startOn}
      seed={{ history: [writer, older], notes }}
    />,
  );

test("a built-in agent opens read only, and says why", () => {
  const html = render(true);
  assert.ok(html.includes(BUILTIN_LOCKED), "the explanation is on the page");
  assert.doesNotMatch(html, /Save as Revision/);
  // Every field of the prompt is disabled.
  assert.match(html, /<textarea[^>]*disabled=""[^>]*>Writes a document\./);
  assert.match(html, /id="agent-prompt"[^>]*disabled=""|disabled=""[^>]*id="agent-prompt"/);
});

test("the same agent, not built in, is editable by a writer", () => {
  const html = render(false);
  assert.match(html, /Save as Revision 3/);
  assert.doesNotMatch(html, /agent-locked/);
  assert.doesNotMatch(html, /<textarea[^>]*disabled=""[^>]*>Writes a document\./);
});

test("someone who may not write sees no save, with or without the lock", () => {
  assert.doesNotMatch(render(false, false), /Save as Revision/);
  assert.doesNotMatch(render(false, false), /agent-locked/);
});

const text = (s: string) => s.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ");
// The controls, by what they hold: the model on the Model tab, the baseline on the Prompt tab.
const model = (html: string) => /<input[^>]*value="offline"[^>]*>/.exec(html)?.[0] ?? "";
const baselineSwitch = (html: string) => {
  const at = html.indexOf("Baseline agent");
  return at < 0 ? "" : (/<input type="checkbox" class="warp-switch[^>]*>/.exec(html.slice(at))?.[0] ?? "");
};

test("A built-in's spec is locked, but the workspace baseline is not", () => {
  assert.match(model(render(true, true, "model")), /disabled=""/);
  // Every switch of the spec's own is locked.
  const access = render(true, true, "access");
  const specSwitches = access.match(/<input type="checkbox" class="warp-switch[^>]*>/g) ?? [];
  assert.ok(specSwitches.length > 0 && specSwitches.every((s) => s.includes('disabled=""')), "the spec's switches are locked");
  const locked = render(true, true, "prompt");
  assert.ok(baselineSwitch(locked), "the baseline switch is on the page");
  assert.doesNotMatch(baselineSwitch(locked), /disabled/);
  assert.doesNotMatch(model(render(false, true, "model")), /disabled/);
  assert.doesNotMatch(baselineSwitch(render(false, true, "prompt")), /disabled/);
  // Without the right to write, neither changes.
  assert.match(model(render(false, false, "model")), /disabled=""/);
  assert.match(baselineSwitch(render(false, false, "prompt")), /disabled=""/);
});

test("Access: calls are picked from a list, and approving a call to another agent is not offered", () => {
  const html = text(render(false, true, "access"));
  assert.match(html, /May call/);
  assert.doesNotMatch(html, /Approve calls to/);
});

test("Revisions: restore is offered for an older revision, never for a built-in", () => {
  assert.match(text(render(false, true, "revisions")), /Restore as Revision 3/);
  assert.match(text(render(true, true, "revisions")), /Revision 1/);
  assert.doesNotMatch(text(render(true, true, "revisions")), /Restore as Revision/);
  assert.doesNotMatch(text(render(false, false, "revisions")), /Restore as Revision/);
});

test("Memory: a writer keeps a built-in's memory, as the server allows; a reader cannot", () => {
  for (const builtin of [true, false]) {
    const html = render(builtin, true, "memory");
    const words = text(html);
    assert.match(words, /Remember/, `builtin ${builtin}`);
    assert.match(words, /Forget/, `builtin ${builtin}`);
    assert.match(words, /Discard All/, `builtin ${builtin}`);
    assert.doesNotMatch(/<input type="checkbox" class="mt-1"[^>]*>/.exec(html)?.[0] ?? "", /disabled/);
  }
  const reader = render(false, false, "memory");
  assert.doesNotMatch(text(reader), /Remember|Forget|Discard All/);
  assert.match(/<input type="checkbox" class="mt-1"[^>]*>/.exec(reader)?.[0] ?? "", /disabled=""/);
});
