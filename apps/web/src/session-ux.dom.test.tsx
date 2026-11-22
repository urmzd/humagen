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
  const box = screen.getByRole("textbox", { name: "Message" }) as HTMLTextAreaElement;
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
    assert.equal(child.getAttribute("href"), "/messages/s_child");
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
    assert.equal(context.getAttribute("href"), "/messages/s_target");
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
    within(card).getByRole("link", { name: "Review Failed Answer" }).getAttribute("href"),
    "/messages/s_controls?message=r_failed",
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
  fireEvent.click(within(card).getByRole("button", { name: "Mark Done" }));
  const undo = await screen.findByRole("button", { name: "Undo alert" });
  await waitFor(() =>
    assert.equal(screen.queryByRole("region", { name: "Alert: Review failed" }), null),
  );
  fireEvent.click(undo);
  await screen.findByRole("region", { name: "Alert: Review failed" });
  assert.deepEqual(writes, [{ state: "resolved" }, { state: "open" }]);
});

test("session UX an alert beside its own message keeps its actions and drops the link back to the session", async () => {
  const { SessionAlerts, useSessionAlerts } = await import("./SessionAlerts.tsx");
  const alert = {
    id: "alert-here",
    session: "s_here",
    message: "m_here",
    title: "Confirm the owner",
    state: "open" as const,
    priority: "normal" as const,
    kind: "session" as const,
    rule: "",
    at: "2026-11-07T12:00:00Z",
  };
  const reconnect = {
    ...alert,
    id: "alert-reconnect",
    title: "Reconnect GitHub",
    action: "reconnect",
  };
  net = stubFetch({ "GET /api/alerts": () => ({ alerts: [alert, reconnect] }) });
  function View({ beside, canWrite = true }: { beside: boolean; canWrite?: boolean }) {
    const attention = useSessionAlerts("s_here", true, 0, () => {});
    return (
      <SessionAlerts
        alerts={attention.rows}
        attention={attention}
        canWrite={canWrite}
        agents={[]}
        beside={beside}
      />
    );
  }
  const view = render(<View beside={false} />);
  const above = await screen.findByRole("region", { name: "Alert: Confirm the owner" });
  assert.equal(
    within(above).getByRole("link", { name: "View" }).getAttribute("href"),
    "/messages/s_here?message=m_here",
  );
  view.rerender(<View beside />);
  const card = await screen.findByRole("region", { name: "Alert: Confirm the owner" });
  assert.equal(within(card).queryByRole("link"), null);
  assert.ok(within(card).getByRole("button", { name: "Mark Done" }));
  // Snooze is a lesser action: it waits in the card's More menu.
  assert.equal(within(card).queryByRole("button", { name: "Snooze 1h" }), null);
  fireEvent.click(within(card).getByRole("button", { name: "More alert actions" }));
  assert.ok(within(card).getByRole("menuitem", { name: "Snooze 1h" }));
  // A link that leads somewhere else stays.
  const other = screen.getByRole("region", { name: "Alert: Reconnect GitHub" });
  assert.equal(
    within(other).getByRole("link", { name: "Open Connections" }).getAttribute("href"),
    "/connections",
  );
  // A reader who may not change alerts is left no empty row of actions.
  view.rerender(<View beside canWrite={false} />);
  const read = await screen.findByRole("region", { name: "Alert: Confirm the owner" });
  assert.equal(within(read).queryByRole("link"), null);
  assert.equal(within(read).queryByRole("button"), null);
  assert.equal(read.lastElementChild?.childElementCount, 0);
  assert.match(read.lastElementChild?.className ?? "", /empty:hidden/);
});

test("session UX a health alert is viewed on its answer and reviewed in a session of its own", async () => {
  const { SessionAlerts, useSessionAlerts } = await import("./SessionAlerts.tsx");
  const health = {
    id: "al-health",
    session: "s_origin",
    message: "r0b65",
    run_id: "r0b65",
    action: "review",
    agent: "humagen",
    title: "humagen needs review: tool_errors",
    state: "open" as const,
    priority: "normal" as const,
    kind: "eval" as const,
    rule: "",
    at: "2026-11-07T12:00:00Z",
  };
  // One raised before kept a session of its own and named only its run.
  const older = {
    ...health,
    id: "al-older",
    session: "s_health",
    message: undefined,
    run_id: "r0123456789abcdef0123456789abcdef",
    title: "lead needs review: tool_errors",
  };
  const reviews: string[] = [];
  let opened = "";
  net = stubFetch({
    "GET /api/alerts": () => ({ alerts: [health, older] }),
    "POST /api/alerts/al-health/review": () => {
      reviews.push("al-health");
      return { id: "s_review" };
    },
  });
  function View({ beside }: { beside: boolean }) {
    const attention = useSessionAlerts("s_origin", true, 0, () => {});
    return (
      <SessionAlerts
        alerts={attention.rows}
        attention={attention}
        canWrite
        agents={["humagen"]}
        beside={beside}
        onOpen={(id) => {
          opened = id;
        }}
      />
    );
  }
  const view = render(<View beside={false} />);
  const above = await screen.findByRole("region", { name: `Alert: ${health.title}` });
  assert.equal(
    within(above).getByRole("link", { name: "View" }).getAttribute("href"),
    "/messages/s_origin?message=r0b65",
  );
  // A normal priority says nothing; there is one control for each job.
  assert.equal(within(above).queryByText("Normal"), null);
  assert.deepEqual(
    within(above)
      .getAllByRole("button")
      .map((b) => b.getAttribute("aria-label") ?? b.textContent),
    ["Review", "Mark Done", "More alert actions"],
  );
  const old = screen.getByRole("region", { name: `Alert: ${older.title}` });
  assert.equal(
    within(old).getByRole("link", { name: "View" }).getAttribute("href"),
    `/messages/${older.run_id}`,
  );
  // Once reviewed, the alert is one quiet line: open the review, or mark it done.
  const reviewed = {
    ...health,
    id: "al-reviewed",
    title: "lead needs review: cost",
    review: "s_done",
    severity: "warning" as const,
  };
  view.rerender(
    <SessionAlerts
      alerts={[reviewed]}
      attention={{
        rows: [],
        problem: undefined,
        busy: false,
        undo: undefined,
        refresh() {},
        change: async () => {},
      }}
      canWrite
      agents={["humagen"]}
      beside
      onOpen={(id) => {
        opened = id;
      }}
    />,
  );
  const line = await screen.findByRole("region", { name: `Alert: ${reviewed.title}` });
  assert.ok(line.hasAttribute("data-reviewed"));
  assert.deepEqual(
    within(line)
      .getAllByRole("button")
      .map((b) => b.textContent),
    ["Open Review", "Mark Done"],
  );
  fireEvent.click(within(line).getByRole("button", { name: "Open Review" }));
  assert.equal(opened, "s_done");
  opened = "";

  // Beside its answer, the card asks about it: Review opens the recap session.
  view.rerender(<View beside />);
  const card = await screen.findByRole("region", { name: `Alert: ${health.title}` });
  assert.equal(within(card).queryByRole("link"), null);
  assert.equal(reviews.length, 0);
  fireEvent.click(within(card).getByRole("button", { name: "Review" }));
  await waitFor(() => assert.equal(opened, "s_review"));
  assert.deepEqual(reviews, ["al-health"]);
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

test("session UX a review session shows its alert's document and never the alert", async () => {
  const { Chat } = await import("./Chat.tsx");
  const { stubEventSource } = await import("./dom.ts");
  const restoreEvents = stubEventSource();
  const thread = {
    id: "s_review",
    version: 1,
    visibility: "private" as const,
    title: "humagen needs review: tool_errors",
    project: "",
    channel: "general",
    participants: [],
    created_by: "ada",
    created_at: "",
    updated_at: "",
    session: { agent: "humagen", parent: "s_origin", label: "review-al-1", source_alert: "al-1" },
  };
  const message = {
    id: "m-1",
    thread: thread.id,
    by: "ada",
    name: "Ada",
    text: "humagen needs review: tool_errors",
    mentions: [],
    via: "console",
    at: "2026-11-22T00:00:00Z",
    attachments: [
      { digest: "sha256:1", media_type: "text/markdown", size: 120, name: "Alert context.md" },
    ],
  };
  const opened: string[] = [];
  net = stubFetch({
    "GET /api/sessions/s_review": () => ({
      id: thread.id,
      agent: "humagen",
      thread,
      parent: "s_origin",
      project: "",
      title: thread.title,
      status: "done",
      messages: [
        {
          id: message.id,
          session: thread.id,
          author: { kind: "person", name: "Ada" },
          status: "done",
          parts: [{ type: "text", text: message.text }],
          at: message.at,
          message,
        },
      ],
      children: [],
      links: [],
      people: [],
      cursor: "cursor",
    }),
    "GET /api/sessions/s_review/messages/m-1/attachments/0": () => {
      opened.push("0");
      return {
        name: "Alert context.md",
        media_type: "text/markdown",
        text: "# humagen needs review: tool_errors\n\n## Tool errors\n\n- runs_summary failed 2 times",
      };
    },
    // Even were the alert listed here, the session it is reviewed in does not show it.
    "GET /api/alerts": () => ({
      alerts: [
        {
          id: "al-1",
          session: thread.id,
          title: "humagen needs review: tool_errors",
          rule: "",
          kind: "eval",
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
  try {
    render(
      <Chat
        threadId={thread.id}
        me="ada"
        summary={undefined}
        tick={0}
        agents={[]}
        projects={[]}
        canRun={false}
        canCreateProject={false}
        greeting=""
        alertAccess={{ write: true }}
        onThread={() => {}}
        onChanged={() => {}}
        onProjectCreated={() => {}}
      />,
    );
    const card = await screen.findByRole("button", { name: "Open Alert context.md" });
    assert.equal(screen.queryByRole("region", { name: /^Alert: / }), null);
    assert.equal(opened.length, 0);
    fireEvent.click(card);
    const dialog = await screen.findByRole("dialog", { name: "Alert context.md" });
    await within(dialog).findByText("runs_summary failed 2 times");
    assert.deepEqual(opened, ["0"]);
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
  const box = screen.getByRole("textbox", { name: "Message" }) as HTMLTextAreaElement;
  assert.equal(box.getAttribute("placeholder"), "Reply");
  fireEvent.change(box, { target: { value: "@lead " } });
  assert.equal(box.getAttribute("placeholder"), "Message @lead");
  cleanup();
});

test("only a projected approval with its pending inline decision is hidden", async () => {
  const { duplicatesInlineApproval } = await import("./SessionAlerts.tsx");
  const alert = { kind: "approval", action: "approve", run_id: "r1" } as never;
  const run = {
    id: "r1",
    agent: "helper",
    status: "parked",
    created_at: "2026-01-01",
    updated_at: "2026-01-01",
    question: { id: "q1", kind: "approval", prompt: "Write file?", tool: { name: "file_write" } },
  };
  const turns = [{ kind: "run", run }] as never;
  assert.equal(duplicatesInlineApproval(alert, turns), true);
  assert.equal(
    duplicatesInlineApproval({ ...(alert as object), kind: "alert" } as never, turns),
    false,
  );
  assert.equal(
    duplicatesInlineApproval({ ...(alert as object), run_id: "other" } as never, turns),
    false,
  );
  assert.equal(
    duplicatesInlineApproval(alert, [
      { kind: "run", run: { ...run, status: "completed" } },
    ] as never),
    false,
  );
  assert.equal(
    duplicatesInlineApproval(alert, [
      { kind: "run", run: { ...run, question: { id: "q1", kind: "reply", prompt: "Question?" } } },
    ] as never),
    false,
  );
});

// A parked run as the console draws it: the adapter's turn in AgentMessage, inside the run's article.
async function parkedRun(ids: string[]) {
  const { WebSurface } = await import("@humagen/render-web");
  const { AgentMessage } = await import("@humagen/web");
  const { turnOf } = await import("./adapter.ts");
  const asks = ids.map((id) => ({
    id,
    kind: "approval",
    prompt: `Write file ${id}?`,
    tool: { name: "file_write", call_id: `call-${id}` },
  }));
  const run = {
    id: "r1",
    agent: "helper",
    status: "parked",
    created_at: "2026-01-01",
    updated_at: "2026-01-01",
    ...(asks.length > 1 ? { questions: asks } : { question: asks[0] }),
  };
  return (
    <WebSurface onAction={() => {}}>
      <article data-turn-id={run.id}>
        <AgentMessage turn={turnOf(run as never)} />
      </article>
    </WebSurface>
  );
}

test("initial approval positioning does not override a message link or later scrolling", async () => {
  const { Thread } = await import("@humagen/web");
  const oldScroll = HTMLElement.prototype.scrollTo;
  const calls: ScrollToOptions[] = [];
  HTMLElement.prototype.scrollTo = (options) => {
    if (typeof options === "object") calls.push(options);
  };
  try {
    const children = await parkedRun(["q1"]);
    const view = render(
      <Thread initialApproval={{ run: "r1", question: "q1" }}>{children}</Thread>,
    );
    assert.equal(
      view.container.querySelector('[data-aui="actions"]')?.getAttribute("data-aui-block-id"),
      "approval:q1:r0",
      "one pending approval has its own action row",
    );
    assert.equal(calls.length, 1);
    view.rerender(<Thread initialApproval={{ run: "r1", question: "q2" }}>{children}</Thread>);
    assert.equal(calls.length, 1, "rerender must not reposition a reader");
    view.unmount();
    calls.length = 0;
    render(
      <Thread focus="linked" initialApproval={{ run: "r1", question: "q1" }}>
        {children}
      </Thread>,
    );
    assert.equal(calls.length, 0, "an explicit link wins over the initial approval");
  } finally {
    HTMLElement.prototype.scrollTo = oldScroll;
  }
});

test("initial approval positioning finds the one action row several pending approvals share", async () => {
  const { Thread } = await import("@humagen/web");
  const oldScroll = HTMLElement.prototype.scrollTo;
  const calls: ScrollToOptions[] = [];
  HTMLElement.prototype.scrollTo = (options) => {
    if (typeof options === "object") calls.push(options);
  };
  try {
    // Room.tsx names the last pending approval; the console draws the run's approvals as one batch.
    const view = render(
      <Thread initialApproval={{ run: "r1", question: "q2" }}>
        {await parkedRun(["q1", "q2"])}
      </Thread>,
    );
    const rows = Array.from(view.container.querySelectorAll('[data-aui="actions"]'));
    assert.deepEqual(
      rows.map((row) => row.getAttribute("data-aui-block-id")),
      ["batch:r1:actions"],
      "several pending approvals share one action row and have none of their own",
    );
    assert.equal(calls.length, 1, "the batch's action row is positioned");
    view.unmount();
    calls.length = 0;
    render(
      <Thread initialApproval={{ run: "other", question: "q2" }}>
        {await parkedRun(["q1", "q2"])}
      </Thread>,
    );
    assert.equal(calls.length, 0, "another run's batch is not this approval's row");
  } finally {
    HTMLElement.prototype.scrollTo = oldScroll;
  }
});

test("initial approval positioning stays at the end when the actions show there, and clears the composer and the jump control when they do not", async () => {
  const { Thread } = await import("@humagen/web");
  const proto = HTMLElement.prototype;
  const oldScroll = proto.scrollTo;
  const oldRect = proto.getBoundingClientRect;
  const calls: ScrollToOptions[] = [];
  proto.scrollTo = function (this: HTMLElement, options) {
    if (typeof options !== "object") return;
    calls.push(options);
    this.scrollTop = options.top ?? 0;
  };
  // A list 600 tall holding 2000 of messages: its end is at 1400. The composer covers it from 450.
  const tall = (name: "scrollHeight" | "clientHeight", size: number) =>
    Object.defineProperty(proto, name, {
      configurable: true,
      get(this: HTMLElement) {
        return this.getAttribute("role") === "log" ? size : 0;
      },
    });
  tall("scrollHeight", 2000);
  tall("clientHeight", 600);
  // Where the action row sits in the list's content, top and bottom.
  let row = [0, 0];
  proto.getBoundingClientRect = function (this: HTMLElement) {
    const edge = (top: number, bottom: number) => ({ top, bottom }) as DOMRect;
    if (this.dataset.layer === "composer-dock") return edge(450, 600);
    if (this.dataset.auiBlock === "approval") {
      const scrolled = this.closest<HTMLElement>('[role="log"]')?.scrollTop ?? 0;
      return edge(row[0]! - scrolled, row[1]! - scrolled);
    }
    return edge(0, 600);
  };
  const open = () =>
    render(
      <Thread initialApproval={{ run: "r1", question: "q1" }} footer={<div>Composer</div>}>
        <div data-turn-id="r1">
          <div data-aui-block="approval" data-aui-block-id="approval:q1:r0" />
        </div>
      </Thread>,
    );
  try {
    // The latest run's approval: with the list at its end the row is on view above the composer.
    row = [1700, 1760];
    let view = open();
    assert.equal(calls.length, 1);
    assert.equal(calls[0]?.top, 1400, "the list opens at its end");
    assert.equal(
      view.container.querySelector('[data-layer="thread-jump"]'),
      null,
      "at the end there is no Jump to latest control and the list keeps following",
    );
    view.unmount();
    calls.length = 0;
    // An approval with a screen of later messages under it: the list leaves the end for it.
    row = [440, 500];
    view = open();
    assert.equal(calls.length, 1);
    assert.equal(
      calls[0]?.top,
      130,
      "the row ends 80px above the composer, clear of the jump control over it",
    );
    assert.ok(
      view.container.querySelector('[data-layer="thread-jump"]'),
      "away from the end the way back is offered",
    );
    view.unmount();
  } finally {
    proto.scrollTo = oldScroll;
    proto.getBoundingClientRect = oldRect;
    delete (proto as { scrollHeight?: number }).scrollHeight;
    delete (proto as { clientHeight?: number }).clientHeight;
  }
});
