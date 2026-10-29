import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { ActivityPanel, conversationMarkdown, type Tree } from "./Activity.tsx";
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
const run = (id: string, extra: Partial<Run> = {}): Run => ({
  id,
  tag_id: id,
  tenant: "test",
  agent: id,
  rev: 1,
  origin: "api",
  text: id,
  root: "root",
  depth: 1,
  thread: "thread",
  lease: 0,
  granted: 0,
  spent: 0,
  calls: 0,
  status: "completed",
  created_at: at,
  updated_at: at,
  ...extra,
});
const tree: Tree = {
  root: "root",
  runs: [
    run("root", { depth: 0 }),
    run("handoff", { parent: "root", mode: "handoff" }),
    run("delegate", { parent: "root", mode: "delegate" }),
    run("tool", { parent: "root" }),
    run("decision", { parent: "root", assistance: "needed" }),
    run("question", {
      parent: "root",
      question: { id: "q1", kind: "choice", prompt: "Choose a direction" },
    }),
    run("authored", { parent: "root" }),
    run("failed", { parent: "root", status: "failed" }),
  ],
  spans: [
    { run_id: "tool", agent: "tool", kind: "tool", name: "read_source", start: at, end: at },
    { run_id: "root", agent: "root", kind: "attempt", name: "model", start: at },
  ],
};
const rows = () =>
  screen.queryAllByTestId("activity-row").map((row) => row.getAttribute("data-run"));

test("Activity lists every run in time order, opens its agent's conversation, and leads back by the trail", () => {
  restore = stubFetch({}).restore;
  rendered = true;
  const twice: Tree = {
    root: "root",
    runs: [
      run("root", { depth: 0, agent: "lead", text: "Plan the launch" }),
      run("a", { parent: "root", agent: "researcher", text: "Find dates", answer: "May 4" }),
      run("b", {
        parent: "root",
        agent: "researcher",
        text: "Find venues",
        created_at: "2026-10-26T10:01:00Z",
        answer: "Hall B",
      }),
    ],
    spans: [],
  };
  let selected: string | null = null;
  const view = render(
    <ActivityPanel
      trees={[twice]}
      selected={selected}
      onSelect={(id) => {
        selected = id;
      }}
      onClose={() => {}}
    />,
  );
  // Every run, oldest first, with the last thing it said.
  assert.deepEqual(
    screen.getAllByTestId("activity-row").map((r) => r.getAttribute("data-run")),
    ["root", "a", "b"],
  );
  assert.ok(screen.getByText("Hall B"));
  // No filters or tool toggle.
  assert.equal(screen.queryByRole("group", { name: defaultCopy.activityControls.filters }), null);
  fireEvent.click(screen.getAllByTestId("activity-row")[2] as HTMLElement);
  assert.equal(selected, "b");
  view.rerender(
    <ActivityPanel
      trees={[twice]}
      selected="b"
      onSelect={(id) => (selected = id)}
      onClose={() => {}}
    />,
  );
  // Both calls, as one conversation, each with who asked.
  assert.equal(screen.getAllByTestId("activity-conversation").length, 2);
  assert.equal(screen.getAllByText(/lead asked/).length, 2);
  const trail = screen.getByRole("navigation", { name: "Called by" });
  fireEvent.click(within(trail).getByRole("button", { name: "lead" }));
  assert.equal(selected, "root");
  fireEvent.click(within(trail).getByRole("button", { name: "All runs" }));
  assert.equal(selected, null);
});

test("Copy Conversation copies the conversation as Markdown", () => {
  assert.equal(
    conversationMarkdown(
      "researcher",
      [run("a", { parent: "root", agent: "researcher", text: "Find dates", answer: "May 4" })],
      new Map([["root", run("root", { depth: 0, agent: "lead" })]]),
    ),
    "**lead → researcher**\n\nFind dates\n\n**researcher**\n\nMay 4",
  );
});
