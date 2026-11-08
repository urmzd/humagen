import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { setAgentHues } from "./agentHues.ts";
import type { Note, Policy, ProjectDetail, ScheduleView, ThreadSummary } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { ProjectKnowledgeSection } from "./ProjectKnowledge.tsx";
import { ProjectPage, ProjectsPage } from "./Projects.tsx";
import { ProjectWork } from "./ProjectWork.tsx";
import {
  east,
  field,
  globex,
  globexDetail,
  globexKnowledge,
  knowledgeSources,
  platform,
  projectView,
  renewals,
  retail,
  stores,
  tree,
  view,
  workspace,
} from "./projects.fixtures.ts";
import type { ProjectTab } from "./route.ts";

const at = "2026-10-01T09:00:00Z";
const fieldLimit: Policy = {
  scope: { kind: "project", name: "Field" },
  period: "day",
  max_runs: 7,
  action: "block",
  version: 1,
  updated_at: at,
};
const note = (id: string, text: string): Note => ({
  id,
  text,
  source: "person",
  status: "approved",
  by: "ada",
  at,
});

let net: ReturnType<typeof stubFetch>;
afterEach(() => {
  cleanup();
  net?.restore();
});

const everything = [
  "projects:read",
  "projects:write",
  "projects:govern",
  "runs:read",
  "runs:write",
  "billing:read",
  "schedules:read",
];

function open(
  detail: ProjectDetail,
  opts: {
    tab?: ProjectTab;
    can?: string[];
    routes?: Record<string, (body: unknown, query: URLSearchParams) => unknown>;
    schedules?: ScheduleView[];
    onMute?: (project: string, muted: boolean) => Promise<void>;
    onPin?: (project: string, pinned: boolean) => Promise<void>;
    pinned?: boolean;
  } = {},
) {
  const address = detail.project.name === "" ? "~" : encodeURIComponent(detail.project.name);
  net = stubFetch({
    [`GET /api/projects/${address}`]: () => detail,
    "GET /api/threads": () => ({ threads: [], next: null }),
    [`GET /api/projects/${address}/memory`]: () => ({ notes: [] }),
    "GET /api/projects/~/memory": () => ({ notes: [note("w1", "Acme ships on Tuesdays.")] }),
    "GET /api/projects/Field/memory": () => ({
      notes: [
        note("f1", "Field covers the east coast."),
        { ...note("f2", "Not approved yet."), status: "pending" },
      ],
    }),
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
      schedules={opts.schedules ?? []}
      agents={[]}
      can={(p) => can.includes(p)}
      onOpen={(name) => opened.push(name)}
      onOpenAll={noop}
      onNewChat={noop}
      onOpenThread={noop}
      onChanged={noop}
      onSchedulesChanged={noop}
      onDeleted={noop}
      onMute={opts.onMute}
      onPin={opts.onPin}
      pinned={opts.pinned}
    />,
  );
  return { opened };
}

test("a project with no recent conversations can be pinned from its actions", async () => {
  let selected: unknown;
  open(globexDetail, {
    onPin: async (project, pinned) => {
      selected = [project, pinned];
    },
  });
  fireEvent.click(await screen.findByRole("button", { name: "More" }));
  fireEvent.click(screen.getByRole("menuitem", { name: "Pin Project" }));
  assert.deepEqual(selected, ["Globex", true]);
});

test("a project pin refusal is reported beside the project actions", async () => {
  open(globexDetail, {
    pinned: true,
    onPin: async () => {
      throw new Error("refused");
    },
  });
  fireEvent.click(await screen.findByRole("button", { name: "More" }));
  fireEvent.click(screen.getByRole("menuitem", { name: "Unpin Project" }));
  await screen.findByText("Could not unpin. Try again.");
});

test("a project page shows where it sits, what sits below it, and every limit above it in words", async () => {
  const { opened } = open(globexDetail);
  const crumbs = await screen.findByRole("navigation", { name: "Where this project sits" });
  const links = within(crumbs)
    .getAllByRole("link")
    .map((a) => [a.textContent, a.getAttribute("href")]);
  assert.deepEqual(links, [
    ["Acme", "/projects/~/context"],
    ["Field engineering", "/projects/Field/context"],
  ]);
  assert.equal(within(crumbs).getByText("Globex").getAttribute("aria-current"), "page");
  fireEvent.click(within(crumbs).getByText("Field engineering"));
  assert.deepEqual(opened, ["Field"]);
  // Back goes to the parent, by its title.
  assert.ok(screen.getByRole("button", { name: "‹ Field engineering" }));
  assert.ok(screen.getByText("Account | Globex Inc | in Field engineering"));

  const below = screen.getByRole("region", { name: "Sub-projects" });
  assert.ok(within(below).getByText("Globex renewals"));
  assert.equal(
    within(below).getByRole("link").getAttribute("href"),
    "/projects/Globex%20renewals/context",
  );

  assert.ok(screen.getByRole("link", { name: "Manage connections" }));
  assert.equal(
    net.seen.some((request) => request.path.startsWith("/api/threads")),
    false,
  );
});

test("a project's meta line names its lead, the agent that answers what tags no agent there, in its color", async () => {
  setAgentHues({ "globex-helper": 210 });
  open(globexDetail, {
    routes: {
      "GET /api/projects/Globex/agents": () => ({
        version: 1,
        available: { all: true, names: [] },
        homes: [],
        default_resolved: { agent: "globex-helper", from: "project:Globex" },
      }),
    },
  });
  const lead = await screen.findByText("globex-helper");
  const meta = lead.closest("span.inline-flex.flex-wrap");
  assert.ok(meta);
  for (const label of ["Account", "Globex Inc", "in Field engineering", "Lead", "globex-helper"])
    assert.ok(meta.textContent?.includes(label), label);
  // Told apart by color: the agent's mark sits beside its name.
  assert.ok(lead.querySelector("[data-accent='agent']"));
});

test("a project's header is one primary action and a More menu, with Archive and Delete last after a rule", async () => {
  open(globexDetail, { onMute: async () => {} });
  const more = await screen.findByRole("button", { name: "More" });
  // Only New Conversation is a button of its own; the rest wait in the menu.
  assert.ok(screen.getByRole("button", { name: "New Session" }));
  for (const name of [
    "Mute Project",
    "Edit Project",
    "Move Project",
    "Archive Project",
    "Delete Project",
  ]) {
    assert.ok(!screen.queryByRole("button", { name }), `${name} is not a button in the header`);
  }
  assert.equal(more.getAttribute("aria-expanded"), "false");
  fireEvent.keyDown(more, { key: "ArrowDown" });
  const menu = screen.getByRole("menu", { name: "More" });
  assert.equal(more.getAttribute("aria-expanded"), "true");
  const children = [...menu.children].map((el) => (el.tagName === "HR" ? "---" : el.textContent));
  assert.deepEqual(children, [
    "Mute Project",
    "Edit Project",
    "Move Project",
    "---",
    "Archive Project",
    "Delete Project",
  ]);
  // Escape closes it and puts focus back on More.
  fireEvent.keyDown(menu, { key: "Escape" });
  assert.ok(!screen.queryByRole("menu"));
  assert.equal(document.activeElement, more);
  // Each item still does what its button did.
  fireEvent.click(more);
  fireEvent.click(screen.getByRole("menuitem", { name: "Edit Project" }));
  assert.ok(!screen.queryByRole("menu"));
  assert.ok(await screen.findByRole("button", { name: "Cancel" }));
});

test("Delete Project cannot be undone, so it asks twice, and Escape or moving away keeps the project", async () => {
  let deleted = 0;
  open(globexDetail, {
    routes: {
      "DELETE /api/projects/Globex": () => {
        deleted++;
        return undefined;
      },
    },
  });
  const ask = async () => {
    fireEvent.click(await screen.findByRole("button", { name: "More" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete Project" }));
    return screen.getByRole("group", { name: /^Delete .*\? This cannot be undone\.$/ });
  };
  // Step one only asks, in words, with focus on keeping it.
  let step = await ask();
  assert.equal(document.activeElement?.textContent, "Keep It");
  fireEvent.keyDown(step, { key: "Escape" });
  assert.ok(!screen.queryByRole("group", { name: /This cannot be undone/ }));
  // Moving focus away keeps it too.
  step = await ask();
  fireEvent.blur(within(step).getByRole("button", { name: "Keep It" }), {
    relatedTarget: document.body,
  });
  assert.ok(!screen.queryByRole("group", { name: /This cannot be undone/ }));
  assert.equal(deleted, 0);
  // Step two deletes.
  step = await ask();
  fireEvent.click(within(step).getByRole("button", { name: "Delete Project" }));
  await waitFor(() => assert.equal(deleted, 1));
});

test("Archive Project can be undone, so it acts at once", async () => {
  open(globexDetail, { routes: { "PUT /api/projects/Globex": () => globex } });
  fireEvent.click(await screen.findByRole("button", { name: "More" }));
  fireEvent.click(screen.getByRole("menuitem", { name: "Archive Project" }));
  assert.ok(!screen.queryByRole("group", { name: /This cannot be undone/ }));
  await waitFor(() =>
    assert.ok(net.seen.some((r) => r.method === "PUT" && r.path === "/api/projects/Globex")),
  );
});

test("Mute Project says so beside it when the mute could not be saved", async () => {
  const asked: [string, boolean][] = [];
  open(globexDetail, {
    onMute: (project, muted) => {
      asked.push([project, muted]);
      return Promise.reject(new Error("the preferences could not be read"));
    },
  });
  fireEvent.click(await screen.findByRole("button", { name: "More" }));
  fireEvent.click(screen.getByRole("menuitem", { name: "Mute Project" }));
  await screen.findByText("Could not mute. Try again.");
  assert.deepEqual(asked, [["Globex", true]]);
});

test("Move Project sends the new parent by its name, spaces and all", async () => {
  const platformDetail: ProjectDetail = {
    project: platform,
    ancestors: [workspace],
    children: [],
    limits: [],
  };
  open(platformDetail, { routes: { "POST /api/projects/Platform/move": () => platform } });
  fireEvent.click(await screen.findByRole("button", { name: "More" }));
  fireEvent.click(screen.getByRole("menuitem", { name: "Move Project" }));
  fireEvent.click(screen.getByRole("button", { name: /^Move under:/ }));
  fireEvent.click(await screen.findByRole("option", { name: /^Globex renewals/ }));
  fireEvent.click(screen.getByRole("button", { name: "Move" }));
  await waitFor(() => assert.ok(net.seen.some((r) => r.method === "POST")));
  const move = net.seen.find((r) => r.method === "POST");
  assert.equal(move?.path, "/api/projects/Platform/move");
  assert.deepEqual(move?.body, { version: 1, parent: "Globex renewals" });
});

test("the Limits tab says, in words, every limit above a project", async () => {
  open(globexDetail, { tab: "limits" });
  const limits = await screen.findByRole("region", { name: "Limits" });
  await within(limits).findByText("Field engineering's limit also applies here: 7 runs a day.");
  assert.ok(within(limits).getByText("The workspace's limit also applies here."));
});

test("a Limits tab the reader cannot change says who can", async () => {
  // A member of Globex, who neither sets billing nor owns a project above it.
  const member = {
    ...globex,
    access: { role: "member" as const, from: "Globex", via: "*" },
    actions: ["propose" as const],
  };
  open({ ...globexDetail, project: member }, { tab: "limits" });
  const limits = await screen.findByRole("region", { name: "Limits" });
  assert.ok(
    await within(limits).findByText(
      "Only an admin can change these. An owner of Field engineering can set a tighter one.",
    ),
  );
  cleanup();
  net.restore();
  // An owner above sets a tighter one there, so is not told only others can.
  open(globexDetail, { tab: "limits" });
  await screen.findByText("A tighter limit, as an owner above");
  assert.ok(!screen.queryByText(/Only an admin can change these/));
});

test("projects expose context, default agents, access and limits without a conversation feed", async () => {
  open(globexDetail);
  await screen.findByRole("navigation", { name: "Where this project sits" });
  const tabs = screen.getAllByRole("tab").map((tab) => tab.textContent);
  assert.deepEqual(tabs, ["Context", "Default agents", "Access", "Limits"]);
  assert.equal(screen.queryByRole("checkbox", { name: "Include sub-projects" }), null);
  assert.equal(
    net.seen.some((request) => request.path.startsWith("/api/threads")),
    false,
  );
});

const workThread = (id: string, active = 0, failed = 0): ThreadSummary => ({
  id,
  title: `Work ${id}`,
  named: true,
  origin: "api",
  project: "Globex",
  channel: "general",
  agents: ["research"],
  people: ["ada"],
  visibility: "shared",
  messages: 1,
  turns: 1,
  branches: 0,
  active,
  failed,
  created_at: at,
  updated_at: at,
});

test("project work filters loaded rows and opens their native conversation", () => {
  net = stubFetch({});
  const opened: string[] = [];
  render(
    <ProjectWork
      project="Globex"
      threads={[workThread("running", 1), workThread("failed", 0, 1), workThread("done")]}
      hasMore
      onOpenThread={(id) => opened.push(id)}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Needs Attention 1" }));
  assert.equal(screen.queryByRole("button", { name: /Work running/ }), null);
  assert.equal(screen.queryByRole("button", { name: /Work done/ }), null);
  assert.ok(screen.getByText(/Filters apply to the conversations loaded/));
  fireEvent.click(screen.getByRole("button", { name: /Work failed/ }));
  assert.deepEqual(opened, ["failed"]);
});

test("the Context tab saves the instructions as a whole from the version read, and shows what comes from above, read only", async () => {
  const puts: unknown[] = [];
  open(globexDetail, {
    tab: "context",
    routes: {
      "PUT /api/projects/Globex": (body) => (
        puts.push(body), { ...globex, version: 5, instructions: "Renewals close in March." }
      ),
    },
  });
  const from = await screen.findByRole("region", { name: "From Acme" });
  assert.ok(within(from).getByText("Acme sells warehouse robots. Answer plainly."));
  await within(from).findByText("Acme ships on Tuesdays.");
  const fromField = screen.getByRole("region", { name: "From Field engineering" });
  assert.ok(within(fromField).getByText("Customer accounts. Never promise dates."));
  await within(fromField).findByText("Field covers the east coast.");
  assert.ok(
    !screen.queryByText("Not approved yet."),
    "a proposed note is not sent, so it is not shown",
  );
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
  const root: ProjectDetail = {
    project: workspace,
    ancestors: [],
    children: [field],
    limits: [{ scope: { kind: "tenant" }, from: "" }],
  };
  // The server says what an operator may do on the workspace: work in it, and change nothing of its own.
  const asOperator: ProjectDetail = {
    ...root,
    project: {
      ...workspace,
      access: { role: "member", from: "", via: "*" },
      actions: ["create", "propose"],
    },
  };
  open(asOperator, { tab: "context", can: ["projects:read", "projects:write"] });
  await screen.findByText("Acme sells warehouse robots. Answer plainly.");
  assert.ok(!screen.queryByRole("textbox", { name: "Instructions" }));
  assert.ok(
    screen.getByText(
      "Only those who govern projects change the workspace and company-wide projects.",
    ),
  );
  assert.ok(
    !screen.queryByRole("navigation", { name: "Where this project sits" }),
    "the workspace sits nowhere",
  );
  assert.ok(!screen.queryByRole("button", { name: "Delete Project" }));
  assert.ok(!screen.queryByRole("menuitem", { name: "Delete Project" }));
  cleanup();
  net.restore();

  open(root, { tab: "context" });
  assert.ok(await screen.findByRole("textbox", { name: "Instructions" }));
});

test("New Sub-project files it under this project, with only the kinds that may sit there", async () => {
  const posts: unknown[] = [];
  const { opened } = open(globexDetail, {
    routes: {
      "POST /api/projects": (body) => (
        posts.push(body),
        {
          ...renewals,
          name: "Globex help",
          path: [...globex.path, { name: "Globex help", kind: "team" }],
        }
      ),
    },
  });
  fireEvent.click(await screen.findByRole("button", { name: "New Sub-project" }));
  const form = screen.getByRole("form", { name: "New project" });
  // An account never sits under an account, and company-wide sits only under company-wide.
  const kinds = within(within(form).getByRole("group", { name: "Kind" }))
    .getAllByRole("button")
    .map((b) => b.textContent);
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
  assert.deepEqual(posts[0], {
    name: "Globex help",
    title: "Support for Globex",
    kind: "team",
    parent: "Globex",
  });
  await waitFor(() => assert.deepEqual(opened, ["Globex help"]));
});

test("nothing can be made below the fourth level", async () => {
  open({ project: stores, ancestors: [workspace, field, east, retail], children: [], limits: [] });
  await screen.findByRole("region", { name: "Sub-projects" });
  assert.ok(!screen.queryByRole("button", { name: "New Sub-project" }));
  assert.ok(
    screen.getByText("Projects go at most four levels deep, so nothing can sit below this one."),
  );
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

type Listing = Parameters<typeof ProjectsPage>[0];

/** The Projects page as App draws it, its filters held as the address holds them. */
function list(over: Partial<Listing> = {}) {
  const filters: Parameters<Listing["onFilter"]>[0][] = [];
  const made: string[] = [];
  const opened: string[] = [];
  const props: Listing = {
    projects: tree,
    root: workspace,
    canCreate: true,
    canGovern: false,
    me: "ada",
    onFilter: (next) => filters.push(next),
    onOpen: (name) => opened.push(name),
    onCreated: (p) => made.push(p.name),
    ...over,
  };
  const drawn = render(<ProjectsPage {...props} />);
  return {
    filters,
    made,
    opened,
    rerender: (more: Partial<Listing>) => drawn.rerender(<ProjectsPage {...props} {...more} />),
  };
}
const cardTitles = () =>
  within(screen.getByRole("list", { name: "Projects" }))
    .getAllByRole("link")
    .map((a) => a.textContent);
const newProject = () => {
  fireEvent.click(screen.getByRole("button", { name: "New Project" }));
  return screen.getByRole("form", { name: "New project" });
};

test("the Projects page is a grid of cards, pinned first, each opening its project in place", async () => {
  const { opened } = list({ pinned: ["Platform"], onPin: async () => {} });
  const titles = cardTitles();
  assert.equal(titles[0], "Platform", "pinned first");
  assert.equal(titles.length, tree.length, "every project but the workspace");
  assert.ok(!screen.queryByRole("heading", { level: 2 }), "no groups");
  const globexLink = screen.getByRole("link", { name: "Globex" });
  assert.equal(globexLink.getAttribute("href"), "/projects/Globex/context");
  fireEvent.click(globexLink);
  assert.deepEqual(opened, ["Globex"]);
  // A modified click is the browser's: a new tab, not a move in place.
  fireEvent.click(globexLink, { metaKey: true });
  assert.deepEqual(opened, ["Globex"]);
  const card = globexLink.closest("article") as HTMLElement;
  assert.ok(within(card).getByText(/^Updated /));
  assert.equal(within(card).getAllByRole("button").length, 1, "the pin is its one control");
});

test("a card's pin pins and unpins its project, and says so when that fails", async () => {
  const calls: unknown[] = [];
  list({
    pinned: ["Globex"],
    onPin: async (project, pinned) => {
      calls.push([project, pinned]);
      if (project === "Globex") throw new Error("refused");
    },
  });
  const pin = screen.getByRole("button", { name: "Pin project Platform" });
  assert.equal(pin.getAttribute("aria-pressed"), "false");
  fireEvent.click(pin);
  const unpin = screen.getByRole("button", { name: "Unpin project Globex" });
  assert.equal(unpin.getAttribute("aria-pressed"), "true");
  fireEvent.click(unpin);
  assert.deepEqual(calls, [
    ["Platform", true],
    ["Globex", false],
  ]);
  await screen.findByText("Could not unpin. Try again.");
});

test("the search and sort go to the address together, and the cards follow them", () => {
  const { filters, rerender } = list({ sort: "name" });
  fireEvent.change(screen.getByRole("searchbox", { name: "Search projects" }), {
    target: { value: "globex" },
  });
  assert.deepEqual(filters.at(-1), { q: "globex", kind: undefined, sort: "name" });
  rerender({ q: "globex", sort: "name" });
  assert.deepEqual(cardTitles(), ["Globex", "Globex renewals"]);
  rerender({ q: "nothing like it" });
  assert.ok(screen.getByText("No project matches."));
  assert.ok(!screen.queryByRole("list", { name: "Projects" }));
  const dated = (name: string, created: string, active: string) =>
    projectView({ name, created_at: created, updated_at: created, active_at: active });
  const old = dated("Old", "2026-01-01T00:00:00Z", "2026-10-06T00:00:00Z");
  const fresh = dated("Fresh", "2026-09-01T00:00:00Z", "2026-09-02T00:00:00Z");
  const middle = dated("Middle", "2026-05-01T00:00:00Z", "2026-10-01T00:00:00Z");
  const projects = [middle, fresh, old];
  rerender({ projects, q: "" });
  assert.deepEqual(cardTitles(), ["Fresh", "Middle", "Old"], "by name");
  rerender({ projects, q: "", sort: undefined });
  assert.deepEqual(cardTitles(), ["Old", "Middle", "Fresh"], "by last activity, the default");
  rerender({ projects, q: "", sort: "created" });
  assert.deepEqual(cardTitles(), ["Fresh", "Middle", "Old"], "newest made first");
});

test("no kind filter shows unless the workspace defines kinds, and then it narrows the cards", () => {
  const { filters, rerender } = list();
  assert.ok(!screen.queryByRole("combobox", { name: "Kind" }));
  assert.ok(screen.getByRole("combobox", { name: "Sort by" }));
  const reseller = view("Reseller", { kind: "partner", parent: field });
  const root = {
    ...workspace,
    kinds: [{ name: "partner", title: "Partners", like: "account" as const }],
  };
  rerender({ projects: [...tree, reseller], root });
  assert.ok(screen.getByRole("combobox", { name: "Kind" }));
  rerender({ projects: [...tree, reseller], root, kind: "partner" });
  assert.deepEqual(cardTitles(), ["Reseller"]);
  fireEvent.change(screen.getByRole("searchbox", { name: "Search projects" }), {
    target: { value: "re" },
  });
  assert.deepEqual(filters.at(-1), { q: "re", kind: "partner", sort: undefined });
});

test("New Project makes a private project, owned by the one who makes it, then opens it", async () => {
  const posts: unknown[] = [];
  net = stubFetch({
    "POST /api/projects": (body) => (posts.push(body), view("Q4 launch", { title: "Q4: launch!" })),
  });
  const { made } = list();
  const form = newProject();
  assert.ok(!within(form).queryByRole("combobox", { name: "Kind" }), "no kinds of its own");
  assert.ok(!within(form).queryByText(/Start From|Inside/), "no template or parent to pick");
  const isPrivate = within(form).getByRole("radio", { name: /^Private/ }) as HTMLInputElement;
  assert.ok(isPrivate.checked, "private is the default");
  fireEvent.change(within(form).getByRole("textbox", { name: "Name" }), {
    target: { value: "Q4: launch!" },
  });
  fireEvent.change(within(form).getByRole("textbox", { name: "Description (optional)" }), {
    target: { value: "The fourth quarter's launch." },
  });
  fireEvent.click(within(form).getByRole("button", { name: "Create Project" }));
  await waitFor(() => assert.equal(posts.length, 1));
  assert.deepEqual(posts[0], {
    name: "Q4 launch",
    title: "Q4: launch!",
    description: "The fourth quarter's launch.",
    members: [{ who: "ada", role: "owner" }],
  });
  await waitFor(() => assert.deepEqual(made, ["Q4 launch"]));
  await waitFor(() => assert.ok(!screen.queryByRole("form", { name: "New project" })));
});

test("New Project for the workspace leaves its members to the platform's default", async () => {
  const posts: unknown[] = [];
  net = stubFetch({ "POST /api/projects": (body) => (posts.push(body), view("Roadmap")) });
  const { made } = list();
  const form = newProject();
  fireEvent.click(within(form).getByRole("radio", { name: /^Workspace/ }));
  fireEvent.change(within(form).getByRole("textbox", { name: "Name" }), {
    target: { value: "Roadmap" },
  });
  fireEvent.click(within(form).getByRole("button", { name: "Create Project" }));
  await waitFor(() => assert.equal(posts.length, 1));
  assert.deepEqual(posts[0], { name: "Roadmap" });
  await waitFor(() => assert.deepEqual(made, ["Roadmap"]));
});

test("New Project asks for a kind only where the workspace defines its own, and says what it could not do", async () => {
  const posts: unknown[] = [];
  net = stubFetch({
    "POST /api/projects": (body) => (
      posts.push(body),
      new Response(JSON.stringify({ error: 'project "Acme partners" exists' }), { status: 409 })
    ),
  });
  const root = {
    ...workspace,
    kinds: [{ name: "partner", title: "Partners", like: "account" as const }],
  };
  const { made } = list({ root });
  const form = newProject();
  const kind = within(form).getByRole("combobox", { name: "Kind" }) as HTMLSelectElement;
  assert.deepEqual(
    [...kind.options].map((o) => o.textContent),
    ["Team", "Account", "Partners"],
    "company-wide only for those who govern",
  );
  assert.equal(kind.value, "team");
  fireEvent.change(kind, { target: { value: "partner" } });
  // A name with nothing a project's name can hold is caught before it is sent.
  fireEvent.change(within(form).getByRole("textbox", { name: "Name" }), {
    target: { value: "!!!" },
  });
  fireEvent.click(within(form).getByRole("button", { name: "Create Project" }));
  assert.ok(await within(form).findByText("Use at least one letter or digit."));
  assert.equal(posts.length, 0);
  fireEvent.change(within(form).getByRole("textbox", { name: "Name" }), {
    target: { value: "Acme partners" },
  });
  fireEvent.click(within(form).getByRole("button", { name: "Create Project" }));
  await waitFor(() => assert.equal(posts.length, 1));
  assert.deepEqual(posts[0], {
    name: "Acme partners",
    kind: "partner",
    members: [{ who: "ada", role: "owner" }],
  });
  assert.ok(await within(form).findByRole("alert"));
  assert.deepEqual(made, []);
});

test("with no projects the page is a title and one New Project", () => {
  list({ projects: [] });
  assert.ok(screen.getByText("No projects yet"));
  assert.equal(screen.getAllByRole("button", { name: "New Project" }).length, 1);
  assert.ok(!screen.queryByRole("searchbox"));
});

test("an old project schedules selection shows its context bundle", async () => {
  open(globexDetail, { tab: "schedules" });
  await screen.findByRole("link", { name: "Manage connections" });
  assert.equal(screen.queryByRole("tab", { name: "Schedules" }), null);
  assert.equal(screen.queryByText(/Scheduled ?,/), null);
});

const knows = [...everything, "knowledge:read"];
const knowledgeRoutes = (more: Record<string, (body: unknown) => unknown> = {}) => ({
  "GET /api/projects/Globex/knowledge": () => globexKnowledge,
  "GET /api/knowledge": () => ({ sources: knowledgeSources }),
  ...more,
});

test("the Context tab: an owner who reads knowledge adds a workspace source and saves the list as a whole from the version read", async () => {
  const puts: unknown[] = [];
  open(globexDetail, {
    tab: "context",
    can: knows,
    routes: knowledgeRoutes({
      "PUT /api/projects/Globex/knowledge": (body) => (
        puts.push(body),
        { ...globexKnowledge, version: 5, knowledge: ["globex-contracts", "pricing"] }
      ),
    }),
  });
  const section = await screen.findByRole("region", { name: "Knowledge" });
  await within(section).findByRole("button", { name: "Remove globex-contracts" });
  const save = within(section).getByRole("button", { name: "Save Knowledge" }) as HTMLButtonElement;
  assert.ok(save.disabled, "nothing to save yet");
  fireEvent.click(await within(section).findByRole("button", { name: /^Add Source/ }));
  fireEvent.click(await screen.findByRole("button", { name: "pricing" }));
  fireEvent.click(save);
  await waitFor(() => assert.equal(puts.length, 1));
  assert.deepEqual(puts[0], { version: 4, knowledge: ["globex-contracts", "pricing"] });
  await within(section).findByText("Saved");
});

test("the Context tab: a member who reads knowledge sees what the project names and what runs are grounded in, read only", async () => {
  const member = {
    ...globex,
    access: { role: "member" as const, from: "Globex", via: "*" },
    actions: ["propose" as const],
  };
  open(
    { ...globexDetail, project: member },
    { tab: "context", can: knows, routes: knowledgeRoutes() },
  );
  const section = await screen.findByRole("region", { name: "Knowledge" });
  await within(section).findByText("globex-contracts");
  assert.ok(!within(section).queryByRole("button", { name: /^Add Source/ }));
  assert.ok(!within(section).queryByRole("button", { name: "Save Knowledge" }));
  const given = within(screen.getByRole("list", { name: "What runs here are grounded in" }))
    .getAllByRole("listitem")
    .map((li) => li.textContent);
  assert.deepEqual(given, [
    "globex-contractsFrom this project",
    "field-playbookFrom Field engineering",
    "handbookFrom the workspace",
  ]);
  assert.ok(
    !net.seen.some((r) => r.path === "/api/knowledge"),
    "the workspace's sources are read only to choose from",
  );
});

test("the Context tab shows nothing about knowledge to a reader without knowledge:read, and asks for none", async () => {
  open(globexDetail, { tab: "context", routes: knowledgeRoutes() });
  await screen.findByRole("textbox", { name: "Instructions" });
  assert.ok(!screen.queryByRole("region", { name: "Knowledge" }));
  assert.ok(!screen.queryByText(/grounded in/));
  assert.ok(!net.seen.some((r) => r.path.includes("knowledge")));
});

test("the Context tab: a knowledge list the platform file sets turns read only once it is refused", async () => {
  open(globexDetail, {
    tab: "context",
    can: knows,
    routes: knowledgeRoutes({
      "PUT /api/projects/Globex/knowledge": () =>
        new Response(
          JSON.stringify({
            error: 'managed by the platform file: the platform file sets knowledge on "Globex"',
          }),
          { status: 409 },
        ),
    }),
  });
  const section = await screen.findByRole("region", { name: "Knowledge" });
  await within(section).findByRole("button", { name: "Remove globex-contracts" });
  fireEvent.click(within(section).getByRole("button", { name: "Remove globex-contracts" }));
  fireEvent.click(within(section).getByRole("button", { name: "Save Knowledge" }));
  await within(section).findByText(
    "The platform file sets these sources. They can't be changed here.",
  );
  assert.ok(!within(section).queryByRole("button", { name: "Save Knowledge" }));
  assert.ok(within(section).getByText("globex-contracts"), "what the file sets is shown again");
});

test("the Context tab: a knowledge list changed since it was read shows the latest and says so", async () => {
  let reads = 0;
  open(globexDetail, {
    tab: "context",
    can: knows,
    routes: knowledgeRoutes({
      "GET /api/projects/Globex/knowledge": () =>
        reads++ === 0
          ? globexKnowledge
          : { ...globexKnowledge, version: 6, knowledge: ["handbook"] },
      "PUT /api/projects/Globex/knowledge": () =>
        new Response(JSON.stringify({ error: "version conflict" }), { status: 409 }),
    }),
  });
  const section = await screen.findByRole("region", { name: "Knowledge" });
  fireEvent.click(await within(section).findByRole("button", { name: "Remove globex-contracts" }));
  fireEvent.click(within(section).getByRole("button", { name: "Save Knowledge" }));
  await within(section).findByText(
    "Someone changed this project since you opened it. Its latest sources are shown now. Make your change again.",
  );
  assert.ok(within(section).getByRole("button", { name: "Remove handbook" }));
});

test("the Context tab: an edit in progress is kept across a change to another field, and never saved over a change to the list", async () => {
  let current = globexKnowledge;
  const puts: unknown[] = [];
  net = stubFetch(
    knowledgeRoutes({
      "GET /api/projects/Globex/knowledge": () => current,
      "PUT /api/projects/Globex/knowledge": (body) => (puts.push(body), current),
    }),
  );
  const section = (version: number) => (
    <ProjectKnowledgeSection
      project="Globex"
      ancestors={[]}
      workspaceName="Acme"
      version={version}
      canRead
      canWrite
    />
  );
  const { rerender } = render(section(4));
  fireEvent.click(await screen.findByRole("button", { name: "Remove globex-contracts" }));
  assert.ok(!screen.queryByRole("button", { name: "Remove globex-contracts" }));

  // Another field changed: the project's version moved, the list did not.
  current = { ...globexKnowledge, version: 5 };
  rerender(section(5));
  await waitFor(() =>
    assert.equal(net.seen.filter((r) => r.path === "/api/projects/Globex/knowledge").length, 2),
  );
  assert.ok(!screen.queryByRole("button", { name: "Remove globex-contracts" }), "the edit is kept");
  assert.ok(!screen.queryByText(/Someone changed this project/));

  // Someone changed the list: their list is shown, and the edit goes.
  current = { ...globexKnowledge, version: 6, knowledge: ["handbook"] };
  rerender(section(6));
  await screen.findByText(
    "Someone changed this project since you opened it. Its latest sources are shown now. Make your change again.",
  );
  assert.ok(screen.getByRole("button", { name: "Remove handbook" }));
  assert.equal(puts.length, 0);
});

test("the Context tab previews a selected agent and channel, including omitted layers", async () => {
  open(globexDetail, {
    tab: "context",
    routes: {
      "GET /api/projects/Globex/context": (_body, query) => {
        assert.equal(query.get("agent"), "helper");
        assert.equal(query.get("channel"), "general");
        return {
          layers: [
            {
              kind: "instructions",
              from: "Globex",
              text: "FOLLOW-GLOBEX",
              budget: 8000,
              used: 13,
              omitted: 0,
              sent: true,
              unit: "characters",
            },
            {
              kind: "ancestor_notes",
              from: "Field",
              text: "(2 more notes not sent)",
              budget: 6000,
              used: 0,
              omitted: 2,
              sent: false,
              unit: "characters",
            },
          ],
        };
      },
    },
  });
  fireEvent.change(await screen.findByRole("textbox", { name: "Preview agent" }), {
    target: { value: "helper" },
  });
  fireEvent.change(screen.getByRole("textbox", { name: "Preview channel" }), {
    target: { value: "general" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Preview Turn" }));
  assert.ok(await screen.findByText("FOLLOW-GLOBEX"));
  assert.ok(screen.getByText("(2 more notes not sent)"));
  assert.ok(screen.getByText("13 / 8,000 characters"));
});
