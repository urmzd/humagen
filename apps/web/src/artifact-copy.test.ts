import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import {
  canEditArtifact,
  copyArtifactToWorkspace,
  MAX_EDITABLE_ARTIFACT_BYTES,
} from "./artifact-copy.ts";
import type { Stored } from "./storage.ts";

const fetchBefore = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = fetchBefore;
});
const artifact: Stored = {
  id: "immutable-artifact@1",
  title: "src/main.ts",
  kind: "code",
  mime: "text/typescript",
  size: 30,
  sha256: "original-hash",
  version: 1,
  run: "run-1",
  thread: "thread/one",
  agent: "writer",
  created_by: "writer",
  created_at: "2026-11-02T12:00:00Z",
};

test("an exact immutable UTF-8 version becomes a uniquely named workspace file without changing the artifact", async () => {
  const original = { ...artifact };
  const content = "\uFEFFconst café = 1;\r\n";
  const calls: { path: string; init?: RequestInit }[] = [];
  globalThis.fetch = (async (input, init) => {
    calls.push({ path: String(input), init });
    return init?.method === "PUT" ? Response.json({ revision: "saved" }) : new Response(content);
  }) as typeof fetch;
  const first = await copyArtifactToWorkspace(artifact, artifact.thread);
  const second = await copyArtifactToWorkspace(artifact, artifact.thread);
  assert.notEqual(first, second);
  assert.match(first, /^copies\/[a-f0-9-]{36}\/main\.ts$/);
  const read = calls[0];
  const write = calls[1];
  assert.ok(read);
  assert.ok(write);
  assert.equal(read.path, "/api/artifacts/immutable-artifact%401/content");
  assert.equal(write.path, "/api/threads/thread%2Fone/files/content");
  assert.equal(read.init?.credentials, "same-origin");
  assert.equal(write.init?.credentials, "same-origin");
  assert.equal(write.init?.method, "PUT");
  assert.deepEqual(JSON.parse(String(write.init?.body)), {
    path: first,
    content,
    revision: "",
  });
  assert.deepEqual(artifact, original);
});

test("copies refuse other conversations, unsupported native binaries, and known oversize files before reading", async () => {
  globalThis.fetch = (() => assert.fail("No file should be read or written")) as typeof fetch;
  await assert.rejects(copyArtifactToWorkspace(artifact, "other"), /not in this conversation/);
  await assert.rejects(
    copyArtifactToWorkspace(
      { ...artifact, size: MAX_EDITABLE_ARTIFACT_BYTES + 1 },
      artifact.thread,
    ),
    /cannot be opened/,
  );
  assert.equal(
    canEditArtifact({
      ...artifact,
      kind: "presentation",
      title: "slides.pptx",
      mime: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
    }),
    false,
  );
  for (const title of ["changes.diff", "changes.patch", "notes.md", "data.csv", "page.html"])
    assert.equal(
      canEditArtifact({ ...artifact, kind: "data", mime: "application/octet-stream", title }),
      true,
    );
});

test("the streaming byte limit is enforced despite an inaccurate artifact size and content length", async () => {
  let cancelled = false;
  let reads = 0;
  globalThis.fetch = (async () => {
    reads++;
    return new Response(
      new ReadableStream<Uint8Array>({
        start(controller) {
          controller.enqueue(new Uint8Array(MAX_EDITABLE_ARTIFACT_BYTES));
          controller.enqueue(new Uint8Array(1));
        },
        cancel() {
          cancelled = true;
        },
      }),
      { headers: { "Content-Length": "1" } },
    );
  }) as typeof fetch;
  await assert.rejects(
    copyArtifactToWorkspace(artifact, artifact.thread),
    /limited to files of 2 MB/,
  );
  assert.equal(reads, 1, "The oversized content is never written");
  assert.equal(cancelled, true);
});

test("invalid UTF-8 and binary control bytes cannot be saved as text copies", async () => {
  let calls = 0;
  for (const bytes of [new Uint8Array([0xff]), new Uint8Array([0x41, 0x00, 0x42])]) {
    globalThis.fetch = (async () => {
      calls++;
      return new Response(bytes);
    }) as typeof fetch;
    await assert.rejects(copyArtifactToWorkspace(artifact, artifact.thread), /UTF-8|Binary/);
  }
  assert.equal(calls, 2);
});

test("read and workspace write refusals retain the server's reason and cannot appear successful", async () => {
  globalThis.fetch = (async () =>
    Response.json({ error: "Artifact access refused." }, { status: 403 })) as typeof fetch;
  await assert.rejects(
    copyArtifactToWorkspace(artifact, artifact.thread),
    /Artifact access refused/,
  );
  globalThis.fetch = (async (_input, init) =>
    init?.method === "PUT"
      ? Response.json({ error: "A run is updating these files." }, { status: 409 })
      : new Response("# Notes")) as typeof fetch;
  await assert.rejects(
    copyArtifactToWorkspace(artifact, artifact.thread),
    /A run is updating these files/,
  );
});
