import assert from "node:assert/strict";
import { test } from "node:test";
import { previewText } from "./artifactThumb.ts";
import type { Stored } from "./storage.ts";

const file = (over: Partial<Stored>): Stored => ({
  id: "a1",
  title: "notes.md",
  kind: "document",
  mime: "text/markdown",
  size: 10,
  sha256: "x",
  version: 1,
  run: "r",
  thread: "t",
  agent: "lead",
  created_by: "lead",
  created_at: "2026-01-01T00:00:00Z",
  ...over,
});

test("a card's preview is the file's own words: Markdown marks and blank lines are left out", () => {
  assert.equal(
    previewText(file({ preview: "# Q4 **launch** plan\n\n\n1. Freeze `scope`.\n" })),
    "Q4 launch plan\n1. Freeze scope.",
  );
});

test("an HTML file's preview drops its tags, scripts and styles and keeps one line per block", () => {
  assert.equal(
    previewText(
      file({
        kind: "html",
        mime: "text/html",
        preview: "<style>p{color:red}</style><h1>2.4</h1><p>Fixes and <b>one</b> new command.</p>",
      }),
    ),
    "2.4\nFixes and one new command.",
  );
});

test("a file with no text has no preview, and a long one is cut", () => {
  assert.equal(previewText(file({ preview: undefined })), "");
  assert.equal(previewText(file({ preview: "   " })), "");
  assert.equal(previewText(file({ preview: "a".repeat(2000) })).length, 420);
});
