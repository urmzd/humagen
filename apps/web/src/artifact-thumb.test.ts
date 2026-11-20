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

test("a Markdown preview keeps an underscore inside a name", () => {
  assert.equal(
    previewText(file({ preview: "Set _one_ of `max_retries` or __retry_after__." })),
    "Set one of max_retries or retry_after.",
  );
});

test("code and plain text are shown as written: names, comment marks and indentation stay", () => {
  assert.equal(
    previewText(
      file({
        title: "seed.py",
        kind: "code",
        mime: "text/x-python",
        preview: "# seed\ndef seed(path):\n\n    rows = path.read_text()\n\treturn rows * 2\n",
      }),
    ),
    "# seed\ndef seed(path):\n    rows = path.read_text()\n  return rows * 2",
  );
  assert.equal(
    previewText(file({ title: "notes.txt", mime: "text/plain", preview: "user_id, *required*" })),
    "user_id, *required*",
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

test("an HTML preview cut off inside a script, a comment or a tag shows none of the markup", () => {
  const html = (preview: string) => previewText(file({ kind: "html", mime: "text/html", preview }));
  assert.equal(html("<h1>Report</h1><script>var secret = 1; fetch("), "Report");
  assert.equal(html("<p>Intro</p><style>body{margin:0"), "Intro");
  assert.equal(html("<p>Intro</p><!-- draft note"), "Intro");
  assert.equal(html('<p>Intro</p><a href="https://example.com/a'), "Intro");
  assert.equal(html("<head><title>Notes</title></head>Line one<br>Line two"), "Line one\nLine two");
  assert.equal(html("<!doctype html><html><head><title>Notes</title><meta"), "");
});

test("an HTML preview shows entities as their characters, and stays text", () => {
  assert.equal(
    previewText(
      file({
        kind: "html",
        mime: "text/html",
        preview: "<p>Q&amp;A&nbsp;&#169; 5 &lt; 6 &#x41;</p>",
      }),
    ),
    "Q&A \u00a9 5 < 6 A",
  );
  // Decoded markup is words on the card, never elements: React renders the string as text.
  assert.equal(
    previewText(
      file({
        kind: "html",
        mime: "text/html",
        preview: "<p>&lt;img src=x onerror=alert(1)&gt;</p>",
      }),
    ),
    "<img src=x onerror=alert(1)>",
  );
});

test("a file with no text has no preview, and a long one is cut", () => {
  assert.equal(previewText(file({ preview: undefined })), "");
  assert.equal(previewText(file({ preview: "   " })), "");
  assert.equal(previewText(file({ preview: "a".repeat(2000) })).length, 420);
});
