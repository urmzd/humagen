import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import type { Turn } from "@humagen/core";
import { multiStepTurn } from "@humagen/core/fixtures";
import { WebSurface } from "@humagen/render-web";
import { AgentMessage, FileLinkContext } from "@humagen/web";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";

// Only this file's renders: other DOM files clean up their own.
let rendered = false;
afterEach(() => {
  if (!rendered) return;
  cleanup();
  rendered = false;
});

function show(turn: Turn, opened: string[] = []) {
  rendered = true;
  return render(
    <FileLinkContext.Provider
      value={{
        href: (path) => `/conversations/c1/workspace?file=${encodeURIComponent(path)}&tab=changes`,
        open: (path) => opened.push(path),
      }}
    >
      <WebSurface>
        <AgentMessage turn={turn} />
      </WebSurface>
    </FileLinkContext.Provider>,
  );
}

test("a turn reads as its words and runs of steps, in the order they came", () => {
  show(multiStepTurn);
  const article = screen.getByRole("article", { name: /message/ });
  const text = article.textContent ?? "";
  const order = [
    "I'll read the pull request",
    "Checked urmzd/playscape#21",
    "The change adds a remote Docker driver",
    "Read 2 files, checked a source, ran a command",
    "I wrote my notes",
    "Wrote scratchpads/review-notes.md",
    "The driver is sound.",
  ].map((s) => text.indexOf(s));
  assert.ok(
    order.every((i) => i >= 0),
    `every part is shown: ${order}`,
  );
  assert.deepEqual(
    [...order].sort((a, b) => a - b),
    order,
    "in the order they came",
  );
  assert.ok(text.includes("Worked for 44s"));
  // A failed call is counted, quietly.
  assert.ok(text.includes("1 failed"));
});

test("a run of steps opens to one row per call, and a row to its arguments and result", () => {
  show(multiStepTurn);
  const groups = screen.getAllByTestId("step-group");
  const mixed = groups.find((g) => (g.textContent ?? "").includes("Read 2 files")) as HTMLElement;
  assert.equal(within(mixed).queryAllByTestId("tool-row").length, 0, "folded at first");
  fireEvent.click(within(mixed).getByRole("button", { expanded: false }));
  const rows = within(mixed).getAllByTestId("tool-row");
  assert.equal(rows.length, 4);
  assert.ok((rows[3]?.textContent ?? "").includes("go test ./pkg/isolate/docker/..."));
  // The command row opens to the command and what it printed.
  fireEvent.click(within(rows[3] as HTMLElement).getByRole("button", { expanded: false }));
  const blocks = within(rows[3] as HTMLElement).getAllByTestId("tool-block");
  assert.equal(blocks[0]?.textContent, "go test ./pkg/isolate/docker/... -run TestRemote -count=1");
  assert.ok((blocks[1]?.textContent ?? "").startsWith("ok"));
  fireEvent.click(within(rows[3] as HTMLElement).getByRole("button", { expanded: true }));
  assert.equal(within(rows[3] as HTMLElement).queryAllByTestId("tool-block").length, 0);
  // And the run folds again.
  fireEvent.click(within(mixed).getAllByRole("button", { expanded: true })[0] as HTMLElement);
  assert.equal(within(mixed).queryAllByTestId("tool-row").length, 0);
});

test("a long result shows a preview with Show more", () => {
  const long = Array.from({ length: 40 }, (_, i) => `line ${i}`).join("\n");
  const turn: Turn = {
    ...multiStepTurn,
    tasks: [{ ...(multiStepTurn.tasks[4] as Turn["tasks"][number]), output: long }],
  };
  show(turn);
  fireEvent.click(
    within(screen.getByTestId("step-group")).getByRole("button", { expanded: false }),
  );
  const row = screen.getByTestId("tool-row");
  fireEvent.click(within(row).getByRole("button", { expanded: false }));
  const result = () => within(row).getAllByTestId("tool-block")[1]?.textContent ?? "";
  assert.ok(!result().includes("line 39"));
  fireEvent.click(within(row).getByRole("button", { name: "Show more" }));
  assert.ok(result().includes("line 39"));
});

test("a file the agent wrote opens in the Workspace at its changes", () => {
  const opened: string[] = [];
  show(multiStepTurn, opened);
  const link = screen.getByRole("link", {
    name: "Open scratchpads/review-notes.md in the Workspace",
  });
  assert.equal(
    link.getAttribute("href"),
    "/conversations/c1/workspace?file=scratchpads%2Freview-notes.md&tab=changes",
  );
  assert.ok((link.textContent ?? "").includes("+4"));
  fireEvent.click(link);
  assert.deepEqual(opened, ["scratchpads/review-notes.md"]);
});

test("while a call runs, its run says which, and a call waiting on a person does not move", () => {
  const running: Turn = {
    ...multiStepTurn,
    status: "working",
    tasks: [
      {
        ...(multiStepTurn.tasks[4] as Turn["tasks"][number]),
        status: "in_progress",
        output: undefined,
      },
    ],
  };
  show(running);
  assert.ok((screen.getByTestId("step-group").textContent ?? "").includes("Running bash"));
  cleanup();
  const waiting: Turn = {
    ...running,
    status: "waiting",
    tasks: [{ ...(running.tasks[0] as Turn["tasks"][number]), status: "waiting" }],
  };
  show(waiting);
  const group = screen.getByTestId("step-group");
  assert.ok((group.textContent ?? "").includes("waiting for you"));
  assert.equal(group.querySelector(".aui-shimmer, [class*='shimmer']"), null, "nothing animates");
  // A run waiting on a person is open, at the step's own row.
  assert.ok(within(group).getByText("Waiting for approval"));
});
