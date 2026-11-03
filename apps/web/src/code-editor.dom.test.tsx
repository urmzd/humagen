import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { ChangeReview, CodeEditor, type ReviewComment } from "@humagen/web";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";

afterEach(cleanup);

function Editor({
  initial = "  first\nsecond",
  onSave = () => {},
  maxBytes,
}: {
  initial?: string;
  onSave?: () => void;
  maxBytes?: number;
}) {
  const [value, setValue] = useState(initial);
  return (
    <CodeEditor
      path="src/app.ts"
      value={value}
      onChange={setValue}
      onSave={onSave}
      maxBytes={maxBytes}
    />
  );
}

test("editor indents, preserves newline indentation, and lets Escape then Tab move focus", async () => {
  let closed = false;
  render(
    // biome-ignore lint/a11y/noNoninteractiveElementInteractions: reproduce the enclosing inspector's Escape handler
    <dialog
      open
      onKeyDown={(event) => {
        if (event.key === "Escape") closed = true;
      }}
    >
      <Editor />
    </dialog>,
  );
  const input = screen.getByRole("textbox", { name: "Edit src/app.ts" }) as HTMLTextAreaElement;
  input.setSelectionRange(7, 7);
  fireEvent.keyDown(input, { key: "Enter" });
  await waitFor(() => assert.equal(input.value, "  first\n  \nsecond"));
  await waitFor(() => assert.equal(input.selectionStart, 10));
  fireEvent.keyDown(input, { key: "Tab" });
  await waitFor(() => assert.equal(input.value, "  first\n    \nsecond"));
  fireEvent.keyDown(input, { key: "Escape" });
  assert.equal(closed, false);
  assert.equal(fireEvent.keyDown(input, { key: "Tab" }), true);
});

test("find selects file content, keyboard save uses host callback, readonly cannot save", () => {
  let saves = 0;
  const view = render(<Editor onSave={() => saves++} />);
  const input = screen.getByRole("textbox", { name: "Edit src/app.ts" }) as HTMLTextAreaElement;
  fireEvent.click(screen.getByRole("button", { name: "Find" }));
  fireEvent.change(screen.getByRole("searchbox", { name: "Find in file" }), {
    target: { value: "second" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Find Next" }));
  assert.equal(input.value.slice(input.selectionStart, input.selectionEnd), "second");
  fireEvent.keyDown(input, { key: "s", ctrlKey: true });
  assert.equal(saves, 1);
  view.rerender(
    <CodeEditor
      path="src/app.ts"
      value="read only"
      onChange={() => assert.fail("readonly changed")}
      readOnly
      onSave={() => saves++}
    />,
  );
  fireEvent.keyDown(screen.getByRole("textbox", { name: "Edit src/app.ts" }), {
    key: "s",
    metaKey: true,
  });
  assert.equal(saves, 1);
  assert.equal((screen.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled, true);
});

test("unindent works from a line start and whole-line selections preserve the next line", async () => {
  render(<Editor initial={"  first\nsecond"} />);
  const input = screen.getByRole("textbox", { name: "Edit src/app.ts" }) as HTMLTextAreaElement;
  input.setSelectionRange(0, 0);
  fireEvent.click(screen.getByRole("button", { name: "Unindent" }));
  await waitFor(() => assert.equal(input.value, "first\nsecond"));
  input.setSelectionRange(0, 6);
  fireEvent.click(screen.getByRole("button", { name: "Indent" }));
  await waitFor(() => assert.equal(input.value, "  first\nsecond"));
});

test("edits over the size bound preserve the file and report the refusal", () => {
  render(<Editor initial="ok" maxBytes={4} />);
  const input = screen.getByRole("textbox", { name: "Edit src/app.ts" }) as HTMLTextAreaElement;
  fireEvent.change(input, { target: { value: "too large" } });
  assert.equal(input.value, "ok");
  assert.ok(screen.getByText("The edit exceeds the file size or 20,000-line limit."));
});

test("selected code prepares exact draft text and line coordinates without saving", () => {
  const selections: { text: string; startLine: number; endLine: number }[] = [];
  render(
    <CodeEditor
      path="a.ts"
      value={"first\nsecond\nthird"}
      onChange={() => {}}
      onAskSelection={(selection) => selections.push(selection)}
    />,
  );
  const input = screen.getByRole("textbox", { name: "Edit a.ts" }) as HTMLTextAreaElement;
  assert.equal(
    (screen.getByRole("button", { name: "Ask About Selection" }) as HTMLButtonElement).disabled,
    true,
  );
  input.setSelectionRange(6, 13);
  fireEvent.select(input);
  assert.deepEqual(selections, []);
  fireEvent.click(screen.getByRole("button", { name: "Ask About Selection" }));
  assert.deepEqual(selections, [{ text: "second\n", startLine: 2, endLine: 2 }]);
});

test("review comments retain exact file, side and line and only prepare a draft", () => {
  const comments: ReviewComment[] = [];
  render(
    <ChangeReview
      patch={"--- a/src/app.ts\n+++ b/src/app.ts\n@@ -8 +8 @@\n-old\n+new\n"}
      onComment={(comment) => comments.push(comment)}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Comment on src/app.ts, old line 8" }));
  fireEvent.change(screen.getByRole("textbox", { name: "Comment on src/app.ts, old line 8" }), {
    target: { value: "Keep this behavior." },
  });
  assert.deepEqual(comments, []);
  fireEvent.click(screen.getByRole("button", { name: "Add To Draft" }));
  assert.deepEqual(comments, [
    { path: "src/app.ts", side: "old", line: 8, text: "Keep this behavior." },
  ]);
  assert.ok(screen.getByText("Review comment prepared in the conversation draft."));
});

test("patch content stays inert text and invalid hunks have no comment action", () => {
  const view = render(
    <ChangeReview before="old" after="<img src=x onerror=alert(1)>" path="page.html" />,
  );
  assert.ok(screen.getByText("<img src=x onerror=alert(1)>"));
  assert.equal(view.container.querySelector("img"), null);
  view.rerender(
    <ChangeReview
      patch="--- a/a\n+++ b/a\n@@ -1,2 +1 @@\n-a\n+b"
      onComment={() => assert.fail("invalid review")}
    />,
  );
  assert.ok(screen.getByRole("alert"));
  assert.deepEqual(screen.queryAllByRole("button", { name: /^Comment on/ }), []);
});

test("switching the reviewed comparison cancels a comment on the previous version", () => {
  const view = render(
    <ChangeReview
      before="old"
      after="new"
      path="one.ts"
      onComment={() => assert.fail("stale comment")}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Comment on one.ts, new line 1" }));
  fireEvent.change(screen.getByRole("textbox", { name: "Comment on one.ts, new line 1" }), {
    target: { value: "Stale review" },
  });
  view.rerender(
    <ChangeReview
      before="old"
      after="other"
      path="two.ts"
      onComment={() => assert.fail("stale comment")}
    />,
  );
  assert.equal(screen.queryByRole("textbox"), null);
});
