import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { setAgentHues } from "./agentHues.ts";
import type { Note, ProjectDetail, Revision, ScheduleView } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { ProjectPage, ProjectsPage } from "./Projects.tsx";
import {
  corpora,
  field,
  globex,
  globexCorpora,
  globexDetail,
  projectView,
  tree,
  view,
  workspace,
} from "./projects.fixtures.ts";
import { href } from "./route.ts";
import type { AgentSessionSummary } from "./sessionCenter.ts";

const at = "2026-10-01T09:00:00Z";
const note = (id: string, text: string, status: Note["status"] = "approved"): Note => ({
  id,
  text,
  source: "person",
  status,
  by: "ada",
  at,
});
const session = (
  id: string,
  title: string,
  over: {
    parent?: string;
    status?: string;
    origin?: string;
    agents?: string[];
    people?: string[];
  } = {},
): AgentSessionSummary =>
  ({
    id,
    title,
    agent: "lead",
    parent: over.parent ?? "console:ada",
    project: "Globex",
    status: over.status ?? "done",
    updated_at: at,
    conversation: {
      origin: over.origin ?? "console",
      agents: over.agents ?? ["lead"],
      people: over.people ?? ["ada"],
    },
  }) as unknown as AgentSessionSummary;
const schedule = (
  name: string,
  project: string,
  over: Partial<ScheduleView> = {},
): ScheduleView => ({
  name,
  expression: "0 9 * * 1-5",
  agent: "lead",
  text: "What changed?",
  project,
  paused: false,
  next: [],
  recent: [],
  ...over,
});
const lead: Revision = {
  rev: 1,
  spec: {
    name: "lead",
    description: "",
    provider: "offline",
    model: "offline",
    system_prompt: ".",
  },
  created_at: at,
  created_by: "ada",
  hash: "lead",
};

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
  "knowledge:read",
  "schedules:read",
  "schedules:write",
  "clients:read",
  "clients:write",
];

type Routes = Record<string, (body: unknown, query: URLSearchParams) => unknown>;

/** A project's page as App draws it, its requests answered in memory. */
function open(
  detail: ProjectDetail,
  opts: {
    can?: string[];
    routes?: Routes;
    onPin?: (project: string, pinned: boolean) => Promise<void>;
    pinned?: boolean;
    onFork?: () => void;
  } = {},
) {
  const address = detail.project.name === "" ? "~" : encodeURIComponent(detail.project.name);
  net = stubFetch({
    [`GET /api/projects/${address}`]: () => detail,
    "GET /api/sessions": () => ({ sessions: [], next: "" }),
    [`GET /api/projects/${address}/corpora`]: () => ({
      version: detail.project.version,
      corpora: [],
      given: [],
    }),
    "GET /api/corpora": () => ({ corpora }),
    [`GET /api/projects/${address}/memory`]: () => ({ notes: [] }),
    "GET /api/schedules": () => ({ schedules: [] }),
    [`GET /api/projects/${address}/channels`]: () => ({ channels: [] }),
    "GET /api/clients": () => ({ clients: [], caps: { max: 10, max_per_surface: 5 } }),
    ...opts.routes,
  });
  const can = opts.can ?? everything;
  const noop = () => {};
  const opened: string[] = [];
  const sessions: string[] = [];
  const placeholders: string[] = [];
  render(
    <ProjectPage
      composer={(placeholder) => {
        placeholders.push(placeholder);
        return <form aria-label="Home composer" />;
      }}
      self="ada"
      name={detail.project.name}
      workspaceName="Acme"
      projects={tree}
      agents={[lead]}
      can={(p) => can.includes(p)}
      onOpen={(name) => opened.push(name)}
      onOpenAll={() => opened.push("all")}
      onOpenThread={(id) => sessions.push(id)}
      onChanged={noop}
      onSchedulesChanged={noop}
      onDeleted={noop}
      onPin={opts.onPin}
      pinned={opts.pinned}
      onFork={opts.onFork}
    />,
  );
  return { opened, sessions, placeholders };
}

/** A side card, by its heading. */
const card = async (title: string) =>
  (await screen.findByRole("heading", { level: 2, name: title })).closest("section") as HTMLElement;

test("a project page is headed by its title under a way back to Projects: no tabs, no agents of its own, no lede and no turn preview", async () => {
  const { opened } = open(globexDetail);
  assert.ok(await screen.findByRole("heading", { level: 1, name: "Globex" }));
  fireEvent.click(screen.getByRole("button", { name: /Projects$/ }));
  assert.deepEqual(opened, ["all"]);
  assert.ok(!screen.queryByRole("tablist"), "no tabs");
  for (const gone of [
    "Renewals and support for Globex.",
    "Account | Globex Inc | in Field engineering",
    "Default agents",
    "Lead",
    "Sub-projects",
    "Manage connections",
    "Turn preview",
  ])
    assert.ok(!screen.queryByText(gone), `${gone} is not on the page`);
  assert.ok(!screen.queryByRole("textbox", { name: "Preview agent" }));
  assert.ok(!screen.queryByRole("button", { name: "New Session" }), "the composer starts one");
  // Agents are not the project's: nothing asks which agents it has.
  assert.ok(net.seen.every((r) => !r.path.includes("/agents")));
  const side = screen.getByRole("complementary", { name: "Project settings" });
  const headings = within(side)
    .getAllByRole("heading", { level: 2 })
    .map((h) => h.textContent);
  assert.deepEqual(headings, ["Instructions", "Context", "Memory", "Scheduled", "Slack", "Access"]);
});

test("the More menu holds Pin, Edit, Fork, then Archive and Delete after a rule; Edit opens a sheet", async () => {
  const puts: unknown[] = [];
  open(globexDetail, {
    onPin: async () => {},
    onFork: () => {},
    routes: { "PUT /api/projects/Globex": (body) => (puts.push(body), globex) },
  });
  const more = await screen.findByRole("button", { name: "More" });
  fireEvent.keyDown(more, { key: "ArrowDown" });
  const menu = screen.getByRole("menu", { name: "More" });
  const children = [...menu.children].map((el) => (el.tagName === "HR" ? "---" : el.textContent));
  assert.deepEqual(children, [
    "Pin Project",
    "Edit Project",
    "Fork Project",
    "---",
    "Archive Project",
    "Delete Project",
  ]);
  fireEvent.click(screen.getByRole("menuitem", { name: "Edit Project" }));
  const sheet = await screen.findByRole("dialog", { name: "Edit project" });
  fireEvent.change(within(sheet).getByRole("textbox", { name: /^Title/ }), {
    target: { value: "Globex Inc" },
  });
  fireEvent.click(within(sheet).getByRole("button", { name: "Save Changes" }));
  await waitFor(() => assert.equal(puts.length, 1));
  assert.equal((puts[0] as { title: string; version: number }).title, "Globex Inc");
  assert.equal((puts[0] as { version: number }).version, globex.version);
  await waitFor(() => assert.ok(!screen.queryByRole("dialog")));
});

test("a project with no recent sessions can be pinned from its menu, and a refusal says so", async () => {
  let selected: unknown;
  open(globexDetail, {
    onPin: async (project, pinned) => {
      selected = [project, pinned];
    },
  });
  fireEvent.click(await screen.findByRole("button", { name: "More" }));
  fireEvent.click(screen.getByRole("menuitem", { name: "Pin Project" }));
  assert.deepEqual(selected, ["Globex", true]);
  cleanup();
  net.restore();
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
  let step = await ask();
  assert.equal(document.activeElement?.textContent, "Keep It");
  fireEvent.keyDown(step, { key: "Escape" });
  assert.ok(!screen.queryByRole("group", { name: /This cannot be undone/ }));
  step = await ask();
  fireEvent.blur(within(step).getByRole("button", { name: "Keep It" }), {
    relatedTarget: document.body,
  });
  assert.ok(!screen.queryByRole("group", { name: /This cannot be undone/ }));
  assert.equal(deleted, 0);
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

test("the page draws Home's composer, saying it starts a session in the project", async () => {
  const { placeholders } = open(globexDetail);
  assert.ok(await screen.findByRole("form", { name: "Home composer" }));
  assert.ok(placeholders.includes("New session in Globex"));
  assert.ok(!screen.queryByRole("button", { name: "Send" }), "no composer of its own");
});

test("Recents lists the project's own sessions newest first, each opening it, with Show More while there are more", async () => {
  const asked: URLSearchParams[] = [];
  const { sessions } = open(globexDetail, {
    routes: {
      "GET /api/sessions": (_body, query) => {
        asked.push(query);
        return query.get("after")
          ? { sessions: [session("s_3", "Kickoff")], next: "" }
          : { sessions: [session("s_1", "Renewal call"), session("s_2", "")], next: "s_2" };
      },
    },
  });
  const recents = await card("Recents");
  await within(recents).findByRole("link", { name: /^Renewal call/ });
  assert.equal(asked[0]?.get("project"), "Globex");
  assert.equal(asked[0]?.get("roots"), "true");
  assert.equal(asked[0]?.get("limit"), "20");
  const links = within(recents)
    .getAllByRole("link")
    .map((a) => [a.querySelector("span")?.textContent, a.getAttribute("href")]);
  assert.deepEqual(links, [
    ["Renewal call", href({ page: "conversation", id: "s_1" })],
    ["Untitled session", href({ page: "conversation", id: "s_2" })],
  ]);
  fireEvent.click(within(recents).getByRole("link", { name: /^Renewal call/ }));
  assert.deepEqual(sessions, ["s_1"]);
  fireEvent.click(within(recents).getByRole("button", { name: "Show More" }));
  await within(recents).findByRole("link", { name: /^Kickoff/ });
  assert.equal(asked.at(-1)?.get("after"), "s_2");
  assert.ok(!within(recents).queryByRole("button", { name: "Show More" }));
});

test("each recent session says where it started, who answered it and its state", async () => {
  setAgentHues({ lead: 210, "globex-helper": 30 });
  open(globexDetail, {
    routes: {
      "GET /api/sessions": () => ({
        sessions: [
          session("s_1", "Renewal question", {
            origin: "slack",
            status: "working",
            agents: ["globex-helper", "lead"],
            people: ["dana"],
          }),
          session("s_2", "Weekly check", {
            parent: "schedule://weekly-recap",
            origin: "cron",
            people: [],
          }),
          session("s_3", "Review the PR", { origin: "github" }),
          session("s_4", "Try it out", {
            agents: ["lead", "a", "b"],
            people: ["ada", "bo"],
          }),
        ],
        next: "",
      }),
    },
  });
  const recents = await card("Recents");
  const row = async (title: string) =>
    (await within(recents).findByRole("link", { name: new RegExp(`^${title}`) })).textContent ?? "";
  const slack = await row("Renewal question");
  for (const part of ["Slack", "globex-helper", "lead", "@dana", "Working"])
    assert.ok(slack.includes(part), part);
  const answered = within(recents).getByRole("link", { name: /^Renewal question/ });
  assert.equal(
    answered.querySelectorAll("[data-accent='agent']").length,
    2,
    "each agent in its color",
  );
  assert.match(await row("Weekly check"), /Scheduled: weekly-recap/);
  assert.match(await row("Review the PR"), /GitHub/);
  const busy = await row("Try it out");
  assert.match(busy, /Console/);
  assert.doesNotMatch(busy, /@ada/, "the reader is not named among who answered");
  assert.match(busy, /\+1/, "past three, the rest are counted");
  assert.ok(
    !within(recents).queryByText("Working", { selector: "[data-tone]" }),
    "a state is quiet words, never a pill",
  );
});

test("Recents with nothing in it says so in one line", async () => {
  open(globexDetail);
  const recents = await card("Recents");
  assert.ok(await within(recents).findByText("No sessions yet"));
  assert.ok(!within(recents).queryByRole("button"));
});

test("Instructions: the card shows their first lines, and Edit saves them as a whole from the version read", async () => {
  const puts: unknown[] = [];
  const instructed = { ...globex, instructions: "Never promise dates.\nAnswer in English." };
  open(
    { ...globexDetail, project: instructed },
    { routes: { "PUT /api/projects/Globex": (body) => (puts.push(body), instructed) } },
  );
  const instructions = await card("Instructions");
  assert.match(instructions.textContent ?? "", /Never promise dates\./);
  fireEvent.click(within(instructions).getByRole("button", { name: "Edit Instructions" }));
  const sheet = await screen.findByRole("dialog", { name: "Instructions" });
  const box = within(sheet).getByRole("textbox", { name: "Instructions" }) as HTMLTextAreaElement;
  assert.equal(box.maxLength, 4000);
  fireEvent.change(box, { target: { value: "Never promise dates." } });
  fireEvent.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => assert.equal(puts.length, 1));
  const put = puts[0] as { instructions: string; version: number; title?: string };
  assert.equal(put.instructions, "Never promise dates.");
  assert.equal(put.version, globex.version);
  await waitFor(() => assert.ok(!screen.queryByRole("dialog")));
  // One with none offers Add.
  cleanup();
  net.restore();
  open(globexDetail);
  const empty = await card("Instructions");
  assert.ok(within(empty).getByText("No instructions"));
  assert.ok(within(empty).getByRole("button", { name: "Add Instructions" }));
});

test("Context: the card names each corpus and its sources, and the sheet saves the list as a whole", async () => {
  const puts: unknown[] = [];
  open(globexDetail, {
    routes: {
      "GET /api/projects/Globex/corpora": () => globexCorpora,
      "PUT /api/projects/Globex/corpora": (body) => (
        puts.push(body),
        {
          ...globexCorpora,
          version: 5,
          corpora: [
            ...globexCorpora.corpora,
            { name: "field-playbook", title: "Field playbook", sources: 2 },
          ],
        }
      ),
    },
  });
  const context = await card("Context");
  await within(context).findByText("Globex contracts, 3 sources");
  fireEvent.click(within(context).getByRole("button", { name: "Edit Context" }));
  const sheet = await screen.findByRole("dialog", { name: "Context" });
  const boxes = within(sheet)
    .getAllByRole("checkbox")
    .map((b) => [b.closest("label")?.textContent, (b as HTMLInputElement).checked]);
  assert.deepEqual(boxes, [
    ["Globex contracts", true],
    ["Field playbook", false],
    ["pricing", false],
  ]);
  fireEvent.click(within(sheet).getByRole("checkbox", { name: "Field playbook" }));
  fireEvent.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => assert.equal(puts.length, 1));
  assert.deepEqual(puts[0], { version: 4, corpora: ["globex-contracts", "field-playbook"] });
  await within(context).findByText("Field playbook, 2 sources");
});

test("Context on a server without corpora is its empty state, with nothing to add", async () => {
  open(globexDetail, {
    routes: {
      "GET /api/projects/Globex/corpora": () => new Response("{}", { status: 404 }),
      "GET /api/corpora": () => new Response("{}", { status: 404 }),
    },
  });
  const context = await card("Context");
  await waitFor(() => assert.ok(net.seen.some((r) => r.path === "/api/corpora")));
  assert.ok(within(context).getByText("No corpora"));
  assert.ok(!within(context).queryByRole("button"));
});

test("Memory: the card counts the notes and what waits, and an owner keeps a waiting one from the sheet", async () => {
  const approved: unknown[] = [];
  let notes = [note("n1", "Renewals close in March."), note("n2", "Prefers email.", "pending")];
  open(
    { ...globexDetail, project: { ...globex, notes: 1, proposed: 1 } },
    {
      routes: {
        "GET /api/projects/Globex/memory": () => ({ notes }),
        "POST /api/projects/Globex/memory/approve": (body) => {
          approved.push(body);
          notes = notes.map((n) => ({ ...n, status: "approved" }));
          return { approved: 1 };
        },
      },
    },
  );
  const memory = await card("Memory");
  assert.ok(within(memory).getByText("1 note, 1 waiting"));
  fireEvent.click(within(memory).getByRole("button", { name: "View Memory" }));
  const sheet = await screen.findByRole("dialog", { name: "Memory" });
  await within(sheet).findByText("Prefers email.");
  fireEvent.click(within(sheet).getByRole("button", { name: "Keep note: Prefers email." }));
  await waitFor(() => assert.deepEqual(approved, [{ ids: ["n2"] }]));
  await waitFor(() => assert.ok(!within(sheet).queryByText("Waiting for approval")));
});

test("Scheduled lists only this project's schedules in words, and Add files a new one in it", async () => {
  const posts: unknown[] = [];
  open(globexDetail, {
    routes: {
      // A server that does not filter by project still shows only this one's.
      "GET /api/schedules": () => ({
        schedules: [
          schedule("globex-weekly", "Globex", { description: "Weekly renewal check" }),
          schedule("field-daily", "Field", { expression: "0 9 * * *" }),
        ],
      }),
      "POST /api/schedules": (body) => (posts.push(body), schedule("globex-digest", "Globex")),
    },
  });
  const scheduled = await card("Scheduled");
  await within(scheduled).findByText("Weekly renewal check");
  assert.ok(within(scheduled).getByText("Weekdays at 09:00 UTC"));
  assert.ok(!within(scheduled).queryByText("field-daily"));
  assert.ok(
    net.seen.some((r) => r.path === "/api/schedules?project=Globex"),
    "it asks for this project's",
  );
  fireEvent.click(within(scheduled).getByRole("button", { name: "Add Schedule" }));
  const sheet = await screen.findByRole("dialog", { name: "New schedule" });
  fireEvent.change(within(sheet).getByRole("textbox", { name: "Name" }), {
    target: { value: "globex-digest" },
  });
  fireEvent.change(within(sheet).getByRole("textbox", { name: "What to ask" }), {
    target: { value: "Sum up the week." },
  });
  fireEvent.click(within(sheet).getByRole("button", { name: "Create Schedule" }));
  await waitFor(() => assert.equal(posts.length, 1));
  assert.deepEqual(posts[0], {
    name: "globex-digest",
    expression: "0 9 * * 1-5",
    agent: "lead",
    text: "Sum up the week.",
    project: "Globex",
  });
});

test("Slack: a linked channel shows with its app, Add links one by its id, and Remove unlinks it", async () => {
  const posts: unknown[] = [];
  const deletes: string[] = [];
  open(globexDetail, {
    routes: {
      "GET /api/projects/Globex/channels": () => ({
        channels: [
          { name: "general", project: "Globex", version: 1 },
          {
            name: "slack-c0123",
            project: "Globex",
            version: 1,
            slack: { client: "acme-slack", channel: "C0123" },
          },
        ],
      }),
      "GET /api/clients": () => ({
        clients: [
          { id: "acme-slack", name: "Acme Slack", surface: "slack" },
          { id: "acme-github", name: "Acme GitHub", surface: "github" },
        ],
        caps: { max: 10, max_per_surface: 5 },
      }),
      "POST /api/projects/Globex/channels": (body) => (posts.push(body), { name: "slack-c0456" }),
      "DELETE /api/projects/Globex/channels/slack-c0123": () => {
        deletes.push("slack-c0123");
        return undefined;
      },
    },
  });
  const slack = await card("Slack");
  await within(slack).findByText("#C0123 via Acme Slack");
  assert.ok(!within(slack).queryByText(/general/), "a channel with no Slack binding is no link");
  fireEvent.click(within(slack).getByRole("button", { name: "Add Slack Channel" }));
  const sheet = await screen.findByRole("dialog", { name: "Link a Slack channel" });
  const apps = within(sheet)
    .getAllByRole("option")
    .map((o) => o.textContent);
  assert.deepEqual(apps, ["Acme Slack"], "only Slack apps");
  const id = within(sheet).getByRole("textbox", { name: /^Slack channel ID/ });
  fireEvent.change(id, { target: { value: "#launch" } });
  fireEvent.click(within(sheet).getByRole("button", { name: "Link Channel" }));
  assert.ok(await within(sheet).findByText("Use a Slack channel ID that starts with C or G."));
  assert.equal(posts.length, 0);
  fireEvent.change(id, { target: { value: " c0456 " } });
  fireEvent.click(within(sheet).getByRole("button", { name: "Link Channel" }));
  await waitFor(() => assert.equal(posts.length, 1));
  assert.deepEqual(posts[0], {
    name: "slack-c0456",
    slack: { client: "acme-slack", channel: "C0456" },
  });
  await waitFor(() => assert.ok(!screen.queryByRole("dialog")));
  fireEvent.click(within(slack).getByRole("button", { name: "Remove #C0123" }));
  await waitFor(() => assert.deepEqual(deletes, ["slack-c0123"]));
});

test("Access says who can see the project, and an owner changes it from the access editor", async () => {
  open(globexDetail, {
    routes: {
      "GET /api/projects/Globex/members": () => ({
        version: 4,
        members: [{ who: "*", role: "member" }],
        inherited: [],
      }),
    },
  });
  const visibility = (await screen.findByRole("heading", { level: 2, name: "Access" }))
    .parentElement as HTMLElement;
  assert.ok(within(visibility).getByText("Workspace"));
  fireEvent.click(within(visibility).getByRole("button", { name: "Change Access" }));
  const sheet = await screen.findByRole("dialog", { name: "Who can see this project" });
  assert.ok(await within(sheet).findByRole("button", { name: "Add Person" }));
  cleanup();
  net.restore();
  // One only its owner reads is private.
  open({ ...globexDetail, project: { ...globex, members: [{ who: "ada", role: "owner" }] } });
  const own = (await screen.findByRole("heading", { level: 2, name: "Access" }))
    .parentElement as HTMLElement;
  assert.ok(within(own).getByText("Private"));
});

test("a reader who may not edit the project sees what it holds and no way to change it", async () => {
  const reader = projectView({
    name: "Globex",
    instructions: "Never promise dates.",
    notes: 1,
    proposed: 1,
  });
  open(
    { ...globexDetail, project: reader },
    {
      can: ["projects:read", "runs:read", "runs:write", "knowledge:read", "schedules:read"],
      routes: {
        "GET /api/projects/Globex/memory": () => ({
          notes: [note("n1", "Renewals close in March."), note("n2", "Prefers email.", "pending")],
        }),
      },
    },
  );
  const side = await screen.findByRole("complementary", { name: "Project settings" });
  await within(side).findByText("Never promise dates.");
  await waitFor(() => assert.ok(net.seen.some((r) => r.path.startsWith("/api/schedules"))));
  const buttons = within(side)
    .getAllByRole("button")
    .map((b) => b.getAttribute("aria-label") ?? b.textContent);
  assert.deepEqual(buttons, ["View Memory"], "only a way to read the notes");
  assert.ok(!screen.queryByRole("button", { name: "More" }), "no menu with nothing in it");
  fireEvent.click(within(side).getByRole("button", { name: "View Memory" }));
  const sheet = await screen.findByRole("dialog", { name: "Memory" });
  await within(sheet).findByText("Prefers email.");
  assert.ok(!within(sheet).queryByRole("button", { name: /^Keep note/ }));
  assert.ok(!within(sheet).queryByRole("button", { name: "Add Note" }));
});

test("the workspace's page is titled with the workspace's name, and its title is not edited here", async () => {
  open(
    { project: workspace, ancestors: [], children: [field], limits: [] },
    { onPin: async () => {} },
  );
  assert.ok(await screen.findByRole("heading", { level: 1, name: "Acme" }));
  fireEvent.click(screen.getByRole("button", { name: "More" }));
  assert.ok(!screen.queryByRole("menuitem", { name: "Edit Project" }));
  assert.ok(screen.getByRole("menuitem", { name: "Pin Project" }));
  fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
  const recents = await card("Recents");
  assert.ok(await within(recents).findByText("No sessions yet"));
  const asked = net.seen.find((r) => r.path.startsWith("/api/sessions"));
  assert.equal(new URLSearchParams(asked?.path.split("?")[1]).get("project"), "~");
});

test("a project that is not there says so, as one page", async () => {
  // Nothing is stubbed, so the project answers 404.
  net = stubFetch({});
  const noop = () => {};
  render(
    <ProjectPage
      name="Gone"
      workspaceName="Acme"
      projects={[]}
      agents={[]}
      can={() => true}
      onOpen={noop}
      onOpenAll={noop}
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
  assert.equal(globexLink.getAttribute("href"), "/projects/Globex");
  fireEvent.click(globexLink);
  assert.deepEqual(opened, ["Globex"]);
  // A modified click is the browser's: a new tab, not a move in place.
  fireEvent.click(globexLink, { metaKey: true });
  assert.deepEqual(opened, ["Globex"]);
  const card = globexLink.closest("article") as HTMLElement;
  assert.ok(within(card).getByText(/^Updated /));
  assert.equal(within(card).getAllByRole("button").length, 1, "its ⋯ menu is its one control");
});

test("a card's ⋯ menu pins and unpins its project, and says so when that fails", async () => {
  const calls: unknown[] = [];
  list({
    pinned: ["Globex"],
    onPin: async (project, pinned) => {
      calls.push([project, pinned]);
      if (project === "Globex") throw new Error("refused");
    },
  });
  const cardOf = (name: string) =>
    screen.getByRole("link", { name }).closest("article") as HTMLElement;
  const choose = (name: string, item: string) => {
    const more = within(cardOf(name)).getByRole("button", { name: "More" });
    fireEvent.keyDown(more, { key: "ArrowDown" });
    fireEvent.click(screen.getByRole("menuitem", { name: item }));
  };
  assert.ok(within(cardOf("Globex").parentElement as HTMLElement).getByLabelText("Pinned"));
  choose("Platform", "Pin Project");
  choose("Globex", "Unpin Project");
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
