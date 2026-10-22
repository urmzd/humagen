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
    { page: "projects", q: "globex" },
    { page: "projects", kind: "account" },
    { page: "projects", q: "renewals", kind: "team" },
    // A kind the workspace defines has an address too.
    { page: "projects", kind: "partner" },
    { page: "project", name: "Q4 launch", tab: "inbox" },
    { page: "project", name: "Q4 launch", tab: "inbox", message: "m7" },
    { page: "project", name: "Q4 launch", tab: "overview" },
    { page: "project", name: "Q4 launch", tab: "context" },
    { page: "project", name: "", tab: "inbox" },
    { page: "project", name: "", tab: "overview" },
    { page: "project", name: "", tab: "context" },
    { page: "project", name: "", tab: "people" },
    { page: "project", name: "Globex", tab: "limits" },
    { page: "project", name: "Globex", tab: "history" },
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
    { page: "connections" },
    { page: "connections", provider: "issues" },
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
  assert.equal(href({ page: "connections" }), "/connections");
  assert.equal(href({ page: "connections", provider: "issues" }), "/connections/issues");
});

test("your connections are your own page, apart from the workspace's Slack and GitHub connections", () => {
  assert.deepEqual(at("/connections"), { page: "connections" });
  assert.deepEqual(at("/connections/issues"), { page: "connections", provider: "issues" });
  // The workspace's clients stay a section of Settings, old links included.
  assert.deepEqual(at("/settings/connections"), { page: "settings", section: "connections" });
  assert.deepEqual(at("/?view=connections"), { page: "settings", section: "connections" });
});

test("the Directory opens on People, and each tab has its own path", () => {
  assert.equal(href({ page: "directory", tab: "people" }), "/directory");
  assert.equal(href({ page: "directory", tab: "agents" }), "/directory/agents");
  assert.equal(href({ page: "directory", tab: "projects" }), "/directory/projects");
  assert.deepEqual(at("/directory/people"), { page: "directory", tab: "people" });
  assert.deepEqual(at("/directory/bogus"), { page: "directory", tab: "people" });
});

test("a project opens on its inbox, and each tab has its own path", () => {
  assert.equal(href({ page: "project", name: "Globex", tab: "inbox" }), "/projects/Globex");
  assert.equal(href({ page: "project", name: "Globex", tab: "inbox", message: "m1" }), "/projects/Globex?message=m1");
  assert.equal(href({ page: "project", name: "", tab: "people" }), "/projects/~/people");
  assert.equal(href({ page: "project", name: "Q4 launch", tab: "history" }), "/projects/Q4%20launch/history");
  assert.deepEqual(at("/projects/Globex/inbox?message=m1"), { page: "project", name: "Globex", tab: "inbox", message: "m1" });
  // A message is a selection in the inbox, and nowhere else.
  assert.deepEqual(at("/projects/Globex/people?message=m1"), { page: "project", name: "Globex", tab: "people" });
});

test("a project's address is its name, ~ is the workspace, and the default tab has no segment", () => {
  assert.equal(href({ page: "project", name: "Globex", tab: "overview" }), "/projects/Globex/overview");
  assert.equal(href({ page: "project", name: "Globex", tab: "context" }), "/projects/Globex/context");
  assert.equal(href({ page: "project", name: "Field engineering", tab: "context" }), "/projects/Field%20engineering/context");
  assert.equal(href({ page: "project", name: "", tab: "inbox" }), "/projects/~");
  assert.equal(href({ page: "project", name: "", tab: "context" }), "/projects/~/context");
  assert.deepEqual(at("/projects/~"), { page: "project", name: "", tab: "inbox" });
  assert.equal(href({ page: "projects", q: "globex", kind: "account" }), "/projects?q=globex&kind=account");
});

test("a project's schedules have their own address", () => {
  assert.equal(href({ page: "project", name: "Field", tab: "schedules" }), "/projects/Field/schedules");
  assert.deepEqual(at("/projects/Field/schedules"), { page: "project", name: "Field", tab: "schedules" });
  assert.deepEqual(at("/projects/~/schedules"), { page: "project", name: "", tab: "schedules" });
});

test("a project's Agents tab has its own path, the workspace's included", () => {
  assert.equal(href({ page: "project", name: "Globex", tab: "agents" }), "/projects/Globex/agents");
  assert.equal(href({ page: "project", name: "", tab: "agents" }), "/projects/~/agents");
  assert.deepEqual(at("/projects/~/agents"), { page: "project", name: "", tab: "agents" });
  assert.deepEqual(at("/projects/Globex/agents"), { page: "project", name: "Globex", tab: "agents" });
});

test("a project tab or kind it does not know falls back to the default", () => {
  assert.deepEqual(at("/projects/Globex/bogus"), { page: "project", name: "Globex", tab: "inbox" });
  assert.deepEqual(at("/projects?kind=Planet!"), { page: "projects" });
  assert.deepEqual(at("/projects?q="), { page: "projects" });
  assert.deepEqual(at("/?view=projects"), { page: "projects" });
});

test("a channel is a page under its project's Channels tab, and ~ is the workspace's", () => {
  assert.equal(href({ page: "project", name: "Globex", tab: "channels" }), "/projects/Globex/channels");
  assert.equal(href({ page: "channel", project: "Globex", channel: "renewals" }), "/projects/Globex/channels/renewals");
  assert.equal(href({ page: "channel", project: "", channel: "general" }), "/projects/~/channels/general");
  assert.equal(href({ page: "channel", project: "Field engineering", channel: "ops" }), "/projects/Field%20engineering/channels/ops");
  for (const r of [
    { page: "channel", project: "Globex", channel: "renewals" },
    { page: "channel", project: "", channel: "general" },
    { page: "project", name: "Globex", tab: "channels" },
  ] as const) {
    assert.deepEqual(at(href(r)), r);
  }
});

test("a task opens at one of its messages, and a new task starts in a channel", () => {
  assert.equal(href({ page: "task", id: "th-0123456789abcdef", message: "m-3" }), "/tasks/th-0123456789abcdef?message=m-3");
  assert.deepEqual(at("/tasks/th-0123456789abcdef?message=m-3"), { page: "task", id: "th-0123456789abcdef", message: "m-3" });
  assert.deepEqual(at("/tasks/th-0123456789abcdef?message="), { page: "task", id: "th-0123456789abcdef" });
  assert.equal(href({ page: "new", project: "Globex", channel: "renewals" }), "/?project=Globex&channel=renewals");
  assert.deepEqual(at("/?project=Globex&channel=renewals"), { page: "new", project: "Globex", channel: "renewals" });
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
