import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy, TurnView } from "@humagen/components";
import { ActionId, type Turn, type UiAction } from "@humagen/core";
import { WebSurface } from "@humagen/render-web";
import { AgentMessage } from "@humagen/web";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useState } from "react";
import { ConversationInfo, ConversationPage } from "./ConversationShell.tsx";
import { Deliverables, revisionPrompt } from "./Deliverables.tsx";
import { stubFetch } from "./dom.ts";
import type { ConversationView } from "./route.ts";
import type { Stored } from "./storage.ts";
import { Composer, type ComposerSeed } from "./Composer.tsx";
import { useQueue } from "@humagen/web";
import type { Thread } from "./api.ts";

const words = defaultCopy.conversation;
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

const turn: Turn = {
  id: "run-1",
  agent: { name: "Writer" },
  status: "done",
  text: "The launch brief is ready.",
  tasks: [
    {
      id: "step-1",
      title: "Read the plan",
      status: "complete",
      tool: "read_file",
      input: '{"path":"plan.md"}',
      output: '{"summary":"The embargo stays on November 12","matches":3}',
    },
  ],
  citations: [],
  interrupts: [],
  notices: [],
};

test("completed work is grouped while the answer stays visible, with technical detail available", () => {
  render(
    <WebSurface>
      <AgentMessage turn={turn} />
    </WebSurface>,
  );
  assert.ok(screen.getByText("The launch brief is ready."));
  const group = document.querySelector("details");
  assert.ok(group);
  assert.equal(group.open, false);
  group.open = true;
  fireEvent(group, new Event("toggle", { bubbles: true }));
  fireEvent.click(screen.getByRole("button", { name: /Read the plan/ }));
  const technical = screen.getByText("Technical details").closest("details");
  assert.ok(technical);
  technical.open = true;
  fireEvent.click(within(technical).getByRole("button", { name: "Input" }));
  assert.ok(within(technical).getByText('{"path":"plan.md"}'));
  fireEvent.click(within(technical).getByRole("button", { name: "Output" }));
  assert.match(technical.textContent ?? "", /"matches":3/);
});

test("a tool waiting for approval opens its work group without animating", () => {
  render(
    <WebSurface>
      <AgentMessage
        turn={{
          ...turn,
          status: "waiting",
          tasks: [{ id: "step-1", title: "Send vendor notice", status: "waiting" }],
        }}
      />
    </WebSurface>,
  );
  assert.equal(document.querySelector("details")?.open, true);
  assert.ok(screen.getByText("Waiting for approval"));
});

test("picking a clarification does not answer or approve until the explicit submit", () => {
  const actions: UiAction[] = [];
  const question: Turn = {
    ...turn,
    tasks: [],
    status: "waiting",
    interrupts: [
      {
        type: "clarification",
        id: "audience",
        question: "Who is the briefing for?",
        allowOther: true,
        options: [
          { id: "leadership", label: "Leadership" },
          { id: "team", label: "Project team" },
        ],
        answer: { kind: "pending" },
      },
      {
        type: "approval",
        id: "send",
        summary: "Send four vendor notices",
        tool: "send_notice",
        decision: { kind: "pending" },
      },
    ],
  };
  render(
    <WebSurface onAction={(action) => actions.push(action)}>
      <TurnView turn={question} />
    </WebSurface>,
  );
  const submit = screen.getByRole("button", { name: words.answerChoice }) as HTMLButtonElement;
  assert.equal(submit.disabled, true);
  fireEvent.click(screen.getByRole("button", { name: "Leadership" }));
  assert.equal(actions.length, 0);
  assert.equal(submit.disabled, false);
  fireEvent.click(submit);
  assert.equal(actions.length, 1);
  assert.equal(actions[0]?.id, ActionId.clarify);
  assert.equal(actions[0]?.payload.option, "leadership");
  assert.ok(screen.getByRole("button", { name: "Approve: Send four vendor notices" }));
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.somethingElse }));
  assert.equal(submit.disabled, true);
  assert.equal(actions.length, 1);
});

test("conversation settings exposes Activity and Deliverables around one page title", () => {
  const chosen: string[] = [];
  render(
    <ConversationPage
      title="Launch readiness"
      project="Spring Launch"
      private
      agents={["Writer", "Ops"]}
      onPanel={(panel) => chosen.push(panel)}
    >
      {null}
    </ConversationPage>,
  );
  assert.equal(screen.getAllByRole("heading", { level: 1 }).length, 1);
  assert.ok(screen.getByRole("button", { name: words.context }));
  fireEvent.click(screen.getByRole("button", { name: words.settings }));
  fireEvent.click(screen.getByRole("button", { name: words.activity }));
  assert.deepEqual(chosen, ["activity"]);
  assert.equal(screen.queryByText("Execution history"), null);
});

test("adding an agent or artifact reference preserves an unsent draft and starts no work", () => {
  const sent: string[] = [];
  function Compose() {
    const [seed, setSeed] = useState<ComposerSeed>();
    const queue = useQueue({
      onStart: (message) => sent.push(message.text),
      onInterrupt: () => {},
    });
    return (
      <>
        <button type="button" onClick={() => setSeed({ text: "@Writer ", mode: "prepend" })}>
          Ask Writer
        </button>
        <Composer placeholder="Message" queue={queue} seed={seed} />
      </>
    );
  }
  render(<Compose />);
  const input = screen.getByRole("textbox") as HTMLTextAreaElement;
  fireEvent.change(input, { target: { value: "Keep my draft." } });
  fireEvent.click(screen.getByRole("button", { name: "Ask Writer" }));
  assert.equal(input.value, "@Writer Keep my draft.");
  assert.equal(document.activeElement === input, true);
  assert.equal(sent.length, 0);
});

const saved: Stored = {
  id: "art-2",
  title: "Launch brief.md",
  kind: "document",
  mime: "text/markdown",
  size: 30,
  sha256: "a".repeat(64),
  version: 2,
  run: "run-1",
  thread: "thread-1",
  agent: "Writer",
  created_by: "ana",
  created_at: "2026-10-23T10:00:00Z",
};

test("the workbench opens an older saved version and sends that exact version for revision", async () => {
  const previous = { ...saved, id: "art-1", version: 1 };
  net = stubFetch({
    "GET /api/artifacts": (_body, query) => {
      assert.equal(query.get("thread"), "thread-1");
      return { artifacts: [saved], next: null };
    },
    "GET /api/artifacts/art-2": () => saved,
    "GET /api/artifacts/art-1": () => previous,
    "GET /api/artifacts/art-2/versions": () => ({ versions: [saved, previous] }),
    "GET /api/artifacts/art-1/versions": () => ({ versions: [saved, previous] }),
    "GET /api/artifacts/art-2/content": () =>
      new Response("# Current brief\n\nThe decision is ready."),
    "GET /api/artifacts/art-1/content": () => new Response("# Original brief\n\nThe first draft."),
  });
  const revised: Stored[] = [];
  function Workbench() {
    const [view, setView] = useState<ConversationView>({
      panel: "deliverables",
      artifact: saved.id,
    });
    return (
      <Deliverables
        thread="thread-1"
        view={view}
        onView={setView}
        onClose={() => {}}
        onRevise={(file) => revised.push(file)}
      />
    );
  }
  render(<Workbench />);
  assert.ok(await screen.findByText("Current brief"));
  fireEvent.click(screen.getByRole("button", { name: "Versions" }));
  fireEvent.click(screen.getByRole("button", { name: /Version 1/ }));
  assert.ok(await screen.findByText("Original brief"));
  fireEvent.click(screen.getByRole("button", { name: words.artifactFeedback }));
  assert.equal(revised[0]?.id, "art-1");
  assert.equal(revised[0]?.version, 1);
  assert.equal(saved.version, 2);
  assert.equal(
    net.seen.some((request) => request.method === "POST"),
    false,
  );
  assert.match(revisionPrompt(previous, 3), /v1, slide 3/);
  assert.match(revisionPrompt(previous, 3), /\/conversations\/thread-1\/deliverables\/art-1/);
});

test("an unavailable version history does not block the saved preview", async () => {
  net = stubFetch({
    "GET /api/artifacts": () => ({ artifacts: [saved], next: null }),
    "GET /api/artifacts/art-2": () => saved,
    "GET /api/artifacts/art-2/versions": () => new Response(null, { status: 503 }),
    "GET /api/artifacts/art-2/content": () => new Response("# Saved brief\n\nStill readable."),
  });
  function Workbench() {
    const [view, setView] = useState<ConversationView>({ artifact: saved.id });
    return <Deliverables thread="thread-1" view={view} onView={setView} onClose={() => {}} />;
  }
  render(<Workbench />);
  assert.ok(await screen.findByText("Saved brief"));
  assert.ok(screen.getByRole("link", { name: "Download Original" }));
  fireEvent.click(screen.getByRole("button", { name: "Versions" }));
  assert.ok(await screen.findByText(words.artifactFailed));
});

test("conversation deliverables page through the server cursor without duplicating files", async () => {
  const older = { ...saved, id: "older", title: "Earlier brief.md" };
  net = stubFetch({
    "GET /api/artifacts": (_body, query) => {
      assert.equal(query.get("thread"), "thread-1");
      return query.get("before") === "cursor-1"
        ? { artifacts: [saved, older], next: null }
        : { artifacts: [saved], next: "cursor-1" };
    },
  });
  render(<Deliverables thread="thread-1" view={{}} onView={() => {}} onClose={() => {}} />);
  fireEvent.click(await screen.findByRole("button", { name: words.more }));
  assert.ok(await screen.findByText("Earlier brief.md"));
  assert.equal(screen.getAllByText(saved.title).length, 1);
  assert.equal(screen.queryByRole("button", { name: words.more }), null);
});

test("a version from a different conversation cannot be presented as this conversation's result", async () => {
  net = stubFetch({
    "GET /api/artifacts": () => ({ artifacts: [], next: null }),
    "GET /api/artifacts/art-2": () => ({ ...saved, thread: "other-thread" }),
    "GET /api/artifacts/art-2/versions": () => ({ versions: [saved] }),
  });
  render(
    <Deliverables
      thread="thread-1"
      view={{ artifact: saved.id }}
      onView={() => {}}
      onClose={() => {}}
    />,
  );
  assert.ok(await screen.findByText(words.artifactFailed));
  assert.equal(screen.queryByRole("link", { name: "Download Original" }), null);
  assert.equal(
    net.seen.some((request) => request.path.endsWith("/content")),
    false,
  );
});

test("project context shows the document's origin and actual import time", async () => {
  net = stubFetch({
    "GET /api/projects/Launch/knowledge": () => ({
      version: 1,
      knowledge: ["supplier"],
      given: [{ name: "supplier", from: "Launch" }],
    }),
    "GET /api/knowledge": () => ({
      sources: [
        {
          name: "supplier",
          documents: 1,
          passages: 4,
          excluded: 0,
          variants: [],
          updated_at: "2026-10-23T09:42:00Z",
        },
      ],
    }),
    "GET /api/knowledge/supplier/documents": () => ({
      documents: [
        {
          id: "d-1",
          title: "Supplier ETA",
          uri: "drive://launch/eta",
          bytes: 300,
          passages: 4,
          excluded: false,
          added_at: "2026-10-23T09:42:00Z",
        },
      ],
    }),
  });
  const thread: Thread = {
    id: "thread-1",
    project: "Launch",
    channel: "general",
    visibility: "private",
    participants: [],
    version: 1,
    created_by: "ana",
    created_at: "",
    updated_at: "",
  };
  render(<ConversationInfo panel="context" thread={thread} project="Launch" onClose={() => {}} />);
  const label = await screen.findByText("supplier");
  const details = label.closest("details");
  assert.ok(details);
  details.open = true;
  fireEvent(details, new Event("toggle", { bubbles: true }));
  assert.ok(await screen.findByText("Origin: drive://launch/eta"));
  const date = document.querySelector("time");
  assert.equal(date?.getAttribute("datetime"), "2026-10-23T09:42:00Z");
  assert.equal(screen.queryByText("Live"), null);
});
