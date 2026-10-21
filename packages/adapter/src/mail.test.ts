import assert from "node:assert/strict";
import { test } from "node:test";
import { messageAsks, messageTemplate } from "@warp/core";
import { fromServer, kinds } from "./mail.ts";

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
