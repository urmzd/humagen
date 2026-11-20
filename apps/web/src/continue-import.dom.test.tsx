import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ApiError, type Doc, type Source, type Thread } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { ImportContext } from "./ForkConversation.tsx";
import { continueRefusal, Knowledge } from "./Knowledge.tsx";

// Continue Conversation: the document row's menu on a source's page, the
// request it sends, each refusal, and what the new conversation says of its
// origin. Every record here is generated.

const words = defaultCopy.continueImport;
const at = "2026-11-20T10:00:00Z";
const digest = "a".repeat(64);
const structure = "b".repeat(64);

const source = (over: Partial<Source> = {}): Source => ({
  name: "sessions-chatgpt-2026-11",
  description: "Generated sessions",
  documents: 3,
  passages: 9,
  excluded: 0,
  variants: [],
  updated_at: at,
  ...over,
});

const doc = (over: Partial<Doc>): Doc => ({
  id: "d-1",
  title: "Generated plan",
  uri: "chatgpt:generated-1",
  bytes: 4096,
  passages: 3,
  excluded: false,
  added_at: at,
  digest,
  structure_bytes: 900,
  structure_digest: structure,
  ...over,
});

const docs: Doc[] = [
  doc({}),
  doc({
    id: "d-2",
    title: "Earlier release import",
    uri: "claude-ai:generated-2",
    structure_digest: undefined,
  }),
  doc({
    id: "d-3",
    title: "Meeting notes",
    uri: "https://example.com/notes",
    structure_bytes: undefined,
    structure_digest: undefined,
  }),
  doc({ id: "d-4", title: "Undigested session", uri: "codex:generated-4", digest: undefined }),
];

const thread = (over: Partial<Thread> = {}): Thread => ({
  id: "th-0123456789abcdef",
  project: "research",
  channel: "general",
  title: "Generated plan",
  visibility: "private",
  participants: [{ kind: "person", name: "dev", owner: true, at }],
  version: 1,
  created_by: "dev",
  created_at: at,
  updated_at: at,
  ...over,
});

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

function sourcePage(
  routes: Parameters<typeof stubFetch>[0],
  props: { canRun?: boolean; source?: Source; opened?: (id: string) => void } = {},
) {
  net = stubFetch({
    "GET /api/knowledge/sessions-chatgpt-2026-11/documents": () => ({ documents: docs }),
    "GET /api/knowledge/sessions-chatgpt-2026-11/attachments": () => ({
      attachments: [{ source: "sessions-chatgpt-2026-11", project: "research" }],
    }),
    ...routes,
  });
  render(
    <Knowledge
      sources={[props.source ?? source()]}
      agents={[]}
      suites={[]}
      selected="sessions-chatgpt-2026-11"
      onSelect={() => {}}
      canWrite={false}
      canEval={false}
      canRun={props.canRun ?? true}
      onChanged={() => {}}
      onSuitesChanged={() => {}}
      onOpenThread={props.opened ?? (() => {})}
    />,
  );
}

async function choose(title: string, item: string = words.action) {
  fireEvent.click(await screen.findByRole("button", { name: words.menu(title) }));
  const row = await screen.findByRole("menuitem", { name: item });
  await waitFor(() => assert.equal((row as HTMLButtonElement).disabled, false));
  fireEvent.click(row);
}

const refuse = (status: number, error: string) => () =>
  new Response(JSON.stringify({ error }), { status });

test("the row menu is offered only for an imported session's document with a digest", async () => {
  sourcePage({});
  await screen.findByText("Generated plan");
  assert.ok(screen.getByRole("button", { name: words.menu("Generated plan") }));
  assert.ok(screen.getByRole("button", { name: words.menu("Earlier release import") }));
  // A plain document, and a session's kept before digests were, offer nothing.
  assert.equal(screen.queryByRole("button", { name: words.menu("Meeting notes") }), null);
  assert.equal(screen.queryByRole("button", { name: words.menu("Undigested session") }), null);
});

test("a reader who may not start runs is not offered it", async () => {
  sourcePage({}, { canRun: false });
  await screen.findByText("Generated plan");
  assert.equal(screen.queryByRole("button", { name: words.menu("Generated plan") }), null);
});

test("the menu row is reached from the keyboard", async () => {
  sourcePage({});
  const more = await screen.findByRole("button", { name: words.menu("Generated plan") });
  fireEvent.keyDown(more, { key: "ArrowDown" });
  const row = await screen.findByRole("menuitem", { name: words.action });
  await waitFor(() => assert.equal(document.activeElement, row));
});

test("continuing sends the listed digests and the project, then opens the conversation", async () => {
  let opened = "";
  sourcePage(
    {
      "POST /api/knowledge/sessions-chatgpt-2026-11/documents/d-1/continue": () =>
        thread({ id: "th-1111111111111111" }),
    },
    { opened: (id) => (opened = id) },
  );
  await choose("Generated plan");
  await waitFor(() => assert.equal(opened, "th-1111111111111111"));
  assert.deepEqual(net?.seen.find((r) => r.method === "POST")?.body, {
    document_digest: digest,
    project: "research",
    structure_digest: structure,
  });
});

test("a document whose listing records no structure digest is continued without one", async () => {
  sourcePage({
    "POST /api/knowledge/sessions-chatgpt-2026-11/documents/d-2/continue": () => thread(),
  });
  await choose("Earlier release import");
  await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "POST")));
  const body = net?.seen.find((r) => r.method === "POST")?.body as Record<string, unknown>;
  assert.deepEqual(body, { document_digest: digest, project: "research" });
  assert.equal("structure_digest" in body, false);
});

test("a source filed nowhere is continued in the workspace, and a scoped one in its project", async () => {
  sourcePage({
    "GET /api/knowledge/sessions-chatgpt-2026-11/attachments": () => ({ attachments: [] }),
    "POST /api/knowledge/sessions-chatgpt-2026-11/documents/d-1/continue": () => thread(),
  });
  await choose("Generated plan");
  await waitFor(() =>
    assert.equal(
      (net?.seen.find((r) => r.method === "POST")?.body as { project: string }).project,
      "~",
    ),
  );
  cleanup();
  net?.restore();
  sourcePage(
    { "POST /api/knowledge/sessions-chatgpt-2026-11/documents/d-1/continue": () => thread() },
    {
      source: source({
        lifecycle: {
          provider: "manual",
          mode: "indexed",
          project: "field",
          status: "available",
          connected_at: at,
          stop_after_seconds: 0,
          attempts: [],
        },
      }),
    },
  );
  await choose("Generated plan");
  await waitFor(() =>
    assert.equal(
      (net?.seen.find((r) => r.method === "POST")?.body as { project: string }).project,
      "field",
    ),
  );
});

test("a source attached to two projects offers one row for each", async () => {
  sourcePage({
    "GET /api/knowledge/sessions-chatgpt-2026-11/attachments": () => ({
      attachments: [
        { source: "sessions-chatgpt-2026-11", project: "research" },
        { source: "sessions-chatgpt-2026-11", project: "research", channel: "notes" },
        { source: "sessions-chatgpt-2026-11", project: "field" },
      ],
    }),
    "POST /api/knowledge/sessions-chatgpt-2026-11/documents/d-1/continue": () => thread(),
  });
  fireEvent.click(await screen.findByRole("button", { name: words.menu("Generated plan") }));
  const rows = await screen.findAllByRole("menuitem");
  assert.deepEqual(
    rows.map((r) => r.textContent),
    [words.actionIn("research"), words.actionIn("field")],
  );
  fireEvent.click(rows[1] as HTMLElement);
  await waitFor(() =>
    assert.equal(
      (net?.seen.find((r) => r.method === "POST")?.body as { project: string }).project,
      "field",
    ),
  );
});

test("while it starts, the page says so and the row cannot be chosen again", async () => {
  let answer: (value: unknown) => void = () => {};
  sourcePage({
    "POST /api/knowledge/sessions-chatgpt-2026-11/documents/d-1/continue": () =>
      new Promise((resolve) => {
        answer = resolve;
      }),
  });
  await choose("Generated plan");
  const status = await screen.findByText(words.starting("Generated plan"));
  assert.equal(status.tagName, "OUTPUT");
  fireEvent.click(screen.getByRole("button", { name: words.menu("Earlier release import") }));
  const row = await screen.findByRole("menuitem", { name: words.action });
  assert.equal((row as HTMLButtonElement).disabled, true);
  answer(thread());
  // The answer is read before the page is looked at again.
  await new Promise((r) => setTimeout(r, 0));
  await waitFor(() => assert.equal(screen.queryByText(words.starting("Generated plan")), null));
});

test("an excluded document's row is shown but cannot be chosen", async () => {
  net = stubFetch({
    "GET /api/knowledge/sessions-chatgpt-2026-11/documents": () => ({
      documents: [doc({ excluded: true })],
    }),
    "GET /api/knowledge/sessions-chatgpt-2026-11/attachments": () => ({ attachments: [] }),
  });
  render(
    <Knowledge
      sources={[source()]}
      agents={[]}
      suites={[]}
      selected="sessions-chatgpt-2026-11"
      onSelect={() => {}}
      canWrite={false}
      canEval={false}
      canRun
      onChanged={() => {}}
      onSuitesChanged={() => {}}
      onOpenThread={() => {}}
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: words.menu("Generated plan") }));
  const row = await screen.findByRole("menuitem", { name: words.action });
  await new Promise((r) => setTimeout(r, 20));
  assert.equal((row as HTMLButtonElement).disabled, true);
});

test("a 409 shows the server's words as given and offers to read the documents again", async () => {
  let lists = 0;
  let attachments = 0;
  const said = "the imported document changed since it was read; read it again";
  sourcePage({
    "GET /api/knowledge/sessions-chatgpt-2026-11/documents": () => {
      lists += 1;
      return { documents: docs };
    },
    "GET /api/knowledge/sessions-chatgpt-2026-11/attachments": () => {
      attachments += 1;
      return { attachments: [{ source: "sessions-chatgpt-2026-11", project: "research" }] };
    },
    "POST /api/knowledge/sessions-chatgpt-2026-11/documents/d-1/continue": refuse(409, said),
  });
  await choose("Generated plan");
  const alert = await screen.findByRole("alert");
  assert.equal(alert.textContent, said);
  const before = { lists, attachments };
  fireEvent.click(screen.getByRole("button", { name: words.reload }));
  // Both what the documents are and where the source is filed are read again.
  await waitFor(() => assert.ok(lists > before.lists && attachments > before.attachments));
  assert.equal(screen.queryByRole("alert"), null);
});

test("refusals are mapped by status only, never by the server's wording", () => {
  const cases: [number, string, string, boolean][] = [
    // One status, several causes: the server's sentence as given, and a reload for a 409.
    [
      409,
      "the imported document changed since it was read; read it again",
      "the imported document changed since it was read; read it again",
      true,
    ],
    [
      409,
      "the source is no longer filed in that project",
      "the source is no longer filed in that project",
      true,
    ],
    [
      400,
      "the imported conversation's structure cannot be trusted",
      "the imported conversation's structure cannot be trusted",
      false,
    ],
    // A status with one meaning has the console's words, whatever the server said.
    [403, "you need to be a member of research", words.forbidden, false],
    [403, "you need runs:write", words.forbidden, false],
    [404, "not found", words.gone, true],
    [413, "request is too large", words.tooLarge, false],
  ];
  for (const [status, said, message, reload] of cases) {
    const refusal = continueRefusal(new ApiError(status, said));
    assert.equal(refusal.message, message, `${status} ${said}`);
    assert.equal(!!refusal.reload, reload, `${status} ${said}`);
  }
  // Wording the console has never seen changes nothing: still the server's text, still the action.
  const unknown409 = continueRefusal(new ApiError(409, "a wording no release has used yet"));
  assert.deepEqual(unknown409, { message: "a wording no release has used yet", reload: true });
  const unknown400 = continueRefusal(new ApiError(400, "another new sentence"));
  assert.deepEqual(unknown400, { message: "another new sentence" });
  // With no sentence, or no answer at all, the console's own fallback.
  assert.equal(continueRefusal(new ApiError(409, "")).message, words.failed);
  assert.equal(continueRefusal(new Error("offline")).message, words.failed);
});

test("an unknown 409 or 400 sentence is shown as given, with the reload only for the 409", async () => {
  for (const [status, said, reload] of [
    [409, "something new changed upstream", true],
    [400, "something new is wrong with it", false],
  ] as const) {
    sourcePage({
      "POST /api/knowledge/sessions-chatgpt-2026-11/documents/d-1/continue": refuse(status, said),
    });
    await choose("Generated plan");
    assert.equal((await screen.findByRole("alert")).textContent, said);
    assert.equal(!!screen.queryByRole("button", { name: words.reload }), reload, String(status));
    cleanup();
    net?.restore();
    net = undefined;
  }
});

const continued = (over: Partial<NonNullable<Thread["import"]>> = {}): Thread =>
  thread({
    import: {
      source: "sessions-chatgpt-2026-11",
      source_id: "src-1",
      document: "d-1",
      uri: "chatgpt:generated-1",
      title: "Generated plan",
      structure: "kept",
      tool: "chatgpt",
      document_digest: digest,
      structure_digest: structure,
      started_at: "2026-11-01T09:00:00Z",
      ended_at: "2026-11-02T17:30:00Z",
      by: "dev",
      at,
      context: [
        { speaker: "user", at: "2026-11-02T17:00:00Z", text: "What is the generated plan?" },
        { speaker: "assistant", at: "2026-11-02T17:01:00Z", text: "Three steps.", cut: true },
      ],
      omitted: { turns: 70, results: 8, cut: 1 },
      branches: 3,
      import_cut: { turns: 0, results: 0, cut_turns: 0, attachments: 2 },
      ...over,
    },
  });

test("the conversation names its origin with references and one row of facts", async () => {
  net = stubFetch({
    "GET /api/knowledge/sessions-chatgpt-2026-11/documents": () => ({ documents: docs }),
  });
  render(<ImportContext thread={continued()} />);
  const section = screen.getByRole("region", { name: words.heading("ChatGPT") });
  const links = within(section).getAllByRole("link");
  assert.deepEqual(
    links.map((a) => [a.textContent, a.getAttribute("href")]),
    [
      ["sessions-chatgpt-2026-11", "/connections/sources/sessions-chatgpt-2026-11"],
      ["Generated plan", "/connections/sources/sessions-chatgpt-2026-11"],
    ],
  );
  const facts = within(section).getByRole("list", { name: words.facts });
  assert.deepEqual(
    within(facts)
      .getAllByRole("listitem")
      .map((li) => li.textContent),
    [words.sent(2, 80), words.mainPath, words.shortened(1), words.branches(3), words.files(2)],
  );
  // When it ran in its tool, and the pinned revision.
  const times = section.querySelectorAll("time");
  assert.ok([...times].some((t) => t.getAttribute("datetime") === "2026-11-01T09:00:00Z"));
  assert.ok([...times].some((t) => t.getAttribute("datetime") === "2026-11-02T17:30:00Z"));
  assert.ok(within(section).getByText(words.revision(digest.slice(0, 12))));
  // Each turn is named as imported from the tool, never as a speaker here.
  assert.ok(within(section).getByText(words.speaker(words.speakers.user, "ChatGPT")));
  assert.ok(within(section).getByText(words.speaker(words.speakers.assistant, "ChatGPT")));
  assert.ok(within(section).getByText(words.turnCut));
  // The document is as it was pinned, so nothing is said of a change.
  await waitFor(() => assert.ok(net?.seen.some((r) => r.path.endsWith("/documents"))));
  await new Promise((r) => setTimeout(r, 10));
  assert.equal(within(section).queryByText(words.changedSince), null);
});

test("a document changed in its source since is said plainly", async () => {
  net = stubFetch({
    "GET /api/knowledge/sessions-chatgpt-2026-11/documents": () => ({
      documents: [doc({ digest: "c".repeat(64) })],
    }),
  });
  render(<ImportContext thread={continued()} />);
  assert.ok(await screen.findByText(words.changedSince));
});

test("a document removed from its source since is said plainly", async () => {
  net = stubFetch({
    "GET /api/knowledge/sessions-chatgpt-2026-11/documents": () => ({ documents: [] }),
  });
  render(<ImportContext thread={continued()} />);
  assert.ok(await screen.findByText(words.goneSince));
});

test("a document with no kept structure says what is not known", async () => {
  net = stubFetch({
    "GET /api/knowledge/sessions-chatgpt-2026-11/documents": () => ({ documents: docs }),
  });
  render(
    <ImportContext
      thread={continued({
        structure: "not_kept",
        tool: undefined,
        structure_digest: undefined,
        branches: undefined,
        import_cut: undefined,
        context: [{ speaker: "document", text: "The whole generated text." }],
        omitted: { turns: 0, results: 0, cut: 0 },
      })}
    />,
  );
  const section = screen.getByRole("region", { name: words.headingUnknown });
  const facts = within(section).getByRole("list", { name: words.facts });
  assert.deepEqual(
    within(facts)
      .getAllByRole("listitem")
      .map((li) => li.textContent),
    [words.sent(1, 1), words.oneBlock, words.speakersUnknown, words.importUnknown],
  );
  assert.ok(within(section).getByText(words.speakers.document));
});

test("a continuation just opened takes focus at its origin", async () => {
  net = stubFetch({
    "GET /api/knowledge/sessions-chatgpt-2026-11/documents": () => ({ documents: docs }),
  });
  render(<ImportContext thread={continued()} autoFocus />);
  const heading = screen.getByRole("heading", { name: words.heading("ChatGPT") });
  await waitFor(() => assert.equal(document.activeElement, heading));
});

test("a thread that continues nothing shows no origin", () => {
  const { container } = render(<ImportContext thread={thread()} />);
  assert.equal(container.textContent, "");
});
