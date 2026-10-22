import assert from "node:assert/strict";
import { test } from "node:test";
import { ActionId, messageAsks, messageTemplate, type UiAction } from "@warp/core";
import { fromServer, kinds, projectTransport, toRequest, updatesOf } from "./mail.ts";

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

test("an announcement never asks anything of the reader, so it sits under Updates", () => {
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

/** An agent's post held for a project's owners, as the platform sends it to one. */
const draft = {
  id: "m-12",
  kind: "announcement",
  subject: "Field shipped two fixes",
  from: { kind: "agent", name: "warp-analyst" },
  at: "2026-10-21T09:00:00Z",
  updated_at: "2026-10-21T09:00:00Z",
  severity: "info",
  state: "open",
  unread: true,
  source: { project: "Field", run: "r1", agent: "warp-analyst" },
  parts: [
    {
      id: "p1",
      type: "draft",
      state: "open",
      actions: ["publish", "discard"],
      draft: { project: "Field", agent: "warp-analyst", run: "r1", subject: "Field shipped two fixes", text: "The export is faster." },
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
      draft: { subject: "Field shipped two fixes", text: "The export is faster.", agent: "warp-analyst", project: "Field" },
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
  const updates = updatesOf(
    [...raw, root, hidden, alert].map(fromServer),
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
