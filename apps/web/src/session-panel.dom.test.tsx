import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { foldAll } from "@humagen/core";
import { responseStreams } from "@humagen/core/fixtures/responses";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { Run } from "./adapter.ts";
import { ConversationPage } from "./ConversationShell.tsx";
import { SessionPanel, sessionProgress } from "./SessionPanel.tsx";
import { connectorOf, fileType, shortList, summarize } from "./sessionSummary.ts";
import { referenceOf } from "./trace.ts";
import { hueOf, setAgentHues } from "./ui.tsx";

// The server assigns each agent its color; here the test does.
setAgentHues({
  "security-reviewer": hueOf("security-reviewer"),
  "perf-reviewer": hueOf("perf-reviewer"),
});

const words = defaultCopy.details;
afterEach(() => cleanup());

const toolsResponse = () => responseStreams.usedTools;

test("a session's summary sorts what it did into agents, outputs, environment and what it used", () => {
  const responses = [foldAll(responseStreams.parallelAgents), foldAll(toolsResponse())];
  const summary = summarize(responses, {
    files: [
      {
        id: "a1",
        title: "Austin apartment options.xlsx",
        kind: "spreadsheet",
        mime: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
      },
    ],
    uploads: ["leases.pdf", "budget.csv", "notes.txt"],
  });
  assert.deepEqual(summary.agents, [
    { name: "security-reviewer", calls: 1 },
    { name: "perf-reviewer", calls: 1 },
  ]);
  assert.deepEqual(
    summary.outputs.map((o) => [o.kind, o.type, o.title, o.url ?? o.artifactId]),
    [
      ["spreadsheet", "XLSX", "Austin apartment options.xlsx", "a1"],
      ["pr", "PR", "urmzd/humagen #300", "https://github.com/urmzd/humagen/pull/300"],
    ],
  );
  assert.deepEqual(summary.environment, [{ kind: "sandbox", label: "Sandbox", detail: "node-20" }]);
  assert.deepEqual(
    summary.used.connections.map((c) => c.label),
    ["GitHub", "Gmail"],
  );
  assert.deepEqual(
    summary.used.knowledge.map((c) => c.label),
    ["runbooks"],
  );
  assert.deepEqual(
    summary.used.web.map((c) => c.label),
    ["https://docs.example/tokens"],
  );
  assert.deepEqual(
    summary.used.memory.map((m) => [m.detail, m.label]),
    [
      ["Read", "deploy-notes"],
      ["Wrote", "token-rotation"],
    ],
  );
  assert.equal(
    shortList(summary.used.uploads.length ? summary.used.uploads : []),
    "leases.pdf, budget.csv +1",
  );
  assert.equal(summary.progress?.status, "done");
  assert.equal(summary.open, 0);
  assert.equal(connectorOf("slack_post_message"), "Slack");
  assert.equal(connectorOf("read_diff"), undefined);
  assert.deepEqual(
    fileType({ id: "x", title: "report.pdf", kind: "pdf", mime: "application/pdf" }),
    { kind: "pdf", type: "PDF" },
  );
});

test("a waiting session says so in Progress and counts what is open", () => {
  const summary = summarize([foldAll(responseStreams.nestedApproval)]);
  assert.equal(summary.progress?.waiting, true);
  assert.ok(summary.open >= 1);
});

test("the panel shows only sections with something in them, and each row opens what it names", () => {
  const responses = [foldAll(responseStreams.parallelAgents), foldAll(toolsResponse())];
  const summary = summarize(responses, { uploads: ["leases.pdf"] });
  const opened: string[] = [];
  render(
    <SessionPanel
      responses={responses}
      summary={summary}
      origin={referenceOf("github://urmzd/humagen/pull/243")}
      onClose={() => {}}
      onOpenFile={(id) => opened.push(id)}
    />,
  );
  const panel = screen.getByTestId("session-panel");
  const sections = within(panel)
    .getAllByTestId("details-section")
    .map((s) => s.getAttribute("aria-label"));
  assert.deepEqual(sections, [
    words.progress,
    words.agents,
    words.outputs,
    words.environment,
    words.usedTitle,
  ]);
  // The progress line counts the whole session, as the Agents section does.
  assert.equal(
    within(panel).getByTestId("details-progress").textContent,
    defaultCopy.trace.worked("15s", 2, 10),
  );
  // Each agent row carries its mark.
  for (const row of within(panel).getAllByTestId("details-agent"))
    assert.ok(row.querySelector("[data-accent=agent]"), "agent mark");
  const used = within(panel)
    .getAllByTestId("details-used")
    .map((r) => r.textContent ?? "");
  assert.ok(
    used.some((t) => t.startsWith(words.used.origin) && t.includes("urmzd/humagen PR #243")),
  );
  assert.ok(used.some((t) => t.startsWith(words.used.connections) && t.includes("GitHub, Gmail")));
  assert.ok(
    used.some(
      (t) =>
        t.startsWith(words.used.memory) &&
        t.includes("Read · deploy-notes; Wrote · token-rotation"),
    ),
  );
  // A made thing opens where it lives.
  const pr = within(panel)
    .getAllByTestId("details-output")
    .find((o) => o.textContent?.includes("#300"));
  assert.equal(pr?.getAttribute("href"), "https://github.com/urmzd/humagen/pull/300");
  // Connections opens its list; Back returns.
  fireEvent.click(
    within(panel)
      .getAllByTestId("details-used")
      .find((r) => r.textContent?.startsWith(words.used.connections)) as HTMLElement,
  );
  assert.equal(screen.getByTestId("session-panel").getAttribute("data-view"), "used");
  assert.deepEqual(
    within(screen.getByTestId("used-list"))
      .getAllByRole("listitem")
      .map((li) => li.textContent),
    ["GitHub", "Gmail"],
  );
  fireEvent.click(screen.getByRole("button", { name: words.back }));
  // An agent opens its calls in Activity.
  fireEvent.click(
    within(screen.getByTestId("session-panel")).getAllByTestId("details-agent")[1] as HTMLElement,
  );
  assert.deepEqual(
    screen.getAllByTestId("activity-lane").map((l) => l.getAttribute("data-agent")),
    ["perf-reviewer"],
  );
});

test("a session with no agents, outputs or tools shows only its progress", () => {
  const responses = [
    foldAll(responseStreams.singleAgent.filter((e) => !String(JSON.stringify(e)).includes("htu_"))),
  ];
  render(<SessionPanel responses={responses} summary={summarize(responses)} onClose={() => {}} />);
  assert.deepEqual(
    screen.getAllByTestId("details-section").map((s) => s.getAttribute("aria-label")),
    [words.progress],
  );
});

test("the header's Details icon counts calls running or waiting and is highlighted while working", () => {
  const run = (id: string, status: Run["status"]) => ({ id, status }) as Run;
  assert.deepEqual(
    sessionProgress([run("a", "running"), run("b", "parked"), run("c", "completed")]),
    {
      open: 2,
      working: true,
    },
  );
  render(
    <ConversationPage
      title="Deploy"
      project="Personal"
      private
      agents={[]}
      progress={{ open: 2, working: true }}
      onPanel={() => {}}
    >
      <div />
    </ConversationPage>,
  );
  const details = screen.getByRole("button", { name: words.open(2) });
  assert.equal(details.textContent, "2");
  assert.equal(details.hasAttribute("data-working"), true);
  cleanup();
  render(
    <ConversationPage
      title="Deploy"
      project="Personal"
      private
      agents={[]}
      progress={{ open: 0, working: false }}
      onPanel={() => {}}
    >
      <div />
    </ConversationPage>,
  );
  const quiet = screen.getByRole("button", { name: words.open(0) });
  assert.equal(quiet.textContent, "");
  assert.equal(quiet.hasAttribute("data-working"), false);
});
