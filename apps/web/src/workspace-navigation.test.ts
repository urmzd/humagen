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
    [{ label: "Alert Rules", active: true, href: "/monitor/rules" }],
  );
  assert.equal(workspaceHub({ page: "inbox" }), null);
  assert.deepEqual(
    hubTabs("operations", { page: "new" }, () => false),
    [],
  );
});
