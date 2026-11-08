import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { Markdown } from "@humagen/primitives";
import { MarkdownHeadingContext, WebSurface } from "@humagen/render-web";
import { Inspector } from "@humagen/web";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { StrictMode, useState } from "react";
import { stubFetch } from "./dom.ts";
import type { ConversationView } from "./route.ts";
import { WorkspaceFiles } from "./WorkspaceFiles.tsx";
import { staticWorkspaceHTML, validWorkspacePath } from "./workspace-files.ts";

const words = defaultCopy.workspaceFiles;
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});
const base = "/api/threads/thread-1/files";
const file = { path: "src/main.ts", content: "const value = 1;\n", revision: "one" };
function Harness({
  initial = { file: file.path },
  requests = [] as string[],
}: {
  initial?: ConversationView;
  requests?: string[];
}) {
  const [view, setView] = useState<ConversationView>(initial);
  const [open, setOpen] = useState(true);
  return (
    <>
      <button type="button" onClick={() => setOpen((value) => !value)}>
        Toggle Workspace
      </button>
      <WorkspaceFiles
        thread="thread-1"
        open={open}
        onClose={() => setOpen(false)}
        view={view}
        onView={setView}
        onRequest={(text) => requests.push(text)}
        onPublished={() => {}}
      />
    </>
  );
}
function setup(extra: Parameters<typeof stubFetch>[0] = {}) {
  let saved = { ...file };
  net = stubFetch({
    "GET /api/me": () => ({ permissions: ["runs:read", "runs:write", "artifacts:write"] }),
    [`GET ${base}`]: () => ({
      files: [{ path: saved.path, size: saved.content.length, revision: saved.revision }],
      writable: true,
      retention_days: 7,
    }),
    [`GET ${base}/content`]: () => saved,
    [`PUT ${base}/content`]: (body) => {
      const value = body as typeof file;
      assert.equal(value.revision, saved.revision);
      saved = { ...value, revision: "two" };
      return saved;
    },
    [`POST ${base}/publish`]: (body) => {
      assert.deepEqual(body, { path: saved.path, revision: saved.revision });
      return { id: "artifact-1", version: 1 };
    },
    ...extra,
  });
  return net;
}
const editor = () =>
  screen.findByRole("textbox", { name: `Edit ${file.path}` }) as Promise<HTMLTextAreaElement>;
const save = () =>
  fireEvent.click(screen.getAllByRole("button", { name: /^Save/ })[0] as HTMLElement);

test("a desktop file picker is modal above Details and restores focus without closing its parent", async () => {
  const matchMedia = window.matchMedia;
  const showModal = HTMLDialogElement.prototype.showModal;
  const modals: HTMLDialogElement[] = [];
  let closed = 0;
  window.matchMedia = ((query: string) => ({
    matches: query === "(prefers-reduced-motion: reduce)",
    addEventListener() {},
    removeEventListener() {},
  })) as unknown as typeof window.matchMedia;
  HTMLDialogElement.prototype.showModal = function () {
    modals.push(this);
    showModal.call(this);
  };
  const plan = { path: "plan.md", content: "# Launch plan\nOwner: Developer\n", revision: "plan1" };
  const network = setup({
    [`GET ${base}`]: () => ({
      files: [{ path: plan.path, size: plan.content.length, revision: plan.revision }],
      writable: true,
      retention_days: 7,
    }),
    [`GET ${base}/content`]: (_body, query) => {
      assert.equal(query.get("path"), plan.path);
      return plan;
    },
  });
  function Details() {
    const [view, setView] = useState<ConversationView>({});
    return (
      <>
        <div id="humagen-inspector-dock" />
        <WorkspaceFiles
          thread="thread-1"
          open
          onClose={() => closed++}
          view={view}
          onView={setView}
          frame={(body) => (
            <Inspector open title="Details" onClose={() => closed++}>
              {body}
            </Inspector>
          )}
        />
      </>
    );
  }
  try {
    render(<Details />);
    const details = (await screen.findByRole("dialog", { name: "Details" })) as HTMLDialogElement;
    assert.equal(details.parentElement?.id, "humagen-inspector-dock");
    const trigger = await within(details).findByRole("button", { name: words.files });
    const openPicker = async () => {
      trigger.focus();
      fireEvent.click(trigger);
      const picker = (await screen.findByRole("dialog", {
        name: words.files,
      })) as HTMLDialogElement;
      assert.equal(document.querySelectorAll("#humagen-inspector-dock > dialog[open]").length, 1);
      assert.ok(
        modals.includes(picker),
        "the nested picker must use native modal behavior on desktop",
      );
      assert.equal(picker.parentElement?.id === "humagen-inspector-dock", false);
      assert.equal(
        document.activeElement === within(picker).getByRole("heading", { name: words.files }),
        true,
      );
      return picker;
    };
    const picker = await openPicker();
    fireEvent.keyDown(within(picker).getByRole("button", { name: plan.path }), { key: "Escape" });
    await waitFor(() => assert.equal(document.activeElement === trigger, true));
    assert.equal(details.open, true);
    assert.equal(closed, 0);

    const cancelled = await openPicker();
    fireEvent(cancelled, new Event("cancel", { bubbles: true, cancelable: true }));
    await waitFor(() => assert.equal(document.activeElement === trigger, true));
    assert.equal(closed, 0, "the nested native cancel must not close Details");

    const chosen = await openPicker();
    fireEvent.click(within(chosen).getByRole("button", { name: plan.path }));
    const input = (await screen.findByRole("textbox", {
      name: `Edit ${plan.path}`,
    })) as HTMLTextAreaElement;
    assert.equal(input.value, plan.content);
    assert.equal(details.open, true);
    assert.equal(closed, 0);
    assert.equal(network.seen.filter((request) => request.method !== "GET").length, 0);
  } finally {
    cleanup();
    window.matchMedia = matchMedia;
    HTMLDialogElement.prototype.showModal = showModal;
  }
});

test("workspace loads in StrictMode and saves then publishes the exact revision", async () => {
  const network = setup();
  render(
    <StrictMode>
      <Harness />
    </StrictMode>,
  );
  const input = await editor();
  fireEvent.change(input, { target: { value: "const value = 2;\n" } });
  assert.equal(
    (screen.getByRole("button", { name: words.publish }) as HTMLButtonElement).disabled,
    true,
  );
  save();
  await waitFor(() => assert.ok(network.seen.some((request) => request.method === "PUT")));
  await waitFor(() =>
    assert.equal(
      (screen.getByRole("button", { name: words.publish }) as HTMLButtonElement).disabled,
      false,
    ),
  );
  fireEvent.click(screen.getByRole("button", { name: words.publish }));
  assert.ok(await screen.findByText(words.published(1)));
  assert.deepEqual(network.seen.find((request) => request.method === "PUT")?.body, {
    path: file.path,
    content: "const value = 2;\n",
    revision: "one",
  });
});

test("stale revision preserves the draft and requires an explicit reviewed base before saving", async () => {
  let stale = false;
  let latestRevision = "one";
  const network = setup({
    [`GET ${base}/content`]: () => ({
      ...file,
      content: stale ? "const value = 9;\n" : file.content,
      revision: latestRevision,
    }),
    [`GET ${base}`]: () => ({
      files: [{ path: file.path, size: 17, revision: latestRevision }],
      writable: true,
      retention_days: 7,
    }),
    [`PUT ${base}/content`]: (body) => {
      if (!stale) {
        stale = true;
        latestRevision = "remote";
        return new Response(JSON.stringify({ error: "conflict" }), { status: 409 });
      }
      assert.equal((body as typeof file).revision, "remote");
      latestRevision = "three";
      return { ...(body as typeof file), revision: latestRevision };
    },
  });
  render(<Harness />);
  fireEvent.change(await editor(), { target: { value: "const value = 2;\n" } });
  save();
  assert.ok(await screen.findByRole("button", { name: words.compareLatest }));
  assert.equal((await editor()).value, "const value = 2;\n");
  fireEvent.click(screen.getByRole("button", { name: words.compareLatest }));
  const useBase = await screen.findByRole("button", { name: words.useLatestBase });
  assert.ok(document.body.textContent?.includes("const value = 9;"));
  fireEvent.click(useBase);
  save();
  await waitFor(() =>
    assert.equal(network.seen.filter((request) => request.method === "PUT").length, 2),
  );
});

test("new file drafts survive drawer closure and prevent accidental navigation", async () => {
  setup();
  render(<Harness initial={{}} />);
  await waitFor(() =>
    assert.equal(
      (screen.getByRole("button", { name: words.newFile }) as HTMLButtonElement).disabled,
      false,
    ),
  );
  fireEvent.click(screen.getByRole("button", { name: words.newFile }));
  fireEvent.change(screen.getByLabelText(words.path), { target: { value: "notes/plan.md" } });
  fireEvent.click(screen.getByRole("button", { name: words.create }));
  const input = (await screen.findByRole("textbox", {
    name: "Edit notes/plan.md",
  })) as HTMLTextAreaElement;
  fireEvent.change(input, { target: { value: "# My pending plan\n" } });
  fireEvent.click(screen.getByRole("button", { name: "Toggle Workspace" }));
  fireEvent.click(screen.getByRole("button", { name: "Toggle Workspace" }));
  await waitFor(() =>
    assert.equal(
      (screen.getByRole("textbox", { name: "Edit notes/plan.md" }) as HTMLTextAreaElement).value,
      "# My pending plan\n",
    ),
  );
  const confirm = window.confirm;
  window.confirm = () => false;
  try {
    const event = new Event("humagen:before-navigate", { cancelable: true });
    assert.equal(window.dispatchEvent(event), false);
    assert.equal(event.defaultPrevented, true);
  } finally {
    window.confirm = confirm;
  }
});

test("read-only workspace shows the actual reason and disables editing", async () => {
  setup({
    [`GET ${base}`]: () => ({
      files: [{ path: file.path, size: 17, revision: "one" }],
      writable: false,
      reason: "An agent is working",
      retention_days: 7,
    }),
  });
  render(<Harness />);
  const input = await editor();
  assert.ok(await screen.findByText("An agent is working"));
  assert.equal(input.readOnly, true);
  assert.equal(
    (screen.getByRole("button", { name: words.newFile }) as HTMLButtonElement).disabled,
    true,
  );
});

test("static HTML preview removes active content, remote resources and navigation", () => {
  const value = staticWorkspaceHTML(
    '<meta http-equiv="refresh" content="0;url=https://example.com"><script>alert(1)</script><iframe src="https://example.com"></iframe><a href="https://example.com" onclick="alert(2)">Link</a><img src="https://example.com/image.png"><h1>Preview</h1>',
  );
  const doc = new DOMParser().parseFromString(value, "text/html");
  assert.equal(doc.querySelectorAll("script,iframe,a[href],[onclick],img[src]").length, 0);
  assert.equal(doc.querySelectorAll('meta[http-equiv="refresh"]').length, 0);
  assert.equal(
    doc
      .querySelector('meta[http-equiv="Content-Security-Policy"]')
      ?.getAttribute("content")
      ?.includes("default-src 'none'"),
    true,
  );
  assert.equal(doc.querySelector("h1")?.textContent, "Preview");
  assert.equal(validWorkspacePath("../secret"), false);
  assert.equal(validWorkspacePath("a".repeat(241)), false);
  assert.equal(validWorkspacePath("src/main.ts"), true);
});

test("imported PR patches prepare line comments without executing work or saving implicitly", async () => {
  const network = setup();
  const requests: string[] = [];
  render(<Harness initial={{}} requests={requests} />);
  await waitFor(() =>
    assert.equal(
      (screen.getByRole("button", { name: words.import }) as HTMLButtonElement).disabled,
      false,
    ),
  );
  const patch =
    "diff --git a/src/main.ts b/src/main.ts\n--- a/src/main.ts\n+++ b/src/main.ts\n@@ -1 +1 @@\n-const value = 1;\n+const value = 2;\n";
  const upload = screen.getByLabelText(words.import, { selector: "input" });
  fireEvent.change(upload, {
    target: { files: [new File([patch], "review.patch", { type: "text/plain" })] },
  });
  assert.ok(await screen.findByRole("textbox", { name: "Edit review.patch" }));
  fireEvent.click(screen.getByRole("button", { name: words.changes }));
  const label = defaultCopy.editor.comment("src/main.ts", "new", 1);
  fireEvent.click(await screen.findByRole("button", { name: label }));
  fireEvent.change(screen.getByRole("textbox", { name: label }), {
    target: { value: "Please explain this constant." },
  });
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.editor.addToDraft }));
  assert.equal(requests.length, 1);
  assert.ok(
    requests[0]?.startsWith(
      'Review comment on "src/main.ts", new line 1: Please explain this constant.',
    ),
  );
  assert.match(requests[0] ?? "", /review\.patch/);
  assert.match(requests[0] ?? "", /Includes unsaved edits/);
  assert.match(requests[0] ?? "", /const value = 2;/);
  assert.equal(
    network.seen.some((request) => request.method !== "GET"),
    false,
  );
});

test("work starting after the file opens pauses saves without inventing a stale revision", async () => {
  let lists = 0;
  setup({
    [`GET ${base}`]: () => ({
      files: [{ path: file.path, size: file.content.length, revision: file.revision }],
      writable: lists++ === 0,
      reason: "Wait for active work to finish",
      retention_days: 7,
    }),
    [`PUT ${base}/content`]: () =>
      new Response(JSON.stringify({ error: "Wait for active work to finish" }), { status: 409 }),
  });
  render(<Harness />);
  const input = await editor();
  fireEvent.change(input, { target: { value: "Keep my draft" } });
  save();
  await screen.findAllByText("Wait for active work to finish");
  assert.equal(input.value, "Keep my draft");
  assert.equal(screen.queryByRole("button", { name: words.compareLatest }), null);
  assert.equal(
    (screen.getAllByRole("button", { name: /^Save/ })[0] as HTMLButtonElement).disabled,
    true,
  );
});

test("binary file import is refused before any file is saved", async () => {
  const network = setup();
  render(<Harness initial={{}} />);
  await waitFor(() =>
    assert.equal(
      (screen.getByRole("button", { name: words.import }) as HTMLButtonElement).disabled,
      false,
    ),
  );
  fireEvent.change(screen.getByLabelText(words.import, { selector: "input" }), {
    target: { files: [new File([new Uint8Array([0, 255])], "binary.dat")] },
  });
  assert.ok(await screen.findByText(words.tooLarge));
  assert.equal(
    network.seen.some((request) => request.method !== "GET"),
    false,
  );
});

test("Markdown preview nests ATX and setext headings below the page title and leaves fenced code literal", async () => {
  const content =
    "# ATX heading\n\nSetext heading\n===\n\n```md\n# literal code\n```\n\n## Subtitle\n";
  setup({
    [`GET ${base}/content`]: () => ({ path: "notes.md", content, revision: "markdown-one" }),
  });
  render(
    <>
      <h1>Conversation title</h1>
      <Harness initial={{ file: "notes.md", workspaceTab: "preview" }} />
    </>,
  );
  assert.ok(await screen.findByRole("heading", { name: "ATX heading", level: 2 }));
  assert.ok(screen.getByRole("heading", { name: "Setext heading", level: 2 }));
  assert.ok(screen.getByRole("heading", { name: "Subtitle", level: 3 }));
  assert.equal(screen.getAllByRole("heading", { level: 1 }).length, 1);
  assert.equal(screen.queryByRole("heading", { name: "literal code" }), null);
  assert.ok(document.body.textContent?.includes("# literal code"));
});

test("Markdown heading context preserves default headings and caps embedded headings at level six", () => {
  const result = render(
    <WebSurface>
      <Markdown text={"# Default title\n\n## Default subtitle"} />
    </WebSurface>,
  );
  assert.ok(screen.getByRole("heading", { name: "Default title", level: 1 }));
  assert.ok(screen.getByRole("heading", { name: "Default subtitle", level: 2 }));
  result.rerender(
    <WebSurface>
      <MarkdownHeadingContext.Provider value={6}>
        <Markdown text={"# Embedded title\n\n## Embedded subtitle"} />
      </MarkdownHeadingContext.Provider>
    </WebSurface>,
  );
  assert.equal(screen.getAllByRole("heading", { level: 6 }).length, 2);
  assert.equal(screen.queryByRole("heading", { level: 1 }), null);
});

test("Markdown heading adjustment preserves footnote anchors and accessible labels", () => {
  render(
    <WebSurface>
      <MarkdownHeadingContext.Provider value={2}>
        <Markdown text={"A source.[^source]\n\n[^source]: Source details"} />
      </MarkdownHeadingContext.Provider>
    </WebSurface>,
  );
  const label = document.getElementById("footnote-label");
  assert.equal(label?.tagName, "H3");
  assert.equal(label?.className, "sr-only");
  assert.equal(label?.id, "footnote-label");
});
