import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ActivityPanel, type Tree } from "./Activity.tsx";
import type { Run } from "./adapter.ts";
import { stubFetch } from "./dom.ts";

let rendered = false;
let restore: (() => void) | undefined;
afterEach(() => {
  if (rendered) cleanup();
  rendered = false;
  restore?.();
  restore = undefined;
});

const at = "2026-10-26T10:00:00Z";
const run = (id: string, extra: Partial<Run> = {}): Run => ({ id, tag_id: id, tenant: "test", agent: id, rev: 1, origin: "api", text: id, root: "root", depth: 1, thread: "thread", lease: 0, granted: 0, spent: 0, calls: 0, status: "completed", created_at: at, updated_at: at, ...extra });
const tree: Tree = {
  root: "root",
  runs: [run("root", { depth: 0 }), run("handoff", { parent: "root", mode: "handoff" }), run("delegate", { parent: "root", mode: "delegate" }), run("tool", { parent: "root" }), run("decision", { parent: "root", assistance: "needed" }), run("question", { parent: "root", question: { id: "q1", kind: "choice", prompt: "Choose a direction" } }), run("authored", { parent: "root" }), run("failed", { parent: "root", status: "failed" })],
  spans: [{ run_id: "tool", agent: "tool", kind: "tool", name: "read_source", start: at, end: at }, { run_id: "root", agent: "root", kind: "attempt", name: "model", start: at }],
};
const rows = () => screen.queryAllByTestId("activity-row").map((row) => row.getAttribute("data-run"));
const copy = defaultCopy.activityControls;

test("Activity filters use recorded modes, tool spans and decisions without treating failures as decisions", () => {
  rendered = true;
  render(<ActivityPanel trees={[tree]} selected={null} onSelect={() => {}} onClose={() => {}} isDecision={(entry) => entry.id === "authored"} />);
  assert.equal(rows().length, tree.runs.length);
  fireEvent.click(screen.getByRole("button", { name: copy.handoffs }));
  assert.deepEqual(rows(), ["handoff", "delegate"]);
  assert.equal(screen.getByRole("button", { name: copy.handoffs }).getAttribute("aria-pressed"), "true");
  fireEvent.click(screen.getByRole("button", { name: copy.tools }));
  assert.deepEqual(rows(), ["tool"]);
  fireEvent.click(screen.getByRole("button", { name: copy.decisions }));
  assert.deepEqual(rows(), ["decision", "question", "authored"]);
  fireEvent.click(screen.getByRole("button", { name: copy.all }));
  assert.equal(rows().length, tree.runs.length);
});

test("Activity controlled filters emit changes and distinguish no matches from no activity", () => {
  const filters: string[] = [];
  const toggles: boolean[] = [];
  rendered = true;
  const view = render(<ActivityPanel trees={[{ ...tree, spans: [] }]} selected={null} filter="tools" showTools={false} onFilterChange={(value) => filters.push(value)} onShowToolsChange={(value) => toggles.push(value)} onSelect={() => {}} onClose={() => {}} />);
  assert.ok(screen.getByText(copy.noMatches));
  fireEvent.click(screen.getByRole("button", { name: copy.all }));
  fireEvent.click(screen.getByRole("button", { name: copy.showTools }));
  assert.deepEqual(filters, ["all"]);
  assert.deepEqual(toggles, [true]);
  assert.equal(rows().length, 0);
  view.rerender(<ActivityPanel trees={[tree]} selected={null} filter="all" showTools onSelect={() => {}} onClose={() => {}} />);
  assert.equal(rows().length, tree.runs.length);
});

test("Activity tool timeline can be hidden and restored without closing the run", () => {
  restore = stubFetch({}).restore;
  rendered = true;
  render(<ActivityPanel trees={[tree]} selected="tool" onSelect={() => {}} onClose={() => {}} />);
  assert.ok(screen.getByTestId("activity-tools"));
  fireEvent.click(screen.getByRole("button", { name: copy.hideTools }));
  assert.equal(screen.queryByTestId("activity-tools"), null);
  assert.ok(screen.getByTestId("activity-detail"));
  fireEvent.click(screen.getByRole("button", { name: copy.showTools }));
  assert.ok(screen.getByTestId("activity-tools"));
});
