import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Person, ProjectAgentsView, ProjectKindSpec, Revision } from "./api.ts";
import { DirectoryPage } from "./Directory.tsx";
import { stubFetch } from "./dom.ts";
import { ProjectAgentsTab } from "./ProjectAgents.tsx";
import { field, globex, tree, view } from "./projects.fixtures.ts";
import type { DirectoryTab } from "./route.ts";

const at = "2026-10-01T09:00:00Z";
const agent = (name: string, description = ""): Revision => ({
  rev: 1,
  spec: { name, description, provider: "offline", model: "offline", system_prompt: `${name}.` },
  created_at: at,
  created_by: "ada",
  hash: name,
});
const agents = [agent("lead", "Coordinates work."), agent("helper"), agent("globex-helper", "Knows the Globex account.")];
const homes = { "globex-helper": "Globex" };
const enabledIn = { lead: ["~"], helper: ["~"], "globex-helper": [] };

// The loader imports every DOM file into one process, where a top-level hook
// runs after other files' tests too, so it acts only on what this file set up.
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

const dana: Person = { subject: "okta|dana", name: "Dana Scully", handle: "dana-scully", roles: ["operator"], picture: "/api/people/dana-scully/photo?v=2" };
const fox: Person = { subject: "okta|fox", handle: "fox", roles: [], picture: null };

function openDirectory(tab: DirectoryTab, can = ["people:read", "agents:read", "projects:read"], projects = tree, kinds: ProjectKindSpec[] = [], agentHomes: Record<string, string> = homes) {
  net = stubFetch({ "GET /api/people": () => ({ people: [dana, fox], next: null }) });
  const opened: string[] = [];
  render(
    <DirectoryPage
      tab={tab}
      onTab={() => {}}
      can={(p) => can.includes(p)}
      agents={agents}
      homes={agentHomes}
      enabledIn={enabledIn}
      projects={projects}
      kinds={kinds}
      workspaceName="Acme"
      onOpenAgent={(n) => opened.push(`agent:${n}`)}
      onOpenProject={(n) => opened.push(`project:${n}`)}
    />,
  );
  return { opened };
}

test("the Directory's people: each by name and handle with their own picture, and no letter in a box for one without", async () => {
  openDirectory("people");
  assert.ok(screen.getByRole("heading", { name: "Directory" }));
  await screen.findByText("Dana Scully");
  assert.ok(screen.getByText(/@dana-scully · operator/));
  assert.ok(screen.getByText(/@fox · No role/));
  const pictures = document.querySelectorAll("img");
  assert.equal(pictures.length, 1);
  assert.equal(pictures[0]?.getAttribute("src"), "/api/people/dana-scully/photo?v=2");
  // Fox has no picture: a still circle, with nothing written in it.
  assert.ok(!screen.queryByText("F"));
});

test("the Directory's agents: where each lives or that it is a platform agent, and where it is enabled", () => {
  const { opened } = openDirectory("agents");
  assert.ok(screen.getByText("@globex-helper"));
  assert.ok(screen.getByText("Globex"));
  assert.equal(screen.getAllByText(/Company agent.*Enabled everywhere/).length, 2);
  fireEvent.click(screen.getByText("@lead"));
  assert.deepEqual(opened, ["agent:lead"]);
});

test("Company home links use the workspace name, canonical route and a touch target", () => {
  const { opened } = openDirectory("agents", undefined, tree, [], { lead: "~" });
  const link = screen.getByRole("link", { name: "Acme" });
  assert.equal(link.getAttribute("href"), "/projects/~/agents");
  assert.match(link.className, /min-h-\[44px\]/);
  fireEvent.click(link);
  assert.deepEqual(opened, ["project:"]);
});

test("the Directory's projects: the tree in words, with the agents that live in each", () => {
  openDirectory("projects");
  assert.ok(screen.getByText("Acme"));
  assert.ok(screen.getByText("Field engineering"));
  assert.ok(screen.getByText("globex-helper"));
});

test("the Directory's projects: a workspace's own kind is called by its title, as the Projects page calls it", () => {
  const launch = view("Launch", { title: "Q4 robot launch", kind: "initiative" });
  const northwind = view("Northwind", { kind: "partner", customer: "Northwind Logistics" });
  const kinds: ProjectKindSpec[] = [
    { name: "initiative", title: "Initiative", like: "team" },
    { name: "partner", title: "Partner", like: "account" },
  ];
  openDirectory("projects", undefined, [...tree, launch, northwind], kinds);
  assert.ok(screen.getByText(/^Initiative · /));
  assert.ok(screen.getByText(/^Partner · /));
  assert.ok(!screen.queryByText(/^initiative|^partner/), "not by its lowercase name");
});

test("a tab someone may not read is not offered, and the page opens on one they may", async () => {
  openDirectory("people", ["agents:read"]);
  assert.ok(!screen.queryByRole("tab", { name: "People" }));
  assert.ok(screen.getByText("@lead"));
});

const fieldAgents: ProjectAgentsView = {
  version: 3,
  enable: ["helper"],
  default: "helper",
  available: { all: true, names: [] },
  offers: [
    { agent: "*", from: "~" },
    { agent: "helper", from: "Field" },
  ],
  homes: [],
  default_resolved: { agent: "helper", from: "project:Field" },
};

function openAgents(name: string, view: ProjectAgentsView, can = { write: true, home: true }) {
  const address = name === "" ? "~" : encodeURIComponent(name);
  net = stubFetch({
    [`GET /api/projects/${address}/agents`]: () => view,
    [`PUT /api/projects/${address}/agents`]: (body) => ({ ...view, ...(body as object), version: view.version + 1 }),
    "PUT /api/agents/lead/home": () => ({ agent: "lead", home: name, enabled_in: [] }),
  });
  let changed = 0;
  render(
    <ProjectAgentsTab
      name={name}
      workspaceName="Acme"
      agents={agents}
      homes={homes}
      enabledIn={enabledIn}
      projects={tree}
      canWrite={can.write}
      canHome={can.home}
      onChanged={() => changed++}
    />,
  );
  return { changed: () => changed };
}

test("a project's Agents tab: what is available and where each comes from, and who answers untagged messages", async () => {
  openAgents(field.name, fieldAgents);
  await screen.findByRole("list", { name: "Available here" });
  assert.ok(screen.getByText("from Acme"));
  assert.ok(screen.getByText("@helper answers untagged messages here."));
});

test("changing the default writes the lists as a whole, at the version read", async () => {
  const opened = openAgents(field.name, fieldAgents);
  await screen.findByRole("list", { name: "Available here" });
  fireEvent.click(screen.getByRole("button", { name: /^Default:/ }));
  fireEvent.click(await screen.findByRole("option", { name: /@auto/ }));
  fireEvent.click(screen.getByRole("button", { name: "Save Agents" }));
  await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "PUT")));
  const put = net?.seen.find((r) => r.method === "PUT");
  assert.deepEqual(put?.body, { version: 3, enable: ["helper"], default: "auto" });
  await waitFor(() => assert.equal(opened.changed(), 1));
});

test("on the workspace the default is the Workspace Default, and someone who may not change it sees it still", async () => {
  openAgents("", { ...fieldAgents, enable: ["*"], default: "auto", default_resolved: { agent: "auto", from: "project:~" } }, { write: false, home: false });
  await screen.findByRole("list", { name: "Available here" });
  const control = screen.getByRole("button", { name: /^Workspace Default:/ }) as HTMLButtonElement;
  assert.ok(control.disabled);
  assert.ok(!screen.queryByRole("button", { name: "Save Agents" }));
  // The workspace has no home for agents: they live in projects.
  assert.ok(!screen.queryByText("Lives here"));
});

test("an agent comes to live in a project from its Agents tab", async () => {
  openAgents(globex.name, { ...fieldAgents, homes: ["globex-helper"] });
  await screen.findByRole("list", { name: "Available here" });
  fireEvent.click(screen.getByRole("button", { name: /Lives Here: Lives here/ }));
  fireEvent.click(await screen.findByRole("button", { name: /^lead/ }));
  await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "PUT" && r.path === "/api/agents/lead/home")));
});
