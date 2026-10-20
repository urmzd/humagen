import assert from "node:assert/strict";
import { test } from "node:test";
import { href, parse, type Route } from "./route.ts";

const at = (url: string) => {
  const u = new URL(url, "http://x");
  return parse(u.pathname, u.search);
};

test("every route survives a round trip through its address", () => {
  const routes: Route[] = [
    { page: "new" },
    { page: "new", project: "launch" },
    { page: "task", id: "t-1" },
    { page: "projects" },
    { page: "project", name: "Q4 launch" },
    { page: "schedules" },
    { page: "schedules", name: "nightly" },
    { page: "alerts", tab: "open" },
    { page: "alerts", tab: "rules" },
    { page: "alerts", tab: "overview" },
    { page: "alerts", tab: "open", alert: "a1" },
    { page: "inbox" },
    { page: "inbox", message: "m1" },
    { page: "knowledge" },
    { page: "knowledge", source: "handbook", tab: "documents" },
    { page: "knowledge", source: "handbook", tab: "evals" },
    { page: "agents" },
    { page: "agent", name: null, tab: "prompt" },
    { page: "agent", name: "lead", tab: "prompt" },
    { page: "agent", name: "lead", tab: "knowledge" },
    { page: "artifacts" },
    { page: "settings", section: "profile" },
    { page: "settings", section: "access", permission: "runs:read" },
  ];
  for (const r of routes) assert.deepEqual(at(href(r)), JSON.parse(JSON.stringify(r)), href(r));
});

test("deep links name pages by path", () => {
  assert.equal(href({ page: "agent", name: "lead", tab: "access" }), "/agents/lead/access");
  assert.equal(href({ page: "task", id: "abc" }), "/tasks/abc");
  assert.equal(href({ page: "knowledge", source: "a b", tab: "search" }), "/knowledge/a%20b/search");
});

test("old ?view= links open the page they named", () => {
  assert.deepEqual(at("/?view=agents"), { page: "agents" });
  assert.deepEqual(at("/?view=storage"), { page: "artifacts" });
  assert.deepEqual(at("/?view=analytics"), { page: "alerts", tab: "overview" });
  assert.deepEqual(at("/?view=alerts&alert=a9"), { page: "alerts", tab: "open", alert: "a9" });
  assert.deepEqual(at("/?view=access&permission=runs:write"), { page: "settings", section: "access", permission: "runs:write" });
  assert.deepEqual(at("/?message=m2"), { page: "inbox", message: "m2" });
  assert.deepEqual(at("/?view=runs"), { page: "new" });
});

test("an address it does not know, or a tab that does not exist, falls back safely", () => {
  assert.deepEqual(at("/nowhere"), { page: "new" });
  assert.deepEqual(at("/agents/lead/bogus"), { page: "agent", name: "lead", tab: "prompt" });
  assert.deepEqual(at("/settings/bogus"), { page: "settings", section: "profile" });
});
