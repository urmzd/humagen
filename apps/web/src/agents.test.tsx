import assert from "node:assert/strict";
import { test } from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import { AgentDetail, BUILTIN_LOCKED } from "./AgentDetail.tsx";
import type { Revision } from "./api.ts";

const writer: Revision = {
  rev: 2,
  spec: { name: "warp-writer", description: "Writes a document.", provider: "offline", model: "offline", system_prompt: "Write it.", access: { spawn: [] } },
  created_at: new Date().toISOString(),
  created_by: "warp",
  hash: "h2",
};

const noop = () => {};
const render = (builtin: boolean, canWrite = true) =>
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
