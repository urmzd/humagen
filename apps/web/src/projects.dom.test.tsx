import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Note, Policy, ProjectDetail } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { ProjectPage, ProjectsPage } from "./Projects.tsx";
import { east, field, globex, globexDetail, renewals, retail, stores, tree, workspace } from "./projects.fixtures.ts";
import type { ProjectTab } from "./route.ts";

const at = "2026-10-01T09:00:00Z";
const fieldLimit: Policy = { scope: { kind: "project", name: "Field" }, period: "day", max_runs: 7, action: "block", version: 1, updated_at: at };
const note = (id: string, text: string): Note => ({ id, text, source: "person", status: "approved", by: "ada", at });

let net: ReturnType<typeof stubFetch>;
afterEach(() => {
  cleanup();
  net.restore();
});

const everything = ["projects:read", "projects:write", "projects:govern", "runs:read", "runs:write", "billing:read", "schedules:read"];

function open(detail: ProjectDetail, opts: { tab?: ProjectTab; can?: string[]; routes?: Record<string, (body: unknown) => unknown> } = {}) {
  const address = detail.project.name === "" ? "~" : encodeURIComponent(detail.project.name);
  net = stubFetch({
    [`GET /api/projects/${address}`]: () => detail,
    "GET /api/threads": () => ({ threads: [], next: null }),
    [`GET /api/projects/${address}/memory`]: () => ({ notes: [] }),
    "GET /api/projects/~/memory": () => ({ notes: [note("w1", "Acme ships on Tuesdays.")] }),
    "GET /api/projects/Field/memory": () => ({ notes: [note("f1", "Field covers the east coast."), { ...note("f2", "Not approved yet."), status: "pending" }] }),
    "GET /api/policies": () => ({ policies: [fieldLimit] }),
    "GET /api/usage": () => ({ scopes: [] }),
    "GET /api/policies/history": () => ({ changes: [] }),
    ...opts.routes,
  });
  const can = opts.can ?? everything;
  const noop = () => {};
  const opened: string[] = [];
  render(
    <ProjectPage
      name={detail.project.name}
      tab={opts.tab ?? "overview"}
      onTab={noop}
      workspaceName="Acme"
      projects={tree}
      schedules={[]}
      agents={[]}
      can={(p) => can.includes(p)}
      onOpen={(name) => opened.push(name)}
      onOpenAll={noop}
      onNewChat={noop}
      onOpenThread={noop}
      onChanged={noop}
      onSchedulesChanged={noop}
      onDeleted={noop}
    />,
  );
  return { opened };
}

test("a project page shows where it sits, what sits below it, and every limit above it in words", async () => {
  const { opened } = open(globexDetail);
  const crumbs = await screen.findByRole("navigation", { name: "Where this project sits" });
  const links = within(crumbs).getAllByRole("link").map((a) => [a.textContent, a.getAttribute("href")]);
  assert.deepEqual(links, [
    ["Acme", "/projects/~"],
    ["Field engineering", "/projects/Field"],
  ]);
  assert.equal(within(crumbs).getByText("Globex").getAttribute("aria-current"), "page");
  fireEvent.click(within(crumbs).getByText("Field engineering"));
  assert.deepEqual(opened, ["Field"]);
  // Back goes to the parent, by its title.
  assert.ok(screen.getByRole("button", { name: "‹ Field engineering" }));
  assert.ok(screen.getByText("Account · Globex Inc · in Field engineering"));

  const below = screen.getByRole("region", { name: "Sub-projects" });
  assert.ok(within(below).getByText("Globex renewals"));
  assert.equal(within(below).getByRole("link").getAttribute("href"), "/projects/Globex%20renewals");

  const limits = screen.getByRole("region", { name: "Limits" });
  await within(limits).findByText("Field engineering's limit also applies here: 7 runs a day.");
  assert.ok(within(limits).getByText("The workspace's limit also applies here."));
  // Its tasks are read from the server, filed in this project only.
  assert.ok(net.seen.some((r) => r.method === "GET" && r.path === "/api/threads?limit=20&project=Globex"));
});

test("including sub-projects reads the tasks of everything below", async () => {
  open(globexDetail);
  fireEvent.click(await screen.findByRole("checkbox", { name: "Include sub-projects" }));
  await waitFor(() => assert.ok(net.seen.some((r) => r.path === "/api/threads?limit=20&within=Globex")));
});

test("the Context tab saves the instructions as a whole from the version read, and shows what comes from above, read only", async () => {
  const puts: unknown[] = [];
  open(globexDetail, {
    tab: "context",
    routes: { "PUT /api/projects/Globex": (body) => (puts.push(body), { ...globex, version: 5, instructions: "Renewals close in March." }) },
  });
  const from = await screen.findByRole("region", { name: "From Acme" });
  assert.ok(within(from).getByText("Acme sells warehouse robots. Answer plainly."));
  await within(from).findByText("Acme ships on Tuesdays.");
  const fromField = screen.getByRole("region", { name: "From Field engineering" });
  assert.ok(within(fromField).getByText("Customer accounts. Never promise dates."));
  await within(fromField).findByText("Field covers the east coast.");
  assert.ok(!screen.queryByText("Not approved yet."), "a proposed note is not sent, so it is not shown");
  // The workspace's comes first, as a turn is sent them.
  const order = screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent);
  assert.deepEqual(order, ["From Acme", "From Field engineering"]);

  const box = screen.getByRole("textbox", { name: "Instructions" }) as HTMLTextAreaElement;
  const save = screen.getByRole("button", { name: "Save Instructions" }) as HTMLButtonElement;
  assert.ok(save.disabled, "nothing to save yet");
  fireEvent.change(box, { target: { value: "Renewals close in March." } });
  fireEvent.click(save);
  await waitFor(() => assert.equal(puts.length, 1));
  assert.deepEqual(puts[0], {
    version: 4,
    description: "Renewals and support for Globex.",
    account: { customer: "Globex Inc" },
    instructions: "Renewals close in March.",
  });
});

test("the workspace and company-wide projects are changed only with projects:govern", async () => {
  const root: ProjectDetail = { project: workspace, ancestors: [], children: [field], limits: [{ scope: { kind: "tenant" }, from: "" }] };
  open(root, { tab: "context", can: ["projects:read", "projects:write"] });
  await screen.findByText("Acme sells warehouse robots. Answer plainly.");
  assert.ok(!screen.queryByRole("textbox", { name: "Instructions" }));
  assert.ok(screen.getByText("Only those who govern projects change the workspace and company-wide projects."));
  assert.ok(!screen.queryByRole("navigation", { name: "Where this project sits" }), "the workspace sits nowhere");
  assert.ok(!screen.queryByRole("button", { name: "Delete Project" }));
  cleanup();
  net.restore();

  open(root, { tab: "context" });
  assert.ok(await screen.findByRole("textbox", { name: "Instructions" }));
});

test("New Sub-project files it under this project, with only the kinds that may sit there", async () => {
  const posts: unknown[] = [];
  const { opened } = open(globexDetail, {
    routes: { "POST /api/projects": (body) => (posts.push(body), { ...renewals, name: "Globex help", path: [...globex.path, { name: "Globex help", kind: "team" }] }) },
  });
  fireEvent.click(await screen.findByRole("button", { name: "New Sub-project" }));
  const form = screen.getByRole("form", { name: "New project" });
  // An account never sits under an account, and company-wide sits only under company-wide.
  const kinds = within(within(form).getByRole("group", { name: "Kind" })).getAllByRole("button").map((b) => b.textContent);
  assert.deepEqual(kinds, ["Team"]);
  const title = within(form).getByRole("textbox", { name: /^Title/ }) as HTMLInputElement;
  const name = within(form).getByRole("textbox", { name: /^Name/ }) as HTMLInputElement;
  fireEvent.change(title, { target: { value: "Globex support!" } });
  assert.equal(name.value, "Globex support", "the name follows the title");
  fireEvent.change(name, { target: { value: "Globex help" } });
  fireEvent.change(title, { target: { value: "Support for Globex" } });
  assert.equal(name.value, "Globex help", "until the person types their own");
  fireEvent.click(within(form).getByRole("button", { name: "Create Project" }));
  await waitFor(() => assert.equal(posts.length, 1));
  assert.deepEqual(posts[0], { name: "Globex help", title: "Support for Globex", kind: "team", parent: "Globex" });
  await waitFor(() => assert.deepEqual(opened, ["Globex help"]));
});

test("nothing can be made below the fourth level", async () => {
  open({ project: stores, ancestors: [workspace, field, east, retail], children: [], limits: [] });
  await screen.findByRole("region", { name: "Sub-projects" });
  assert.ok(!screen.queryByRole("button", { name: "New Sub-project" }));
  assert.ok(screen.getByText("Projects go at most four levels deep, so nothing can sit below this one."));
});

test("a project that is not there says so, as one page", async () => {
  // Nothing is stubbed, so the project answers 404.
  net = stubFetch({});
  const noop = () => {};
  render(
    <ProjectPage
      name="Gone"
      tab="overview"
      onTab={noop}
      workspaceName="Acme"
      projects={[]}
      schedules={[]}
      agents={[]}
      can={() => true}
      onOpen={noop}
      onOpenAll={noop}
      onNewChat={noop}
      onOpenThread={noop}
      onChanged={noop}
      onSchedulesChanged={noop}
      onDeleted={noop}
    />,
  );
  assert.ok(await screen.findByRole("heading", { level: 1, name: "No project named Gone" }));
});

test("New Project offers company-wide only to those who govern, and puts the project where it was chosen", async () => {
  const posts: unknown[] = [];
  net = stubFetch({ "POST /api/projects": (body) => (posts.push(body), globex) });
  const made: string[] = [];
  render(
    <ProjectsPage projects={tree} root={workspace} workspaceName="Acme" onFilter={() => {}} canCreate canGovern={false} narrow={false} onOpen={() => {}} onCreated={(p) => made.push(p.name)} />,
  );
  fireEvent.click(screen.getByRole("button", { name: "New Project" }));
  const form = screen.getByRole("form", { name: "New project" });
  assert.ok(within(form).getByText("Only those who govern projects make company-wide ones."));
  assert.ok(!within(form).queryByRole("button", { name: "Company-wide" }));
  fireEvent.click(within(form).getByRole("button", { name: "Account" }));
  fireEvent.change(within(form).getByRole("textbox", { name: /^Title/ }), { target: { value: "Hooli" } });
  fireEvent.change(within(form).getByRole("textbox", { name: /^Customer/ }), { target: { value: "Hooli Inc" } });
  fireEvent.click(within(form).getByRole("button", { name: "Create Project" }));
  await waitFor(() => assert.equal(posts.length, 1));
  assert.deepEqual(posts[0], { name: "Hooli", kind: "account", account: { customer: "Hooli Inc" } });
  await waitFor(() => assert.deepEqual(made, ["Globex"]));
});
