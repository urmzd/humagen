import assert from "node:assert/strict";
import { test } from "node:test";
import { href, parse, type Route } from "./route.ts";
import { hubTabs, settingsPageLabel, workspaceHub } from "./workspace-navigation.ts";

test("Settings pages have one navigation frame without a legacy Operations tab strip", () => {
  for (const [path, label] of [
    ["/settings/people", "People"],
    ["/settings/usage", "Usage"],
    ["/settings/health", "Health"],
    ["/operations/policies", "Policies"],
    ["/settings/audit", "Audit"],
    ["/agent-controls", "Agent controls"],
  ] as const) {
    const route = parse(path, "");
    assert.equal(settingsPageLabel(route), label, path);
    assert.equal(workspaceHub(route), null, path);
  }
  // Projects has its own place in the sidebar, not under Settings.
  assert.equal(settingsPageLabel(parse("/projects", "")), undefined);
  assert.equal(workspaceHub({ page: "operations", section: "context" }), "operations");
  assert.equal(settingsPageLabel({ page: "operations", section: "context" }), undefined);
});

test("Agents holds skills and knowledge in its tabs (memory is each agent's own), and artifacts have their own page", () => {
  const route: Route = {
    page: "library",
    section: "skills",
    scope: "personal",
    skill: "sk-one",
    version: 2,
    file: "references/style.md",
    libraryTab: "contents",
  };
  const url = new URL(href(route), "http://localhost");
  assert.deepEqual(parse(url.pathname, url.search), route);
  assert.equal(workspaceHub(route), "agents");
  const tabs = hubTabs("agents", route, () => true);
  assert.deepEqual(
    tabs.map((tab) => tab.label),
    ["Agents", "Skills", "Knowledge"],
  );
  assert.deepEqual(
    tabs.filter((tab) => tab.active).map((tab) => tab.label),
    ["Skills"],
  );
  assert.equal(workspaceHub({ page: "artifacts", artifact: "file-one" }), null);
  const deliverable: Route = { page: "library", section: "deliverables", artifact: "file-one" };
  const link = new URL(href(deliverable), "http://localhost");
  assert.deepEqual(parse(link.pathname, link.search), deliverable);
});

test("consolidated tabs retain native permissions and alert rule discovery", () => {
  assert.deepEqual(
    hubTabs("library", { page: "new" }, (permission) => permission === "runs:read").map(
      (tab) => tab.label,
    ),
    ["Memory"],
  );
  // Memory is on each agent's page, not a tab of the Agents hub.
  assert.deepEqual(
    hubTabs("agents", { page: "new" }, (permission) => permission === "runs:read").map(
      (tab) => tab.label,
    ),
    [],
  );
  assert.deepEqual(
    hubTabs(
      "operations",
      { page: "monitor", tab: "rules" },
      (permission) => permission === "alerts:read",
    ).map((tab) => ({ label: tab.label, active: tab.active, href: href(tab.route) })),
    [{ label: "Health", active: false, href: "/settings/health" }],
  );
  assert.deepEqual(
    hubTabs("operations", { page: "new" }, () => false),
    [],
  );
});

test("Notifications is one page with one title: no tab strip above it", () => {
  for (const route of [
    { page: "inbox", message: "review-one" },
    { page: "notifications" },
    { page: "monitor", tab: "rules" },
  ] as const) {
    assert.equal(workspaceHub(route), "notifications");
    const link = new URL(href(route), "http://localhost");
    assert.deepEqual(
      parse(link.pathname, link.search),
      route.page === "monitor" ? { page: "alerts", tab: "watching" } : { page: "alerts" },
    );
    assert.deepEqual(
      hubTabs("notifications", route, () => true),
      [],
    );
  }
});

test("the Knowledge tab opens the corpora and stays current on a corpus and its sources", () => {
  for (const route of [
    { page: "knowledge" },
    { page: "corpus", name: "hr-policies", tab: "access" },
    { page: "context", corpus: "hr-policies", source: "handbook" },
    { page: "context", source: "handbook" },
  ] as Route[]) {
    assert.equal(workspaceHub(route), "agents");
    const knowledge = hubTabs("agents", route, () => true).find((t) => t.label === "Knowledge");
    assert.equal(knowledge?.active, true);
    assert.equal(href(knowledge!.route), "/knowledge");
  }
});
