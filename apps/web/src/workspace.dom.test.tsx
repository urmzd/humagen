import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy, TurnView } from "@humagen/components";
import { ActionId, type Task, type Turn, type UiAction } from "@humagen/core";
import { WebSurface } from "@humagen/render-web";
import { AgentMessage, useQueue } from "@humagen/web";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { ArtifactPromotion } from "./ArtifactPromotion.tsx";
import type { Thread } from "./api.ts";
import { Composer, type ComposerSeed } from "./Composer.tsx";
import { ConversationInfo, ConversationPage } from "./ConversationShell.tsx";
import { Deliverables, revisionPrompt } from "./Deliverables.tsx";
import { stubFetch } from "./dom.ts";
import type { ConversationView } from "./route.ts";
import type { Stored } from "./storage.ts";

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

test("each step sits where it happened, as one line that opens to its input and output", () => {
  render(
    <WebSurface>
      <AgentMessage
        turn={{
          ...turn,
          text: "Checking the plan. The launch brief is ready.",
          tasks: [{ ...(turn.tasks[0] as Task), at: "Checking the plan. ".length }],
        }}
      />
    </WebSurface>,
  );
  const text = document.body.textContent ?? "";
  const before = text.indexOf("Checking the plan.");
  const step = text.indexOf("Read plan.md");
  const after = text.indexOf("The launch brief is ready.");
  assert.ok(before >= 0 && before < step && step < after, `${before} ${step} ${after}`);
  // Folded, the line says what was done; open, each step shows its input and output.
  assert.equal(screen.queryByText("Arguments"), null);
  fireEvent.click(screen.getByRole("button", { name: "Read plan.md" }));
  fireEvent.click(screen.getByRole("button", { name: /Read the plan/ }));
  assert.ok(screen.getByText("Arguments"));
  assert.match(document.body.textContent ?? "", /"path": "plan.md"/);
  assert.ok(screen.getByText("Result"));
  assert.match(document.body.textContent ?? "", /The embargo stays on November 12/);
  // The trace folds under "Worked", leaving the answer.
  fireEvent.click(screen.getByRole("button", { name: /^Worked/ }));
  assert.equal(screen.queryByText("Read plan.md"), null);
  assert.ok(screen.getByText(/The launch brief is ready\./));
});

test("a tool waiting for approval opens its step line without animating", () => {
  render(
    <WebSurface>
      <AgentMessage
        turn={{
          ...turn,
          status: "waiting",
          tasks: [{ id: "step-1", title: "Send vendor notice", status: "waiting", at: 0 }],
        }}
      />
    </WebSurface>,
  );
  assert.equal(
    screen.getAllByRole("button", { name: /waiting for you/ })[0]?.getAttribute("aria-expanded"),
    "true",
  );
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

test("the conversation offers one Details action and its project around one page title", () => {
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
  fireEvent.click(screen.getByRole("button", { name: "Details" }));
  assert.deepEqual(chosen, ["details"]); // Details opens the session's Details panel.
  assert.equal(screen.queryByRole("button", { name: words.settings }), null);
  assert.ok(screen.getByText("Spring Launch"));
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
  assert.ok(await screen.findByText("Current brief", {}, { timeout: 5000 }));
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
  assert.match(revisionPrompt(previous, 3), /file_read path artifact:art-1@1/);
  assert.match(
    revisionPrompt(previous, 3),
    /\/messages\/thread-1\?panel=files&legacy_panel=deliverables&artifact=art-1/,
  );
});

test("Edit a Copy saves the selected exact text version and opens it only after the workspace accepted it", async () => {
  const content = "# Current brief\n\nThe decision is ready.";
  let fail = true;
  let complete: ((value: unknown) => void) | undefined;
  const opened: string[] = [];
  net = stubFetch({
    "GET /api/me": () => ({ permissions: ["runs:read", "runs:write"] }),
    "GET /api/artifacts": () => ({ artifacts: [saved], next: null }),
    "GET /api/artifacts/art-2": () => saved,
    "GET /api/artifacts/art-2/versions": () => ({ versions: [saved] }),
    "GET /api/artifacts/art-2/content": () => new Response(content),
    "PUT /api/threads/thread-1/files/content": (body) => {
      const input = body as { path: string; content: string; revision: string };
      assert.equal(input.content, content);
      assert.equal(input.revision, "");
      assert.match(input.path, /^copies\/[a-f0-9-]{36}\/Launch_brief\.md$/);
      if (fail) return Response.json({ error: "A run is updating these files." }, { status: 409 });
      return new Promise((resolve) => {
        complete = resolve;
      });
    },
  });
  render(
    <Deliverables
      thread="thread-1"
      view={{ artifact: saved.id }}
      onView={() => {}}
      onClose={() => {}}
      onEditCopy={(path) => opened.push(path)}
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: words.artifactEditCopy }));
  assert.ok(await screen.findByText("A run is updating these files."));
  assert.deepEqual(opened, []);
  fail = false;
  fireEvent.click(screen.getByRole("button", { name: words.artifactEditCopy }));
  await waitFor(() => assert.ok(complete));
  assert.equal(
    (screen.getByRole("button", { name: words.artifactCopyBusy }) as HTMLButtonElement).disabled,
    true,
  );
  assert.deepEqual(opened, []);
  complete?.({ revision: "saved-revision" });
  await waitFor(() => assert.equal(opened.length, 1));
  assert.match(opened[0] ?? "", /^copies\//);
  assert.equal(saved.id, "art-2");
  assert.equal(saved.version, 2);
  assert.equal(
    net.seen.some(
      (request) => request.method !== "GET" && request.path.startsWith("/api/artifacts"),
    ),
    false,
  );
});

test("read-only people cannot create editable artifact copies", async () => {
  net = stubFetch({
    "GET /api/me": () => ({ permissions: ["runs:read"] }),
    "GET /api/artifacts": () => ({ artifacts: [saved], next: null }),
    "GET /api/artifacts/art-2": () => saved,
    "GET /api/artifacts/art-2/versions": () => ({ versions: [saved] }),
    "GET /api/artifacts/art-2/content": () => new Response("# Saved brief"),
  });
  render(
    <Deliverables
      thread="thread-1"
      view={{ artifact: saved.id }}
      onView={() => {}}
      onClose={() => {}}
      onEditCopy={() => assert.fail("No copy may be created")}
    />,
  );
  assert.ok(await screen.findByText("Saved brief"));
  assert.equal(screen.queryByRole("button", { name: words.artifactEditCopy }), null);
  assert.equal(
    net.seen.some((request) => request.method === "PUT"),
    false,
  );
});

test("human-produced artifacts show their creator without an agent mark, run, or empty mention", async () => {
  const human = { ...saved, agent: "", run: "", created_by: "ana" };
  net = stubFetch({
    "GET /api/artifacts": () => ({ artifacts: [human], next: null }),
    "GET /api/artifacts/art-2": () => human,
    "GET /api/artifacts/art-2/versions": () => ({ versions: [human] }),
    "GET /api/artifacts/art-2/content": () => new Response("# Human brief"),
  });
  function HumanArtifact() {
    const [view, setView] = useState<ConversationView>({ artifact: human.id });
    return <Deliverables thread="thread-1" view={view} onView={setView} onClose={() => {}} />;
  }
  render(<HumanArtifact />);
  assert.ok(await screen.findByText("Human brief"));
  assert.ok(screen.getByText("ana"));
  fireEvent.click(screen.getByRole("button", { name: words.artifactSources }));
  assert.ok(screen.getByText(words.artifactCreatedBy));
  assert.equal(screen.queryByText("Agent"), null);
  assert.equal(screen.queryByText("Run"), null);
  assert.match(revisionPrompt(human), /^Please revise /);
  assert.doesNotMatch(revisionPrompt(human), /^@/);
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

test("PDF export uses the selected immutable version and a refusal keeps the original available", async () => {
  const native = {
    ...saved,
    title: "Launch brief.docx",
    mime: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  };
  net = stubFetch({
    "GET /api/artifacts": () => ({ artifacts: [native], next: null }),
    "GET /api/artifacts/art-2": () => native,
    "GET /api/artifacts/art-2/versions": () => ({ versions: [native] }),
    "GET /api/artifacts/art-2/content": () => new Response("not a valid Office preview"),
    "GET /api/artifacts/art-2/pdf": () =>
      new Response(JSON.stringify({ error: "This file has no native revision model to export." }), {
        status: 400,
      }),
  });
  render(
    <Deliverables
      thread="thread-1"
      view={{ panel: "deliverables", artifact: "art-2" }}
      onView={() => {}}
      onClose={() => {}}
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: words.artifactPDF }));
  assert.ok(await screen.findByText("This file has no native revision model to export."));
  assert.equal(
    screen.getByRole("link", { name: "Download Original" }).getAttribute("href"),
    "/api/artifacts/art-2/content",
  );
  assert.ok(net.seen.some((request) => request.path === "/api/artifacts/art-2/pdf"));
  assert.ok(!net.seen.some((request) => request.method === "POST"));
});

test("accepted results require whole-file review and a same-project manual indexed source", async () => {
  net = stubFetch({
    "GET /api/knowledge": () => ({
      sources: [
        { name: "accepted", lifecycle: { mode: "indexed", provider: "manual", project: "Launch" } },
        { name: "broader", lifecycle: { mode: "indexed", provider: "manual", project: "" } },
        { name: "live", lifecycle: { mode: "live", provider: "drive", project: "Launch" } },
      ],
    }),
    "POST /api/artifacts/art-2/promote-knowledge": () => ({
      id: "document-1",
      uri: "humagen://artifacts/art-2?version=2",
    }),
  });
  render(
    <ArtifactPromotion
      file={saved}
      access={{ project: "Launch", shared: true, knowledge: true, memory: false }}
    />,
  );
  const source = await screen.findByRole("combobox");
  assert.ok(!screen.queryByRole("option", { name: "broader" }));
  assert.ok(!screen.queryByRole("option", { name: "live" }));
  const submit = screen.getByRole("button", {
    name: words.artifactAddKnowledge,
  }) as HTMLButtonElement;
  assert.equal(submit.disabled, true);
  fireEvent.change(source, { target: { value: "accepted" } });
  assert.equal(submit.disabled, true);
  fireEvent.click(screen.getByRole("checkbox", { name: words.artifactAccept }));
  fireEvent.click(submit);
  await screen.findByText(words.artifactPromoted);
  const posted = net.seen.find((request) => request.method === "POST");
  assert.deepEqual(posted?.body, { version: 2, accepted: true, source: "accepted" });
});

test("private results offer no promotion and query no destination sources", () => {
  net = stubFetch({});
  render(
    <ArtifactPromotion
      file={saved}
      access={{ project: "Launch", shared: false, knowledge: true, memory: true }}
    />,
  );
  assert.ok(screen.getByText(words.artifactSharedRequired));
  assert.equal(screen.queryByRole("button"), null);
  assert.equal(net.seen.length, 0);
});

test("a failed artifact preview can retry its actual content without changing the selected immutable version", async () => {
  let reads = 0;
  net = stubFetch({
    "GET /api/artifacts": () => ({ artifacts: [saved], next: null }),
    "GET /api/artifacts/art-2": () => saved,
    "GET /api/artifacts/art-2/versions": () => ({ versions: [saved] }),
    "GET /api/artifacts/art-2/content": () =>
      ++reads === 1
        ? new Response(null, { status: 503 })
        : new Response("# Retried brief\n\nThe saved file remains intact."),
  });
  render(
    <Deliverables
      thread="thread-1"
      view={{ panel: "deliverables", artifact: saved.id }}
      onView={() => {}}
      onClose={() => {}}
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: defaultCopy.retry }));
  assert.ok(await screen.findByText("Retried brief"));
  assert.equal(reads, 2);
  assert.ok(net.seen.every((request) => request.method === "GET"));
});
