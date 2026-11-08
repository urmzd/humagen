import assert from "node:assert/strict";
import { test } from "node:test";
import { documentTitle, href, parse, type Route, screenOf } from "./route.ts";

const at = (url: string) => {
  const u = new URL(url, "http://x");
  return parse(u.pathname, u.search);
};

test("canonical session selections survive copied links", () => {
  const routes: Route[] = [
    { page: "new" },
    { page: "new", project: "launch" },
    { page: "conversation", id: "t-1" },
    { page: "conversation", id: "t-1", share: true },
    { page: "conversation", id: "t-1", fork: "m-5" },
    { page: "conversation", id: "t-1", bringBack: "r/1" },
    { page: "conversation", id: "t-1", panel: "context" },
    { page: "conversation", id: "t-1", panel: "overview" },
    { page: "conversation", id: "t-1", panel: "people" },
    { page: "conversation", id: "t-1", panel: "activity" },
    { page: "conversation", id: "t-1", panel: "activity", run: "run/with spaces" },
    { page: "conversation", id: "t-1", panel: "deliverables" },
    { page: "conversation", id: "t-1", panel: "workspace" },
    { page: "conversation", id: "t-1", panel: "workspace", workspaceSection: "sandboxes" },
    {
      page: "conversation",
      id: "t-1",
      panel: "workspace",
      workspaceSection: "sandboxes",
      sandbox: "r/with spaces",
    },
    { page: "conversation", id: "t-1", panel: "workspace", workspaceSection: "artifacts" },
    { page: "conversation", id: "t-1", panel: "workspace", workspaceSection: "scratchpads" },
    { page: "conversation", id: "t-1", panel: "workspace", workspaceSection: "memories" },
    {
      page: "conversation",
      id: "t-1",
      panel: "workspace",
      workspaceSection: "memories",
      memoryRun: "r-1",
    },
    {
      page: "conversation",
      id: "t-1",
      panel: "workspace",
      workspaceSection: "scratchpads",
      scratchpad: "sp-0123",
    },
    {
      page: "conversation",
      id: "t-1",
      panel: "workspace",
      file: "src/a file.ts",
      workspaceTab: "edit",
    },
    {
      page: "conversation",
      id: "t-1",
      panel: "workspace",
      file: "scratchpads/想法.md",
      workspaceTab: "preview",
    },
    {
      page: "conversation",
      id: "t-1",
      panel: "workspace",
      file: "pr #42.diff",
      workspaceTab: "changes",
    },
    { page: "conversation", id: "t-1", panel: "deliverables", artifact: "art-1" },
    {
      page: "conversation",
      id: "t-1",
      panel: "deliverables",
      artifact: "art-2",
      artifactTab: "versions",
      compare: "art-1",
    },
    {
      page: "conversation",
      id: "t-1",
      panel: "workspace",
      tabs: ["artifact:art-2"],
      artifactTab: "versions",
      compare: "art-1",
    },
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
  ];
  for (const route of routes) assert.deepEqual(at(href(route)), route, href(route));
});

test("all connection addresses lead to the shared Connections page", () => {
  assert.deepEqual(at("/connections"), { page: "connections" });
  assert.deepEqual(at("/connections/issues"), { page: "connections", provider: "issues" });
  // The workspace's clients stay a section of Settings, old links included.
  assert.deepEqual(at("/settings/connections"), { page: "connections" });
  assert.deepEqual(at("/?view=connections"), { page: "connections" });
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

test("an unknown page stays visible as not found while legacy tabs fall back safely", () => {
  assert.deepEqual(at("/nowhere"), { page: "not-found", path: "/nowhere" });
  assert.equal(href(at("/nowhere")), "/nowhere");
  assert.equal(documentTitle(at("/nowhere")), "Page not found · Humagen");
  assert.deepEqual(at("/agents/lead/bogus"), { page: "agent", name: "lead", tab: "prompt" });
  assert.deepEqual(at("/settings/bogus"), { page: "settings", section: "profile" });
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

test("catalog search has an address", () => {
  const route = { page: "library", section: "catalog", catalogQuery: "customer" } as const;
  assert.equal(href(at(href(route))), href(route));
});

test("canonical sessions and alerts retain filters and anchors", () => {
  const routes: Route[] = [
    { page: "sessions" },
    { page: "sessions", q: "code review", project: "a b", agent: "reviewer", status: "waiting" },
    { page: "alerts" },
    { page: "alerts", tab: "watching" },
    {
      page: "alerts",
      state: "snoozed",
      priority: "urgent",
      mine: false,
      source: "run_failed",
      alert: "a1",
    },
    { page: "conversation", id: "s_a/b", panel: "activity", step: "step/1", run: "r1" },
    { page: "conversation", id: "s_a", panel: "context", contextTab: "instructions" },
    { page: "project", name: "Q4 launch", tab: "context" },
    { page: "project", name: "", tab: "people" },
    { page: "library", section: "catalog", catalogQuery: "warehouse" },
    { page: "connections", provider: "callback" },
  ];
  for (const route of routes) assert.deepEqual(at(href(route)), route);
  assert.equal(
    href({ page: "conversation", id: "s_a", panel: "activity", step: "step1" }),
    "/sessions/s_a?panel=steps&step=step1",
  );
  assert.deepEqual(at("/sessions/s_a?panel=steps&step=step1"), {
    page: "conversation",
    id: "s_a",
    panel: "activity",
    step: "step1",
  });
});
test("old conversation and panel URLs retain selections under canonical sessions", () => {
  const links: [string, Route][] = [
    ["/conversations/abc?message=m1", { page: "conversation", id: "abc", message: "m1" }],
    ["/tasks/abc/share", { page: "conversation", id: "abc", share: true }],
    [
      "/conversations/abc/activity/r1?tools=0&step=st1&filter=tools",
      {
        page: "conversation",
        id: "abc",
        panel: "activity",
        run: "r1",
        step: "st1",
        filter: "tools",
        showTools: false,
      },
    ],
    [
      "/conversations/abc/context/instructions",
      { page: "conversation", id: "abc", panel: "context", contextTab: "instructions" },
    ],
    [
      "/conversations/abc/workspace/scratchpads/sp1?file=note.md",
      {
        page: "conversation",
        id: "abc",
        panel: "workspace",
        workspaceSection: "scratchpads",
        scratchpad: "sp1",
        file: "note.md",
      },
    ],
  ];
  for (const [url, route] of links) {
    assert.deepEqual(at(url), route);
    assert.match(href(at(url)), /^\/sessions\//);
  }
});
test("project chat addresses migrate to context or filtered sessions", () => {
  for (const tab of ["", "/inbox", "/history", "/overview", "/schedules", "/unknown"])
    assert.deepEqual(at(`/projects/Launch${tab}`), {
      page: "project",
      name: "Launch",
      tab: "context",
    });
  assert.deepEqual(at("/projects/Launch/channels/deals"), { page: "sessions", project: "Launch" });
  assert.equal(
    href({ page: "project", name: "Launch", tab: "inbox", message: "old" }),
    "/projects/Launch/context",
  );
});
test("old hubs resolve to the seven-entry information architecture", () => {
  const links: [string, Route][] = [
    ["/history?status=needs&q=review", { page: "sessions", status: "waiting", q: "review" }],
    ["/notifications?message=old", { page: "alerts" }],
    ["/inbox?alert=a1", { page: "alerts", alert: "a1" }],
    ["/alerts/rules", { page: "alerts", tab: "watching" }],
    ["/?view=alerts", { page: "alerts" }],
    ["/customize/agents", { page: "agents" }],
    ["/customize/tools", { page: "connections", provider: "other" }],
    ["/library/sources", { page: "connections" }],
    ["/operations/integrations", { page: "connections" }],
    ["/you/profile", { page: "settings", section: "profile" }],
    ["/directory", { page: "directory", tab: "people" }],
  ];
  for (const [url, route] of links) assert.deepEqual(at(url), route, url);
  assert.equal(href({ page: "directory", tab: "people" }), "/settings/people");
  assert.equal(documentTitle({ page: "sessions" }), "Sessions · Humagen");
  assert.equal(documentTitle({ page: "alerts" }, 2), "(2) Alerts · Humagen");
});

test("agent health filters have a bounded canonical address", () => {
  const route = { page: "agents", state: "needs-attention" } as const;
  assert.equal(href(route), "/agents?state=needs-attention");
  assert.deepEqual(at(href(route)), route);
  assert.deepEqual(at(`/agents?state=${"a".repeat(81)}`), { page: "agents" });
});
