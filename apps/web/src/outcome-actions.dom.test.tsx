import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { OutcomeLearningPreview } from "../../../packages/client/src/generated/types.ts";
import { OutcomeActions } from "./OutcomeActions.tsx";
import type { Run } from "./adapter.ts";
import { stubFetch } from "./dom.ts";

const copy = defaultCopy.outcomeLearning;
const run = { id: "run-learning", status: "completed", outcome: { kind: "decision", summary: "Review the launch", decisions: ["Keep Tuesday"], next_action: "Confirm the owner" } } as Run;
const preview: OutcomeLearningPreview = { run: run.id, snapshot: "snapshot-one", scope: { kind: "personal", subject: "ada" }, outcome: run.outcome!, project: "Launch", project_title: "Launch", may_project: true, can_draft: true, can_remember: true };
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => { if (!net) return; cleanup(); net.restore(); net = undefined; });

test("Save as Skill posts a reviewed personal tool-free draft and leaves test/publication to Library", async () => {
  net = stubFetch({
    "GET /api/runs/run-learning/learning": (_, query) => { assert.equal(query.get("scope"), "personal"); return preview; },
    "POST /api/runs/run-learning/skill-draft": (body) => {
      const input = body as { snapshot: string; scope: { kind: string }; definition: { name: string; instructions: string; tools: string[] } };
      assert.equal(input.snapshot, preview.snapshot);
      assert.equal(input.scope.kind, "personal");
      assert.equal(input.definition.name, "launch-review");
      assert.equal(input.definition.instructions, "Review the evidence and ask the owner.");
      assert.deepEqual(input.definition.tools, []);
      return { ...input.definition, id: "sk-learning", scope: preview.scope, version: 1, by: "ada", at: "2026-10-26T00:00:00Z", tests: [] };
    },
  });
  render(<OutcomeActions run={run} canWrite />);
  assert.equal(net.seen.length, 0);
  fireEvent.click(screen.getByRole("button", { name: copy.skill }));
  fireEvent.change(await screen.findByRole("textbox", { name: copy.name }), { target: { value: "launch-review" } });
  fireEvent.change(screen.getByRole("textbox", { name: copy.steps }), { target: { value: "Review the evidence and ask the owner." } });
  fireEvent.click(screen.getByRole("button", { name: copy.createDraft }));
  await screen.findByText(copy.skillSaved);
  const target = screen.getByRole("link", { name: copy.openSkill }).getAttribute("href") ?? "";
  assert.ok(target.includes("/library/skills?") && target.includes("scope=personal") && target.includes("skill=sk-learning"));
  assert.equal(net.seen.filter((item) => item.method === "POST").length, 1);
  assert.equal(net.seen.some((item) => /test|publish|approve/.test(item.path)), false);
});

test("remembering requires explicit whole-text human review and submits no external approval", async () => {
  let reviewed = "";
  net = stubFetch({
    "GET /api/runs/run-learning/learning": () => preview,
    "POST /api/runs/run-learning/decision-memory": (body) => {
      const input = body as { decision: number; text: string; scope: { kind: string } };
      assert.equal(input.decision, 0); assert.equal(input.scope.kind, "personal"); reviewed = input.text;
      return { id: "m_reviewed", text: input.text, status: "approved", kind: "decision", by: "ada", reviewed_by: "ada" };
    },
  });
  render(<OutcomeActions run={run} canWrite />);
  fireEvent.click(screen.getByRole("button", { name: copy.remember }));
  const text = await screen.findByRole("textbox", { name: copy.text });
  assert.equal(net.seen.some((item) => item.method === "POST"), false);
  assert.ok(screen.getByText(copy.noExternalApproval));
  fireEvent.change(text, { target: { value: "Human-reviewed Tuesday decision" } });
  fireEvent.click(screen.getByRole("button", { name: copy.approveRemember }));
  await screen.findByText(copy.memorySaved);
  assert.equal(reviewed, "Human-reviewed Tuesday decision");
  assert.equal(net.seen.some((item) => /answers|approvals|calls/.test(item.path)), false);
});

test("a stale learning write keeps the reviewed text and retries with a fresh snapshot", async () => {
  let reads = 0; let writes = 0;
  net = stubFetch({
    "GET /api/runs/run-learning/learning": () => ({ ...preview, snapshot: `snapshot-${++reads}` }),
    "POST /api/runs/run-learning/decision-memory": (body) => {
      if (++writes === 1) return new Response(JSON.stringify({ error: "the outcome changed" }), { status: 409 });
      assert.equal((body as { snapshot: string }).snapshot, "snapshot-2");
      assert.equal((body as { text: string }).text, "My complete reviewed decision");
      return { id: "m_reviewed", status: "approved" };
    },
  });
  render(<OutcomeActions run={run} canWrite />);
  fireEvent.click(screen.getByRole("button", { name: copy.remember }));
  fireEvent.change(await screen.findByRole("textbox", { name: copy.text }), { target: { value: "My complete reviewed decision" } });
  fireEvent.click(screen.getByRole("button", { name: copy.approveRemember }));
  await screen.findByText(copy.stale);
  assert.equal((screen.getByRole("textbox", { name: copy.text }) as HTMLTextAreaElement).value, "My complete reviewed decision");
  fireEvent.click(screen.getByRole("button", { name: copy.retry }));
  await waitFor(() => assert.equal(reads, 2));
  fireEvent.click(await screen.findByRole("button", { name: copy.approveRemember }));
  await screen.findByText(copy.memorySaved);
});
