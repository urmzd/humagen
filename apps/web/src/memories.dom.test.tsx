import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { stubFetch } from "./dom.ts";
import { memoryEditHref } from "./Memories.tsx";
import type { ConversationView } from "./route.ts";
import { Workspace } from "./Workspace.tsx";

const words = defaultCopy.workspace.memories;
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

const at = "2026-11-01T10:00:00Z";
const mine = { kind: "person", self: true, dir: "/me/memory", head: 5, files: 3 };
const project = { kind: "project", project: "Launch", dir: "/project/memory", head: 2, files: 1 };
const memory = {
  isolated: true,
  hidden: 1,
  more: false,
  conversation: [project, mine],
  session: [
    { ...mine, runs: 2, versions: [3, 4], commits: [4, 5], proposals: 0, notes: 0 },
    { ...project, runs: 2, versions: [2], commits: [], proposals: 1, notes: 1 },
  ],
  runs: [
    {
      run: "r-2",
      agent: "Lead",
      depth: 0,
      created_at: at,
      hidden: 1,
      trees: [
        { ...mine, installed: true, version: 4, finished: true, committed: 5 },
        { ...project, installed: true, version: 2, finished: true, proposals: 1, dropped: 2 },
      ],
      notes: [
        {
          id: "n1",
          kind: "project",
          owner: "Launch",
          text: "Releases go out on Tuesdays.",
          status: "pending",
          at,
        },
      ],
    },
  ],
};

function Harness({
  initial,
  views = [],
}: {
  initial: ConversationView;
  views?: ConversationView[];
}) {
  const [view, setView] = useState<ConversationView>(initial);
  return (
    <Workspace
      thread="thread-1"
      open
      view={view}
      onView={(next) => {
        views.push(next);
        setView(next);
      }}
      onClose={() => {}}
      onArtifact={() => {}}
    />
  );
}

function setup(value: unknown = memory) {
  net = stubFetch({
    "GET /api/threads/thread-1/memory": () => value,
    "GET /api/threads/thread-1/files": () => ({ files: [], writable: true, retention_days: 7 }),
  });
}

test("Memories shows the conversation's, the session's and each message's memory, read only", async () => {
  setup();
  const views: ConversationView[] = [];
  render(<Harness initial={{ panel: "workspace", workspaceSection: "memories" }} views={views} />);
  assert.ok(await screen.findByText(words.conversation));
  assert.ok(screen.getByText(words.hint(true)));
  assert.ok(screen.getByText(words.hidden(1)));
  // The session: versions used and written across its runs.
  assert.ok(screen.getByText(words.versionsUsed([3, 4])));
  assert.ok(screen.getByText(words.commits([4, 5])));
  assert.ok(screen.getByText(words.proposals(1)));
  // Links go where memory is changed; nothing here edits it.
  const links = screen
    .getAllByRole("link", { name: words.edit })
    .map((a) => a.getAttribute("href"));
  assert.ok(links.includes("/library/memory?scope=personal"));
  assert.ok(links.includes("/library/memory?scope=project&owner=Launch"));
  assert.equal(screen.queryByRole("button", { name: /Save|Delete/ }), null);
  // A message: what it was given and kept, and the notes it drew.
  fireEvent.click(screen.getByRole("button", { name: /Lead/ }));
  assert.equal(views.at(-1)?.memoryRun, "r-2");
  assert.ok(await screen.findByText(words.installed(4)));
  assert.ok(screen.getByText(words.committed(5)));
  assert.ok(screen.getByText(words.dropped(2)));
  assert.ok(screen.getByText("Releases go out on Tuesdays."));
});

test("an unisolated deployment says memory reaches runs through context", async () => {
  setup({
    ...memory,
    isolated: false,
    runs: [
      {
        ...memory.runs[0],
        trees: [{ ...project, installed: false }],
        notes: [],
        hidden: 0,
      },
    ],
  });
  render(
    <Harness initial={{ panel: "workspace", workspaceSection: "memories", memoryRun: "r-2" }} />,
  );
  assert.ok(await screen.findByText(words.inContext));
});

test("memory edit links never point at another person's memory", () => {
  assert.equal(memoryEditHref({ kind: "person", dir: "/me/memory", head: 1, files: 1 }), undefined);
  assert.equal(
    memoryEditHref({
      kind: "agent",
      agent: "lead",
      dir: "/project/memory/lead",
      head: 0,
      files: 0,
    }),
    "/agents/lead/memory",
  );
  assert.equal(
    memoryEditHref({ kind: "project", project: "", dir: "/project/memory", head: 0, files: 0 }),
    "/library/memory?scope=workspace",
  );
});
