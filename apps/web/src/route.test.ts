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
    { page: "new", agent: "warp-analyst", ask: "How did runs go today?" },
    { page: "monitor", tab: "overview" },
    { page: "monitor", tab: "rules" },
    { page: "inbox", kind: "alert" },
    { page: "inbox", alert: "a1" },
    { page: "inbox" },
    { page: "inbox", message: "m1" },
    { page: "context" },
    { page: "context", source: "handbook", tab: "documents" },
    { page: "context", source: "handbook", tab: "evals" },
    { page: "agents" },
    { page: "agent", name: null, tab: "prompt" },
    { page: "agent", name: "lead", tab: "prompt" },
    { page: "agent", name: "lead", tab: "context" },
    { page: "artifacts" },
    { page: "artifacts", artifact: "art-2" },
    { page: "directory", tab: "people" },
    { page: "directory", tab: "agents" },
    { page: "directory", tab: "projects" },
    { page: "settings", section: "profile" },
    { page: "settings", section: "access", permission: "runs:read" },
  ];
  for (const r of routes) assert.deepEqual(at(href(r)), JSON.parse(JSON.stringify(r)), href(r));
});

test("deep links name pages by path", () => {
  assert.equal(href({ page: "agent", name: "lead", tab: "access" }), "/agents/lead/access");
  assert.equal(href({ page: "task", id: "abc" }), "/tasks/abc");
  assert.equal(href({ page: "artifacts", artifact: "art-2" }), "/artifacts/art-2");
  assert.equal(href({ page: "context", source: "a b", tab: "search" }), "/context/a%20b/search");
  assert.equal(href({ page: "monitor", tab: "rules" }), "/monitor/rules");
  assert.equal(href({ page: "inbox", kind: "alert" }), "/inbox?kind=alert");
});

test("the Directory opens on People, and each tab has its own path", () => {
  assert.equal(href({ page: "directory", tab: "people" }), "/directory");
  assert.equal(href({ page: "directory", tab: "agents" }), "/directory/agents");
  assert.equal(href({ page: "directory", tab: "projects" }), "/directory/projects");
  assert.deepEqual(at("/directory/people"), { page: "directory", tab: "people" });
  assert.deepEqual(at("/directory/bogus"), { page: "directory", tab: "people" });
});

test("old ?view= links open the page they named", () => {
  assert.deepEqual(at("/?view=agents"), { page: "agents" });
  assert.deepEqual(at("/?view=storage"), { page: "artifacts" });
  assert.deepEqual(at("/?view=analytics"), { page: "monitor", tab: "overview" });
  // What fired lives in the inbox now: an alert's old link opens the message it raised.
  assert.deepEqual(at("/?view=alerts&alert=a9"), { page: "inbox", alert: "a9" });
  assert.deepEqual(at("/?view=alerts"), { page: "inbox", kind: "alert" });
  assert.deepEqual(at("/?view=knowledge"), { page: "context" });
  assert.deepEqual(at("/?view=access&permission=runs:write"), { page: "settings", section: "access", permission: "runs:write" });
  assert.deepEqual(at("/?message=m2"), { page: "inbox", message: "m2" });
  assert.deepEqual(at("/?view=runs"), { page: "new" });
});

test("an address it does not know, or a tab that does not exist, falls back safely", () => {
  assert.deepEqual(at("/nowhere"), { page: "new" });
  assert.deepEqual(at("/agents/lead/bogus"), { page: "agent", name: "lead", tab: "prompt" });
  assert.deepEqual(at("/settings/bogus"), { page: "settings", section: "profile" });
});

test("Alerts became Monitor and Knowledge became Context, and old paths still land", () => {
  assert.deepEqual(at("/alerts"), { page: "inbox", kind: "alert" });
  assert.deepEqual(at("/alerts/open"), { page: "inbox", kind: "alert" });
  assert.deepEqual(at("/alerts?alert=a1"), { page: "inbox", alert: "a1" });
  assert.deepEqual(at("/alerts/rules"), { page: "monitor", tab: "rules" });
  assert.deepEqual(at("/alerts/overview"), { page: "monitor", tab: "overview" });
  assert.deepEqual(at("/knowledge/handbook/search"), { page: "context", source: "handbook", tab: "search" });
  assert.deepEqual(at("/agents/lead/knowledge"), { page: "agent", name: "lead", tab: "context" });
});
