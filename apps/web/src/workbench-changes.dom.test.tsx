import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import {
  createHumanActivityStore,
  type HumanActivityInput,
  type ResponseState,
  type ResponseTool,
} from "@humagen/core";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useState } from "react";
import { stubFetch } from "./dom.ts";
import { recordFileDelete, recordFileSave } from "./fileActivity.ts";
import { WorkbenchChanges } from "./WorkbenchChanges.tsx";
import { WORKBENCH_TABS, type WorkbenchTabProps } from "./workbench.ts";
import {
  changesAvailable,
  cleanPath,
  comparison,
  diffText,
  fileDiff,
  LARGE_FILE_BYTES,
  previousText,
  revisionOf,
  sessionChanges,
  sessionWrites,
} from "./workbenchChanges.ts";

const words = defaultCopy.workbenchChanges;
const thread = "s_changes";
const base = `/api/threads/${thread}`;
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

const call = (id: string, input: Record<string, unknown>, more: Partial<ResponseTool> = {}) =>
  ({
    type: "tool",
    id,
    name: "file_write",
    kind: "tool",
    input,
    depth: 1,
    status: "done",
    content: [],
    ...more,
  }) as ResponseTool;
const response = (agent: string, content: ResponseTool[]): ResponseState => ({
  status: "done",
  agent,
  content,
  tools: Object.fromEntries(content.map((t) => [t.id, t])),
  partial: {},
});

const first = "# Report\n\nRevenue rose.\nCosts fell.\n";
const second = "# Report\n\nRevenue rose 4%.\nCosts fell.\nMargin widened.\n";
const responses = [
  response("writer", [call("c1", { path: "./report.md", content: first })]),
  response("writer", [
    call("c2", { path: "report.md", content: second }),
    call("c3", { path: "notes.txt", content: "one\n" }),
  ]),
];

test("only a call that wrote a file's whole text counts as a write", () => {
  const nested = call(
    "a1",
    {},
    {
      kind: "agent",
      name: "call_agent",
      agent: "analyst",
      content: [call("c4", { path: "data.csv", content: "a,b\n" })],
    },
  );
  const writes = sessionWrites([
    response("lead", [
      call("ok", { path: "a.md", content: "a" }),
      call("failed", { path: "b.md", content: "b" }, { status: "error" }),
      call("refused", { path: "b2.md", content: "b" }, {
        result: { text: "action failed", sources: [], isError: true },
      } as Partial<ResponseTool>),
      call("native", { path: "deck.pptx", content: "{}", format: "native" }),
      call("publish", { path: "a.md", content: "a" }, { name: "publish_artifact" }),
      call("edit", { path: "a.md", old_string: "a", new_string: "b" }, { name: "file_edit" }),
      nested,
    ]),
  ]);
  assert.deepEqual(
    writes.map((w) => [w.path, w.agent]),
    [
      ["a.md", "lead"],
      ["data.csv", "analyst"],
    ],
  );
  assert.equal(cleanPath("./src/a.ts"), "src/a.ts");
  assert.deepEqual(sessionWrites(undefined), []);
});

test("the files a session changed are those its runs recorded, each once, with what is known of each", async () => {
  const changes = sessionChanges(
    {
      session: { isolated: false, files: 2, bytes: 10, retention_days: 7, writable: false },
      more: true,
      runs: [
        {
          run: "r2",
          agent: "writer",
          depth: 0,
          status: "completed",
          created_at: "2026-11-19T12:00:00Z",
          isolated: false,
          mounts: [],
          files: [
            { path: "report.md", size: 60, revision: await revisionOf(second) },
            { path: "old.txt", size: 0, removed: true },
          ],
          conflicts: ["report.md"],
        },
        {
          run: "r1",
          agent: "writer",
          depth: 0,
          status: "completed",
          created_at: "2026-11-19T11:00:00Z",
          isolated: false,
          mounts: [],
          files: [
            { path: "report.md", size: 40, revision: await revisionOf(first) },
            { path: "built.bin", size: 9 },
          ],
        },
      ],
    } as never,
    {
      files: [
        { path: "report.md", size: 60, revision: "x".repeat(64) },
        { path: "built.bin", size: 9, revision: "y".repeat(64) },
        { path: "mine.txt", size: 1, revision: "z".repeat(64) },
      ],
      writable: false,
      reason: "wait for active work to finish",
      retention_days: 7,
    },
    sessionWrites(responses),
  );
  const by = Object.fromEntries(changes.files.map((f) => [f.path, f]));
  // A file only the person made is not one of the session's changes.
  assert.deepEqual(Object.keys(by).sort(), ["built.bin", "notes.txt", "old.txt", "report.md"]);
  assert.equal(by["report.md"]?.state, "changed");
  assert.equal(by["report.md"]?.texts.length, 2);
  assert.equal(by["report.md"]?.recorded.length, 2);
  assert.equal(by["report.md"]?.conflict, true);
  assert.equal(by["built.bin"]?.state, "added");
  assert.equal(by["old.txt"]?.state, "removed");
  // Written by a call, and no longer among the files.
  assert.equal(by["notes.txt"]?.state, "removed");
  assert.equal(changes.files[0]?.at, "2026-11-19T12:00:00Z");
  assert.equal(changes.writable, false);
  assert.equal(changes.reason, "wait for active work to finish");
  assert.equal(changes.partial, true);
});

test("a comparison numbers its lines, folds long unchanged runs and is cut for the agent", () => {
  const file = { texts: sessionWrites(responses).filter((w) => w.path === "report.md") };
  assert.deepEqual(comparison(file, second, "all"), { before: "", after: second });
  assert.deepEqual(comparison(file, second, "last"), { before: first, after: second });
  // Edited by hand since: the last change is from what the session wrote.
  assert.deepEqual(comparison(file, "mine\n", "last"), { before: second, after: "mine\n" });
  assert.equal(previousText(file, "mine\n")?.text, second);
  assert.equal(previousText(file, second)?.text, first);
  assert.equal(comparison({ texts: [] }, "only\n", "last"), undefined);
  assert.deepEqual(comparison(file, undefined, "all"), { before: second, after: "" });

  const long = Array.from({ length: 40 }, (_, i) => `line ${i + 1}`);
  const edited = [...long];
  edited[1] = "line two";
  edited[38] = "line thirty-nine";
  const diff = fileDiff(long.join("\n"), edited.join("\n"), "a.txt");
  assert.equal(diff.added, 2);
  assert.equal(diff.removed, 2);
  const fold = diff.rows.find((r) => r.kind === "fold");
  assert.ok(fold && fold.kind === "fold" && fold.count > 20);
  const removed = diff.rows.find((r) => r.kind === "remove");
  assert.ok(removed && removed.kind === "remove" && removed.oldLine === 2);
  assert.equal(diffText(diff), "-2: line 2\n+2: line two\n-39: line 39\n+39: line thirty-nine");
  const cut = diffText(diff, 30);
  assert.ok(cut.endsWith("(2 more changed lines)"), cut);
  assert.deepEqual(fileDiff("same", "same", "a.txt").rows, []);
});

test("the tab is in the registry, and is offered once the session wrote a file", () => {
  assert.ok(WORKBENCH_TABS.some((t) => t.id === "changes"));
  const session = { thread, runs: [], summary: undefined, canPost: true };
  assert.equal(changesAvailable({ ...session, responses: undefined }), false);
  assert.equal(changesAvailable({ ...session, responses: [] }), false);
  assert.equal(changesAvailable({ ...session, responses }), true);
});

type Net = Parameters<typeof stubFetch>[0];
async function world(extra: Net = {}, more: { writable?: boolean; reason?: string } = {}) {
  const state = { content: second as string | undefined, revision: await revisionOf(second) };
  const recorded = [await revisionOf(first), await revisionOf(second)];
  net = stubFetch({
    [`GET ${base}/sandboxes`]: () => ({
      session: { isolated: false, files: 1, bytes: 60, retention_days: 7, writable: true },
      more: false,
      runs: [
        {
          run: "r2",
          agent: "writer",
          depth: 0,
          status: "completed",
          created_at: new Date(Date.now() - 120_000).toISOString(),
          isolated: false,
          mounts: [],
          files: [{ path: "report.md", size: 60, revision: recorded[1] }],
        },
        {
          run: "r1",
          agent: "writer",
          depth: 0,
          status: "completed",
          created_at: new Date(Date.now() - 600_000).toISOString(),
          isolated: false,
          mounts: [],
          files: [{ path: "report.md", size: 40, revision: recorded[0] }],
        },
      ],
    }),
    [`GET ${base}/files`]: () => ({
      files:
        state.content === undefined
          ? []
          : [{ path: "report.md", size: state.content.length, revision: state.revision }],
      writable: more.writable ?? true,
      ...(more.reason ? { reason: more.reason } : {}),
      retention_days: 7,
    }),
    [`GET ${base}/files/content`]: () => ({
      path: "report.md",
      content: state.content,
      revision: state.revision,
    }),
    [`PUT ${base}/files/content`]: async (body) => {
      const put = body as { path: string; content: string; revision: string };
      assert.equal(put.revision, state.revision);
      state.content = put.content;
      state.revision = await revisionOf(put.content);
      return { path: put.path, content: put.content, revision: state.revision };
    },
    [`DELETE ${base}/files/content`]: (_body, query) => {
      assert.equal(query.get("revision"), state.revision);
      state.content = undefined;
      return undefined;
    },
    ...extra,
  });
  return { net, state };
}

function Tab({
  recorded,
  initial,
  writes = responses,
}: {
  recorded: HumanActivityInput[];
  initial?: string;
  writes?: ResponseState[];
}) {
  const [selection, setSelection] = useState(initial);
  const props: WorkbenchTabProps = {
    thread,
    runs: [],
    responses: writes,
    summary: undefined,
    canPost: true,
    selection,
    onSelection: setSelection,
    openTab: () => {},
    record: (input) => {
      recorded.push(input);
      return undefined;
    },
    expanded: false,
  };
  return (
    <>
      <output data-testid="selection">{selection ?? ""}</output>
      <WorkbenchChanges {...props} />
    </>
  );
}

test("a changed file opens at its address and shows added and removed lines with their numbers", async () => {
  await world();
  const recorded: HumanActivityInput[] = [];
  render(<Tab recorded={recorded} />);
  const row = await screen.findByRole("button", { name: /report\.md/ });
  assert.match(row.textContent ?? "", new RegExp(words.state.changed));
  assert.match(row.textContent ?? "", /by writer/);
  assert.equal(row.getAttribute("aria-expanded"), "false");
  fireEvent.click(row);
  assert.equal(screen.getByTestId("selection").textContent, "report.md");
  const diff = await screen.findByTestId("changes-diff");
  assert.equal(diff.getAttribute("aria-label"), words.diffFor("report.md"));
  // The last change: one line replaced, one added.
  const lines = [...diff.querySelectorAll("tr[data-line]")].map((tr) => [
    tr.getAttribute("data-line"),
    ...[...tr.querySelectorAll("td")].map((td) => td.textContent),
  ]);
  assert.deepEqual(
    lines.filter((l) => l[0] !== "context"),
    [
      ["remove", "3", "", `−${words.lineRemoved}`, "Revenue rose."],
      ["add", "", "3", `+${words.lineAdded}`, "Revenue rose 4%."],
      ["add", "", "5", `+${words.lineAdded}`, "Margin widened."],
    ],
  );
  assert.ok(screen.getByText(words.linesAdded(2)));
  assert.ok(screen.getByText(words.linesRemoved(1)));
  // The whole file, against nothing known before it.
  fireEvent.click(screen.getByRole("button", { name: words.compareAll }));
  await waitFor(() =>
    assert.equal(
      screen.getByTestId("changes-diff").querySelectorAll('[data-line="add"]').length,
      5,
    ),
  );
  assert.ok(screen.getByText(words.noEarlier));
  // Editing is the Workspace pane's.
  const edit = screen.getByRole("link", { name: words.edit });
  assert.equal(
    edit.getAttribute("href"),
    // The pane's tabs are written encoded, then as a query (route.ts).
    `/messages/${thread}?${new URLSearchParams({ panel: "files", tabs: encodeURIComponent("file:report.md") })}`,
  );
  assert.equal(recorded.length, 0);
  // Pressing the row again closes it.
  fireEvent.click(row);
  assert.equal(screen.getByTestId("selection").textContent, "");
});

test("putting a file back writes its earlier text at the revision read, and is recorded", async () => {
  const { net: seen, state } = await world();
  const recorded: HumanActivityInput[] = [];
  render(<Tab recorded={recorded} initial="report.md" />);
  fireEvent.click(await screen.findByRole("button", { name: words.restore }));
  const sure = screen.getByRole("group", { name: words.confirmRestore("report.md") });
  // Keep It has focus, and nothing is written until the action is pressed.
  assert.equal(document.activeElement?.textContent, words.keep);
  assert.ok(!seen.seen.some((r) => r.method === "PUT"));
  fireEvent.click(within(sure).getByRole("button", { name: words.restore }));
  await waitFor(() => assert.equal(state.content, first));
  await waitFor(() => assert.equal(recorded.length, 1));
  assert.equal(recorded[0]?.source, "changes");
  assert.equal(recorded[0]?.summary, words.recordRestored("report.md", 1, 2));
  assert.match(
    recorded[0]?.detail ?? "",
    /^-3: Revenue rose 4%\.\n\+3: Revenue rose\.\n-5: Margin widened\.$/,
  );
  assert.equal(recorded[0]?.key, "discard:report.md");
  await screen.findByText(words.restored);
});

test("deleting a file asks first, deletes at the revision read, and is recorded", async () => {
  const { state } = await world();
  const recorded: HumanActivityInput[] = [];
  render(<Tab recorded={recorded} initial="report.md" />);
  fireEvent.click(await screen.findByRole("button", { name: words.remove }));
  const sure = screen.getByRole("group", { name: words.confirmRemove("report.md") });
  fireEvent.keyDown(sure, { key: "Escape" });
  assert.equal(state.content, second);
  fireEvent.click(screen.getByRole("button", { name: words.remove }));
  fireEvent.click(
    within(screen.getByRole("group", { name: words.confirmRemove("report.md") })).getByRole(
      "button",
      { name: words.remove },
    ),
  );
  await waitFor(() => assert.equal(state.content, undefined));
  await waitFor(() => assert.equal(recorded.length, 1));
  assert.equal(recorded[0]?.summary, words.recordRemoved("report.md", 5));
  // The file is listed as removed, with the text the session last wrote.
  await waitFor(() =>
    assert.match(
      screen.getByRole("button", { name: /report\.md/ }).textContent ?? "",
      new RegExp(words.state.removed),
    ),
  );
  // The agent did not remove it, so the row does not say it did.
  assert.doesNotMatch(
    screen.getByRole("button", { name: /report\.md/ }).textContent ?? "",
    /by writer/,
  );
  await waitFor(() =>
    assert.equal(
      screen.getByTestId("changes-diff").querySelectorAll('[data-line="remove"]').length,
      5,
    ),
  );
  assert.ok(!screen.queryByRole("button", { name: words.remove }));
});

test("a refused write says why and changes nothing that is recorded", async () => {
  await world({
    [`PUT ${base}/files/content`]: () =>
      new Response(JSON.stringify({ error: "the file changed; refresh before saving" }), {
        status: 409,
      }),
  });
  const recorded: HumanActivityInput[] = [];
  render(<Tab recorded={recorded} initial="report.md" />);
  fireEvent.click(await screen.findByRole("button", { name: words.restore }));
  fireEvent.click(
    within(screen.getByRole("group", { name: words.confirmRestore("report.md") })).getByRole(
      "button",
      { name: words.restore },
    ),
  );
  assert.equal(
    (await screen.findByRole("alert")).textContent,
    "the file changed; refresh before saving",
  );
  assert.equal(recorded.length, 0);
});

test("files that cannot be changed now say why and offer no way to change them", async () => {
  await world({}, { writable: false, reason: "wait for active work to finish" });
  render(<Tab recorded={[]} initial="report.md" />);
  await screen.findByTestId("changes-diff");
  assert.ok(screen.getByText("wait for active work to finish"));
  assert.ok(!screen.queryByRole("button", { name: words.restore }));
  assert.ok(!screen.queryByRole("button", { name: words.remove }));
  assert.ok(screen.getByRole("link", { name: words.edit }));
});

test("a text a run did not record leaving is compared but never written back", async () => {
  // Two writes inside one run: only the second has a recorded hash.
  await world();
  const twice = [
    response("writer", [
      call("c1", { path: "report.md", content: "draft\n" }),
      call("c2", { path: "report.md", content: second }),
    ]),
  ];
  render(<Tab recorded={[]} initial="report.md" writes={twice} />);
  await screen.findByTestId("changes-diff");
  assert.ok(screen.getByRole("button", { name: words.remove }));
  await new Promise((done) => setTimeout(done, 20));
  assert.ok(!screen.queryByRole("button", { name: words.restore }));
});

test("a binary file and a large file say so instead of being compared", async () => {
  const { net: seen } = await world({
    [`GET ${base}/files`]: () => ({
      files: [{ path: "report.md", size: LARGE_FILE_BYTES + 1, revision: "r".repeat(64) }],
      writable: true,
      retention_days: 7,
    }),
    [`GET ${base}/files/content`]: () =>
      new Response(
        JSON.stringify({ error: "sandbox: this binary file cannot be edited as text" }),
        { status: 400 },
      ),
  });
  render(<Tab recorded={[]} initial="report.md" />);
  await screen.findByText(words.large("256 KB"));
  // Nothing was read until it is asked for.
  assert.ok(!seen.seen.some((r) => r.path.includes("/files/content")));
  fireEvent.click(screen.getByRole("button", { name: words.compareAnyway }));
  await screen.findByText(words.binary("256 KB"));
  assert.ok(!screen.queryByTestId("changes-diff"));
  assert.ok(!screen.queryByRole("link", { name: words.edit }));
});

test("a session that changed nothing says so, with one way on", async () => {
  net = stubFetch({
    [`GET ${base}/sandboxes`]: () => ({
      session: { isolated: false, files: 0, bytes: 0, retention_days: 7, writable: true },
      more: false,
      runs: [],
    }),
    [`GET ${base}/files`]: () => ({ files: [], writable: true, retention_days: 7 }),
  });
  render(<Tab recorded={[]} writes={[]} />);
  const empty = await screen.findByTestId("changes-empty");
  assert.match(empty.textContent ?? "", new RegExp(words.empty));
  assert.equal(
    within(empty).getByRole("link", { name: words.openFiles }).getAttribute("href"),
    `/messages/${thread}?panel=files&tabs=files`,
  );
});

test("a list that cannot be read says so and can be tried again", async () => {
  let fail = true;
  net = stubFetch({
    [`GET ${base}/sandboxes`]: () =>
      fail
        ? new Response(JSON.stringify({ error: "not now" }), { status: 500 })
        : { session: {}, more: false, runs: [] },
    [`GET ${base}/files`]: () => ({ files: [], writable: true, retention_days: 7 }),
  });
  render(<Tab recorded={[]} writes={[]} />);
  assert.equal((await screen.findByRole("alert")).textContent, "not now");
  fail = false;
  fireEvent.click(screen.getByRole("button", { name: words.retry }));
  await screen.findByTestId("changes-empty");
});

test("saving one file over and over is one record, against the text before the first save", () => {
  const store = createHumanActivityStore();
  const c = "s_files";
  recordFileSave(c, "a.md", "one\n", "one\ntwo\n", store);
  recordFileSave(c, "a.md", "one\ntwo\n", "one\ntwo\nthree\n", store);
  let records = store.list(c);
  assert.equal(records.length, 1);
  assert.equal(records[0]?.source, "files");
  assert.equal(records[0]?.summary, words.recordEdited("a.md", 2, 0));
  assert.equal(records[0]?.detail, "+2: two\n+3: three");
  // Saved back to what it was: nothing was done.
  recordFileSave(c, "a.md", "one\ntwo\nthree\n", "one\n", store);
  assert.equal(store.list(c).length, 0);
  // Once sent, the next save starts from the text it found.
  recordFileSave(c, "a.md", "one\n", "one\ntwo\n", store);
  store.clear(c);
  recordFileSave(c, "a.md", "one\ntwo\n", "one\n2\n", store);
  records = store.list(c);
  assert.equal(records[0]?.summary, words.recordEdited("a.md", 1, 1));
  // A file made and deleted between two messages was never there.
  recordFileSave(c, "new.md", undefined, "x\ny\n", store);
  assert.equal(store.list(c).at(-1)?.summary, words.recordCreated("new.md", 2));
  recordFileDelete(c, "new.md", store);
  assert.ok(!store.list(c).some((r) => r.summary.includes("new.md")));
  // Deleting a file that was there is recorded, in place of its edits.
  recordFileDelete(c, "a.md", store);
  assert.deepEqual(
    store.list(c).map((r) => r.summary),
    [words.recordDeleted("a.md")],
  );
});
