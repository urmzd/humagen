import assert from "node:assert/strict";
import { test } from "node:test";
import { href, parse } from "./route.ts";

const at = (url: string) => {
  const u = new URL(url, "http://x");
  return parse(u.pathname, u.search);
};
test("every message has one address: a session's root, or a run focused on its own", () => {
  assert.equal(href({ page: "conversation", id: "s_a" }), "/messages/s_a");
  assert.deepEqual(at("/messages/s_a"), at("/sessions/s_a"));
  const run = `r${"a1".repeat(16)}`;
  assert.deepEqual(at(`/messages/${run}`), { page: "message", id: run });
  assert.equal(href({ page: "message", id: run }), `/messages/${run}`);
  assert.deepEqual(at("/messages"), { page: "sessions" });
});
