import assert from "node:assert/strict";
import { test } from "node:test";
import { HOME_PROJECTS_MAX } from "@warp/core";
import { projectsOf, type WireProject } from "./home.ts";

/** Projects as `GET /api/projects` lists them, in the tree's order. */
const views: WireProject[] = [
  { name: "Field", title: "Field engineering", kind: "team", path: [{ name: "Field", title: "Field engineering" }], active: 1, active_at: "2026-10-20T09:00:00Z" },
  {
    name: "Globex",
    kind: "account",
    path: [
      { name: "Field", title: "Field engineering" },
      { name: "Globex" },
    ],
    active: 0,
    active_at: "2026-10-21T09:00:00Z",
  },
  { name: "Launch", kind: "team", path: [{ name: "Launch" }], active: 0 },
  { name: "Old", kind: "team", archived: true, path: [{ name: "Old" }], active: 0, active_at: "2026-10-22T09:00:00Z" },
];

test("Your projects is every project the reader has a role in but archived ones, busiest first", () => {
  const projects = projectsOf(views, (name) => `/projects/${name}`);
  assert.deepEqual(
    projects.map((p) => p.name),
    ["Globex", "Field", "Launch"],
  );
  assert.deepEqual(projects[0], {
    name: "Globex",
    title: "Globex",
    within: ["Field engineering"],
    kind: "account",
    active: 0,
    activeAt: "2026-10-21T09:00:00Z",
    url: "/projects/Globex",
  });
  assert.deepEqual(projects[2], { name: "Launch", title: "Launch", within: [], kind: "team", active: 0, url: "/projects/Launch" });
});

test("Your projects keeps them all, for Home to show the first eight and count the rest", () => {
  const many = Array.from({ length: 12 }, (_, i): WireProject => ({ name: `P${i}`, kind: "team", path: [{ name: `P${i}` }] }));
  const projects = projectsOf(many);
  assert.equal(projects.length, 12);
  assert.ok(projects.length > HOME_PROJECTS_MAX);
  assert.equal(projects[0]?.url, undefined);
});
