import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { ActionId, type QueuedMessage, type Turn } from "@humagen/core";
import { Rating } from "@humagen/render-web";
import { useQueue } from "@humagen/web";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { ProjectView } from "./api.ts";
import { Composer, type ComposerSeed } from "./Composer.tsx";
import { stubFetch } from "./dom.ts";
import { RunFeedbackHost } from "./RunFeedback.tsx";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});
const turn = {
  id: "persisted-feedback",
  agent: { name: "helper" },
  status: "done",
  text: "Answer",
  tasks: [],
  artifacts: [],
  citations: [],
} as unknown as Turn;
function Ratings({ id = turn.id }: { id?: string }) {
  return (
    <RunFeedbackHost turn={{ ...turn, id }}>
      {() => (
        <>
          <Rating
            up={{ id: ActionId.feedback, payload: { turnId: id, target: "up" } }}
            down={{ id: ActionId.feedback, payload: { turnId: id, target: "down" } }}
          />
          <Rating
            labels={{ up: "Finding helpful", down: "Finding not helpful" }}
            up={{ id: ActionId.feedback, payload: { turnId: id, target: "up", findingId: "f1" } }}
            down={{
              id: ActionId.feedback,
              payload: { turnId: id, target: "down", findingId: "f1" },
            }}
          />
        </>
      )}
    </RunFeedbackHost>
  );
}

test("session UX feedback reloads, offers positive reasons, persists comments and clears independently", async () => {
  const records = new Map<string, unknown>();
  net = stubFetch({
    "GET /api/runs/persisted-feedback/feedback": () => ({ feedback: [...records.values()] }),
    "PUT /api/runs/persisted-feedback/feedback": (body) => {
      const record = { ...(body as object), mine: true, run: turn.id };
      records.set((body as { finding_id?: string }).finding_id || "", record);
      return record;
    },
    "DELETE /api/runs/persisted-feedback/feedback": (_, q) => {
      records.delete(q.get("finding_id") || "");
    },
  });
  const mounted = render(<Ratings />);
  fireEvent.click(screen.getByRole("button", { name: "Helpful" }));
  const dialog = await screen.findByRole("dialog", { name: "What was helpful?" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Accurate" }));
  fireEvent.change(within(dialog).getByRole("textbox"), { target: { value: "Useful evidence" } });
  fireEvent.click(within(dialog).getByRole("button", { name: "Send" }));
  await waitFor(() =>
    assert.equal((records.get("") as { comment?: string })?.comment, "Useful evidence"),
  );
  assert.equal(document.activeElement?.getAttribute("aria-label"), "Helpful");
  fireEvent.click(screen.getByRole("button", { name: "Finding not helpful" }));
  fireEvent.click(await screen.findByRole("button", { name: "Skip" }));
  await waitFor(() => assert.ok(records.has("f1")));
  mounted.unmount();
  render(<Ratings />);
  await waitFor(() =>
    assert.equal(
      screen.getByRole("button", { name: "Helpful" }).getAttribute("aria-pressed"),
      "true",
    ),
  );
  assert.equal(
    screen.getByRole("button", { name: "Finding not helpful" }).getAttribute("aria-pressed"),
    "true",
  );
  fireEvent.click(screen.getByRole("button", { name: "Finding not helpful" }));
  await waitFor(() => assert.equal(records.has("f1"), false));
  assert.ok(records.has(""));
});

test("session UX feedback failure restores the saved selection and names the failure", async () => {
  net = stubFetch({
    "GET /api/runs/failed-feedback/feedback": () => ({ feedback: [] }),
    "PUT /api/runs/failed-feedback/feedback": () =>
      new Response(JSON.stringify({ error: "Storage unavailable" }), { status: 503 }),
  });
  render(<Ratings id="failed-feedback" />);
  fireEvent.click(screen.getByRole("button", { name: "Helpful" }));
  fireEvent.click(await screen.findByRole("button", { name: "Skip" }));
  await screen.findByText("Feedback was not saved: Storage unavailable");
  assert.equal(
    screen.getByRole("button", { name: "Helpful" }).getAttribute("aria-pressed"),
    "false",
  );
});

test("session UX starter appends and replacing or removing a reply preserves the draft", () => {
  const sent: string[] = [];
  function Box({ seed }: { seed?: ComposerSeed }) {
    const queue = useQueue({
      onStart: (message) => sent.push(message.text),
      onInterrupt: () => {},
    });
    return <Composer queue={queue} placeholder="old" seed={seed} />;
  }
  const view = render(<Box />);
  const box = screen.getByRole("textbox", { name: "Message" }) as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: "Existing draft" } });
  view.rerender(<Box seed={{ text: "Review the change", mode: "append" }} />);
  assert.equal(box.value, "Existing draft\n\nReview the change");
  view.rerender(<Box seed={{ quote: { label: "first answer", url: "/sessions/one" } }} />);
  view.rerender(<Box seed={{ quote: { label: "second answer", url: "/sessions/two" } }} />);
  assert.equal(document.querySelectorAll("[data-reply-quote]").length, 1);
  fireEvent.click(screen.getByRole("button", { name: "Remove reply" }));
  assert.equal(box.value, "Existing draft\n\nReview the change");
  assert.equal(document.activeElement, box);
  fireEvent.keyDown(box, { key: "Enter" });
  assert.deepEqual(sent, ["Existing draft\n\nReview the change"]);
});

test("session UX choosing Start fresh preserves the draft and its queued routing choice", async () => {
  const sent: QueuedMessage[] = [];
  let finish: (() => void) | undefined;
  function Box() {
    const queue = useQueue({ onStart: (message) => sent.push(message), onInterrupt() {} });
    finish = () => queue.turnEnded("done");
    return (
      <Composer
        queue={queue}
        placeholder="Message"
        sessionAgents={["reviewer"]}
        tags={{
          label: "Agents",
          options: [{ kind: "agent", token: "reviewer", label: "Reviewer" }],
        }}
      />
    );
  }
  render(<Box />);
  const box = screen.getByRole("combobox", { name: "Message" }) as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: "First request" } });
  fireEvent.keyDown(box, { key: "Enter" });
  fireEvent.change(box, { target: { value: "Compare @rev", selectionStart: 12 } });
  fireEvent.keyDown(box, { key: "Enter" });
  const chooser = await screen.findByRole("dialog", { name: "Session for @reviewer" });
  assert.equal(sent.length, 1);
  fireEvent.click(within(chooser).getByRole("button", { name: "Start fresh" }));
  assert.equal(box.value, "Compare @reviewer ");
  assert.equal(document.activeElement, box);
  fireEvent.keyDown(box, { key: "Enter" });
  assert.equal(sent.length, 1);
  finish?.();
  await waitFor(() => assert.equal(sent.length, 2));
  assert.deepEqual(sent[1]?.pick, { agent: "reviewer", fresh: true });
});

test("session UX mention from Plus offers Continue and clearing the mention drops its routing choice", async () => {
  const sent: QueuedMessage[] = [];
  function Box({ seed }: { seed?: ComposerSeed }) {
    const queue = useQueue({ onStart: (message) => sent.push(message), onInterrupt() {} });
    return (
      <Composer queue={queue} placeholder="Message" seed={seed} sessionAgents={["reviewer"]} />
    );
  }
  const view = render(<Box />);
  view.rerender(<Box seed={{ mention: "reviewer" }} />);
  const chooser = await screen.findByRole("dialog", { name: "Session for @reviewer" });
  fireEvent.click(within(chooser).getByRole("button", { name: "Continue" }));
  const box = screen.getByRole("textbox", { name: "Message" }) as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: "A different request" } });
  fireEvent.keyDown(box, { key: "Enter" });
  assert.equal(sent[0]?.pick, undefined);
});

test("session details loads linked children, summarizes and validates a context link before saving", async () => {
  const { SessionRelations } = await import("./SessionDetails.tsx");
  const { stubEventSource } = await import("./dom.ts");
  const restore = stubEventSource();
  let linked = false;
  net = stubFetch({
    "GET /api/sessions/s_details": (_body, query) => ({
      children: [
        { id: "s_child", agent: "reviewer", title: "Review dependencies", status: "completed" },
      ],
      links: linked ? [{ target: "s_target", kind: "context" }] : [],
      ...(query.get("summary") === "true"
        ? { summary: "The review completed with two findings." }
        : {}),
    }),
    "POST /api/sessions/s_details/links": (body) => {
      assert.deepEqual(body, { target: "s_target", kind: "context" });
      linked = true;
    },
  });
  try {
    render(<SessionRelations id="s_details" canWrite />);
    const child = await screen.findByRole("link", { name: /Review dependencies/ });
    assert.equal(child.getAttribute("href"), "/sessions/s_child");
    fireEvent.click(screen.getByRole("button", { name: "Catch me up" }));
    await screen.findByText("The review completed with two findings.");
    fireEvent.change(screen.getByRole("textbox", { name: "Link a session" }), {
      target: { value: "not a session" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Link session" }));
    await screen.findByText("Paste a session link from this workspace.");
    assert.equal(linked, false);
    fireEvent.change(screen.getByRole("textbox", { name: "Link a session" }), {
      target: { value: "/sessions/s_target" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Link session" }));
    const context = await screen.findByRole("link", { name: "context" });
    assert.equal(context.getAttribute("href"), "/sessions/s_target");
    assert.equal(
      (screen.getByRole("textbox", { name: "Link a session" }) as HTMLInputElement).value,
      "",
    );
  } finally {
    cleanup();
    restore();
  }
});

test("session UX header rename preserves routing and deletion waits for explicit confirmation", async () => {
  const { SessionActions } = await import("./SessionActions.tsx");
  const current = {
    id: "s_controls",
    title: "Before",
    version: 3,
    visibility: "private" as const,
    project: "",
    channel: "general",
    default: "reviewer",
    agents: ["reviewer"],
    mode: "join" as const,
    participants: [],
    created_by: "ada",
    created_at: "",
    updated_at: "",
  };
  const writes: unknown[] = [];
  let updated = "";
  net = stubFetch({
    "GET /api/marks": () => ({ archived: [], read: {}, tips: [] }),
    "GET /api/threads/s_controls": () => ({ thread: current }),
    "PUT /api/threads/s_controls": (body) => {
      writes.push(body);
      return { ...current, ...(body as object) };
    },
    "POST /api/threads/s_controls/archive": (body) => {
      writes.push(body);
      return current;
    },
  });
  render(
    <SessionActions
      thread={current}
      canManage
      onChanged={() => {}}
      onUpdated={(thread) => {
        updated = thread.title ?? "";
      }}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Session actions" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Rename" }));
  fireEvent.change(await screen.findByRole("textbox", { name: "Name" }), {
    target: { value: "After" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save name" }));
  await waitFor(() => assert.equal(updated, "After"));
  assert.deepEqual(writes[0], {
    version: 3,
    title: "After",
    visibility: "private",
    default: "reviewer",
    agents: ["reviewer"],
    mode: "join",
  });
  fireEvent.click(screen.getByRole("button", { name: "Session actions" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Delete" }));
  const dialog = await screen.findByRole("dialog", { name: "Delete session" });
  assert.equal(writes.length, 1);
  assert.match(dialog.textContent ?? "", /Nothing is deleted until an administrator approves/);
  fireEvent.click(within(dialog).getByRole("button", { name: "Request deletion" }));
  await waitFor(() => assert.deepEqual(writes[1], { delete_now: true }));
});

test("session UX header moves a session to another project from its menu, not the composer", async () => {
  const { SessionActions } = await import("./SessionActions.tsx");
  const current = {
    id: "s_move",
    title: "Pricing",
    version: 2,
    visibility: "private" as const,
    project: "personal-ada",
    channel: "general",
    agents: [],
    participants: [],
    created_by: "ada",
    created_at: "",
    updated_at: "",
  };
  const moves: unknown[] = [];
  let movedTo = "";
  net = stubFetch({
    "GET /api/marks": () => ({ archived: [], read: {}, tips: [] }),
    "GET /api/threads/s_move": () => ({ thread: current, turns: [], next: null }),
    "PUT /api/threads/s_move": (body) => {
      moves.push(body);
      return { ...current, project: (body as { project: string }).project };
    },
  });
  const launch = {
    name: "Launch",
    kind: "team",
    title: "Launch",
    path: [{ name: "Launch", kind: "team" }],
  } as unknown as ProjectView;
  render(
    <SessionActions
      thread={current}
      canManage
      projects={[launch]}
      personalProject="personal-ada"
      onChanged={() => {}}
      onUpdated={(thread) => {
        movedTo = thread.project;
      }}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Session actions" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Move to Project…" }));
  const dialog = await screen.findByRole("dialog", { name: "Move to project" });
  fireEvent.click(await within(dialog).findByRole("button", { name: /^Launch/ }));
  await waitFor(() => assert.equal(movedTo, "Launch"));
  assert.equal(moves.length, 1);
  assert.equal((moves[0] as { project: string }).project, "Launch");
});

test("session UX header archive updates sidebar marks and Undo restores only my archive", async () => {
  const { SessionActions } = await import("./SessionActions.tsx");
  const { useMarks } = await import("./marks.ts");
  const thread = {
    id: "s_archive",
    version: 1,
    visibility: "private" as const,
    project: "",
    channel: "general",
    participants: [],
    created_by: "ada",
    created_at: "",
    updated_at: "",
  };
  let archived = false;
  const writes: unknown[] = [];
  net = stubFetch({
    "GET /api/marks": () => ({ archived: archived ? [thread.id] : [], read: {}, tips: [] }),
    "PATCH /api/threads/s_archive/marks": (body) => {
      writes.push(body);
      archived = (body as { archived: boolean }).archived;
      return { archived: archived ? [thread.id] : [], read: {}, tips: [] };
    },
  });
  function Witness() {
    const { marks } = useMarks(true);
    return (
      <output>
        {marks.archived.includes(thread.id) ? "Archived in sidebar" : "Visible in sidebar"}
      </output>
    );
  }
  render(
    <>
      <Witness />
      <SessionActions thread={thread} canManage={false} onChanged={() => {}} onUpdated={() => {}} />
    </>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Session actions" }));
  const archive = await screen.findByRole("menuitem", { name: "Archive" });
  assert.equal(screen.queryByRole("menuitem", { name: "Delete" }), null);
  fireEvent.click(archive);
  await screen.findByText("Archived in sidebar");
  fireEvent.click(screen.getByRole("button", { name: "Undo" }));
  await screen.findByText("Visible in sidebar");
  assert.deepEqual(writes, [{ archived: true }, { archived: false }]);
});

test("session UX alerts read without resolving, offer governed fix review, and keep Undo after refresh", async () => {
  const { SessionAlerts, useSessionAlerts } = await import("./SessionAlerts.tsx");
  const { useState } = await import("react");
  const alert = {
    id: "alert-fix",
    session: "s_controls",
    message: "r_failed",
    action: "retry",
    agent: "reviewer",
    title: "Review failed",
    body: "Service was unavailable",
    state: "open" as const,
    priority: "urgent" as const,
    kind: "run_failed" as const,
    severity: "warning" as const,
    rule: "",
    at: "2026-11-07T12:00:00Z",
  };
  let state = "open";
  const writes: unknown[] = [];
  const fixes: unknown[] = [];
  let opened = "";
  net = stubFetch({
    "GET /api/alerts": (_, query) => {
      assert.equal(query.get("session"), "s_controls");
      return { alerts: state === "open" ? [alert] : [] };
    },
    "PATCH /api/alerts/alert-fix": (body) => {
      writes.push(body);
      state = (body as { state: string }).state;
      return { ...alert, state };
    },
    "POST /api/sessions": (body) => {
      fixes.push(body);
      return { id: "s_fix" };
    },
  });
  function View() {
    const [tick, setTick] = useState(0);
    const attention = useSessionAlerts("s_controls", true, tick, () => setTick((v) => v + 1));
    return (
      <>
        <SessionAlerts
          alerts={attention.rows}
          attention={attention}
          canWrite
          agents={["reviewer"]}
          onOpen={(id) => {
            opened = id;
          }}
        />
        {attention.undo && (
          <button onClick={() => void attention.change(attention.undo!, "open")}>Undo alert</button>
        )}
      </>
    );
  }
  render(<View />);
  const card = await screen.findByRole("region", { name: "Alert: Review failed" });
  assert.equal(writes.length, 0);
  assert.equal(card.getAttribute("data-alert-message"), "r_failed");
  assert.equal(
    within(card).getByRole("link", { name: "Review failed answer" }).getAttribute("href"),
    "/sessions/s_controls?message=r_failed",
  );
  fireEvent.click(within(card).getByRole("button", { name: "Fix with @reviewer" }));
  const dialog = await screen.findByRole("dialog", { name: "Start a fix session" });
  assert.equal(fixes.length, 0);
  fireEvent.change(within(dialog).getByRole("textbox", { name: "Request" }), {
    target: { value: "Investigate before editing." },
  });
  fireEvent.click(within(dialog).getByRole("button", { name: "Start fix" }));
  await waitFor(() => assert.equal(opened, "s_fix"));
  assert.deepEqual(fixes, [
    { source_alert: "alert-fix", agent: "reviewer", text: "Investigate before editing." },
  ]);
  fireEvent.click(within(card).getByRole("button", { name: "Resolve" }));
  const undo = await screen.findByRole("button", { name: "Undo alert" });
  await waitFor(() =>
    assert.equal(screen.queryByRole("region", { name: "Alert: Review failed" }), null),
  );
  fireEvent.click(undo);
  await screen.findByRole("region", { name: "Alert: Review failed" });
  assert.deepEqual(writes, [{ state: "resolved" }, { state: "open" }]);
});

test("session UX canonical sharing uses project access without a project channel picker", async () => {
  const { ShareDialog } = await import("./Share.tsx");
  const thread = {
    id: "s_share",
    version: 1,
    visibility: "private" as const,
    project: "Launch",
    channel: "general",
    participants: [],
    created_by: "ada",
    created_at: "",
    updated_at: "",
  };
  let channelReads = 0;
  net = stubFetch({
    "GET /api/me": () => ({ permissions: ["runs:write"], principal: { subject: "ada" } }),
    "GET /api/projects/Launch": () => ({ project: { access: { role: "owner" } } }),
    "GET /api/projects/Launch/channels": () => {
      channelReads++;
      return { channels: [] };
    },
    "GET /api/threads/s_share/placement": () => ({
      thread: "s_share",
      project: "Launch",
      channel: "general",
      audience: "Launch members",
      runs: 0,
      workspace_files: 0,
      token: "consent",
      exclusions: [],
    }),
  });
  render(
    <ShareDialog
      thread={thread}
      hasRuns={false}
      projects={[]}
      workspaceName="Workspace"
      me="ada"
      onShared={() => {}}
      onClose={() => {}}
    />,
  );
  await waitFor(() =>
    assert.equal(
      (screen.getByRole("button", { name: "Share" }) as HTMLButtonElement).disabled,
      false,
    ),
  );
  assert.equal(screen.queryByLabelText("Channel"), null);
  assert.equal(channelReads, 0);
  assert.match(document.body.textContent ?? "", /project's members/);
});

test("session UX synthetic sessions show alerts before any message and load an old anchor with one bounded read", async () => {
  const { Chat } = await import("./Chat.tsx");
  const { stubEventSource } = await import("./dom.ts");
  const restoreEvents = stubEventSource();
  const thread = {
    id: "s_empty",
    version: 1,
    visibility: "private" as const,
    title: "Needs attention",
    project: "",
    channel: "general",
    participants: [],
    created_by: "ada",
    created_at: "",
    updated_at: "",
  };
  const reads: string[] = [];
  net = stubFetch({
    "GET /api/sessions/s_empty": (_, query) => {
      reads.push(query.toString());
      const message = {
        id: "old-message",
        thread: thread.id,
        by: "ada",
        name: "Ada",
        text: "Earlier decision",
        mentions: [],
        via: "console",
        at: "2026-10-01T00:00:00Z",
      };
      return {
        id: thread.id,
        agent: "reviewer",
        thread,
        parent: "system:alert",
        project: "",
        title: thread.title,
        status: "waiting",
        messages:
          query.get("around") === message.id
            ? [
                {
                  id: message.id,
                  session: thread.id,
                  author: { kind: "person", name: "Ada" },
                  status: "done",
                  parts: [{ type: "text", text: message.text }],
                  at: message.at,
                  message,
                },
              ]
            : [],
        children: [],
        links: [],
        people: [],
        cursor: "cursor",
      };
    },
    "GET /api/alerts": () => ({
      alerts: [
        {
          id: "a",
          session: thread.id,
          title: "A decision is needed",
          rule: "",
          kind: "session",
          priority: "normal",
          state: "open",
          at: "",
        },
      ],
    }),
    "GET /api/projects/~/agents": () => ({ available: { all: false, names: [] } }),
    "GET /api/skills": () => ({ skills: [] }),
    "GET /api/mentions": () => ({ kinds: [] }),
    "GET /api/people": () => ({ people: [] }),
    "GET /api/approvals/calls": () => ({ messages: [] }),
  });
  const props = {
    threadId: thread.id,
    me: "ada",
    summary: undefined,
    tick: 0,
    agents: [],
    projects: [],
    canRun: false,
    canCreateProject: false,
    greeting: "",
    alertAccess: { write: false },
    onThread() {},
    onChanged() {},
    onProjectCreated() {},
  };
  try {
    const view = render(<Chat {...props} />);
    await screen.findByRole("region", { name: "Alert: A decision is needed" });
    assert.ok(screen.getByRole("button", { name: "Details" }));
    view.rerender(<Chat {...props} focus="old-message" />);
    await screen.findByText("Earlier decision");
    assert.equal(reads.filter((query) => query === "around=old-message").length, 1);
    assert.equal(
      reads.some((query) => query.includes("before=")),
      false,
    );
  } finally {
    cleanup();
    restoreEvents();
  }
});

test("an empty composer says its own words, never the default agent's token, and a mention names who it asks", () => {
  function Box() {
    const queue = useQueue({ onStart: () => {}, onInterrupt() {} });
    return (
      <Composer
        queue={queue}
        placeholder="Reply"
        tags={{
          label: "Agents",
          defaultAgent: "*",
          options: [{ kind: "agent", token: "lead", label: "Lead" }],
        }}
      />
    );
  }
  render(<Box />);
  const box = screen.getByRole("combobox", { name: "Message" }) as HTMLTextAreaElement;
  assert.equal(box.getAttribute("placeholder"), "Reply");
  fireEvent.change(box, { target: { value: "@lead " } });
  assert.equal(box.getAttribute("placeholder"), "Message @lead");
  cleanup();
});
