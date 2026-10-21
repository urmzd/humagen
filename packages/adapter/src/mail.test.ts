import assert from "node:assert/strict";
import { test } from "node:test";
import { ActionId, messageAsks, messageTemplate, type UiAction } from "@warp/core";
import { fromServer, kinds, toRequest } from "./mail.ts";

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
