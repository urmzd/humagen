import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { stubFetch } from "./dom.ts";
import { MemoryFiles } from "./MemoryFiles.tsx";
import { MemoryLibrary } from "./MemoryLibrary.tsx";

const copy = defaultCopy.memoryFiles;
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
  window.history.replaceState(null, "", "/");
});

const at = "2026-11-01T10:00:00Z";
const mine = { kind: "person", person: "ada" };
const tree = {
  tree: mine,
  mount: "/me/memory",
  version: 3,
  by: "ada",
  at,
  entries: [
    { path: "notes/m-1.md", hash: "a1", size: 24 },
    { path: "notes/m-2.md", hash: "b2", size: 31 },
    { path: "pictures/chart.png", hash: "c3", size: 2048 },
  ],
  proposals: [],
};
const versions = [
  { tree: mine, version: 3, parent: 2, entries: null, by: "ada", at, message: "Added a note" },
  { tree: mine, version: 2, parent: 1, entries: null, by: "ada", at },
  { tree: mine, version: 1, entries: null, by: "ada", at },
];
const first = "Releases go out on Tuesdays.";
const second = "Ada prefers short answers.";
const text: Record<string, string> = { "notes/m-1.md": first, "notes/m-2.md": second };
/** The read routes every test starts from. */
const reads = {
  "GET /api/memory/files": () => tree,
  "GET /api/memory/files/history": () => ({ versions }),
  "GET /api/memory/files/content": (_: unknown, query: URLSearchParams) => {
    const path = query.get("file") ?? "";
    const entry = tree.entries.find((e) => e.path === path);
    return path.endsWith(".png")
      ? { ...entry, version: 0, content: "", binary: true }
      : { ...entry, version: Number(query.get("version") ?? 0), content: text[path] };
  },
};

test("the tree lists its files and opening one shows what it says", async () => {
  net = stubFetch(reads);
  const opened: string[] = [];
  render(<MemoryFiles scope="me" onFile={(path) => opened.push(path)} />);
  // The first file is open to begin with.
  await screen.findByText(first);
  for (const entry of tree.entries) assert.ok(screen.getByTitle(entry.path));
  assert.ok(net.seen.some((r) => r.path === "/api/memory/files?scope=me"));
  fireEvent.click(screen.getByTitle("notes/m-2.md"));
  await screen.findByText(second);
  assert.deepEqual(opened, ["notes/m-2.md"]);
  assert.ok(
    net.seen.some((r) => r.path === "/api/memory/files/content?scope=me&file=notes%2Fm-2.md"),
  );
});

test("a file that is not text says it cannot be shown", async () => {
  net = stubFetch(reads);
  render(<MemoryFiles scope="me" file="pictures/chart.png" />);
  await screen.findByText(copy.binary);
});

test("a file too large to show is never fetched", async () => {
  net = stubFetch({
    ...reads,
    "GET /api/memory/files": () => ({
      ...tree,
      entries: [{ path: "notes/big.md", hash: "d4", size: 4 * 1024 * 1024 }],
    }),
  });
  render(<MemoryFiles scope="me" />);
  await screen.findByText(copy.tooLarge("4.0 MB"));
  assert.ok(!net.seen.some((r) => r.path.startsWith("/api/memory/files/content")));
});

test("an empty tree and a tree that cannot be read each say so", async () => {
  net = stubFetch({ ...reads, "GET /api/memory/files": () => ({ ...tree, entries: [] }) });
  render(<MemoryFiles scope="me" />);
  await screen.findByText(copy.empty);
  cleanup();
  net.restore();
  net = stubFetch({});
  render(<MemoryFiles scope="me" />);
  await screen.findByText(copy.loadFailed);
  assert.ok(screen.getByRole("button", { name: copy.retry }));
});

test("an earlier version is read as it was", async () => {
  net = stubFetch(reads);
  render(<MemoryFiles scope="project" project="Launch" />);
  fireEvent.click(await screen.findByRole("button", { name: `${copy.view} ${copy.version(2)}` }));
  await screen.findByText(copy.viewing(2));
  await waitFor(() =>
    assert.ok(
      net?.seen.some(
        (r) =>
          r.path ===
          "/api/memory/files/content?scope=project&project=Launch&file=notes%2Fm-1.md&version=2",
      ),
    ),
  );
  fireEvent.click(screen.getByRole("button", { name: copy.backToNewest }));
  await waitFor(() => assert.ok(!screen.queryByText(copy.viewing(2))));
});

test("restore asks first and then posts the tree and the version", async () => {
  net = stubFetch({
    ...reads,
    "POST /api/memory/files/restore": () => ({ ...tree, version: 4 }),
  });
  render(<MemoryFiles scope="agent" agent="helper" />);
  fireEvent.click(
    await screen.findByRole("button", { name: `${copy.restore} ${copy.version(2)}` }),
  );
  // Nothing is sent until the person agrees to the whole tree going back.
  assert.ok(screen.getByText(copy.restoreAsk(2)));
  assert.ok(!net.seen.some((r) => r.method === "POST"));
  fireEvent.click(screen.getByRole("button", { name: copy.restore }));
  await screen.findByText(copy.restored(2));
  assert.deepEqual(
    net.seen.filter((r) => r.method === "POST").map((r) => r.body),
    [{ scope: "agent", agent: "helper", version: 2 }],
  );
});

test("keeping it sends nothing", async () => {
  net = stubFetch(reads);
  render(<MemoryFiles scope="me" />);
  fireEvent.click(
    await screen.findByRole("button", { name: `${copy.restore} ${copy.version(1)}` }),
  );
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.confirm.keep }));
  assert.ok(!screen.queryByText(copy.restoreAsk(1)));
  assert.ok(!net.seen.some((r) => r.method === "POST"));
});

test("a refused restore says the person may not and leaves the view as it was", async () => {
  net = stubFetch({
    ...reads,
    "POST /api/memory/files/restore": () =>
      new Response(JSON.stringify({ error: "you may not change this memory" }), { status: 403 }),
  });
  render(<MemoryFiles scope="project" project="Launch" />);
  fireEvent.click(
    await screen.findByRole("button", { name: `${copy.restore} ${copy.version(2)}` }),
  );
  fireEvent.click(screen.getByRole("button", { name: copy.restore }));
  await screen.findByText(defaultCopy.noPermission);
  assert.ok(screen.getByText(first));
  assert.ok(screen.getByText(copy.newest));
  assert.ok(!screen.queryByText(copy.restored(2)));
});

test("a proposal is approved or discarded with its tree", async () => {
  const proposal = {
    id: "p-1",
    tree: { kind: "project", project: "Launch" },
    base: 3,
    changes: [
      { path: "notes/m-9.md", hash: "e5", size: 12 },
      { path: "notes/m-1.md", delete: true },
    ],
    by: "helper",
    at,
    message: "Learned from the launch review",
  };
  let open = [proposal];
  net = stubFetch({
    ...reads,
    "GET /api/memory/files": () => ({ ...tree, proposals: open }),
    "POST /api/memory/files/proposals/p-1": () => {
      open = [];
      return { ...tree, version: 4, proposals: [] };
    },
  });
  render(<MemoryFiles scope="project" project="Launch" />);
  await screen.findByText(proposal.message);
  assert.ok(screen.getByText(`${copy.writes} notes/m-9.md`));
  assert.ok(screen.getByText(`${copy.removes} notes/m-1.md`));
  fireEvent.click(screen.getByRole("button", { name: copy.approve }));
  await waitFor(() => assert.ok(!screen.queryByText(proposal.message)));
  assert.deepEqual(
    net.seen.filter((r) => r.method === "POST").map((r) => [r.path, r.body]),
    [
      [
        "/api/memory/files/proposals/p-1",
        { scope: "project", project: "Launch", decision: "approve" },
      ],
    ],
  );
});

test("the Files tab of a memory chain opens that chain's tree", async () => {
  const folder = {
    scope: "personal",
    title: "Personal",
    notes: 0,
    pending: 0,
    can_write: true,
    can_propose: true,
    can_promote: false,
    management: "manual",
    can_chat: false,
  };
  net = stubFetch({
    ...reads,
    "GET /api/library/memory": () => ({
      folders: [
        { ...folder, id: "personal" },
        { ...folder, id: "workspace", scope: "workspace", title: "Company" },
      ],
    }),
    "GET /api/me/memory": () => ({ notes: [] }),
  });
  const places: unknown[] = [];
  render(<MemoryLibrary onLocationChange={(place) => places.push(place)} />);
  fireEvent.click(await screen.findByRole("tab", { name: copy.files }));
  await screen.findByText(first);
  assert.deepEqual(places, [{ scope: "personal", owner: "", memoryView: "files" }]);
  assert.ok(net.seen.some((r) => r.path === "/api/memory/files?scope=me"));
});
