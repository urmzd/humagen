import assert from "node:assert/strict";
import { test } from "node:test";
import type { ThreadImport } from "@humagen/client/types";
import { continuable, continueBody, importDrift, importedTool, importFacts } from "./imported.ts";

const digest = "a".repeat(64);
const structure = "b".repeat(64);

const kept = (over: Partial<ThreadImport> = {}): ThreadImport => ({
  source: "sessions-chatgpt-2026-11",
  source_id: "src-1",
  document: "d-1",
  uri: "chatgpt:generated-1",
  title: "Generated plan",
  structure: "kept",
  tool: "chatgpt",
  document_digest: digest,
  structure_digest: structure,
  by: "dev",
  at: "2026-11-20T10:00:00Z",
  context: Array.from({ length: 12 }, (_, i) => ({
    speaker: i % 2 ? "assistant" : "user",
    text: `turn ${i}`,
  })),
  omitted: { turns: 50, results: 18, cut: 2 },
  branches: 3,
  import_cut: { turns: 4, results: 1, cut_turns: 6, attachments: 2 },
  ...over,
});

test("only the four session schemes name a tool", () => {
  assert.equal(importedTool("chatgpt:abc"), "chatgpt");
  assert.equal(importedTool("claude-ai:abc"), "claude-ai");
  assert.equal(importedTool("claude:abc"), "claude");
  assert.equal(importedTool("codex:abc"), "codex");
  for (const uri of [undefined, "", "https://example.com/a", "chatgpt:", ":abc", "ChatGPT:abc", "notes"])
    assert.equal(importedTool(uri), undefined, String(uri));
});

test("a session document is offered only with the digest the request pins", () => {
  assert.equal(continuable({ uri: "chatgpt:x", digest }), true);
  assert.equal(continuable({ uri: "chatgpt:x" }), false);
  assert.equal(continuable({ uri: "https://example.com", digest }), false);
});

test("the body names the listed digests and the project, and no structure digest the listing lacks", () => {
  assert.deepEqual(continueBody({ digest, structure_digest: structure }, "research"), {
    document_digest: digest,
    project: "research",
    structure_digest: structure,
  });
  const none = continueBody({ digest }, "");
  assert.deepEqual(none, { document_digest: digest, project: "~" });
  assert.equal("structure_digest" in none, false);
});

test("the facts count what was sent of what the import kept", () => {
  assert.deepEqual(importFacts(kept()), {
    known: true,
    sent: 12,
    kept: 80,
    shortened: 2,
    branches: 3,
    files: 2,
    importLeftOut: 5,
    importShortened: 6,
  });
  const whole = importFacts(kept({ omitted: { turns: 0, results: 0, cut: 0 }, branches: undefined, import_cut: undefined }));
  assert.equal(whole.kept, 12);
  assert.equal(whole.branches, 0);
  assert.equal(whole.files, 0);
  assert.equal(whole.importLeftOut, 0);
});

test("a document with no kept structure says its counts are unknown, not zero", () => {
  const facts = importFacts(
    kept({
      structure: "not_kept",
      tool: undefined,
      branches: undefined,
      import_cut: undefined,
      context: [{ speaker: "document", text: "the whole text", cut: true }],
      omitted: { turns: 0, results: 0, cut: 1 },
    }),
  );
  assert.equal(facts.known, false);
  assert.equal(facts.sent, 1);
  assert.equal(facts.branches, undefined);
  assert.equal(facts.files, undefined);
  assert.equal(facts.importLeftOut, undefined);
  assert.equal(facts.importShortened, undefined);
});

test("the pinned revision is compared with the source's current listing", () => {
  const imp = kept();
  assert.equal(importDrift(imp, [{ id: "d-1", digest }]), "same");
  assert.equal(importDrift(imp, [{ id: "d-1", digest: "c".repeat(64) }]), "changed");
  assert.equal(importDrift(imp, [{ id: "d-1" }]), "changed");
  assert.equal(importDrift(imp, [{ id: "d-2", digest }]), "gone");
});
