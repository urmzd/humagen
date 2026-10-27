import assert from "node:assert/strict";
import { test } from "node:test";
import { hubTabs, workspaceHub } from "./workspace-navigation.ts";
import { href, parse, type Route } from "./route.ts";

test("Library has one hub and preserves selected files in its four tabs", () => {
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
  const tabs = hubTabs("library", route, () => true);
  assert.deepEqual(
    tabs.map((tab) => tab.label),
    ["Sources", "Skills", "Memory", "Deliverables"],
  );
  assert.deepEqual(
    tabs.filter((tab) => tab.active).map((tab) => tab.label),
    ["Skills"],
  );
  assert.equal(workspaceHub({ page: "artifacts", artifact: "file-one" }), "library");
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
  assert.deepEqual(
    hubTabs("agents", { page: "new" }, (permission) => permission === "runs:read").map(
      (tab) => tab.label,
    ),
    ["Agent Network", "Assignments"],
  );
  assert.deepEqual(
    hubTabs(
      "operations",
      { page: "monitor", tab: "rules" },
      (permission) => permission === "alerts:read",
    ).map((tab) => ({ label: tab.label, active: tab.active, href: href(tab.route) })),
    [],
  );
  assert.deepEqual(
    hubTabs("operations", { page: "new" }, () => false),
    [],
  );
});

test("Notifications has one feed with permission-aware Alert Rules beside it", () => {
  const canReadRuns = (permission: string) => permission === "runs:read";
  for (const route of [{ page: "inbox", message: "review-one" }, { page: "notifications" }] as const) {
    assert.equal(workspaceHub(route), "notifications");
    const link = new URL(href(route), "http://localhost");
    assert.deepEqual(parse(link.pathname, link.search), route);
    const tabs = hubTabs("notifications", route, canReadRuns);
    assert.deepEqual(tabs.map(tab => ({ label: tab.label, href: href(tab.route), active: tab.active })), [
      { label: "Notifications", href: "/notifications", active: true },
    ]);
  }
  const rules: Route = { page: "monitor", tab: "rules" };
  assert.equal(workspaceHub(rules), "notifications");
  assert.deepEqual(hubTabs("notifications", rules, permission => permission === "alerts:read").map(tab => ({ label: tab.label, active: tab.active })), [
    { label: "Alert Rules", active: true },
  ]);
  assert.deepEqual(hubTabs("notifications", rules, () => true).map(tab => tab.label), ["Notifications", "Alert Rules"]);
  assert.deepEqual(hubTabs("notifications", { page: "inbox" }, () => false), []);
});
