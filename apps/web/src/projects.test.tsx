import assert from "node:assert/strict";
import { test } from "node:test";
import { defaultCopy as copy } from "@humagen/components";
import type { ReactElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import type { Me, Preferences, ProjectChange, Settings as Workspace } from "./api.ts";
import { KnowledgeLists, MAX_KNOWLEDGE } from "./ProjectKnowledge.tsx";
import { ProjectChoices, ProjectPicker, placeOf } from "./ProjectPicker.tsx";
import { ProjectsPage } from "./Projects.tsx";
import {
  east,
  field,
  globex,
  globexKnowledge,
  initech,
  knowledgeSources,
  news,
  renewals,
  retail,
  stores,
  tree,
  view,
  workspace,
} from "./projects.fixtures.ts";
import {
  kindsUnder,
  nameFrom,
  narrowAt,
  treeOf,
  updateOf,
  validName,
  WORKSPACE,
} from "./projects.ts";
import { Settings } from "./Settings.tsx";

const html = (el: ReactElement) => renderToStaticMarkup(el);
const noop = () => {};
const rowsOf = (
  narrow: boolean,
  opts: { q?: string; kind?: "company" | "team" | "account" } = {},
) =>
  treeOf(tree, { narrow, ...opts }).map((g) => [
    g.title,
    g.rows.map((r) => [r.project.name, r.indent, r.path.join(" › ")]),
  ]);

test("projects are grouped by kind, Company-wide, Teams, then Accounts, each in tree order", () => {
  assert.deepEqual(
    treeOf(tree, { narrow: false }).map((g) => [g.title, g.rows.map((r) => r.project.name)]),
    [
      ["Company-wide", ["Company news"]],
      ["Teams", ["Field", "East", "Retail", "Stores", "Globex renewals", "Platform"]],
      ["Accounts", ["Globex", "Initech"]],
    ],
  );
});

test("a project of the workspace's own kind is listed after humagen's kinds, in a group by its title", () => {
  const partner = view("Reseller", { kind: "partner", parent: field });
  const kinds = [{ name: "partner", title: "Partners", like: "account" as const }];
  assert.deepEqual(
    treeOf([...tree, partner], { narrow: false, kinds }).map((g) => [
      g.title,
      g.rows.map((r) => [r.project.name, r.path.join(" › ")]),
    ]),
    [
      ["Company-wide", [["Company news", ""]]],
      [
        "Teams",
        [
          ["Field", ""],
          ["East", ""],
          ["Retail", ""],
          ["Stores", ""],
          ["Globex renewals", "Field engineering › Globex"],
          ["Platform", ""],
        ],
      ],
      [
        "Accounts",
        [
          ["Globex", "Field engineering"],
          ["Initech", "Field engineering"],
        ],
      ],
      ["Partners", [["Reseller", "Field engineering"]]],
    ],
  );
  // Narrowed to it, only it; and a kind the console was not told of still shows, by its name.
  assert.deepEqual(
    treeOf([...tree, partner], { narrow: false, kinds, kind: "partner" }).map((g) => g.title),
    ["Partners"],
  );
  assert.deepEqual(treeOf([...tree, partner], { narrow: false }).at(-1)?.title, "partner");
  // It behaves as an account: no account under it, and it never sits under one.
  const reseller = { name: "Reseller", kind: "partner", depth: 2 };
  assert.deepEqual(kindsUnder(reseller, true, kinds), ["team"]);
  assert.deepEqual(kindsUnder(globex, true, kinds), ["team"]);
  assert.deepEqual(kindsUnder(field, false, kinds), ["team", "account", "partner"]);
});

test("the tree at depth 4 indents one level at 390px, and deeper rows say where they sit", () => {
  assert.equal(stores.depth, 4, "the fixture is as deep as the tree goes");
  assert.ok(narrowAt(390), "390px is a phone");
  assert.ok(!narrowAt(1280));
  const teams = (narrow: boolean) => rowsOf(narrow)[1]?.[1]?.slice(0, 4);
  assert.deepEqual(teams(narrowAt(390)), [
    ["Field", 0, ""],
    ["East", 1, ""],
    ["Retail", 1, "Field engineering › East"],
    ["Stores", 1, "Field engineering › East › Retail"],
  ]);
  // On a wide screen each level nests under its parent, so no row needs its path.
  assert.deepEqual(teams(narrowAt(1280)), [
    ["Field", 0, ""],
    ["East", 1, ""],
    ["Retail", 2, ""],
    ["Stores", 3, ""],
  ]);

  // As drawn: nothing indents more than one 16px level, and the deep rows print where they are.
  const phone = html(
    <ProjectsPage
      projects={tree}
      root={workspace}
      workspaceName="Acme"
      onFilter={noop}
      canCreate
      canGovern={false}
      narrow={narrowAt(390)}
      onOpen={noop}
      onCreated={noop}
    />,
  );
  const indents = [...phone.matchAll(/margin-left:(\d+)px/g)].map((m) => Number(m[1]));
  assert.ok(indents.length > 0);
  assert.ok(
    indents.every((px) => px === 16),
    `indents at 390px: ${indents}`,
  );
  assert.match(phone, /in Field engineering › East › Retail/);
  const desk = html(
    <ProjectsPage
      projects={tree}
      root={workspace}
      workspaceName="Acme"
      onFilter={noop}
      canCreate
      canGovern={false}
      narrow={false}
      onOpen={noop}
      onCreated={noop}
    />,
  );
  assert.match(desk, /margin-left:48px/);
  assert.doesNotMatch(desk, /in Field engineering › East › Retail/);
});

test("a row whose parent is in another group says where it sits", () => {
  const [, teams, accounts] = rowsOf(false);
  assert.deepEqual(accounts?.[1], [
    ["Globex", 0, "Field engineering"],
    ["Initech", 0, "Field engineering"],
  ]);
  assert.deepEqual(teams?.[1]?.[4], ["Globex renewals", 0, "Field engineering › Globex"]);
  // A top-level row has nothing above it to name.
  assert.deepEqual(teams?.[1]?.[5], ["Platform", 0, ""]);
});

test("a search finds a project by name, title, description or customer, and the rows it keeps say where they sit", () => {
  const found = (q: string) =>
    treeOf(tree, { narrow: false, q }).flatMap((g) => g.rows.map((r) => r.project.name));
  assert.deepEqual(found("initech llc"), ["Initech"], "by customer");
  assert.deepEqual(found("engineering"), ["Field"], "by title");
  assert.deepEqual(found("support for"), ["Globex"], "by description");
  assert.deepEqual(found("RETAIL"), ["Retail"], "ignoring case");
  assert.deepEqual(found("nothing like it"), []);
  // Retail's parent is not shown, so it starts the left edge and names its path.
  assert.deepEqual(rowsOf(false, { q: "retail" }), [
    ["Teams", [["Retail", 0, "Field engineering › East"]]],
  ]);
  assert.deepEqual(
    rowsOf(false, { kind: "account" }).map(([title]) => title),
    ["Accounts"],
  );
});

test("the projects page heads Company-wide with the workspace, links every row, and offers New Project only to those who may make one", () => {
  const page = (over: Partial<Parameters<typeof ProjectsPage>[0]> = {}) =>
    html(
      <ProjectsPage
        projects={tree}
        root={workspace}
        workspaceName="Acme"
        onFilter={noop}
        canCreate={false}
        canGovern={false}
        narrow={false}
        onOpen={noop}
        onCreated={noop}
        {...over}
      />,
    );
  const out = page();
  assert.match(out, /<h1[^>]*>Projects</);
  const heads = ["Company-wide", "Teams", "Accounts"].map((h) => out.indexOf(`>${h}</h2>`));
  assert.ok(
    heads.every((i) => i >= 0) && [...heads].sort((a, b) => a - b).join() === heads.join(),
    `groups in order: ${heads}`,
  );
  assert.ok(
    out.indexOf('href="/projects/~"') < out.indexOf('href="/projects/Company%20news"'),
    "the workspace comes first",
  );
  assert.match(out, />Acme</);
  assert.match(out, /href="\/projects\/Globex"/);
  for (const label of ["Globex Inc", "in Field engineering"]) assert.ok(out.includes(label));
  assert.match(out, />1 running<\/span>/);
  assert.match(out, />4 conversations<\/span>/);
  assert.doesNotMatch(out, /1 running ·/);
  assert.doesNotMatch(out, /New Project/);
  assert.match(page({ canCreate: true }), /New Project/);
  assert.match(page({ projects: [], root: null, canCreate: true }), /No projects yet. Make one/);
  assert.match(page({ q: "zzz" }), /No project matches/);
  // A search leaves the workspace row out: it is the page, not a match.
  assert.doesNotMatch(page({ q: "globex" }), /href="\/projects\/~"/);
  assert.match(page({ projects: null }), /data-testid="skeleton"/);
});

test("the kind rules decide what may be made where", () => {
  assert.deepEqual(
    kindsUnder(WORKSPACE, false),
    ["team", "account"],
    "company-wide needs projects:govern",
  );
  assert.deepEqual(kindsUnder(WORKSPACE, true), ["company", "team", "account"]);
  assert.deepEqual(
    kindsUnder(news, true),
    ["company", "team", "account"],
    "company-wide sits under company-wide",
  );
  assert.deepEqual(kindsUnder(field, true), ["team", "account"], "but never under a team");
  assert.deepEqual(kindsUnder(globex, true), ["team"], "an account never sits under an account");
  assert.deepEqual(kindsUnder(stores, true), [], "nothing goes below the fourth level");
  assert.deepEqual(kindsUnder(retail, false), ["team", "account"]);
});

test("a name offered from a title is one the platform accepts", () => {
  assert.equal(nameFrom("Field engineering"), "Field engineering");
  assert.equal(nameFrom("  ~Q4: launch!"), "Q4 launch");
  assert.equal(nameFrom("Café & bar"), "Caf bar");
  assert.ok(validName(nameFrom("Renewals (2027)")));
  assert.ok(!validName("~"));
  assert.ok(!validName("a/b"));
  assert.ok(!validName(" lead"));
  assert.equal(nameFrom("x".repeat(90)).length, 63);
});

test("an update carries every field it does not change, from the version read, and leaves an emptied one out to clear it", () => {
  assert.deepEqual(updateOf(globex, { instructions: "Renewals close in March." }), {
    version: 4,
    description: "Renewals and support for Globex.",
    account: { customer: "Globex Inc" },
    instructions: "Renewals close in March.",
  });
  assert.deepEqual(updateOf(field, { title: "  " }), {
    version: 1,
    instructions: "Customer accounts. Never promise dates.",
  });
  assert.deepEqual(updateOf(globex, { account: { customer: "", url: "" } }), {
    version: 4,
    description: "Renewals and support for Globex.",
  });
});

test("the picker shows each project's path and kind, and searches its customer", () => {
  const out = html(
    <ProjectChoices projects={tree} value="Globex" onChange={noop} onCreated={noop} close={noop} />,
  );
  assert.match(out, /Account Globex Inc in Field engineering/);
  assert.match(out, /Team in Field engineering › East › Retail/);
  assert.match(out, />Field engineering</, "a project is shown by its title");
  assert.match(out, /Create Project/);
  assert.equal(placeOf(renewals, []), "Team in Field engineering › Globex");
  assert.equal(placeOf(initech, []), "Account Initech LLC in Field engineering");
  assert.equal(placeOf(east, []), "Team in Field engineering");
  // Without projects:write it only chooses.
  assert.doesNotMatch(
    html(
      <ProjectChoices
        projects={tree}
        value={null}
        canCreate={false}
        onChange={noop}
        onCreated={noop}
        close={noop}
      />,
    ),
    /Create Project/,
  );
  // A project of one of the workspace's own kinds is called by the kind's title.
  const reseller = view("Reseller", { kind: "partner", parent: field });
  const kinds = [{ name: "partner", title: "Partner", like: "account" as const }];
  assert.equal(placeOf(reseller, kinds), "Partner in Field engineering");
  assert.match(
    html(
      <ProjectChoices
        projects={[reseller]}
        kinds={kinds}
        value={null}
        onChange={noop}
        onCreated={noop}
        close={noop}
      />,
    ),
    /Partner in Field engineering/,
  );
  // The closed picker names the chosen project by its title.
  assert.match(
    html(<ProjectPicker projects={tree} value="Field" onChange={noop} onCreated={noop} />),
    /aria-label="Select project: Field engineering"/,
  );
});

test("settings no longer edit projects: the workspace section links to Projects and to the workspace itself", () => {
  const me: Me = {
    principal: { subject: "ada", tenant: "acme", kind: "user", name: "Ada", roles: ["admin"] },
    permissions: ["agents:write"],
  };
  const prefs: Preferences = {
    display_name: "",
    theme: "system",
    accent: "purple",
    has_photo: false,
    photo_version: 0,
    pinned: [],
    muted: [],
  };
  const settings: Workspace = {
    history: "recent",
    history_limit: 20000,
    title_agent: "",
    memory_agent: "",
    memory_auto_approve: false,
    router: { enabled: false },
  };
  const out = html(
    <Settings
      me={me}
      prefs={prefs}
      workspace={settings}
      agents={[]}
      projects={["Field"]}
      canAdmin
      section="workspace"
      onSection={noop}
      explain="runs:read"
      onExplain={noop}
      onPrefs={noop}
      onWorkspace={noop}
      onOpenProjects={noop}
      onOpenWorkspace={noop}
    />,
  );
  assert.doesNotMatch(out, /Add Project/);
  assert.doesNotMatch(out, /No projects yet/);
  assert.match(out, /href="\/projects"[^>]*>Open Projects</);
  assert.match(out, /href="\/projects\/~"[^>]*>Open the Workspace</);
});

test("every kind of change in a project's history reads as words", () => {
  // Listing each kind as a key makes typecheck name one the API adds.
  const kinds: Record<ProjectChange["what"], true> = {
    created: true,
    updated: true,
    deleted: true,
    managed: true,
    moved: true,
    archived: true,
    members: true,
    agents: true,
    home: true,
    experts: true,
    skills: true,
    knowledge: true,
    channel: true,
  };
  for (const what of Object.keys(kinds))
    assert.ok(copy.project.what[what], `no words for a ${what} change`);
});

const knowledgeOf = (
  opts: { editable?: boolean; fileOwned?: boolean; draft?: string[]; project?: string } = {},
) =>
  html(
    <KnowledgeLists
      project={opts.project ?? "Globex"}
      view={globexKnowledge}
      titles={{ "": "Acme", Field: "Field engineering" }}
      sources={knowledgeSources}
      draft={opts.draft ?? globexKnowledge.knowledge}
      editable={opts.editable ?? false}
      fileOwned={opts.fileOwned ?? false}
    />,
  );

test("knowledge: an owner who reads knowledge gets an editor of the sources the project names", () => {
  const out = knowledgeOf({ editable: true });
  assert.match(out, />Add Source</);
  assert.match(out, />Save Knowledge</);
  assert.match(out, /aria-label="Remove globex-contracts"/);
  assert.doesNotMatch(out, /The platform file sets/);
});

test("knowledge: an owner in a workspace with no sources is told so, and offered no Save with nothing to save", () => {
  const out = html(
    <KnowledgeLists
      project="Globex"
      view={{ ...globexKnowledge, knowledge: [] }}
      titles={{}}
      sources={[]}
      draft={[]}
      editable
      fileOwned={false}
    />,
  );
  assert.match(out, /The workspace has no sources yet/);
  assert.doesNotMatch(out, /Save Knowledge|Add Source/);
  // Both parts are titled sections, as the rest of the Context tab is.
  assert.equal([...out.matchAll(/<section aria-labelledby=/g)].length, 2);
});

test("knowledge: at the cap of 8 nothing more can be added", () => {
  const names = Array.from({ length: MAX_KNOWLEDGE }, (_, i) => `source-${i}`);
  const out = knowledgeOf({ editable: true, draft: names });
  assert.doesNotMatch(out, />Add Source</);
  assert.match(out, /the most a project names/);
});

test("knowledge: a member sees the sources the project names, read only", () => {
  const out = knowledgeOf();
  assert.match(out, /<li[^>]*>globex-contracts<\/li>/);
  assert.doesNotMatch(out, /Add Source|Save Knowledge|Remove globex-contracts/);
});

test("knowledge: what runs here are grounded in says where each source comes from, nearest first", () => {
  const out = knowledgeOf();
  assert.match(out, /What runs here are grounded in/);
  const given = [
    ...out.matchAll(/<span class="min-w-0[^"]*">([^<]+)<\/span><span[^>]*>([^<]+)<\/span>/g),
  ].map((m) => [m[1], m[2]]);
  assert.deepEqual(given, [
    ["globex-contracts", "From this project"],
    ["field-playbook", "From Field engineering"],
    ["handbook", "From the workspace"],
  ]);
  // On the workspace itself, what it names is its own.
  assert.match(knowledgeOf({ project: "" }), /handbook<\/span><span[^>]*>From this project</);
});

test("knowledge: a list the platform file sets is read only, and says so", () => {
  const out = knowledgeOf({ editable: true, fileOwned: true });
  assert.match(out, /The platform file sets these sources\./);
  assert.doesNotMatch(out, /Add Source|Save Knowledge/);
  assert.match(out, /<li[^>]*>globex-contracts<\/li>/);
});

test("project rows show named agents and a still unread indicator only for unread updates", () => {
  const unread = { ...view("Unread"), unread: 2, row_agents: ["lead", "helper"] };
  const read = { ...view("Read"), unread: 0, row_agents: [] };
  const markup = html(
    <ProjectsPage
      projects={[unread, read]}
      root={null}
      workspaceName="Acme"
      canCreate={false}
      canGovern={false}
      narrow={false}
      onFilter={noop}
      onOpen={noop}
      onCreated={noop}
    />,
  );
  assert.match(markup, />lead<\/span>/);
  assert.match(markup, />helper<\/span>/);
  assert.equal((markup.match(/aria-label="Unread updates"/g) ?? []).length, 1);
  assert.doesNotMatch(markup, /animate-/);
});
