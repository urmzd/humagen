import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { stubFetch } from "./dom.ts";
import { activeKey } from "./pane.ts";
import type { ConversationView } from "./route.ts";
import { Workspace } from "./Workspace.tsx";
import type { Scratchpad } from "./workspace-files.ts";

const words = defaultCopy.workspace.scratchpads;
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

const base = "/api/threads/thread-1/scratchpads";
const at = "2026-11-01T10:00:00Z";

/** An in-memory scratchpad server with the API's version and name rules. */
function server(writable = true) {
  const pads = new Map<string, Scratchpad>();
  let n = 0;
  const conflict = () => new Response(JSON.stringify({ error: "conflict" }), { status: 409 });
  const make = (name: string, content: string): Scratchpad => ({
    id: `sp-${++n}`,
    thread: "thread-1",
    name,
    content,
    size: content.length,
    version: 1,
    created_by: "ana",
    created_at: at,
    updated_by: "ana",
    updated_at: at,
  });
  const taken = (name: string, except?: string) =>
    [...pads.values()].some((p) => p.id !== except && p.name.toLowerCase() === name.toLowerCase());
  const routes: Parameters<typeof stubFetch>[0] = {
    [`GET ${base}`]: () => ({
      scratchpads: [...pads.values()].map((p) => ({ ...p, content: "" })),
      writable,
      limit: 50,
    }),
    [`POST ${base}`]: (body) => {
      const { name, content } = body as { name: string; content: string };
      if (taken(name)) return conflict();
      const pad = make(name, content);
      pads.set(pad.id, pad);
      return pad;
    },
  };
  return { pads, routes, make, taken, conflict };
}

function withPad(s: ReturnType<typeof server>, pad: Scratchpad) {
  s.pads.set(pad.id, pad);
  const at = `${base}/${pad.id}`;
  s.routes[`GET ${at}`] = () => s.pads.get(pad.id);
  s.routes[`PUT ${at}`] = (body) => {
    const next = body as { name: string; content: string; version: number };
    const current = s.pads.get(pad.id);
    if (!current || current.version !== next.version || s.taken(next.name, pad.id))
      return s.conflict();
    const saved = { ...current, ...next, size: next.content.length, version: current.version + 1 };
    s.pads.set(pad.id, saved);
    return saved;
  };
  s.routes[`DELETE ${at}`] = (_body, query) => {
    if (Number(query.get("version")) !== s.pads.get(pad.id)?.version) return s.conflict();
    s.pads.delete(pad.id);
    return undefined;
  };
}

function Harness({
  initial,
  views = [],
}: {
  initial: ConversationView;
  views?: ConversationView[];
}) {
  const [view, setView] = useState<ConversationView>(initial);
  const [open, setOpen] = useState(true);
  return (
    <>
      <button type="button" onClick={() => setOpen((v) => !v)}>
        Toggle Workspace
      </button>
      <Workspace
        thread="thread-1"
        open={open}
        view={view}
        onView={(next) => {
          views.push(next);
          setView(next);
        }}
        onClose={() => setOpen(false)}
      />
    </>
  );
}

const files = {
  "GET /api/threads/thread-1/files": () => ({ files: [], writable: true, retention_days: 7 }),
  "GET /api/me": () => ({ permissions: ["runs:read", "runs:write"] }),
};

test("a person creates a named scratchpad, edits it and saves from the version read", async () => {
  const s = server();
  // Created scratchpads get routes as they are made.
  const create = s.routes[`POST ${base}`];
  s.routes[`POST ${base}`] = (body, query) => {
    const out = create?.(body, query) as Scratchpad;
    if (!(out instanceof Response)) withPad(s, out);
    return out;
  };
  Object.assign(s.routes, files);
  net = stubFetch(s.routes);
  const views: ConversationView[] = [];
  render(
    <Harness initial={{ panel: "workspace", workspaceSection: "scratchpads" }} views={views} />,
  );
  assert.ok(await screen.findByText(words.empty));
  fireEvent.click(screen.getByRole("button", { name: words.new }));
  fireEvent.change(screen.getByLabelText(words.name), { target: { value: "Launch plan" } });
  fireEvent.click(screen.getByRole("button", { name: words.create }));
  await waitFor(() => assert.equal(activeKey(views.at(-1) ?? {}), "scratchpad:sp-1"));
  const editor = (await screen.findByRole("textbox", {
    name: "Edit Launch plan.md",
  })) as HTMLTextAreaElement;
  assert.equal(editor.value, "# Launch plan\n\n");
  assert.ok(screen.getByText(words.agentPath("Launch plan")));
  fireEvent.change(editor, { target: { value: "# Launch plan\n\n- Ship Tuesday\n" } });
  assert.ok(screen.getByText(words.unsaved));
  fireEvent.click(screen.getAllByRole("button", { name: /^Save/ })[0] as HTMLElement);
  await screen.findByText(words.saved);
  assert.equal(s.pads.get("sp-1")?.content, "# Launch plan\n\n- Ship Tuesday\n");
  assert.equal(s.pads.get("sp-1")?.version, 2);
  // Preview renders the Markdown.
  fireEvent.click(screen.getByRole("button", { name: words.preview }));
  assert.ok(await screen.findByText("Ship Tuesday"));
});

test("rename, a taken name, and delete after confirmation", async () => {
  const s = server();
  withPad(s, s.make("Plan", "# Plan"));
  withPad(s, s.make("Risks", "# Risks"));
  net = stubFetch({ ...files, ...s.routes });
  const views: ConversationView[] = [];
  render(
    <Harness
      initial={{ panel: "workspace", workspaceSection: "scratchpads", scratchpad: "sp-1" }}
      views={views}
    />,
  );
  await screen.findByRole("textbox", { name: "Edit Plan.md" });
  fireEvent.click(screen.getByRole("button", { name: words.rename }));
  fireEvent.change(screen.getByLabelText(words.renameTo), { target: { value: "risks" } });
  fireEvent.click(screen.getAllByRole("button", { name: words.rename }).at(-1) as HTMLElement);
  assert.ok(await screen.findByText(words.conflict));
  assert.equal(s.pads.get("sp-1")?.name, "Plan");
  fireEvent.change(screen.getByLabelText(words.renameTo), { target: { value: "Launch plan" } });
  fireEvent.click(screen.getAllByRole("button", { name: words.rename }).at(-1) as HTMLElement);
  await screen.findByRole("textbox", { name: "Edit Launch plan.md" });
  assert.equal(s.pads.get("sp-1")?.name, "Launch plan");
  fireEvent.click(screen.getByRole("button", { name: words.delete }));
  assert.ok(screen.getByText(words.deleteConfirm("Launch plan")));
  fireEvent.click(screen.getAllByRole("button", { name: words.delete }).at(-1) as HTMLElement);
  await waitFor(() => assert.equal(s.pads.has("sp-1"), false));
  assert.equal(activeKey(views.at(-1) ?? {}), "scratchpads");
});

test("a stale save keeps the draft and offers the latest", async () => {
  const s = server();
  withPad(s, s.make("Plan", "# Plan"));
  net = stubFetch({ ...files, ...s.routes });
  render(
    <Harness
      initial={{ panel: "workspace", workspaceSection: "scratchpads", scratchpad: "sp-1" }}
    />,
  );
  const editor = (await screen.findByRole("textbox", {
    name: "Edit Plan.md",
  })) as HTMLTextAreaElement;
  // Someone else saved meanwhile.
  const pad = s.pads.get("sp-1") as Scratchpad;
  s.pads.set("sp-1", { ...pad, content: "# Theirs", version: 2 });
  fireEvent.change(editor, { target: { value: "# Mine" } });
  fireEvent.click(screen.getAllByRole("button", { name: /^Save/ })[0] as HTMLElement);
  assert.ok(await screen.findByText(words.conflict));
  assert.equal(editor.value, "# Mine");
  fireEvent.click(screen.getByRole("button", { name: words.reload }));
  await waitFor(() =>
    assert.equal(
      (screen.getByRole("textbox", { name: "Edit Plan.md" }) as HTMLTextAreaElement).value,
      "# Theirs",
    ),
  );
});

test("drafts survive closing the panel and switching sections, and guard navigation", async () => {
  const s = server();
  withPad(s, s.make("Plan", "# Plan"));
  net = stubFetch({
    ...files,
    ...s.routes,
    "GET /api/threads/thread-1/sandboxes": () => ({
      session: { isolated: false, files: 0, bytes: 0, retention_days: 7, writable: true },
      runs: [],
      more: false,
    }),
  });
  render(
    <Harness
      initial={{ panel: "workspace", workspaceSection: "scratchpads", scratchpad: "sp-1" }}
    />,
  );
  const editor = (await screen.findByRole("textbox", {
    name: "Edit Plan.md",
  })) as HTMLTextAreaElement;
  fireEvent.change(editor, { target: { value: "# Pending" } });
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.workspace.pane.open }));
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.workspace.pane.pick.sandboxes }));
  await screen.findByText(defaultCopy.workspace.sessionSandbox);
  fireEvent.click(screen.getByRole("button", { name: "Toggle Workspace" }));
  fireEvent.click(screen.getByRole("button", { name: "Toggle Workspace" }));
  // The scratchpad's tab is named for it.
  fireEvent.click(await screen.findByRole("tab", { name: "Plan" }));
  await waitFor(() =>
    assert.equal(
      (screen.getByRole("textbox", { name: "Edit Plan.md" }) as HTMLTextAreaElement).value,
      "# Pending",
    ),
  );
  const confirm = window.confirm;
  window.confirm = () => false;
  try {
    const event = new Event("humagen:before-navigate", { cancelable: true });
    assert.equal(window.dispatchEvent(event), false);
  } finally {
    window.confirm = confirm;
  }
});

test("read-only scratchpads say why and offer no changes", async () => {
  const s = server(false);
  withPad(s, s.make("Plan", "# Plan"));
  net = stubFetch({
    ...files,
    ...s.routes,
    [`GET ${base}`]: () => ({
      scratchpads: [{ ...s.pads.get("sp-1"), content: "" }],
      writable: false,
      reason: "this conversation is archived or read only",
      limit: 50,
    }),
  });
  render(<Harness initial={{ panel: "workspace", workspaceSection: "scratchpads" }} />);
  assert.ok(await screen.findByText("this conversation is archived or read only"));
  assert.equal(
    (screen.getByRole("button", { name: words.new }) as HTMLButtonElement).disabled,
    true,
  );
  fireEvent.click(screen.getByRole("button", { name: /Plan/ }));
  const editor = (await screen.findByRole("textbox", {
    name: "Edit Plan.md",
  })) as HTMLTextAreaElement;
  assert.equal(editor.readOnly, true);
  assert.equal(screen.queryByRole("button", { name: words.rename }), null);
  assert.equal(screen.queryByRole("button", { name: words.delete }), null);
});
