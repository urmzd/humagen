import assert from "node:assert/strict";
import { test } from "node:test";
import { compareText, parseUnifiedDiff } from "../../../packages/web/src/code-diff.ts";

test("unified review keeps file identity and exact old/new line coordinates", () => {
  const result = parseUnifiedDiff(
    "diff --git a/src/a.ts b/src/a.ts\n--- a/src/a.ts\n+++ b/src/a.ts\n@@ -10,2 +10,3 @@ handler\n before\n-old\n+new\n+extra\ndiff --git a/new.md b/new.md\n--- /dev/null\n+++ b/new.md\n@@ -0,0 +1 @@\n+# New\n",
  );
  assert.equal(result.error, undefined);
  assert.deepEqual(
    result.files.map(({ path, additions, deletions }) => ({ path, additions, deletions })),
    [
      { path: "src/a.ts", additions: 2, deletions: 1 },
      { path: "new.md", additions: 1, deletions: 0 },
    ],
  );
  assert.deepEqual(result.files[0]?.lines.slice(1), [
    { kind: "context", text: "before", oldLine: 10, newLine: 10 },
    { kind: "remove", text: "old", oldLine: 11 },
    { kind: "add", text: "new", newLine: 11 },
    { kind: "add", text: "extra", newLine: 12 },
  ]);
});

test("malformed, incomplete, and unbounded patches cannot become reviewable changes", () => {
  for (const patch of [
    "@@ -1 +1 @@\n-a\n+b",
    "--- a/a\n+++ b/a\n@@ -1,2 +1 @@\n-a\n+b",
    "--- a/a\n+++ b/a\n@@ -1 +1 @@\n-a\n+b\n+unaccounted",
    "not a diff",
    "x".repeat(2 * 1024 * 1024 + 1),
  ]) {
    const result = parseUnifiedDiff(patch);
    assert.ok(result.error);
    assert.deepEqual(result.files, []);
  }
});

test("deleted files and no-newline markers preserve the old side", () => {
  const result = parseUnifiedDiff(
    "--- a/old.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-old\n\\ No newline at end of file\n",
  );
  assert.equal(result.error, undefined);
  assert.equal(result.files[0]?.path, "old.txt");
  assert.deepEqual(result.files[0]?.lines[1], { kind: "remove", text: "old", oldLine: 1 });
  assert.equal(result.files[0]?.lines[2]?.kind, "meta");
});

test("draft comparison retains unchanged context and exact insertion coordinates", () => {
  const result = compareText(
    "first\nkeep\nold\nlast",
    "first\nkeep\nnew\nextra\nlast",
    "src/app.ts",
  );
  assert.equal(result.files[0]?.path, "src/app.ts");
  assert.deepEqual(result.files[0]?.lines, [
    { kind: "context", text: "first", oldLine: 1, newLine: 1 },
    { kind: "context", text: "keep", oldLine: 2, newLine: 2 },
    { kind: "remove", text: "old", oldLine: 3 },
    { kind: "add", text: "new", newLine: 3 },
    { kind: "add", text: "extra", newLine: 4 },
    { kind: "context", text: "last", oldLine: 4, newLine: 5 },
  ]);
  assert.deepEqual(compareText("same", "same"), { files: [] });
});

test("large unrelated files have bounded work and a declared render limit", () => {
  const before = Array.from({ length: 10000 }, (_, index) => `before ${index}`).join("\n");
  const after = Array.from({ length: 10000 }, (_, index) => `after ${index}`).join("\n");
  const result = compareText(before, after);
  assert.equal(result.truncated, true);
  assert.equal(result.files[0]?.lines.length, 4000);
  assert.equal(result.files[0]?.additions, 10000);
  assert.equal(result.files[0]?.deletions, 10000);
});
