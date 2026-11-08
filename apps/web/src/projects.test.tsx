import assert from "node:assert/strict";
import { test } from "node:test";
import { defaultCopy as copy } from "@humagen/components";
import type { ReactElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import type { Me, Preferences, ProjectChange, ProjectView, Settings as Workspace } from "./api.ts";
import { ProjectChoices, ProjectPicker, placeOf } from "./ProjectPicker.tsx";
import { ProjectsPage } from "./Projects.tsx";
import {
  east,
  field,
  globex,
  initech,
  news,
  projectView,
  renewals,
  retail,
  stores,
  tree,
  view,
  workspace,
} from "./projects.fixtures.ts";
import {
  cardsOf,
  kindsUnder,
  nameFrom,
  updateOf,
  validName,
  visibilityOf,
  WORKSPACE,
} from "./projects.ts";
import { Settings } from "./Settings.tsx";

const html = (el: ReactElement) => renderToStaticMarkup(el);
const noop = () => {};
const page = (over: Partial<Parameters<typeof ProjectsPage>[0]> = {}) =>
  html(
    <ProjectsPage
      projects={tree}
      root={workspace}
      canCreate={false}
      canGovern={false}
      me="ada"
      onFilter={noop}
      onOpen={noop}
      onCreated={noop}
      {...over}
    />,
  );
/** The cards' titles, in the order they are drawn, each with where it links. */
const cardsIn = (out: string) =>
  [...out.matchAll(/<a href="(\/projects\/[^"]+)"[^>]*>([^<]*)<\/a>/g)].map((m) => [m[2], m[1]]);
/** A project made at `created`, last active at `active` when given, and changed when it was made. */
const dated = (name: string, created: string, active?: string, over: Partial<ProjectView> = {}) =>
  projectView({
    name,
    created_at: created,
    updated_at: created,
    ...(active ? { active_at: active } : {}),
    ...over,
  });
const partners = {
  ...workspace,
  kinds: [{ name: "partner", title: "Partners", like: "account" as const }],
};
const reseller = view("Reseller", { kind: "partner", parent: field });

test("the projects page is one title over a flat grid of cards: every project but the workspace, sub-projects too", () => {
  const out = page();
  assert.match(out, /<h1[^>]*>Projects<\/h1>/);
  assert.doesNotMatch(out, /Where work belongs/, "no lede");
  assert.deepEqual(
    cardsIn(out).map(([title]) => title),
    [
      "Company news",
      "East",
      "Field engineering",
      "Globex",
      "Globex renewals",
      "Initech",
      "Platform",
      "Retail",
      "Stores",
    ],
  );
  assert.ok(cardsIn(out).some(([, to]) => to === "/projects/Globex"));
  assert.doesNotMatch(out, /href="\/projects\/~"/, "the workspace is not a card");
  // No tree: no groups, no indent, no expander, and no counts.
  for (const gone of [
    /Company-wide/,
    />Teams</,
    />Accounts</,
    /margin-left/,
    /<details/,
    /<h2/,
    /conversations?</,
    /running</,
  ])
    assert.doesNotMatch(out, gone);
  assert.match(out, /<time dateTime="2026-10-01T09:00:00Z">Updated [^<]+<\/time>/);
  assert.doesNotMatch(out, /New Project/);
  assert.equal((page({ canCreate: true }).match(/New Project/g) ?? []).length, 1);
  assert.match(page({ projects: null }), /data-testid="skeleton"/);
  assert.match(page({ q: "zzz" }), /No project matches\./);
  assert.ok(!out.includes(String.fromCharCode(0x2014)), "no em dashes");
});

test("with no projects the page is a title and one action", () => {
  const empty = page({ projects: [], canCreate: true });
  assert.match(empty, />No projects yet</);
  assert.equal((empty.match(/New Project/g) ?? []).length, 1, "not in the header as well");
  assert.doesNotMatch(empty, /Search projects/);
  // The workspace alone is no project of the reader's.
  assert.match(page({ projects: [workspace] }), />No projects yet</);
  assert.doesNotMatch(page({ projects: [] }), /New Project/);
});

test("cards come pinned first, then by last activity, by title or newest made, as the address asks", () => {
  const alpha = dated("alpha", "2026-01-01T00:00:00Z", "2026-10-05T00:00:00Z");
  const bravo = dated("bravo", "2026-03-01T00:00:00Z");
  const apex = dated("charlie", "2026-02-01T00:00:00Z", "2026-10-06T00:00:00Z", { title: "Apex" });
  const all = [alpha, bravo, apex];
  const order = (opts: Parameters<typeof cardsOf>[1]) => cardsOf(all, opts).map((p) => p.name);
  assert.deepEqual(order({}), ["charlie", "alpha", "bravo"], "last activity, else last change");
  assert.deepEqual(order({ sort: "name" }), ["alpha", "charlie", "bravo"], "by title, A to Z");
  assert.deepEqual(order({ sort: "created" }), ["bravo", "charlie", "alpha"], "newest made");
  assert.deepEqual(order({ pinned: ["bravo"] }), ["bravo", "charlie", "alpha"]);
  assert.deepEqual(order({ sort: "name", pinned: ["bravo", "charlie"] }), [
    "charlie",
    "bravo",
    "alpha",
  ]);
  // As drawn, with the sort the address names.
  const drawn = (over: Partial<Parameters<typeof ProjectsPage>[0]>) =>
    cardsIn(page({ projects: all, ...over })).map(([title]) => title);
  assert.deepEqual(drawn({}), ["Apex", "alpha", "bravo"]);
  assert.deepEqual(drawn({ sort: "name" }), ["alpha", "Apex", "bravo"]);
  assert.deepEqual(drawn({ sort: "created", pinned: ["alpha"] }), ["alpha", "bravo", "Apex"]);
});

test("a search finds a project by its title, description or name, and leaves the rest out", () => {
  const found = (q: string) => cardsOf(tree, { q, sort: "name" }).map((p) => p.name);
  assert.deepEqual(found("engineering"), ["Field"], "by title");
  assert.deepEqual(found("support for"), ["Globex"], "by description");
  assert.deepEqual(found("RETAIL"), ["Retail"], "by name, ignoring case");
  assert.deepEqual(found("nothing like it"), []);
  assert.deepEqual(
    cardsIn(page({ q: "globex" })).map(([title]) => title),
    ["Globex", "Globex renewals"],
  );
});

test("a kind filter shows only when the workspace defines kinds of its own", () => {
  assert.doesNotMatch(page(), /aria-label="Kind"/);
  // An old link's kind is not applied where there is no filter to show it.
  assert.equal(cardsIn(page({ kind: "account" })).length, tree.length);
  const own = page({ projects: [...tree, reseller], root: partners });
  assert.match(own, /aria-label="Kind"/);
  assert.deepEqual(
    cardsIn(page({ projects: [...tree, reseller], root: partners, kind: "partner" })).map(
      ([title]) => title,
    ),
    ["Reseller"],
  );
});

test("a card is its title, when it last changed and a pin: no agents, counts or unread marks", () => {
  const busy = {
    ...dated("Busy", "2026-10-01T09:00:00Z", "2026-10-02T09:00:00Z"),
    unread: 2,
    active: 1,
    conversations: 4,
    row_agents: ["lead", "helper"],
  };
  const out = page({ projects: [busy], onPin: async () => {} });
  assert.doesNotMatch(out, /\blead\b|\bhelper\b/);
  assert.doesNotMatch(out, /Unread updates|1 running|4 conversations/);
  assert.match(
    out,
    /<time dateTime="2026-10-02T09:00:00Z">Updated /,
    "last activity, not last change",
  );
  assert.doesNotMatch(out, /animate-/);
});

test("a pin says what it does to which project, and shows once pinned", () => {
  const out = page({ pinned: ["Globex"], onPin: async () => {} });
  assert.match(out, /aria-pressed="true" aria-label="Unpin project Globex"/);
  assert.match(out, /aria-pressed="false" aria-label="Pin project Platform"/);
  assert.equal((out.match(/aria-pressed="true"/g) ?? []).length, 1);
  assert.equal(cardsIn(out)[0]?.[0], "Globex", "pinned first");
  assert.doesNotMatch(page(), /aria-pressed/, "no pins without a way to save them");
});

test("a private project says so on its card; a workspace one and the reader's Personal project do not", () => {
  const owned = [{ who: "ada", role: "owner" as const }];
  const secret = dated("Secret", "2026-10-01T09:00:00Z", undefined, { members: owned });
  const personal = dated("personal-ada", "2026-10-01T09:00:00Z", undefined, {
    title: "Personal",
    personal_owner: "ada",
    members: owned,
  });
  const out = page({ projects: [secret, personal, ...tree], sort: "name" });
  assert.equal((out.match(/>Private</g) ?? []).length, 1);
  assert.ok(
    cardsIn(out).some(([title]) => title === "Personal"),
    "Personal is a card of its own",
  );
  assert.match(out, /Secret<\/a>(?:(?!<article).)*>Private</s);
});

test("a project's visibility is the server's word for it, else read from its entries as the server reads them", () => {
  const as = (members: ProjectView["members"], over: Partial<ProjectView> = {}) =>
    visibilityOf(projectView({ name: "P", members, ...over }));
  assert.equal(as([{ who: "*", role: "member" }]), "workspace");
  assert.equal(as([{ who: "ada", role: "owner" }]), "private");
  assert.equal(as([{ who: "ada", role: "owner" }], { personal_owner: "ada" }), "private");
  assert.equal(
    as([
      { who: "ada", role: "owner" },
      { who: "bo", role: "member" },
    ]),
    "shared",
  );
  assert.equal(as([{ who: "role:sales", role: "member" }]), "shared");
  // A server that says so is believed.
  assert.equal(
    visibilityOf({ ...projectView({ name: "P" }), visibility: "shared" }),
    "shared",
    "the server's word wins",
  );
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
  // A workspace's own kind behaves as the built-in it is like: no account under it, and it never sits under one.
  const kinds = partners.kinds;
  assert.deepEqual(kindsUnder({ name: "Reseller", kind: "partner", depth: 2 }, true, kinds), [
    "team",
  ]);
  assert.deepEqual(kindsUnder(globex, true, kinds), ["team"]);
  assert.deepEqual(kindsUnder(field, false, kinds), ["team", "account", "partner"]);
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
