import assert from "node:assert/strict";
import { test } from "node:test";
import { ActionId, messageAsks, messageTemplate, type UiAction } from "@humagen/core";
import { fromServer, kinds, MailError, mailTransport, notificationsOf, projectOf, projectTransport, shownAbout, toRequest, updatesOf } from "./mail.ts";

/** An announcement as the platform sends it: a broadcast in a project's inbox, only there to be read. */
const announcement = {
  id: "m-9",
  kind: "announcement",
  subject: "Renewal signed",
  from: { kind: "person", name: "Dana" },
  at: "2026-10-21T09:00:00Z",
  updated_at: "2026-10-21T09:00:00Z",
  severity: "info",
  state: "open",
  unread: true,
  source: { project: "Globex" },
  parts: [{ id: "p1", type: "text", state: "info", actions: [], text: "Globex renewed for two years." }],
};

test("an announcement is laid out as one, about its project, from the person who posted it", () => {
  const m = fromServer(announcement);
  assert.equal(m.kind, "announcement");
  assert.equal(m.template, "announcement");
  assert.equal(messageTemplate(m), "announcement");
  assert.deepEqual(m.from, { kind: "person", name: "Dana" });
  assert.deepEqual(m.source, { kind: "project", id: "Globex", label: "Project Globex" });
  assert.deepEqual(m.parts, [{ id: "p1", type: "text", text: "Globex renewed for two years." }]);
});

test("a person's announcement shows what they are called, and keeps who they are to tell the reader's own apart", () => {
  const m = fromServer({ ...announcement, from: { kind: "person", name: "tailscale:dana@example.com", display: "Dana Scully" } });
  assert.deepEqual(m.from, { kind: "person", name: "Dana Scully" });
  assert.equal(m.sender, "tailscale:dana@example.com");
  // An agent's is named as it is.
  const agent = fromServer({ ...announcement, from: { kind: "agent", name: "recap" } });
  assert.deepEqual(agent.from, { kind: "agent", name: "recap" });
  assert.equal(agent.sender, "recap");
});

test("an announcement never asks anything of the reader, so it sits under What's new", () => {
  assert.equal(messageAsks(fromServer(announcement)), false);
});

test("announcements are a kind the inbox offers to filter by, once", () => {
  assert.ok(kinds.some((k) => k.id === "announcement"));
  assert.equal(new Set(kinds.map((k) => k.id)).size, kinds.length);
});

test("an announcement reads as hidden for the reader who hid it", () => {
  assert.equal(fromServer({ ...announcement, state: "dismissed" }).dismissedAt, announcement.updated_at);
  const later = fromServer({ ...announcement, state: "snoozed", snoozed_until: "2026-10-22T09:00:00Z" });
  assert.equal(later.snoozedUntil, "2026-10-22T09:00:00Z");
});

test("a message keeps the time it was raised after it is resolved or set aside", () => {
  const answered = fromServer({ ...announcement, updated_at: "2026-10-21T09:05:00Z" });
  assert.equal(answered.at, "2026-10-21T09:00:00Z");
  const hidden = fromServer({ ...announcement, state: "dismissed", updated_at: "2026-10-21T09:05:00Z" });
  assert.equal(hidden.at, "2026-10-21T09:00:00Z");
  assert.equal(hidden.dismissedAt, "2026-10-21T09:05:00Z");
  const alert = fromServer({
    ...announcement,
    kind: "alert",
    updated_at: "2026-10-21T09:05:00Z",
    parts: [{ id: "p1", type: "alert", state: "open", actions: ["acknowledge"], alert: { id: "a1", rule: "api", severity: "warning", title: "Slow" } }],
  });
  assert.equal(alert.at, "2026-10-21T09:00:00Z");
  const part = alert.parts[0];
  assert.ok(part?.type === "alert");
  assert.equal(part.alert.startedAt, "2026-10-21T09:00:00Z");
  assert.equal(part.alert.updatedAt, "2026-10-21T09:05:00Z");
});

test("an announcement the reader hid for themselves is theirs to bring back, and nothing else is", async () => {
  const hidden = fromServer({ ...announcement, state: "dismissed", hidden: true });
  assert.equal(hidden.hidden, true);
  const unhide: UiAction = { id: ActionId.unhide, payload: { turnId: hidden.id, target: hidden.id } };
  assert.deepEqual(toRequest(hidden, unhide), { call: "unhide" });
  // Withdrawn for everyone, it is not the reader's to bring back.
  const withdrawn = fromServer({ ...announcement, state: "dismissed" });
  assert.equal(withdrawn.hidden, undefined);
  assert.equal(toRequest(withdrawn, unhide), null);
  const seen: string[] = [];
  const via = projectTransport(async <T,>(method: string, path: string) => {
    seen.push(`${method} ${path}`);
    return { ...announcement } as T;
  }, "Globex");
  const back = await via.send(hidden, { call: "unhide" });
  assert.deepEqual(seen, ["POST /api/inbox/m-9/unhide"]);
  assert.equal(back.hidden, undefined);
});

test("a project's inbox reads by id only what is filed in it, so another project's message is not found there", async () => {
  const via = (project: string, children = false) =>
    projectTransport(async <T,>() => ({ message: { ...announcement }, history: [] }) as T, project, children);
  assert.equal(projectOf(fromServer(announcement)), "Globex");
  assert.equal((await via("Globex").get("m-9")).id, "m-9");
  // Read as the reader may, it is still Globex's: Field's inbox does not show it, below Field or not.
  for (const children of [false, true]) {
    await assert.rejects(via("Field", children).get("m-9"), (e: unknown) => e instanceof MailError && e.status === 404);
  }
  // The workspace's own inbox holds what is filed in no project.
  const root = projectTransport(async <T,>() => ({ message: { ...announcement, source: {} }, history: [] }) as T, "~");
  assert.equal((await root.get("m-9")).id, "m-9");
  await assert.rejects(projectTransport(async <T,>() => ({ message: { ...announcement } }) as T, "").get("m-9"));
});

/** An agent's post held for a project's owners, as the platform sends it to one. */
const draft = {
  id: "m-12",
  kind: "announcement",
  subject: "Field shipped two fixes",
  from: { kind: "agent", name: "humagen-analyst" },
  at: "2026-10-21T09:00:00Z",
  updated_at: "2026-10-21T09:00:00Z",
  severity: "info",
  state: "open",
  unread: true,
  source: { project: "Field", run: "r1", agent: "humagen-analyst" },
  parts: [
    {
      id: "p1",
      type: "draft",
      state: "open",
      actions: ["publish", "discard"],
      draft: { project: "Field", agent: "humagen-analyst", run: "r1", subject: "Field shipped two fixes", text: "The export is faster." },
    },
  ],
};

test("a draft asks its owner to publish or discard it, and sits in Needs you", () => {
  const m = fromServer(draft);
  assert.equal(messageAsks(m), true);
  assert.equal(messageTemplate(m), "announcement");
  assert.deepEqual(m.parts, [
    {
      id: "p1",
      type: "draft",
      draft: { subject: "Field shipped two fixes", text: "The export is faster.", agent: "humagen-analyst", project: "Field" },
      allowed: [ActionId.publishDraft, ActionId.discardDraft],
    },
  ]);
});

test("publishing and discarding a draft are the platform's publish and discard", () => {
  const m = fromServer(draft);
  const on = (id: string) => toRequest(m, { id, payload: { turnId: "m-12", target: "p1" } } as UiAction);
  assert.deepEqual(on(ActionId.publishDraft), { call: "resolve", partId: "p1", action: "publish", values: undefined });
  assert.deepEqual(on(ActionId.discardDraft), { call: "resolve", partId: "p1", action: "discard", values: undefined });
  // A reader who may not decide is offered nothing.
  const viewer = fromServer({ ...draft, parts: [{ ...draft.parts[0], actions: [] }] });
  assert.deepEqual((viewer.parts[0] as { allowed?: string[] }).allowed, []);
});

test("the person's inbox reads What's new with news=true and what needs them with needs=true, as Home does", async () => {
  const calls: string[] = [];
  const http = async <T>(_method: string, path: string): Promise<T> => {
    calls.push(path);
    return { messages: [], next: null, open: 3, unread: 1 } as T;
  };
  const via = mailTransport(http);
  await via.list({ limit: 20, state: "open", news: true });
  const needs = await via.list({ limit: 5, state: "open", needs: true });
  assert.deepEqual(calls, ["/api/inbox?limit=20&state=open&news=true", "/api/inbox?limit=5&state=open&needs=true"]);
  assert.equal(needs.open, 3);
});

test("a project's inbox lists through the project's own route, and answers through the inbox's", async () => {
  const calls: string[] = [];
  const http = async <T>(method: string, path: string): Promise<T> => {
    calls.push(`${method} ${path}`);
    if (path.includes("/inbox?")) return { messages: [announcement, { ...announcement, id: "m-8", state: "dismissed" }], next: null } as T;
    return { ...announcement, state: "dismissed" } as T;
  };
  const via = projectTransport(http, "", true);
  const open = await via.list({ limit: 30, state: "open", kind: "announcement" });
  assert.equal(calls[0], "GET /api/projects/~/inbox?limit=30&state=open&children=true&kind=announcement");
  assert.equal(open.open, 1);
  assert.equal(open.unread, 1);
  const done = await via.list({ limit: 30, state: "resolved" });
  assert.equal(calls[1], "GET /api/projects/~/inbox?limit=30&state=all&children=true");
  assert.deepEqual(done.messages.map((m) => m.id), ["m-8"]);
  await via.send(fromServer(announcement), { call: "dismiss" });
  assert.equal(calls[2], "POST /api/inbox/m-9/dismiss");
  await projectTransport(http, "Q4 launch").list({ limit: 5 });
  assert.equal(calls[3], "GET /api/projects/Q4%20launch/inbox?limit=5&state=open");
});

test("What's new holds the open announcements, unread first, at most five, each where it opens", () => {
  const at = (i: number) => `2026-10-2${i}T09:00:00Z`;
  const raw = [1, 2, 3, 4, 5, 6].map((i) => ({ ...announcement, id: `m-${i}`, at: at(i), updated_at: at(i), unread: i === 1 }));
  const root = { ...announcement, id: "m-root", source: {}, unread: false, at: at(7), updated_at: at(7) };
  const hidden = { ...announcement, id: "m-hidden", state: "dismissed" };
  const alert = { ...announcement, id: "m-alert", kind: "alert" };
  // An owner's inbox holds an agent's draft under the same kind, unpublished.
  const unpublished = { ...draft, id: "m-draft", at: at(8), updated_at: at(8) };
  const updates = updatesOf(
    [...raw, root, hidden, alert, unpublished].map(fromServer),
    (p) => (p === "" ? "Acme" : `${p} Inc`),
    (p, id) => `/projects/${p || "~"}?message=${id}`,
  );
  assert.deepEqual(
    updates.map((u) => u.id),
    ["m-1", "m-root", "m-6", "m-5", "m-4"],
  );
  assert.deepEqual(updates[1], {
    id: "m-root",
    project: "",
    projectTitle: "Acme",
    subject: "Renewal signed",
    from: { kind: "person", name: "Dana" },
    at: at(7),
    unread: false,
    url: "/projects/~?message=m-root",
  });
  assert.equal(updates[0]?.projectTitle, "Globex Inc");
});

test("what needs the reader on Home is every open message but posted announcements, an agent's draft among them, said from its sender", () => {
  const mention = {
    ...announcement,
    id: "m-mention",
    kind: "mention",
    subject: "Ana mentioned you in #renewals",
    from: { kind: "person", name: "Ana" },
    parts: [{ id: "p1", type: "mention", state: "open", actions: ["reply", "done"], mention: { thread: "t1", message: "x", excerpt: "can you check?" } }],
  };
  const alert = {
    ...announcement,
    id: "m-alert",
    kind: "alert",
    subject: "6 of 10 runs failed",
    severity: "critical",
    from: { kind: "rule", name: "failure-rate" },
    parts: [{ id: "p1", type: "alert", state: "open", actions: ["acknowledge"], alert: { id: "a1", rule: "failure-rate", kind: "run_failed", severity: "warning", title: "6 of 10 runs failed" } }],
  };
  const settled = { ...mention, id: "m-done", parts: [{ ...mention.parts[0], state: "resolved", actions: [], outcome: { action: "done", by: "ana", at: announcement.at } }] };
  // An owner's draft is an announcement only once it is published: until then it waits on them.
  const decided = { ...draft, id: "m-decided", parts: [{ ...draft.parts[0], state: "resolved", actions: [], outcome: { action: "publish", by: "ana", at: draft.at } }] };
  const items = notificationsOf([mention, alert, settled, announcement, draft, decided].map(fromServer), (id) => `/inbox?message=${id}`);
  assert.deepEqual(items, [
    { kind: "inbox", id: "m-mention", from: "Ana", summary: "mentioned you in #renewals", at: announcement.at, url: "/inbox?message=m-mention" },
    // Above normal, so it says so; a normal one carries no severity.
    { kind: "inbox", id: "m-alert", from: "failure-rate", summary: "6 of 10 runs failed", at: announcement.at, severity: "critical", url: "/inbox?message=m-alert" },
    { kind: "inbox", id: "m-12", from: "humagen-analyst", summary: "Field shipped two fixes", at: draft.at, url: "/inbox?message=m-12" },
  ]);
  // The same page makes What's new from what is left: the posted announcement, and never the draft.
  assert.deepEqual(
    updatesOf([mention, alert, settled, announcement, draft, decided].map(fromServer), (p) => p).map((u) => u.id),
    [announcement.id],
  );
});

test("what needs the reader on Home is every open message but published announcements, said from its sender", () => {
  const mention = {
    ...announcement,
    id: "m-mention",
    kind: "mention",
    subject: "Ana mentioned you in #renewals",
    from: { kind: "person", name: "Ana" },
    parts: [{ id: "p1", type: "mention", state: "open", actions: ["reply", "done"], mention: { thread: "t1", message: "x", excerpt: "can you check?" } }],
  };
  const alert = {
    ...announcement,
    id: "m-alert",
    kind: "alert",
    subject: "6 of 10 runs failed",
    from: { kind: "rule", name: "failure-rate" },
    parts: [{ id: "p1", type: "alert", state: "open", actions: ["acknowledge"], alert: { id: "a1", rule: "failure-rate", kind: "run_failed", severity: "warning", title: "6 of 10 runs failed" } }],
  };
  const settled = { ...mention, id: "m-done", parts: [{ ...mention.parts[0], state: "resolved", actions: [], outcome: { action: "done", by: "ana", at: announcement.at } }] };
  const items = notificationsOf([mention, alert, settled, announcement, draft].map(fromServer), (id) => `/inbox?message=${id}`);
  assert.deepEqual(items, [
    { kind: "inbox", id: "m-mention", from: "Ana", summary: "mentioned you in #renewals", at: announcement.at, url: "/inbox?message=m-mention" },
    { kind: "inbox", id: "m-alert", from: "failure-rate", summary: "6 of 10 runs failed", at: announcement.at, url: "/inbox?message=m-alert" },
    // A draft is kind announcement, but it waits on its owners: it needs them.
    { kind: "inbox", id: "m-12", from: "humagen-analyst", summary: "Field shipped two fixes", at: draft.at, url: "/inbox?message=m-12" },
  ]);
  // And it is not news yet.
  assert.deepEqual(updatesOf([draft].map(fromServer), (p) => p), []);
});

test("what a question is about names its conversation by what it is called, and never by an id", () => {
  const question = {
    id: "m-4",
    kind: "clarification",
    subject: "ledger asks the billing experts: INV-2041 bills the March maintenance twice…",
    from: { kind: "agent", name: "ledger" },
    at: "2026-10-22T10:26:00Z",
    severity: "info",
    state: "open",
    source: { run: "r7141b73d4cd9f6d7fbee963ea0c1d2e3", thread: "th-083973d191362dea", agent: "ledger", project: "Globex", thread_title: "INV-2041 double charge" },
    parts: [],
  };
  const m = fromServer(question);
  // The run stays in what it is about, by its id: a surface finds the run
  // there, as Slack does to post into the run's own thread.
  assert.deepEqual(
    m.about.map((s) => `${s.kind}:${s.id}:${s.label}`),
    [
      "run:r7141b73d4cd9f6d7fbee963ea0c1d2e3:INV-2041 double charge",
      "thread:th-083973d191362dea:INV-2041 double charge",
      "agent:ledger:Agent ledger",
      "project:Globex:Project Globex",
    ],
  );
  assert.deepEqual(m.source, { kind: "run", id: "r7141b73d4cd9f6d7fbee963ea0c1d2e3", label: "INV-2041 double charge" });
  // Drawn, the run and its thread are one conversation.
  assert.deepEqual(
    shownAbout(m.about).map((s) => `${s.kind}:${s.label}`),
    ["thread:INV-2041 double charge", "agent:Agent ledger", "project:Project Globex"],
  );
  // A run in no thread is the conversation itself.
  const alone = fromServer({ ...question, source: { run: "r-1", agent: "ledger" } });
  assert.deepEqual(
    shownAbout(alone.about).map((s) => `${s.kind}:${s.id}`),
    ["run:r-1", "agent:ledger"],
  );
  // A conversation the reader is not told the name of is called one.
  const unnamed = fromServer({ ...question, source: { ...question.source, thread_title: undefined } });
  assert.deepEqual(
    shownAbout(unnamed.about).map((s) => s.label),
    ["Conversation", "Agent ledger", "Project Globex"],
  );
  for (const s of [...m.about, ...unnamed.about]) assert.ok(!/\b(r[0-9a-f]{8,}|th-[0-9a-f]+)\b/.test(s.label), s.label);
});

test("operation attention opens durable work and never claims a successful run failed", () => {
  const message = fromServer({
    id: "attention",
    kind: "operation",
    subject: "Agent work needs a person",
    from: { kind: "system", name: "Humagen" },
    at: "2026-10-25T12:00:00Z",
    updated_at: "2026-10-25T12:00:00Z",
    state: "open",
    unread: true,
    severity: "warning",
    source: { run: "completed-run", project: "Field" },
    parts: [
      {
        id: "blocker",
        type: "operation",
        state: "open",
        actions: ["acknowledge"],
        operation: { kind: "assignment", id: "work", reason: "The deadline passed." },
      },
    ],
  });
  assert.equal(message.template, "note");
  assert.equal(message.parts[0]?.type, "custom");
  assert.equal(
    message.parts.some((p) => p.type === "failure"),
    false,
  );
  const part = message.parts[0];
  if (!part || part.type !== "custom") throw new Error("missing operation part");
  assert.equal(part.resolvable, true);
  assert.deepEqual(part.data, { kind: "assignment", id: "work", reason: "The deadline passed." });
  assert.ok(!part.allowed?.includes(ActionId.retry));
});
