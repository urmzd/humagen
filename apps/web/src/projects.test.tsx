import assert from "node:assert/strict";
import { test } from "node:test";
import type { ReactElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import type { Me, Preferences, Settings as Workspace } from "./api.ts";
import { ProjectChoices, ProjectPicker, placeOf } from "./ProjectPicker.tsx";
import { ProjectsPage } from "./Projects.tsx";
import { east, field, globex, initech, news, renewals, retail, stores, tree, workspace } from "./projects.fixtures.ts";
import { kindsUnder, nameFrom, narrowAt, treeOf, updateOf, validName, WORKSPACE } from "./projects.ts";
import { Settings } from "./Settings.tsx";

const html = (el: ReactElement) => renderToStaticMarkup(el);
const noop = () => {};
const rowsOf = (narrow: boolean, opts: { q?: string; kind?: "company" | "team" | "account" } = {}) =>
  treeOf(tree, { narrow, ...opts }).map((g) => [g.title, g.rows.map((r) => [r.project.name, r.indent, r.path.join(" › ")])]);

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
  const phone = html(<ProjectsPage projects={tree} root={workspace} workspaceName="Acme" onFilter={noop} canCreate canGovern={false} narrow={narrowAt(390)} onOpen={noop} onCreated={noop} />);
  const indents = [...phone.matchAll(/padding-left:(\d+)px/g)].map((m) => Number(m[1]));
  assert.ok(indents.length > 0);
  assert.ok(indents.every((px) => px === 16), `indents at 390px: ${indents}`);
  assert.match(phone, /in Field engineering › East › Retail/);
  const desk = html(<ProjectsPage projects={tree} root={workspace} workspaceName="Acme" onFilter={noop} canCreate canGovern={false} narrow={false} onOpen={noop} onCreated={noop} />);
  assert.match(desk, /padding-left:48px/);
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
  const found = (q: string) => treeOf(tree, { narrow: false, q }).flatMap((g) => g.rows.map((r) => r.project.name));
  assert.deepEqual(found("initech llc"), ["Initech"], "by customer");
  assert.deepEqual(found("engineering"), ["Field"], "by title");
  assert.deepEqual(found("support for"), ["Globex"], "by description");
  assert.deepEqual(found("RETAIL"), ["Retail"], "ignoring case");
  assert.deepEqual(found("nothing like it"), []);
  // Retail's parent is not shown, so it starts the left edge and names its path.
  assert.deepEqual(rowsOf(false, { q: "retail" }), [["Teams", [["Retail", 0, "Field engineering › East"]]]]);
  assert.deepEqual(rowsOf(false, { kind: "account" }).map(([title]) => title), ["Accounts"]);
});

test("the projects page heads Company-wide with the workspace, links every row, and offers New Project only to those who may make one", () => {
  const page = (over: Partial<Parameters<typeof ProjectsPage>[0]> = {}) =>
    html(<ProjectsPage projects={tree} root={workspace} workspaceName="Acme" onFilter={noop} canCreate={false} canGovern={false} narrow={false} onOpen={noop} onCreated={noop} {...over} />);
  const out = page();
  assert.match(out, /<h1[^>]*>Projects</);
  const heads = ["Company-wide", "Teams", "Accounts"].map((h) => out.indexOf(`>${h}</h2>`));
  assert.ok(heads.every((i) => i >= 0) && [...heads].sort((a, b) => a - b).join() === heads.join(), `groups in order: ${heads}`);
  assert.ok(out.indexOf('href="/projects/~"') < out.indexOf('href="/projects/Company%20news"'), "the workspace comes first");
  assert.match(out, />Acme</);
  assert.match(out, /href="\/projects\/Globex"/);
  assert.match(out, /Account · Globex Inc · in Field engineering/);
  assert.match(out, /1 running · 4 tasks/);
  assert.doesNotMatch(out, /New Project/);
  assert.match(page({ canCreate: true }), /New Project/);
  assert.match(page({ projects: [], root: null, canCreate: true }), /No projects yet. Make one/);
  assert.match(page({ q: "zzz" }), /No project matches/);
  // A search leaves the workspace row out: it is the page, not a match.
  assert.doesNotMatch(page({ q: "globex" }), /href="\/projects\/~"/);
  assert.match(page({ projects: null }), /data-testid="skeleton"/);
});

test("the kind rules decide what may be made where", () => {
  assert.deepEqual(kindsUnder(WORKSPACE, false), ["team", "account"], "company-wide needs projects:govern");
  assert.deepEqual(kindsUnder(WORKSPACE, true), ["company", "team", "account"]);
  assert.deepEqual(kindsUnder(news, true), ["company", "team", "account"], "company-wide sits under company-wide");
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
  assert.deepEqual(updateOf(field, { title: "  " }), { version: 1, instructions: "Customer accounts. Never promise dates." });
  assert.deepEqual(updateOf(globex, { account: { customer: "", url: "" } }), { version: 4, description: "Renewals and support for Globex." });
});

test("the picker shows each project's path and kind, and searches its customer", () => {
  const out = html(<ProjectChoices projects={tree} value="Globex" onChange={noop} onCreated={noop} close={noop} />);
  assert.match(out, /Account · Globex Inc · in Field engineering/);
  assert.match(out, /Team · in Field engineering › East › Retail/);
  assert.match(out, />Field engineering</, "a project is shown by its title");
  assert.match(out, /Create Project/);
  assert.equal(placeOf(renewals), "Team · in Field engineering › Globex");
  assert.equal(placeOf(initech), "Account · Initech LLC · in Field engineering");
  assert.equal(placeOf(east), "Team · in Field engineering");
  // Without projects:write it only chooses.
  assert.doesNotMatch(html(<ProjectChoices projects={tree} value={null} canCreate={false} onChange={noop} onCreated={noop} close={noop} />), /Create Project/);
  // The closed picker names the chosen project by its title.
  assert.match(html(<ProjectPicker projects={tree} value="Field" onChange={noop} onCreated={noop} />), /aria-label="Select project: Field engineering"/);
});

test("settings no longer edit projects: the workspace section links to Projects and to the workspace itself", () => {
  const me: Me = { principal: { subject: "ada", tenant: "acme", kind: "user", name: "Ada", roles: ["admin"] }, permissions: ["agents:write"] };
  const prefs: Preferences = { display_name: "", theme: "system", accent: "purple", has_photo: false, photo_version: 0, pinned: [] };
  const settings: Workspace = { baseline: "", history: "recent", history_limit: 20000, title_agent: "", memory_agent: "", memory_auto_approve: false };
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
