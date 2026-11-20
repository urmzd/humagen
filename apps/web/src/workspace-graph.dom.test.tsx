import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { stubFetch } from "./dom.ts";
import { WorkspaceGraph } from "./WorkspaceGraph.tsx";

const words = defaultCopy.corpora;
const gw = words.graph;
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

const at = "2026-11-07T12:00:00Z";
const corpora = {
  corpora: [
    {
      name: "hr-policies",
      title: "HR policies",
      description: "",
      owner: "ada",
      readers: [],
      members: [
        {
          kind: "source",
          name: "handbook",
          holds: "files",
          provider: "manual",
          items: 2,
          status: "ready",
        },
      ],
      agents: [],
      label: "internal",
      links: "isolated",
      created_at: at,
      updated_at: at,
      version: 1,
      mine: false,
    },
  ],
};
const graph = {
  nodes: [
    {
      id: "n1",
      key: "doc:leave",
      type: "document",
      name: "Leave policy",
      uri: "https://wiki.example.com/leave",
      member: "source/handbook",
      owner: "me",
      owner_name: "Me",
      owner_state: "suggested",
      staleness: { score: 0.9, stale: true, reasons: ["old"] },
      corpora: ["hr-policies"],
    },
    {
      id: "n2",
      key: "table:employees",
      type: "Employee",
      name: "HR.PUBLIC.EMPLOYEES",
      member: "catalog/people-warehouse",
      owner_state: "unowned",
      // A corpus the list did not return goes by its name.
      corpora: ["hr-policies", "finance"],
    },
    { id: "p1", key: "person:ada", type: "person", name: "Ada", owner_state: "" },
  ],
  edges: [{ from: "p1", to: "n1", type: "authored" }],
  health: {
    nodes: 3,
    edges: 1,
    by_type: { document: 1, Employee: 1, person: 1 },
    unowned: 1,
    stale: 1,
    built_at: at,
  },
  more: true,
};

const page = (opened: string[] = [], back: string[] = []) => (
  <WorkspaceGraph
    viewer="me"
    onBack={() => back.push("knowledge")}
    onOpenCorpus={(name) => opened.push(name)}
  />
);

test("the workspace graph shows its health and each document with its owner, staleness and corpora, and nothing to decide", async () => {
  net = stubFetch({
    "GET /api/graph": () => graph,
    "GET /api/corpora": () => corpora,
  });
  const opened: string[] = [];
  render(page(opened));
  await screen.findByText(
    /^3 nodes \| 1 relation \| 2 documents \| 0 systems \| 1 person \| 1 unowned \| 1 stale \| built /,
  );
  const leave = await screen.findByRole("listitem", { name: "Leave policy" });
  assert.equal(
    within(leave).getByRole("link", { name: "Leave policy" }).getAttribute("href"),
    "https://wiki.example.com/leave",
  );
  assert.ok(within(leave).getByText(gw.you));
  assert.ok(within(leave).getByText(gw.states.suggested));
  assert.ok(within(leave).getByText(gw.staleLabel));
  // Its corpus is named by its title and opens that corpus's Graph tab.
  const from = await within(leave).findByRole("link", { name: "HR policies" });
  assert.equal(from.getAttribute("href"), "/knowledge/hr-policies/graph");
  fireEvent.click(from);
  assert.deepEqual(opened, ["hr-policies"]);
  // Owners decide on the corpus's own tab: here there is no menu and no Rebuild.
  assert.equal(within(leave).queryByRole("button"), null);
  assert.equal(screen.queryByRole("button", { name: gw.rebuild }), null);

  const table = screen.getByRole("listitem", { name: "HR.PUBLIC.EMPLOYEES" });
  assert.ok(within(table).getByText("Employee"));
  assert.ok(within(table).getByText(gw.noOwner));
  assert.ok(within(table).getByRole("link", { name: "finance" }));
  // People are part of the graph, not rows of it.
  assert.equal(screen.queryByRole("listitem", { name: "Ada" }), null);
  assert.ok(screen.getByText(gw.more));
  assert.deepEqual(net.seen.map((r) => `${r.method} ${r.path}`).sort(), [
    "GET /api/corpora",
    "GET /api/graph",
  ]);
});

test("a workspace with no graph yet says how one gets built and leads back to Knowledge", async () => {
  net = stubFetch({
    "GET /api/graph": () => ({
      nodes: [],
      edges: [],
      health: { nodes: 0, edges: 0, by_type: {}, unowned: 0, stale: 0 },
    }),
    "GET /api/corpora": () => ({ corpora: [] }),
  });
  const back: string[] = [];
  render(page([], back));
  await screen.findByText(gw.empty);
  assert.ok(screen.getByText(gw.howBuilt));
  assert.ok(screen.getByText(new RegExp(`${gw.notBuilt}$`)));
  assert.equal(screen.queryByRole("list", { name: gw.workspaceTitle }), null);
  fireEvent.click(screen.getByRole("button", { name: gw.openKnowledge }));
  assert.deepEqual(back, ["knowledge"]);
});

test("a graph that fails to load says so, shows no empty state, and loads on Retry", async () => {
  let fail = true;
  net = stubFetch({
    "GET /api/graph": () =>
      fail ? new Response(JSON.stringify({ error: "boom" }), { status: 500 }) : graph,
    "GET /api/corpora": () => corpora,
  });
  render(page());
  const alert = await screen.findByRole("alert");
  assert.equal(alert.textContent, gw.failed);
  assert.equal(screen.queryByText(gw.empty), null);
  fail = false;
  fireEvent.click(screen.getByRole("button", { name: gw.retry }));
  await screen.findByRole("listitem", { name: "Leave policy" });
  assert.equal(screen.queryByRole("alert"), null);
});
