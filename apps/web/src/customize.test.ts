import assert from "node:assert/strict";
import { test } from "node:test";
import { href, parse } from "./route.ts";
import { hubTabs, workspaceHub } from "./workspace-navigation.ts";

test("Customize sections have stable addresses", () => {
  for (const section of ["agents", "skills", "connections", "tools"] as const) {
    const route = { page: "customize", section } as const;
    const target =
      section === "agents"
        ? { page: "agents" }
        : section === "skills"
          ? { page: "library", section: "skills" }
          : section === "tools"
            ? { page: "connections", provider: "other" }
            : { page: "connections" };
    assert.deepEqual(parse(href(route)), target);
    assert.equal(
      workspaceHub(route),
      section === "agents" ? "agents" : section === "skills" ? "library" : null,
    );
  }
});
test("Customize sections follow existing read permissions", () => {
  const route = { page: "customize", section: "agents" } as const;
  assert.equal(hubTabs("customize", route, () => false).length, 0);
  assert.deepEqual(
    hubTabs("customize", route, (p) => p === "runs:read").map((t) => t.label),
    ["Connected Accounts", "Tool Servers"],
  );
});

test("Customize skill links retain file and version selections", () => {
  const route = {
    page: "customize",
    section: "skills",
    scope: "personal",
    skill: "review",
    version: 2,
    file: "SKILL.md",
    libraryTab: "contents",
  } as const;
  const address = new URL(href(route), "https://example.com");
  assert.deepEqual(parse(address.pathname, address.search), { ...route, page: "library" });
});
