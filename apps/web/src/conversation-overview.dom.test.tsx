import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { WebSurface } from "@humagen/render-web";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useState } from "react";
import type { Run } from "./adapter.ts";
import type { Thread } from "./api.ts";
import { ConversationOverview } from "./ConversationOverview.tsx";
import { ConversationPage } from "./ConversationShell.tsx";
import { stubFetch } from "./dom.ts";
import type { ConversationPanel } from "./route.ts";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
});
const thread = { id: "c1", project: "launch" } as Thread;
const run = {
  id: "r1",
  agent: "code-reviewer",
  status: "completed",
  text: "Review https://github.com/acme/api/pull/12",
  calls: 1,
  usage: { own: {}, total: {}, context: { tokens: 44, window: 100 } },
} as Run;
const sandbox = {
  run: "r1",
  agent: "code-reviewer",
  status: "completed",
  files: [{ path: "a.go", size: 4 }],
  mounts: [],
  depth: 0,
};
const base = {
  "GET /api/threads/c1/sandboxes": () => ({ session: {}, runs: [sandbox] }),
  "GET /api/runs/r1/trace": () => ({
    runs: [run, { ...run, id: "r2", agent: "security-reviewer", parent: "r1", status: "parked" }],
  }),
  "GET /api/runs/r1/events": () => ({
    events: [
      {
        type: "run.delta.tool.call.v1",
        data: {
          id: "edit",
          name: "edit_file",
          arguments: { path: "a.go", old_string: "old", new_string: "new\nline" },
        },
      },
      { type: "run.delta.tool.done.v1", data: { id: "edit", name: "edit_file", result: "done" } },
    ],
  }),
  "GET /api/runs/r2/events": () => ({ events: [] }),
  "GET /api/github/pull": () => ({
    subject: { title: "Repair lease", state: "merged", head: { ref: "fix/lease" } },
    files: [],
    comments: [],
    workflows: [],
  }),
};

test("overview reads environment, latest context, delegated agents and changes with a pull destination", async () => {
  net = stubFetch(base);
  render(
    <WebSurface>
      <ConversationOverview thread={thread} project="Launch" runs={[run]} />
    </WebSurface>,
  );
  await screen.findByText("Repair lease");
  assert.ok(
    screen.getByRole("link", { name: "Launch" }).getAttribute("href")?.includes("/projects/launch"),
  );
  assert.ok(screen.getByText("Connected"));
  assert.ok(screen.getByText("44%"));
  assert.ok(screen.getByText("security-reviewer"));
  assert.ok(screen.getByText("waiting"));
  assert.ok(screen.getByText("fix/lease", { exact: false }));
  assert.ok(screen.getAllByText("+2 -1").length >= 1);
  assert.ok(screen.getByText("Merged"));
  const link = screen.getByRole("link", { name: /Merged acme\/api #12 Repair lease/ });
  assert.ok(link.getAttribute("href")?.includes("/conversations/c1/workspace"));
  assert.ok(link.getAttribute("href")?.includes("pull%3Aacme%2Fapi%2312"));
  const context = screen.getByRole("heading", { name: "Context" }).closest("details")!;
  fireEvent.click(within(context).getByText("Context"));
  assert.ok(within(context).getByText("44%"));
});

test("overview shows calm empty states and retries failed reads", async () => {
  let fail = true;
  net = stubFetch({
    "GET /api/threads/c1/sandboxes": () =>
      fail ? new Response("{}", { status: 500 }) : { session: {}, runs: [] },
  });
  render(
    <WebSurface>
      <ConversationOverview thread={thread} project="Launch" runs={[]} />
    </WebSurface>,
  );
  await screen.findByRole("alert");
  fail = false;
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  await screen.findByText("No repositories yet");
  for (const text of [
    "Not connected",
    "Context usage not reported yet",
    "No agents yet",
    "No changes yet",
    "No pull requests yet",
  ])
    assert.ok(screen.getByText(text));
  assert.equal(screen.queryByRole("button", { name: "Create PR" }), null);
});

test("header panel glyphs are unique and each panel toggles aria-pressed", async () => {
  function Header() {
    const [panel, setPanel] = useState<ConversationPanel>();
    return (
      <ConversationPage
        title="Review"
        project="Launch"
        private
        agents={[]}
        panel={panel}
        onPanel={setPanel}
        onClosePanel={() => setPanel(undefined)}
        onProject={() => {}}
      >
        {null}
      </ConversationPage>
    );
  }
  render(
    <WebSurface>
      <Header />
    </WebSurface>,
  );
  const labels = ["Overview", defaultCopy.workspace.menu, "Context", "Activity"];
  const glyphs = [...labels, defaultCopy.threads.share, defaultCopy.conversation.settings].map(
    (name) => screen.getByRole("button", { name }).querySelector("svg")?.innerHTML,
  );
  assert.equal(new Set(glyphs).size, glyphs.length);
  for (const name of labels) {
    const button = screen.getByRole("button", { name });
    assert.equal(button.title, name);
    fireEvent.click(button);
    await waitFor(() => assert.equal(button.getAttribute("aria-pressed"), "true"));
    fireEvent.click(button);
    assert.equal(button.getAttribute("aria-pressed"), "false");
  }
});
