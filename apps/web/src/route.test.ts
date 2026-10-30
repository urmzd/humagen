import assert from "node:assert/strict";
import { test } from "node:test";
import { documentTitle, href, parse, type Route, screenOf } from "./route.ts";

const at = (url: string) => {
  const u = new URL(url, "http://x");
  return parse(u.pathname, u.search);
};

test("every route survives a round trip through its address", () => {
  const routes: Route[] = [
    { page: "new" },
    { page: "new", project: "launch" },
    { page: "conversation", id: "t-1" },
    { page: "conversation", id: "t-1", share: true },
    { page: "conversation", id: "t-1", fork: "m-5" },
    { page: "conversation", id: "t-1", bringBack: "r/1" },
    { page: "conversation", id: "t-1", panel: "context" },
    { page: "conversation", id: "t-1", panel: "people" },
    { page: "conversation", id: "t-1", panel: "activity" },
    { page: "conversation", id: "t-1", panel: "activity", run: "run/with spaces" },
    { page: "conversation", id: "t-1", panel: "deliverables" },
    { page: "conversation", id: "t-1", panel: "deliverables", artifact: "art-1" },
    {
      page: "conversation",
      id: "t-1",
      panel: "deliverables",
      artifact: "art-1",
      artifactTab: "preview",
      slide: 3,
    },
    {
      page: "conversation",
      id: "t-1",
      panel: "deliverables",
      artifact: "art-1",
      artifactTab: "versions",
    },
    {
      page: "conversation",
      id: "t-1",
      panel: "deliverables",
      artifact: "art-1",
      artifactTab: "provenance",
    },
    { page: "projects" },
    { page: "projects", q: "globex" },
    { page: "projects", kind: "account" },
    { page: "projects", q: "renewals", kind: "team" },
    // A kind the workspace defines has an address too.
    { page: "projects", kind: "partner" },
    { page: "project", name: "Q4 launch", tab: "inbox" },
    { page: "project", name: "Q4 launch", tab: "inbox", fork: true },
    { page: "project", name: "Q4 launch", tab: "inbox", message: "m7" },
    { page: "project", name: "Q4 launch", tab: "overview" },
    { page: "project", name: "Q4 launch", tab: "context" },
    { page: "project", name: "", tab: "inbox" },
    { page: "project", name: "", tab: "overview" },
    { page: "project", name: "", tab: "context" },
    { page: "project", name: "", tab: "people" },
    { page: "project", name: "Globex", tab: "limits" },
    { page: "project", name: "Globex", tab: "history" },
    { page: "operations", section: "integrations", selected: "drive/account", tab: "resources" },
    { page: "operations", section: "context", selected: "source name" },
    { page: "schedules" },
    { page: "schedules", name: "nightly" },
    { page: "new", agent: "humagen-analyst", ask: "How did runs go today?" },
    { page: "monitor", tab: "overview" },
    { page: "monitor", tab: "rules" },
    { page: "monitor", tab: "health" },
    { page: "inbox", kind: "alert" },
    { page: "inbox", alert: "a1" },
    { page: "inbox" },
    { page: "notifications" },
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
  assert.equal(href({ page: "conversation", id: "abc" }), "/conversations/abc");
  assert.equal(href({ page: "artifacts", artifact: "art-2" }), "/artifacts/art-2");
  assert.equal(href({ page: "context", source: "a b", tab: "search" }), "/context/a%20b/search");
  assert.equal(href({ page: "monitor", tab: "rules" }), "/monitor/rules");
  assert.equal(href({ page: "monitor", tab: "health" }), "/monitor/health");
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
  assert.equal(
    href({ page: "project", name: "Globex", tab: "inbox", message: "m1" }),
    "/projects/Globex?message=m1",
  );
  assert.equal(href({ page: "project", name: "", tab: "people" }), "/projects/~/people");
  assert.equal(
    href({ page: "project", name: "Q4 launch", tab: "history" }),
    "/projects/Q4%20launch/history",
  );
  assert.deepEqual(at("/projects/Globex/inbox?message=m1"), {
    page: "project",
    name: "Globex",
    tab: "inbox",
    message: "m1",
  });
  // A message is a selection in the inbox, and nowhere else.
  assert.deepEqual(at("/projects/Globex/people?message=m1"), {
    page: "project",
    name: "Globex",
    tab: "people",
  });
});

test("a project's address is its name, ~ is the workspace, and the default tab has no segment", () => {
  assert.equal(
    href({ page: "project", name: "Globex", tab: "overview" }),
    "/projects/Globex/overview",
  );
  assert.equal(
    href({ page: "project", name: "Globex", tab: "context" }),
    "/projects/Globex/context",
  );
  assert.equal(
    href({ page: "project", name: "Field engineering", tab: "context" }),
    "/projects/Field%20engineering/context",
  );
  assert.equal(href({ page: "project", name: "", tab: "inbox" }), "/projects/~");
  assert.equal(href({ page: "project", name: "", tab: "context" }), "/projects/~/context");
  assert.deepEqual(at("/projects/~"), { page: "project", name: "", tab: "inbox" });
  assert.equal(
    href({ page: "projects", q: "globex", kind: "account" }),
    "/projects?q=globex&kind=account",
  );
});

test("a project's schedules have their own address", () => {
  assert.equal(
    href({ page: "project", name: "Field", tab: "schedules" }),
    "/projects/Field/schedules",
  );
  assert.deepEqual(at("/projects/Field/schedules"), {
    page: "project",
    name: "Field",
    tab: "schedules",
  });
  assert.deepEqual(at("/projects/~/schedules"), { page: "project", name: "", tab: "schedules" });
});

test("a project's Agents tab has its own path, the workspace's included", () => {
  assert.equal(href({ page: "project", name: "Globex", tab: "agents" }), "/projects/Globex/agents");
  assert.equal(href({ page: "project", name: "", tab: "agents" }), "/projects/~/agents");
  assert.deepEqual(at("/projects/~/agents"), { page: "project", name: "", tab: "agents" });
  assert.deepEqual(at("/projects/Globex/agents"), {
    page: "project",
    name: "Globex",
    tab: "agents",
  });
});

test("a project tab or kind it does not know falls back to the default", () => {
  assert.deepEqual(at("/projects/Globex/bogus"), { page: "project", name: "Globex", tab: "inbox" });
  assert.deepEqual(at("/projects?kind=Planet!"), { page: "projects" });
  assert.deepEqual(at("/projects?q="), { page: "projects" });
  assert.deepEqual(at("/?view=projects"), { page: "projects" });
});

test("a channel is a page under its project's Channels tab, and ~ is the workspace's", () => {
  assert.equal(
    href({ page: "project", name: "Globex", tab: "channels" }),
    "/projects/Globex/channels",
  );
  assert.equal(
    href({ page: "channel", project: "Globex", channel: "renewals" }),
    "/projects/Globex/channels/renewals",
  );
  assert.equal(
    href({ page: "channel", project: "", channel: "general" }),
    "/projects/~/channels/general",
  );
  assert.equal(
    href({ page: "channel", project: "Field engineering", channel: "ops" }),
    "/projects/Field%20engineering/channels/ops",
  );
  for (const r of [
    { page: "channel", project: "Globex", channel: "renewals" },
    { page: "channel", project: "", channel: "general" },
    { page: "project", name: "Globex", tab: "channels" },
  ] as const) {
    assert.deepEqual(at(href(r)), r);
  }
});

test("a task opens at one of its messages, and a new task starts in a channel", () => {
  assert.equal(
    href({ page: "conversation", id: "th-0123456789abcdef", message: "m-3" }),
    "/conversations/th-0123456789abcdef?message=m-3",
  );
  assert.deepEqual(at("/conversations/th-0123456789abcdef?message=m-3"), {
    page: "conversation",
    id: "th-0123456789abcdef",
    message: "m-3",
  });
  assert.deepEqual(at("/tasks/th-0123456789abcdef?message="), {
    page: "conversation",
    id: "th-0123456789abcdef",
  });
  assert.equal(
    href({ page: "new", project: "Globex", channel: "renewals" }),
    "/?project=Globex&channel=renewals",
  );
  assert.deepEqual(at("/?project=Globex&channel=renewals"), {
    page: "new",
    project: "Globex",
    channel: "renewals",
  });
});

test("a conversation's Share dialog has an address of its own, and closing it goes back to the conversation", () => {
  assert.equal(
    href({ page: "conversation", id: "th-0123456789abcdef", share: true }),
    "/conversations/th-0123456789abcdef/share",
  );
  assert.deepEqual(at("/conversations/th-0123456789abcdef/share"), {
    page: "conversation",
    id: "th-0123456789abcdef",
    share: true,
  });
  assert.deepEqual(at(href({ page: "conversation", id: "a b", share: true })), {
    page: "conversation",
    id: "a b",
    share: true,
  });
  // Another word after the task is the task itself.
  assert.deepEqual(at("/conversations/th-0123456789abcdef/other"), {
    page: "conversation",
    id: "th-0123456789abcdef",
  });
});

test("a conversation's old address, from when it was called a task, opens it and is rewritten to its own", () => {
  for (const [old, now] of [
    ["/tasks/th-0123456789abcdef", { page: "conversation", id: "th-0123456789abcdef" }],
    [
      "/tasks/th-0123456789abcdef?message=m-3",
      { page: "conversation", id: "th-0123456789abcdef", message: "m-3" },
    ],
    [
      "/tasks/th-0123456789abcdef/share",
      { page: "conversation", id: "th-0123456789abcdef", share: true },
    ],
  ] as const) {
    assert.deepEqual(at(old), now);
    assert.equal(href(at(old)), old.replace("/tasks/", "/conversations/"));
  }
});

test("old ?view= links open the page they named", () => {
  assert.deepEqual(at("/?view=agents"), { page: "agents" });
  assert.deepEqual(at("/?view=storage"), { page: "artifacts" });
  assert.deepEqual(at("/?view=analytics"), { page: "monitor", tab: "overview" });
  // What fired lives in the inbox now: an alert's old link opens the message it raised.
  assert.deepEqual(at("/?view=alerts&alert=a9"), { page: "inbox", alert: "a9" });
  assert.deepEqual(at("/?view=alerts"), { page: "inbox", kind: "alert" });
  assert.deepEqual(at("/?view=knowledge"), { page: "context" });
  assert.deepEqual(at("/?view=access&permission=runs:write"), {
    page: "settings",
    section: "access",
    permission: "runs:write",
  });
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
  assert.deepEqual(at("/knowledge/handbook/search"), {
    page: "context",
    source: "handbook",
    tab: "search",
  });
  assert.deepEqual(at("/agents/lead/knowledge"), { page: "agent", name: "lead", tab: "context" });
});

test("the read audit has a Settings address", () => {
  assert.equal(href({ page: "settings", section: "read-audit" }), "/settings/read-audit");
  assert.deepEqual(at("/settings/read-audit"), { page: "settings", section: "read-audit" });
});

test("the tab title names the place, then the product, with unread in front", () => {
  assert.equal(documentTitle({ page: "inbox" }), "Notifications · Humagen");
  assert.equal(documentTitle({ page: "inbox" }, 3), "(3) Notifications · Humagen");
  assert.equal(
    documentTitle({ page: "agent", name: "analyst", tab: "prompt" }),
    "analyst · Humagen",
  );
  assert.equal(documentTitle({ page: "project", name: "", tab: "inbox" }), "Workspace · Humagen");
  assert.equal(
    documentTitle({ page: "channel", project: "ops", channel: "launch" }),
    "#launch · Humagen",
  );
});

test("an explicit Company composer destination is distinct from the personal Home default", () => {
  assert.equal(href({ page: "new", project: "" }), "/?project=%7E");
  assert.deepEqual(at("/?project=%7E"), { page: "new", project: "" });
  assert.deepEqual(at("/"), { page: "new" });
});

test("Library Operations and You selections have reloadable canonical addresses", () => {
  const routes: Route[] = [
    { page: "library", section: "skills" },
    { page: "library", section: "memory" },
    { page: "library", section: "sources" },
    { page: "operations", section: "integrations" },
    { page: "operations", section: "context" },
    { page: "assignments", id: "as/shared 1" },
    { page: "agent-network", handoff: "h/shared 1" },
    { page: "agent-control", agent: "helper" },
    { page: "agent-control" },
    { page: "action-policy", scope: "agent", name: "helper" },
    { page: "audit" },
    { page: "you", section: "profile" },
    { page: "you", section: "connections", provider: "drive" },
    { page: "agent-setup", name: "draft" },
    { page: "agent-setup" },
    { page: "usage", days: 14, day: "2026-10-20", display: "table" },
  ];
  for (const route of routes) {
    const url = new URL(href(route), "http://localhost");
    assert.deepEqual(parse(url.pathname, url.search), route, href(route));
  }
  assert.deepEqual(parse("/operations/usage", "?day=2026-02-30&days=999&display=unknown"), {
    page: "usage",
    days: 14,
    display: "chart",
  });
});

test("Activity filters and tool visibility survive copied conversation links", () => {
  const route: Route = {
    page: "conversation",
    id: "thread-1",
    panel: "activity",
    run: "run-2",
    filter: "decisions",
    showTools: false,
  };
  const url = new URL(href(route), "http://localhost");
  assert.deepEqual(parse(url.pathname, url.search), route);
  assert.equal(href(route), "/conversations/thread-1/activity/run-2?filter=decisions&tools=0");
});

test("Library resource links retain their confined scope and selection", () => {
  for (const route of [
    {
      page: "library",
      section: "skills",
      scope: "project",
      project: "Field engineering",
      skill: "sk-123",
    },
    { page: "library", section: "memory", scope: "agent", owner: "helper" },
  ] as Route[]) {
    const url = new URL(href(route), "http://localhost");
    assert.deepEqual(parse(url.pathname, url.search), route);
  }
  assert.deepEqual(parse("/library/skills", `?scope=unexpected&skill=${"x".repeat(161)}`), {
    page: "library",
    section: "skills",
  });
});

test("Company policy links use workspace authority rather than an empty project policy", () => {
  assert.equal(
    href({ page: "action-policy", scope: "project", name: "~" }),
    "/operations/policies",
  );
  assert.equal(href({ page: "action-policy", scope: "project", name: "" }), "/operations/policies");
  assert.equal(
    at(href({ page: "action-policy", scope: "project", name: "Launch" })).page,
    "action-policy",
  );
});

test("exact published skill links preserve the selected immutable version", () => {
  const route: Route = {
    page: "library",
    section: "skills",
    scope: "personal",
    skill: "opaque-skill",
    version: 1,
  };
  assert.deepEqual(at(href(route)), route);
  assert.equal(at("/library/skills?skill=x&version=-1").page, "library");
});

test("conversation Instructions context is addressable and reloadable", () => {
  const route = {
    page: "conversation",
    id: "saved thread",
    panel: "context",
    contextTab: "instructions",
  } as const;
  assert.equal(href(route), "/conversations/saved%20thread/context/instructions");
  assert.deepEqual(parse(href(route)), route);
});

test("delivered notifications have a top-level address distinct from delivery preferences", () => {
  assert.equal(href({ page: "notifications" }), "/notifications");
  assert.deepEqual(at("/notifications"), { page: "notifications" });
  assert.equal(documentTitle({ page: "notifications" }), "Notifications · Humagen");
  assert.equal(href({ page: "you", section: "notifications" }), "/you/notifications");
  assert.deepEqual(at("/you/notifications"), { page: "you", section: "notifications" });
});

test("Notifications deep links preserve the selected message and combined feed kind", () => {
  const route = { page: "notifications", message: "review-one", kind: "approval" } as const;
  const url = new URL(href(route), "http://localhost");
  assert.equal(url.pathname, "/notifications");
  assert.deepEqual(parse(url.pathname, url.search), route);
  assert.deepEqual(at("/notifications?alert=rule-one"), {
    page: "notifications",
    alert: "rule-one",
  });
});

test("a change of screen is told from a change on the same screen", () => {
  const same = (a: Route, b: Route) => screenOf(a) === screenOf(b);
  // Another page, or the same page about another thing, is another screen.
  assert.ok(!same({ page: "agents" }, { page: "projects" }));
  assert.ok(!same({ page: "conversation", id: "a" }, { page: "conversation", id: "b" }));
  assert.ok(!same({ page: "library", section: "skills" }, { page: "library", section: "memory" }));
  // A tab, a panel or a filter on the same screen is not.
  assert.ok(
    same(
      { page: "project", name: "launch", tab: "inbox" },
      { page: "project", name: "launch", tab: "agents" },
    ),
  );
  assert.ok(
    same({ page: "conversation", id: "a" }, { page: "conversation", id: "a", share: true }),
  );
  assert.ok(same({ page: "projects" }, { page: "projects", q: "launch" }));
  // The old inbox address is the Notifications screen.
  assert.ok(same({ page: "inbox" }, { page: "notifications" }));
});
