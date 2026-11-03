import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { stubFetch } from "./dom.ts";
import type { ConversationView } from "./route.ts";
import { Workspace } from "./Workspace.tsx";

const words = defaultCopy.workspace;
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

const base = "/api/threads/thread-1";
const sandboxes = {
  session: { isolated: true, files: 2, bytes: 2048, retention_days: 7, writable: true },
  more: false,
  runs: [
    {
      run: "r-lead",
      agent: "Lead",
      depth: 0,
      status: "completed",
      created_at: "2026-11-01T10:00:00Z",
      isolated: true,
      mounts: [
        { dir: "/work", kind: "session" },
        { dir: "/project/memory", kind: "project", name: "Launch" },
        { dir: "/project/memory/@parents/%40workspace", kind: "project", read_only: true },
      ],
      files: [
        { path: "report.md", size: 12, revision: "a".repeat(64) },
        { path: "old.txt", size: 0, removed: true },
      ],
      conflicts: ['workspace files: conflict: "plan.md" changed after this run loaded it'],
    },
    {
      run: "r-writer",
      agent: "Writer",
      parent: "r-lead",
      depth: 1,
      status: "completed",
      created_at: "2026-11-01T10:01:00Z",
      isolated: true,
      mounts: [{ dir: "/work", kind: "session" }],
      files: [],
    },
  ],
};

function setup() {
  net = stubFetch({
    "GET /api/me": () => ({ permissions: ["runs:read", "runs:write", "artifacts:write"] }),
    [`GET ${base}/sandboxes`]: () => sandboxes,
    [`GET ${base}/files`]: () => ({
      files: [{ path: "report.md", size: 12, revision: "a".repeat(64) }],
      writable: true,
      retention_days: 7,
    }),
    [`GET ${base}/files/content`]: (_body, query) => ({
      path: query.get("path"),
      content: "# Report\n",
      revision: "a".repeat(64),
    }),
    "GET /api/artifacts": (_body, query) => {
      assert.equal(query.get("thread"), "thread-1");
      return {
        artifacts: [
          {
            id: "art-1",
            title: "Launch brief.md",
            kind: "document",
            version: 3,
            mime: "text/markdown",
            size: 1,
            sha256: "b".repeat(64),
            run: "r-lead",
            thread: "thread-1",
            agent: "Lead",
            created_by: "ana",
            created_at: "2026-11-01T10:00:00Z",
          },
        ],
        next: null,
      };
    },
  });
  return net;
}

function Harness({
  initial,
  opened,
  views,
}: {
  initial: ConversationView;
  opened: string[];
  views?: ConversationView[];
}) {
  const [view, setView] = useState<ConversationView>(initial);
  return (
    <Workspace
      thread="thread-1"
      open
      view={view}
      onView={(next) => {
        views?.push(next);
        setView(next);
      }}
      onClose={() => {}}
      onArtifact={(id) => opened.push(`artifact:${id}`)}
      onRun={(run) => opened.push(`run:${run}`)}
    />
  );
}

test("the Workspace panel holds its sections as tabs, each with its own address", async () => {
  setup();
  const views: ConversationView[] = [];
  render(<Harness initial={{ panel: "workspace" }} opened={[]} views={views} />);
  const tabs = await screen.findAllByRole("tab");
  assert.deepEqual(
    tabs.map((t) => t.textContent),
    [
      words.section.sandboxes,
      words.section.files,
      words.section.scratchpads,
      words.section.memories,
      words.section.artifacts,
    ],
  );
  // Files is the default section, at the panel's own address.
  assert.equal(
    screen.getByRole("tab", { name: words.section.files }).getAttribute("aria-selected"),
    "true",
  );
  fireEvent.click(screen.getByRole("tab", { name: words.section.sandboxes }));
  assert.equal(views.at(-1)?.workspaceSection, "sandboxes");
  fireEvent.click(screen.getByRole("tab", { name: words.section.files }));
  assert.equal(views.at(-1)?.workspaceSection, undefined);
});

test("Sandboxes shows the session sandbox and a nested sandbox per agent run", async () => {
  setup();
  const opened: string[] = [];
  const views: ConversationView[] = [];
  render(
    <Harness
      initial={{ panel: "workspace", workspaceSection: "sandboxes" }}
      opened={opened}
      views={views}
    />,
  );
  assert.ok(await screen.findByText(words.sessionSandbox));
  assert.ok(screen.getByText(words.sessionHint(true)));
  assert.ok(screen.getByText(words.sessionStats(2, words.size(2048))));
  // A called run is nested under its caller.
  const writer = screen.getByRole("button", { name: /Writer/ });
  assert.equal(writer.closest("li")?.getAttribute("style"), "padding-inline-start: 16px;");
  fireEvent.click(screen.getByRole("button", { name: /^.*Lead.*2 files/ }));
  assert.equal(views.at(-1)?.sandbox, "r-lead");
  // The run's sandbox: its mounts, what it left, and what copyback refused.
  assert.ok(await screen.findByText("/work"));
  assert.ok(screen.getByText(`${words.mountKind.project}: Launch`));
  assert.ok(screen.getByText(`${words.mountKind.project}, ${words.readOnly}`));
  assert.ok(screen.getByText(words.removed));
  assert.ok(screen.getByText(words.conflictHint));
  fireEvent.click(screen.getByRole("button", { name: words.openActivity }));
  assert.deepEqual(opened, ["run:r-lead"]);
  // A file it left opens in Files.
  fireEvent.click(screen.getByRole("button", { name: words.openFile("report.md") }));
  assert.equal(views.at(-1)?.workspaceSection, undefined);
  assert.equal(views.at(-1)?.file, "report.md");
  assert.ok(await screen.findByRole("textbox", { name: "Edit report.md" }));
});

test("a run that left nothing and a conversation with no runs say so", async () => {
  setup();
  render(
    <Harness
      initial={{ panel: "workspace", workspaceSection: "sandboxes", sandbox: "r-writer" }}
      opened={[]}
    />,
  );
  assert.ok(await screen.findByText(words.noFiles));
  cleanup();
  net?.restore();
  net = stubFetch({
    [`GET ${base}/sandboxes`]: () => ({ ...sandboxes, runs: [] }),
    [`GET ${base}/files`]: () => ({ files: [], writable: true, retention_days: 7 }),
  });
  render(<Harness initial={{ panel: "workspace", workspaceSection: "sandboxes" }} opened={[]} />);
  assert.ok(await screen.findByText(words.noRuns));
});

test("a failed sandboxes read is recoverable", async () => {
  let calls = 0;
  net = stubFetch({
    [`GET ${base}/sandboxes`]: () => {
      calls++;
      return calls === 1 ? new Response("{}", { status: 500 }) : sandboxes;
    },
    [`GET ${base}/files`]: () => ({ files: [], writable: true, retention_days: 7 }),
  });
  render(<Harness initial={{ panel: "workspace", workspaceSection: "sandboxes" }} opened={[]} />);
  assert.ok(await screen.findByText(words.failed));
  fireEvent.click(screen.getByRole("button", { name: words.retry }));
  assert.ok(await screen.findByText(words.sessionSandbox));
});

test("Artifacts lists saved versions and opens one in Deliverables", async () => {
  setup();
  const opened: string[] = [];
  render(
    <Harness initial={{ panel: "workspace", workspaceSection: "artifacts" }} opened={opened} />,
  );
  const item = await screen.findByRole("button", { name: /Launch brief\.md/ });
  assert.ok(screen.getByText(words.artifactVersion(3)));
  fireEvent.click(item);
  await waitFor(() => assert.deepEqual(opened, ["artifact:art-1"]));
});
