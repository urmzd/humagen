import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { Toaster } from "@humagen/render-web/ui/sonner";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Corpus, Source } from "./api.ts";
import { CorpusPage, KnowledgePage } from "./Corpora.tsx";
import { stubFetch } from "./dom.ts";

const words = defaultCopy.corpora;
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

const at = "2026-11-07T12:00:00Z";
const corpus = (over: Partial<Corpus> = {}): Corpus => ({
  name: "hr-policies",
  title: "HR policies",
  description: "What HR has written down",
  owner: "ada",
  readers: [],
  members: [
    {
      kind: "source",
      name: "handbook",
      holds: "files",
      provider: "manual",
      items: 12,
      status: "ready",
    },
    {
      kind: "source",
      name: "drive-hr",
      holds: "references",
      provider: "drive",
      items: 3,
      status: "syncing",
    },
    {
      kind: "catalog",
      name: "people-warehouse",
      holds: "records",
      provider: "bigquery",
      items: 4,
      status: "failed",
    },
  ],
  agents: [],
  label: "internal",
  links: "isolated",
  created_at: at,
  updated_at: at,
  version: 3,
  mine: true,
  ...over,
});
const source = (name: string, provider?: string): Source => ({
  name,
  documents: 1,
  passages: 1,
  excluded: 0,
  variants: [],
  updated_at: at,
  ...(provider
    ? {
        lifecycle: {
          provider,
          mode: "live",
          status: "available",
          connected_at: at,
          stop_after_seconds: 60,
          attempts: [],
        },
      }
    : {}),
});
const directory = {
  "GET /api/people": () => ({
    people: [
      { subject: "ada", name: "Ada", handle: "ada", roles: [], picture: null },
      { subject: "bob", name: "Bob", handle: "bob", roles: [], picture: null },
    ],
    next: null,
  }),
  "GET /api/projects": () => ({ projects: [], next: null }),
};

/** Every card's ⋯ is the shared More menu; a card is found by its list item's name. */
const more = defaultCopy.more;
async function openMenu(name: string) {
  const card = await screen.findByRole("listitem", { name });
  fireEvent.click(within(card).getByRole("button", { name: more }));
}
async function choose(item: string) {
  fireEvent.click(await screen.findByRole("menuitem", { name: item }));
}

test("Knowledge lists corpora as cards by title: providers, items, one pill, Open and an owner's menu", async () => {
  net = stubFetch({
    "GET /api/corpora": () => ({
      corpora: [
        corpus(),
        corpus({
          name: "finance",
          title: undefined,
          label: "public",
          mine: false,
          links: "shared",
          members: [],
        }),
      ],
    }),
  });
  render(<KnowledgePage sources={[]} canWrite onOpen={() => {}} />);
  const hr = await screen.findByRole("listitem", { name: "HR policies" });
  // The title is the card's one link.
  assert.ok(within(hr).getByRole("link", { name: "HR policies" }));
  assert.ok(within(hr).getByText("Uploads"));
  assert.ok(within(hr).getByText("Google Drive"));
  assert.ok(within(hr).getByText("BigQuery"));
  // Each provider is a reference with what it brings.
  assert.ok(within(hr).getByText("· 12"));
  assert.ok(within(hr).getByText("· 3"));
  assert.ok(within(hr).getByText("· 4"));
  assert.ok(within(hr).getByText(words.labels.internal));
  assert.equal(
    within(hr).getByRole("link", { name: "HR policies" }).getAttribute("href"),
    "/knowledge/hr-policies",
  );
  assert.ok(within(hr).getByRole("button", { name: more }));
  // A corpus with no title shows its name.
  const finance = screen.getByRole("listitem", { name: "finance" });
  assert.ok(within(finance).getByText(words.labels.public));
  // Only the owner changes a corpus.
  assert.equal(within(finance).queryByRole("button", { name: more }), null);
  // With corpora shown, New Corpus is the page's one action.
  assert.equal(screen.getAllByRole("button", { name: words.newCorpus }).length, 1);
});

test("an empty Knowledge is a title and New Corpus", async () => {
  net = stubFetch({ "GET /api/corpora": () => ({ corpora: [] }) });
  render(<KnowledgePage sources={[]} canWrite onOpen={() => {}} />);
  await screen.findByText(words.empty);
  assert.equal(screen.getAllByRole("button", { name: words.newCorpus }).length, 1);
});

test("New Corpus asks for a title, names it from the title, sets who reads it, and opens it", async () => {
  let opened = "";
  net = stubFetch({
    "GET /api/corpora": () => ({ corpora: [] }),
    "POST /api/corpora": (body) => corpus({ ...(body as Partial<Corpus>), members: [] }),
    ...directory,
  });
  render(<KnowledgePage sources={[]} canWrite onOpen={(name) => (opened = name)} />);
  fireEvent.click(await screen.findByRole("button", { name: words.newCorpus }));
  const form = await screen.findByRole("form", { name: words.form.newTitle });
  const title = within(form).getByRole("textbox", {
    name: new RegExp(`^${words.form.title}`),
  }) as HTMLInputElement;
  fireEvent.change(title, { target: { value: "HR policies!" } });
  assert.equal(title.value, "HR policies!");
  assert.ok(within(form).getByText(words.form.nameIs("hr-policies")));
  fireEvent.change(within(form).getByRole("textbox", { name: words.form.description }), {
    target: { value: "Leave and benefits" },
  });
  fireEvent.click(within(form).getByRole("radio", { name: words.form.workspace }));
  fireEvent.click(within(form).getByRole("button", { name: words.form.create }));
  await waitFor(() => assert.equal(opened, "hr-policies"));
  assert.deepEqual(net.seen.find((r) => r.method === "POST")?.body, {
    name: "hr-policies",
    title: "HR policies!",
    description: "Leave and benefits",
    readers: [{ kind: "workspace" }],
    label: "internal",
  });
});

test("Delete asks first, then deletes at the version read", async () => {
  net = stubFetch({
    "GET /api/corpora": () => ({ corpora: [corpus()] }),
    "DELETE /api/corpora/hr-policies": () => undefined,
  });
  render(<KnowledgePage sources={[]} canWrite onOpen={() => {}} />);
  await openMenu("HR policies");
  await choose(words.delete);
  const ask = await screen.findByRole("group", { name: words.deleteAsk("HR policies") });
  assert.ok(!net.seen.some((r) => r.method === "DELETE"));
  fireEvent.click(within(ask).getByRole("button", { name: words.deleteAction }));
  await waitFor(() =>
    assert.ok(
      net?.seen.some(
        (r) => r.method === "DELETE" && r.path === "/api/corpora/hr-policies?version=3",
      ),
    ),
  );
});

test("sources in no corpus are one quiet row that files them into a corpus", async () => {
  net = stubFetch({
    "GET /api/corpora": () => ({ corpora: [corpus()] }),
    "POST /api/corpora/hr-policies/members": () => corpus(),
  });
  render(
    <KnowledgePage
      sources={[source("handbook"), source("wiki"), source("notes", "notion")]}
      canWrite
      onOpen={() => {}}
    />,
  );
  // handbook is in hr-policies already.
  fireEvent.click(await screen.findByRole("button", { name: words.unfiled(2) }));
  const form = await screen.findByRole("form", { name: words.file.title });
  fireEvent.click(within(form).getByRole("checkbox", { name: "notes" }));
  fireEvent.click(within(form).getByRole("button", { name: words.file.action }));
  await waitFor(() =>
    assert.deepEqual(
      net?.seen.filter((r) => r.method === "POST").map((r) => r.body),
      [{ kind: "source", name: "wiki" }],
    ),
  );
});

test("a corpus page has Sources, Search and Access tabs, members with what they hold and their state", async () => {
  const tabs: string[] = [];
  net = stubFetch({ "GET /api/corpora/hr-policies": () => corpus(), ...directory });
  render(
    <CorpusPage
      name="hr-policies"
      tab="sources"
      sources={[]}
      canWrite
      onTab={(t) => tabs.push(t)}
      onBack={() => {}}
      onOpenSource={() => {}}
    />,
  );
  const handbook = await screen.findByRole("listitem", { name: "handbook" });
  assert.ok(within(handbook).getByText(words.holds.files));
  assert.ok(within(handbook).getByText("· 12"));
  assert.ok(within(handbook).getByText(words.status.ready));
  assert.equal(
    within(handbook).getByRole("link", { name: "handbook" }).getAttribute("href"),
    "/knowledge/hr-policies/sources/handbook",
  );
  const warehouse = screen.getByRole("listitem", { name: "people-warehouse" });
  assert.ok(within(warehouse).getByText(words.holds.records));
  assert.ok(within(warehouse).getByText("· 4"));
  assert.ok(within(warehouse).getByText(words.status.failed));
  assert.ok(screen.getByText("What HR has written down"));
  // The header names it by its title.
  assert.ok(screen.getByText("HR policies", { selector: "span" }));
  fireEvent.click(screen.getByRole("tab", { name: words.tabs.search }));
  fireEvent.click(screen.getByRole("tab", { name: words.tabs.access }));
  assert.deepEqual(tabs, ["search", "access"]);
  assert.ok(screen.getByRole("button", { name: words.addSource }));
});

test("the owner adds an existing source and removes a member", async () => {
  let current = corpus();
  net = stubFetch({
    "GET /api/corpora/hr-policies": () => current,
    "GET /api/catalog": () => ({ connectors: [], entries: [] }),
    "POST /api/corpora/hr-policies/members": (body) => {
      const ref = body as { kind: "source"; name: string };
      current = corpus({
        members: [
          ...current.members,
          { ...ref, holds: "files", provider: "manual", items: 1, status: "ready" },
        ],
      });
      return current;
    },
    "DELETE /api/corpora/hr-policies/members/source/handbook": () => {
      current = corpus({ members: current.members.filter((m) => m.name !== "handbook") });
      return current;
    },
  });
  render(
    <>
      <Toaster />
      <CorpusPage
        name="hr-policies"
        tab="sources"
        sources={[source("handbook"), source("wiki")]}
        canWrite
        onTab={() => {}}
        onBack={() => {}}
        onOpenSource={() => {}}
      />
    </>,
  );
  fireEvent.click(await screen.findByRole("button", { name: words.addSource }));
  const sheet = await screen.findByRole("dialog", { name: words.add.title });
  // What the corpus holds is not offered again.
  assert.equal(within(sheet).queryByRole("button", { name: words.add.addTo("handbook") }), null);
  // Every new kind is set up here, Snowflake included.
  assert.equal(
    within(sheet).getByRole("button", { name: "Snowflake" }).hasAttribute("disabled"),
    false,
  );
  fireEvent.click(within(sheet).getByRole("button", { name: words.add.addTo("wiki") }));
  await screen.findByText(words.added("wiki"));
  assert.deepEqual(net.seen.find((r) => r.method === "POST")?.body, {
    kind: "source",
    name: "wiki",
  });
  fireEvent.keyDown(sheet, { key: "Escape" });
  await waitFor(() => assert.equal(screen.queryByRole("dialog"), null));
  await screen.findByRole("listitem", { name: "wiki" });
  await openMenu("handbook");
  await choose(words.remove);
  await screen.findByText(words.removed("handbook"));
  await waitFor(() => assert.equal(screen.queryByRole("listitem", { name: "handbook" }), null));
});

test("Upload Files makes a source inside the corpus, adds its text files and holds it", async () => {
  let current = corpus({ members: [] });
  net = stubFetch({
    "GET /api/corpora/hr-policies": () => current,
    "GET /api/catalog": () => ({ connectors: [], entries: [] }),
    "POST /api/knowledge": (body) => ({ ...source("leave-policy"), ...(body as object) }),
    "POST /api/knowledge/leave-policy/documents": (body) => ({ id: "d1", ...(body as object) }),
    "POST /api/corpora/hr-policies/members": (body) => {
      current = corpus({
        members: [
          {
            ...(body as { kind: "source"; name: string }),
            holds: "files",
            provider: "manual",
            items: 1,
            status: "ready",
          },
        ],
      });
      return current;
    },
  });
  let changed = 0;
  render(
    <CorpusPage
      name="hr-policies"
      tab="sources"
      sources={[]}
      canWrite
      onTab={() => {}}
      onBack={() => {}}
      onOpenSource={() => {}}
      onSourcesChanged={() => changed++}
    />,
  );
  await screen.findByText(words.noSources);
  fireEvent.click(screen.getByRole("button", { name: words.addSource }));
  fireEvent.click(await screen.findByRole("button", { name: words.add.upload }));
  const form = await screen.findByRole("form", { name: words.add.upload });
  const name = within(form).getByRole("textbox", {
    name: new RegExp(`^${words.form.name}`),
  }) as HTMLInputElement;
  assert.equal(name.value, "hr-policies-files");
  fireEvent.change(name, { target: { value: "Leave policy" } });
  const files = within(form).getByLabelText(words.add.files) as HTMLInputElement;
  const file = new File(["Twenty days a year."], "leave.md", { type: "text/markdown" });
  Object.defineProperty(files, "files", { value: [file] });
  fireEvent.change(files);
  assert.equal(
    (within(form).getByRole("button", { name: words.add.uploadAction }) as HTMLButtonElement)
      .disabled,
    false,
  );
  // The file list is set by hand here, which form validation does not see, so submit directly.
  fireEvent.submit(form);
  await screen.findByRole("listitem", { name: "leave-policy" });
  assert.deepEqual(
    net.seen.filter((r) => r.method === "POST").map((r) => [r.path, r.body]),
    [
      ["/api/knowledge", { name: "leave-policy", description: "" }],
      [
        "/api/knowledge/leave-policy/documents",
        { title: "leave", uri: "", text: "Twenty days a year." },
      ],
      ["/api/corpora/hr-policies/members", { kind: "source", name: "leave-policy" }],
    ],
  );
  assert.equal(changed, 1);
});

test("a member a reader cannot see is listed without its name, and they cannot act on it", async () => {
  net = stubFetch({
    "GET /api/corpora/hr-policies": () =>
      corpus({
        mine: false,
        members: [{ kind: "source", name: "", items: 0, status: "hidden" }],
      }),
  });
  render(
    <CorpusPage
      name="hr-policies"
      tab="sources"
      sources={[]}
      canWrite
      onTab={() => {}}
      onBack={() => {}}
      onOpenSource={() => {}}
    />,
  );
  const hidden = await screen.findByRole("listitem", { name: words.hidden });
  assert.equal(within(hidden).queryByRole("button"), null);
  assert.equal(within(hidden).queryByRole("link"), null);
  assert.equal(within(hidden).queryByText(/items/), null);
});

test("a reader who does not own a corpus sees it read-only", async () => {
  net = stubFetch({
    "GET /api/corpora/hr-policies": () => corpus({ mine: false, readers: [{ kind: "workspace" }] }),
    ...directory,
  });
  const view = render(
    <CorpusPage
      name="hr-policies"
      tab="sources"
      sources={[]}
      canWrite
      onTab={() => {}}
      onBack={() => {}}
      onOpenSource={() => {}}
    />,
  );
  await screen.findByRole("listitem", { name: "handbook" });
  assert.equal(screen.queryByRole("button", { name: words.addSource }), null);
  assert.equal(
    within(screen.getByRole("listitem", { name: "handbook" })).queryByRole("button", {
      name: more,
    }),
    null,
  );
  view.rerender(
    <CorpusPage
      name="hr-policies"
      tab="access"
      sources={[]}
      canWrite
      onTab={() => {}}
      onBack={() => {}}
      onOpenSource={() => {}}
    />,
  );
  const readers = await screen.findByRole("region", { name: words.access.readers });
  assert.ok(within(readers).getByText(words.form.workspace));
  assert.equal(screen.queryByRole("radio"), null);
  assert.equal(screen.queryByRole("button", { name: words.form.save }), null);
  assert.equal(screen.queryByRole("button", { name: words.access.handOff }), null);
  await screen.findByText("Ada");
});

test("a change refused because someone else changed the corpus reloads it and says so", async () => {
  let reads = 0;
  net = stubFetch({
    "GET /api/corpora/hr-policies": () => {
      reads++;
      return corpus({ version: 3 + reads, description: reads > 1 ? "Changed elsewhere" : "First" });
    },
    "PATCH /api/corpora/hr-policies": () =>
      new Response(JSON.stringify({ error: "corpus hr-policies changed" }), { status: 409 }),
    ...directory,
  });
  render(
    <>
      <Toaster />
      <CorpusPage
        name="hr-policies"
        tab="access"
        sources={[]}
        canWrite
        onTab={() => {}}
        onBack={() => {}}
        onOpenSource={() => {}}
      />
    </>,
  );
  fireEvent.click(await screen.findByRole("radio", { name: words.form.workspace }));
  fireEvent.click(screen.getByRole("button", { name: words.form.save }));
  await screen.findByText(words.changed);
  await screen.findByText("Changed elsewhere");
  assert.equal(reads, 2);
  assert.deepEqual(net.seen.find((r) => r.method === "PATCH")?.body, {
    readers: [{ kind: "workspace" }],
    version: 4,
  });
  // The form shows what is saved now, so a retry starts from it.
  assert.equal(
    (screen.getByRole("radio", { name: words.form.onlyMe }) as HTMLInputElement).checked,
    true,
  );
});

test("the owner hands a corpus off, and then sees it read-only", async () => {
  net = stubFetch({
    "GET /api/corpora/hr-policies": () => corpus(),
    "PATCH /api/corpora/hr-policies": (body) =>
      corpus({ owner: (body as { owner: string }).owner, mine: false, version: 4 }),
    ...directory,
  });
  render(
    <CorpusPage
      name="hr-policies"
      tab="access"
      sources={[]}
      canWrite
      onTab={() => {}}
      onBack={() => {}}
      onOpenSource={() => {}}
    />,
  );
  const owner = await screen.findByRole("region", { name: words.access.owner });
  fireEvent.change(await within(owner).findByRole("combobox", { name: words.access.handOffTo }), {
    target: { value: "bob" },
  });
  fireEvent.click(within(owner).getByRole("button", { name: words.access.handOff }));
  await within(owner).findByText("Bob");
  assert.deepEqual(net.seen.find((r) => r.method === "PATCH")?.body, { owner: "bob", version: 3 });
  assert.equal(screen.queryByRole("button", { name: words.form.save }), null);
});

test("an old /knowledge/{source} link that names no corpus opens the source", async () => {
  let missing = "";
  net = stubFetch({
    "GET /api/corpora/handbook": () =>
      new Response(JSON.stringify({ error: "not found" }), { status: 404 }),
  });
  render(
    <CorpusPage
      name="handbook"
      tab="sources"
      sources={[source("handbook")]}
      canWrite
      onTab={() => {}}
      onBack={() => {}}
      onOpenSource={() => {}}
      onMissing={(name) => (missing = name)}
    />,
  );
  await waitFor(() => assert.equal(missing, "handbook"));
});

const page = (over: Partial<Parameters<typeof CorpusPage>[0]> = {}) => (
  <>
    <Toaster />
    <CorpusPage
      name="hr-policies"
      tab="sources"
      sources={[]}
      canWrite
      onTab={() => {}}
      onBack={() => {}}
      onOpenSource={() => {}}
      {...over}
    />
  </>
);
const warehouse = (
  name: string,
  system: string,
  login: "connected" | "missing" | "unavailable",
) => ({
  name,
  system,
  entries: 4,
  generation: 1,
  capabilities: ["catalog", "live"],
  mine: false,
  login,
});

test("the owner sees a member they can no longer see by name, and removes it", async () => {
  net = stubFetch({
    "GET /api/corpora/hr-policies": () =>
      corpus({ members: [{ kind: "source", name: "old-wiki", items: 0, status: "hidden" }] }),
    "DELETE /api/corpora/hr-policies/members/source/old-wiki": () => corpus({ members: [] }),
  });
  render(page());
  const card = await screen.findByRole("listitem", { name: "old-wiki" });
  assert.ok(within(card).getByText(words.hiddenToOwner));
  assert.equal(within(card).queryByRole("link"), null);
  await openMenu("old-wiki");
  await choose(words.remove);
  await screen.findByText(words.removed("old-wiki"));
  await screen.findByText(words.noSources);
});

test("Search runs at its address and shows hits, where each came from, and members that failed", async () => {
  const searched: string[] = [];
  net = stubFetch({
    "GET /api/corpora/hr-policies": () => corpus(),
    "GET /api/corpora/hr-policies/search": (_body, query) => {
      searched.push(query.get("q") ?? "");
      return query.get("q") === "nothing"
        ? { hits: [], warnings: [] }
        : {
            hits: [
              {
                corpus: "hr-policies",
                member: { kind: "source", name: "handbook" },
                holds: "files",
                title: "Parental leave",
                uri: "https://wiki.example.com/leave",
                text: "Twenty weeks of paid leave.",
                score: 0.03,
                also: [{ kind: "source", name: "drive-hr" }],
              },
              {
                corpus: "hr-policies",
                member: { kind: "catalog", name: "people-warehouse" },
                holds: "records",
                title: "HR.PUBLIC.LEAVE",
                uri: "javascript:alert(1)",
                text: "Leave taken per person",
                score: 0.02,
              },
            ],
            warnings: [
              {
                corpus: "hr-policies",
                member: { kind: "source", name: "drive-hr" },
                cause: "auth_expired",
              },
            ],
          };
    },
  });
  const typed: string[] = [];
  render(page({ tab: "search", q: "leave", onSearch: (q) => typed.push(q) }));
  const leave = await screen.findByRole("listitem", { name: "Parental leave" });
  assert.equal(
    within(leave).getByRole("link", { name: "Parental leave" }).getAttribute("href"),
    "https://wiki.example.com/leave",
  );
  assert.ok(within(leave).getByText("handbook"));
  assert.ok(within(leave).getByText("drive-hr"));
  assert.ok(within(leave).getByText(words.holds.files));
  assert.ok(within(leave).getByText("Twenty weeks of paid leave."));
  // A link that is not to the web is not a link.
  const table = screen.getByRole("listitem", { name: "HR.PUBLIC.LEAVE" });
  assert.equal(within(table).queryByRole("link"), null);
  assert.ok(within(table).getByText(words.holds.records));
  assert.ok(
    screen.getByText(
      words.search.warning(
        "drive-hr",
        defaultCopy.sourceLifecycle.cause.auth_expired.toLowerCase(),
      ),
    ),
  );
  const box = screen.getByRole("searchbox", { name: words.search.label });
  fireEvent.change(box, { target: { value: "nothing" } });
  fireEvent.click(screen.getByRole("button", { name: words.search.action }));
  await screen.findByText(words.search.none);
  assert.deepEqual(searched, ["leave", "nothing"]);
  assert.deepEqual(typed, ["nothing"]);
});

test("a search that fails says so", async () => {
  net = stubFetch({
    "GET /api/corpora/hr-policies": () => corpus(),
    "GET /api/corpora/hr-policies/search": () =>
      new Response(JSON.stringify({ error: "boom" }), { status: 503 }),
  });
  render(page({ tab: "search", q: "leave" }));
  await screen.findByText(words.search.failed);
});

test("Snowflake is set up inside Add Source, held by the corpus, then asks for the owner's login", async () => {
  let current = corpus({ members: [] });
  net = stubFetch({
    "GET /api/corpora/hr-policies": () => current,
    "GET /api/catalog": () => ({ connectors: [], entries: [] }),
    "POST /api/catalog/connectors/snowflake": (body) => ({
      name: (body as { name: string }).name,
      system: "snowflake",
      entries: 0,
      generation: 0,
    }),
    "POST /api/corpora/hr-policies/members": (body) => {
      current = corpus({
        members: [
          {
            ...(body as { kind: "catalog"; name: string }),
            holds: "records",
            provider: "snowflake",
            items: 0,
            status: "syncing",
          },
        ],
      });
      return current;
    },
    "PUT /api/catalog/connectors/hr-policies-snowflake/login": () => undefined,
  });
  render(page());
  await screen.findByText(words.noSources);
  fireEvent.click(screen.getByRole("button", { name: words.addSource }));
  fireEvent.click(await screen.findByRole("button", { name: "Snowflake" }));
  const form = await screen.findByRole("form", { name: "Snowflake" });
  const field = (label: string) =>
    within(form).getByRole("textbox", { name: new RegExp(`^${label.replace(/[()]/g, "\\$&")}`) });
  assert.equal((field(words.form.name) as HTMLInputElement).value, "hr-policies-snowflake");
  fireEvent.change(field(words.snowflake.account), { target: { value: "acme-prod" } });
  fireEvent.change(field(words.snowflake.database), { target: { value: "HR" } });
  fireEvent.change(field(words.snowflake.warehouse), { target: { value: "READER_WH" } });
  fireEvent.click(within(form).getByRole("radio", { name: words.snowflake.pat }));
  fireEvent.click(within(form).getByRole("button", { name: words.snowflake.create }));
  const login = await screen.findByRole("form", {
    name: words.login.title("hr-policies-snowflake"),
  });
  assert.deepEqual(
    net.seen.filter((r) => r.method === "POST").map((r) => [r.path, r.body]),
    [
      [
        "/api/catalog/connectors/snowflake",
        {
          name: "hr-policies-snowflake",
          account: "acme-prod",
          database: "HR",
          warehouse: "READER_WH",
          token_type: "PROGRAMMATIC_ACCESS_TOKEN",
        },
      ],
      ["/api/corpora/hr-policies/members", { kind: "catalog", name: "hr-policies-snowflake" }],
    ],
  );
  fireEvent.change(
    within(login).getByRole("textbox", { name: new RegExp(`^${words.login.refLabel}`) }),
    {
      target: { value: "op://hr/snowflake/token" },
    },
  );
  fireEvent.click(within(login).getByRole("button", { name: words.login.save }));
  await screen.findByText(words.login.saved);
  assert.deepEqual(net.seen.find((r) => r.method === "PUT")?.body, {
    ref: "op://hr/snowflake/token",
  });
});

test("a warehouse member asks for the reader's own login, shows a refusal in the server's words, and forgets it", async () => {
  let login: "missing" | "connected" = "missing";
  net = stubFetch({
    "GET /api/corpora/hr-policies": () =>
      corpus({
        mine: false,
        members: [
          {
            kind: "catalog",
            name: "sales-wh",
            holds: "records",
            provider: "snowflake",
            items: 9,
            status: "ready",
          },
        ],
      }),
    "GET /api/catalog": () => ({
      connectors: [warehouse("sales-wh", "snowflake", login)],
      entries: [],
    }),
    "PUT /api/catalog/connectors/sales-wh/login": (body) =>
      (body as { value?: string }).value
        ? new Response(JSON.stringify({ error: "this workspace keeps references only" }), {
            status: 400,
          })
        : ((login = "connected"), undefined),
    "DELETE /api/catalog/connectors/sales-wh/login": () => {
      login = "missing";
      return undefined;
    },
  });
  render(page());
  const card = await screen.findByRole("listitem", { name: "sales-wh" });
  fireEvent.click(await within(card).findByRole("button", { name: words.login.connect }));
  const form = await screen.findByRole("form", { name: words.login.title("sales-wh") });
  fireEvent.click(within(form).getByRole("radio", { name: words.login.value }));
  fireEvent.change(within(form).getByLabelText(words.login.valueLabel), {
    target: { value: "secret-token" },
  });
  fireEvent.click(within(form).getByRole("button", { name: words.login.save }));
  await within(form).findByText("this workspace keeps references only");
  fireEvent.click(within(form).getByRole("radio", { name: words.login.ref }));
  fireEvent.change(
    within(form).getByRole("textbox", { name: new RegExp(`^${words.login.refLabel}`) }),
    {
      target: { value: "op://me/snowflake" },
    },
  );
  fireEvent.click(within(form).getByRole("button", { name: words.login.save }));
  await within(card).findByText(words.login.connected);
  // A reader who does not own the corpus manages only their own login.
  await openMenu("sales-wh");
  assert.equal(screen.queryByRole("menuitem", { name: words.remove }), null);
  await choose(words.login.forget);
  await screen.findByText(words.login.forgotten);
  await within(card).findByRole("button", { name: words.login.connect });
  assert.ok(
    net.seen.some(
      (r) => r.method === "DELETE" && r.path === "/api/catalog/connectors/sales-wh/login",
    ),
  );
});

test("a BigQuery member without the reader's login starts their BigQuery connection", async () => {
  net = stubFetch({
    "GET /api/corpora/hr-policies": () => corpus(),
    "GET /api/catalog": () => ({
      connectors: [warehouse("people-warehouse", "bigquery", "missing")],
      entries: [],
    }),
    "POST /api/connections/bigquery/authorize": () =>
      new Response(JSON.stringify({ error: "Google sign-in is off" }), { status: 403 }),
  });
  render(page());
  const card = await screen.findByRole("listitem", { name: "people-warehouse" });
  fireEvent.click(await within(card).findByRole("button", { name: words.login.connect }));
  await screen.findByText("Google sign-in is off");
  assert.deepEqual(net.seen.find((r) => r.method === "POST")?.body, { scope: "person" });
  // An OAuth login is the person's connection: there is nothing to forget here.
  assert.equal(screen.queryByRole("dialog"), null);
});

test("the owner grants the corpus to an agent; a reader sees the agents read-only", async () => {
  net = stubFetch({
    "GET /api/corpora/hr-policies": () => corpus(),
    "PATCH /api/corpora/hr-policies": (body) =>
      corpus({ agents: (body as { agents: string[] }).agents, version: 4 }),
    ...directory,
  });
  const view = render(page({ tab: "access", agents: ["lead", "hr-helper"] }));
  const agents = await screen.findByRole("region", { name: words.agents.title });
  assert.ok(within(agents).getByText(words.agents.none));
  assert.ok(within(agents).getByText(words.agents.hint));
  fireEvent.click(
    within(agents).getByRole("button", { name: `${words.agents.add}: ${words.agents.title}` }),
  );
  fireEvent.click(await screen.findByRole("button", { name: "hr-helper" }));
  await screen.findByText(words.form.saved);
  assert.deepEqual(net.seen.find((r) => r.method === "PATCH")?.body, {
    agents: ["hr-helper"],
    version: 3,
  });
  await within(screen.getByRole("region", { name: words.agents.title })).findByRole("button", {
    name: "Remove hr-helper",
  });
  view.unmount();
  net.restore();
  net = stubFetch({
    "GET /api/corpora/hr-policies": () => corpus({ mine: false, agents: ["hr-helper"] }),
    ...directory,
  });
  render(page({ tab: "access", agents: ["lead", "hr-helper"] }));
  const readOnly = await screen.findByRole("region", { name: words.agents.title });
  await within(readOnly).findByText("hr-helper");
  assert.equal(within(readOnly).queryByRole("button"), null);
});

const graphView = {
  nodes: [
    {
      id: "n1",
      key: "doc:leave",
      type: "document",
      name: "Leave policy",
      uri: "https://wiki.example.com/leave",
      member: "source/handbook",
      owner: "me",
      owner_state: "suggested",
      staleness: { score: 0.9, stale: true, reasons: ["old", "superseded"] },
    },
    {
      id: "n2",
      key: "doc:faq",
      type: "document",
      name: "Benefits FAQ",
      member: "source/handbook",
      owner: "bob",
      // The graph names its owners; the directory is never asked.
      owner_name: "Bob Builder",
      owner_state: "confirmed",
      staleness: { score: 0.1, stale: false },
    },
    {
      id: "n3",
      key: "table:employees",
      type: "Employee",
      name: "HR.PUBLIC.EMPLOYEES",
      member: "catalog/people-warehouse",
      owner_state: "unowned",
    },
    { id: "p1", key: "person:ada", type: "person", name: "Ada", owner_state: "" },
  ],
  edges: [],
  health: {
    nodes: 5,
    edges: 3,
    by_type: { document: 2, Employee: 1, person: 1, system: 1 },
    unowned: 1,
    stale: 1,
    built_at: at,
  },
};

test("the Graph tab shows its health and each document's owner and staleness, and owners decide", async () => {
  net = stubFetch({
    "GET /api/corpora/hr-policies": () => corpus(),
    "GET /api/corpora/hr-policies/graph": () => graphView,
    "POST /api/corpora/hr-policies/graph/build": () => graphView,
    "PUT /api/graph/nodes/n1/owner": () => ({ ...graphView.nodes[0], owner_state: "confirmed" }),
    "PUT /api/graph/nodes/n3/owner": (body) => ({
      ...graphView.nodes[2],
      owner: (body as { to: string }).to,
      owner_name: "Robert",
      owner_state: "confirmed",
    }),
    ...directory,
  });
  render(page({ tab: "graph", viewer: "me" }));
  await screen.findByText(/^3 documents \| 1 system \| 1 person \| 1 unowned \| 1 stale \| built /);
  const leave = await screen.findByRole("listitem", { name: "Leave policy" });
  assert.equal(
    within(leave).getByRole("link", { name: "Leave policy" }).getAttribute("href"),
    "https://wiki.example.com/leave",
  );
  assert.ok(within(leave).getByText(words.graph.you));
  assert.ok(within(leave).getByText(words.graph.states.suggested));
  assert.ok(within(leave).getByText(words.graph.staleLabel));
  assert.ok(within(leave).getByText("old, superseded"));
  const faq = screen.getByRole("listitem", { name: "Benefits FAQ" });
  assert.ok(within(faq).getByText("Bob Builder"));
  assert.ok(within(faq).getByText(words.graph.states.confirmed));
  assert.equal(within(faq).queryByText(words.graph.staleLabel), null);
  // Someone else's document is theirs to decide.
  assert.equal(within(faq).queryByRole("button"), null);
  const table = screen.getByRole("listitem", { name: "HR.PUBLIC.EMPLOYEES" });
  assert.ok(within(table).getByText("Employee"));
  assert.ok(within(table).getByText(words.graph.noOwner));
  // People are part of the graph, not rows of it.
  assert.equal(screen.queryByRole("listitem", { name: "Ada" }), null);

  await openMenu("Leave policy");
  assert.ok(await screen.findByRole("menuitem", { name: words.graph.decline }));
  await choose(words.graph.confirm);
  await screen.findByText(words.graph.confirmed("Leave policy"));
  await within(screen.getByRole("listitem", { name: "Leave policy" })).findByText(
    words.graph.states.confirmed,
  );

  // The corpus's owner hands on a table nobody owns.
  await openMenu("HR.PUBLIC.EMPLOYEES");
  assert.equal(screen.queryByRole("menuitem", { name: words.graph.decline }), null);
  await choose(words.graph.handOff);
  const form = await screen.findByRole("form", {
    name: words.graph.handOffTitle("HR.PUBLIC.EMPLOYEES"),
  });
  fireEvent.change(within(form).getByRole("combobox", { name: words.graph.to }), {
    target: { value: "bob" },
  });
  fireEvent.click(within(form).getByRole("button", { name: words.graph.handOff }));
  await screen.findByText(words.graph.handedOff("HR.PUBLIC.EMPLOYEES", "Robert"));
  assert.deepEqual(
    net.seen.filter((r) => r.method === "PUT").map((r) => [r.path, r.body]),
    [
      ["/api/graph/nodes/n1/owner", { action: "confirm" }],
      ["/api/graph/nodes/n3/owner", { action: "hand_off", to: "bob" }],
    ],
  );
  fireEvent.click(screen.getByRole("button", { name: words.graph.rebuild }));
  await screen.findByText(words.graph.rebuilt);
});

test("a reader who owns no document and not the corpus sees the graph without actions", async () => {
  net = stubFetch({
    "GET /api/corpora/hr-policies": () => corpus({ mine: false }),
    "GET /api/corpora/hr-policies/graph": () => graphView,
    ...directory,
  });
  render(page({ tab: "graph", viewer: "carol" }));
  await screen.findByRole("listitem", { name: "Leave policy" });
  assert.equal(screen.queryByRole("button", { name: words.graph.rebuild }), null);
  for (const name of ["HR.PUBLIC.EMPLOYEES", "Leave policy"])
    assert.equal(
      within(screen.getByRole("listitem", { name })).queryByRole("button", { name: more }),
      null,
    );
});

const ontology = {
  $defs: {
    Policy: { type: "object", properties: { jurisdiction: { type: "string" } } },
    Employee: { type: "object", properties: { start_date: { type: "string" } } },
  },
  "x-relations": [{ name: "governs", from: "Policy", to: "Employee", cardinality: "many" }],
};
const proposal = {
  $defs: {
    Order: { type: "object", properties: { id: { type: "string" }, total: { type: "number" } } },
    Customer: { type: "object", properties: { id: { type: "string" } } },
  },
};

test("the Types tab shows declared types and accepts the selected proposed ones", async () => {
  let current = corpus({ ontology, proposed_ontology: proposal });
  net = stubFetch({
    "GET /api/corpora/hr-policies": () => current,
    "POST /api/corpora/hr-policies/ontology/accept": () => {
      current = corpus({ ontology, version: 4 });
      return current;
    },
  });
  render(page({ tab: "types" }));
  const policy = await screen.findByRole("listitem", { name: "Policy" });
  assert.ok(within(policy).getByText(`${words.types.properties}: jurisdiction`));
  assert.ok(
    within(policy).getByText(
      `${words.types.relations}: ${words.types.relation("governs", "Employee", true)}`,
    ),
  );
  const proposed = screen.getByRole("region", { name: words.types.proposed });
  fireEvent.click(within(proposed).getByRole("checkbox", { name: "Customer" }));
  fireEvent.click(within(proposed).getByRole("button", { name: words.types.acceptSelected }));
  await screen.findByText(words.types.accepted);
  assert.deepEqual(net.seen.find((r) => r.method === "POST")?.body, { types: ["Order"] });
  await waitFor(() =>
    assert.equal(screen.queryByRole("region", { name: words.types.proposed }), null),
  );
});

test("the owner proposes types from warehouses, discards a proposal, and edits types as JSON", async () => {
  let current = corpus({ proposed_ontology: proposal });
  net = stubFetch({
    "GET /api/corpora/hr-policies": () => current,
    "POST /api/corpora/hr-policies/ontology/infer": () =>
      new Response(JSON.stringify({ error: "hr-policies holds no warehouse to infer from" }), {
        status: 400,
      }),
    "POST /api/corpora/hr-policies/ontology/discard": () => {
      current = corpus({ version: 4 });
      return current;
    },
    "PATCH /api/corpora/hr-policies": () =>
      new Response(JSON.stringify({ error: "ontology: a relation to nothing" }), { status: 400 }),
  });
  render(page({ tab: "types" }));
  fireEvent.click(await screen.findByRole("button", { name: words.types.infer }));
  await screen.findByText("hr-policies holds no warehouse to infer from");
  fireEvent.click(screen.getByRole("button", { name: words.types.discard }));
  await screen.findByText(words.types.discarded);
  await waitFor(() =>
    assert.equal(screen.queryByRole("region", { name: words.types.proposed }), null),
  );
  const form = screen.getByRole("form", { name: words.types.editJson });
  const box = within(form).getByRole("textbox", { name: words.types.json });
  fireEvent.change(box, { target: { value: "{ not json" } });
  fireEvent.click(within(form).getByRole("button", { name: words.types.save }));
  await screen.findByText(words.types.badJson);
  assert.ok(!net.seen.some((r) => r.method === "PATCH"));
  fireEvent.change(
    within(screen.getByRole("form", { name: words.types.editJson })).getByRole("textbox", {
      name: words.types.json,
    }),
    {
      target: {
        value: JSON.stringify({
          "x-relations": [{ name: "governs", from: "Policy", to: "Employee" }],
        }),
      },
    },
  );
  fireEvent.click(
    within(screen.getByRole("form", { name: words.types.editJson })).getByRole("button", {
      name: words.types.save,
    }),
  );
  await screen.findByText("ontology: a relation to nothing");
  assert.deepEqual(net.seen.find((r) => r.method === "PATCH")?.body, {
    ontology: { "x-relations": [{ name: "governs", from: "Policy", to: "Employee" }] },
    version: 4,
  });
});

test("New Corpus asks for its label, and for a purpose when it is restricted", async () => {
  net = stubFetch({
    "GET /api/corpora": () => ({ corpora: [] }),
    "POST /api/corpora": (body) => corpus({ ...(body as Partial<Corpus>), members: [] }),
    ...directory,
  });
  render(<KnowledgePage sources={[]} canWrite onOpen={() => {}} />);
  fireEvent.click(await screen.findByRole("button", { name: words.newCorpus }));
  const form = await screen.findByRole("form", { name: words.form.newTitle });
  const label = within(form).getByRole("combobox", {
    name: new RegExp(`^${words.labels.title}`),
  }) as HTMLSelectElement;
  assert.equal(label.value, "internal");
  assert.ok(within(form).getByText(words.labels.meaning));
  assert.equal(
    within(form).queryByRole("textbox", { name: new RegExp(`^${words.labels.purpose}`) }),
    null,
  );
  fireEvent.change(
    within(form).getByRole("textbox", { name: new RegExp(`^${words.form.title}`) }),
    {
      target: { value: "Pay" },
    },
  );
  fireEvent.change(label, { target: { value: "restricted" } });
  fireEvent.change(
    within(form).getByRole("textbox", { name: new RegExp(`^${words.labels.purpose}`) }),
    {
      target: { value: "Payroll questions" },
    },
  );
  fireEvent.click(within(form).getByRole("button", { name: words.form.create }));
  await waitFor(() =>
    assert.deepEqual(net?.seen.find((r) => r.method === "POST")?.body, {
      name: "pay",
      title: "Pay",
      description: "",
      readers: [],
      label: "restricted",
      purpose: "Payroll questions",
    }),
  );
});

test("a card's pill is its label; Edit searches the graph too, and restricting it may need an admin", async () => {
  net = stubFetch({
    "GET /api/corpora": () => ({
      corpora: [corpus({ label: "confidential", agents: ["hr-helper"] })],
    }),
    "PATCH /api/corpora/hr-policies": () =>
      new Response(JSON.stringify({ error: "forbidden" }), { status: 403 }),
    ...directory,
  });
  render(<KnowledgePage sources={[]} canWrite onOpen={() => {}} />);
  const card = await screen.findByRole("listitem", { name: "HR policies" });
  assert.ok(within(card).getByText(words.labels.confidential));
  assert.equal(within(card).queryByText(words.isolated), null);
  await openMenu("HR policies");
  await choose(words.edit);
  const form = await screen.findByRole("form", { name: words.form.editTitle("HR policies") });
  fireEvent.click(
    within(form).getByRole("checkbox", { name: new RegExp(`^${words.labels.graphSearch}`) }),
  );
  fireEvent.change(
    within(form).getByRole("combobox", { name: new RegExp(`^${words.labels.title}`) }),
    {
      target: { value: "restricted" },
    },
  );
  fireEvent.change(
    within(form).getByRole("textbox", { name: new RegExp(`^${words.labels.purpose}`) }),
    {
      target: { value: "Pay questions" },
    },
  );
  fireEvent.click(within(form).getByRole("button", { name: words.form.save }));
  await within(form).findByText(words.labels.adminNeeded);
  const sent = net.seen.find((r) => r.method === "PATCH")?.body as Record<string, unknown>;
  assert.equal(sent.label, "restricted");
  assert.equal(sent.purpose, "Pay questions");
  assert.equal(sent.graph_search, true);
});

test("Isolated or Shared is on the Access tab, where the owner changes it", async () => {
  net = stubFetch({
    "GET /api/corpora/hr-policies": () => corpus(),
    "PATCH /api/corpora/hr-policies": () => corpus({ links: "shared", version: 4 }),
    ...directory,
  });
  render(page({ tab: "access" }));
  const links = await screen.findByRole("region", { name: words.links.title });
  assert.ok(within(links).getByText(words.isolated));
  fireEvent.click(
    within(links).getByRole("checkbox", { name: new RegExp(`^${words.links.shared}`) }),
  );
  // The tab is drawn again from the corpus the change returned.
  await waitFor(() =>
    assert.ok(
      within(screen.getByRole("region", { name: words.links.title })).getByText(words.shared),
    ),
  );
  assert.deepEqual(net.seen.find((r) => r.method === "PATCH")?.body, {
    links: "shared",
    version: 3,
  });
});

test("a knowledge alert reads as one and opens where its decision is made, with or without a session", async () => {
  const { AlertsPage } = await import("./SessionCenter.tsx");
  const knowledgeAlert = (id: string, action: string, session?: string) => ({
    id,
    rule: "knowledge",
    kind: "knowledge",
    severity: "warning",
    priority: "normal",
    state: "open",
    title: `HR policies needs you (${id})`,
    action,
    at,
    ...(session ? { session } : {}),
  });
  net = stubFetch({
    "GET /api/alerts": () => ({
      alerts: [
        knowledgeAlert("a1", "/knowledge/hr-policies/graph"),
        knowledgeAlert("a2", "/knowledge/hr-policies/types", "s_builder"),
        knowledgeAlert("a3", "/knowledge/hr-policies"),
      ],
    }),
  });
  const went: unknown[] = [];
  render(
    <AlertsPage
      route={{ page: "alerts" }}
      go={(to) => went.push(to)}
      canWrite={false}
      onChanged={() => {}}
      tick={0}
    />,
  );
  // With no session, the alert itself opens the page it is about.
  const alone = await screen.findByRole("link", { name: /\(a1\)/ });
  assert.equal(alone.getAttribute("href"), "/knowledge/hr-policies/graph");
  assert.equal(
    screen.getByRole("link", { name: /\(a2\)/ }).getAttribute("href"),
    "/messages/s_builder",
  );
  assert.ok(screen.getAllByText("Knowledge").length >= 3);
  assert.equal(screen.queryByText("/knowledge/hr-policies/graph"), null);
  fireEvent.click(screen.getByRole("button", { name: words.openGraph }));
  fireEvent.click(screen.getByRole("button", { name: words.openTypes }));
  fireEvent.click(screen.getByRole("button", { name: words.openCorpus }));
  assert.deepEqual(went, [
    { page: "corpus", name: "hr-policies", tab: "graph" },
    { page: "corpus", name: "hr-policies", tab: "types" },
    { page: "corpus", name: "hr-policies", tab: "sources" },
  ]);
});

test("the whole card opens its corpus, with no Open button, and its menu stays apart", async () => {
  const opened: string[] = [];
  net = stubFetch({ "GET /api/corpora": () => ({ corpora: [corpus()] }) });
  render(<KnowledgePage sources={[]} canWrite onOpen={(name) => opened.push(name)} />);
  const card = await screen.findByRole("listitem", { name: "HR policies" });
  assert.equal(within(card).queryByRole("button", { name: /^Open/ }), null);
  assert.equal(within(card).getAllByRole("link").length, 1);
  const link = within(card).getByRole("link", { name: "HR policies" });
  link.focus();
  assert.equal(document.activeElement, link);
  fireEvent.click(link);
  assert.deepEqual(opened, ["hr-policies"]);
  // A modified click is the browser's: a new tab, not a move in place.
  fireEvent.click(link, { metaKey: true });
  assert.deepEqual(opened, ["hr-policies"]);
  await openMenu("HR policies");
  await choose(words.edit);
  await screen.findByRole("form", { name: words.form.editTitle("HR policies") });
  assert.deepEqual(opened, ["hr-policies"]);
});

test("a corpus made before labels reads as internal, never an empty pill", async () => {
  net = stubFetch({
    "GET /api/corpora": () => ({ corpora: [corpus({ label: "" as Corpus["label"] })] }),
    ...directory,
  });
  render(<KnowledgePage sources={[]} canWrite onOpen={() => {}} />);
  const card = await screen.findByRole("listitem", { name: "HR policies" });
  assert.ok(within(card).getByText(words.labels.internal));
  await openMenu("HR policies");
  await choose(words.edit);
  const form = await screen.findByRole("form", { name: words.form.editTitle("HR policies") });
  assert.equal(
    (
      within(form).getByRole("combobox", {
        name: new RegExp(`^${words.labels.title}`),
      }) as HTMLSelectElement
    ).value,
    "internal",
  );
});

test("corpus cards, source cards and search hits are the console's shared cards and references", async () => {
  net = stubFetch({
    "GET /api/corpora": () => ({ corpora: [corpus()] }),
    "GET /api/corpora/hr-policies": () => corpus(),
    "GET /api/corpora/hr-policies/search": () => ({
      hits: [
        {
          corpus: "hr-policies",
          member: { kind: "source", name: "handbook" },
          holds: "files",
          title: "Parental leave",
          text: "Twenty weeks of paid leave.",
          score: 0.03,
          also: [{ kind: "source", name: "drive-hr" }],
        },
      ],
      warnings: [],
    }),
  });
  const shared = (item: HTMLElement) => {
    // One ItemCard: an article whose title link covers it, with references in its facts.
    const card = item.querySelector("article");
    assert.ok(card, "an ItemCard");
    return card as HTMLElement;
  };
  const list = render(<KnowledgePage sources={[]} canWrite onOpen={() => {}} />);
  const corpusCard = shared(await screen.findByRole("listitem", { name: "HR policies" }));
  assert.deepEqual(
    [...corpusCard.querySelectorAll("[data-reference]")].map((r) => [
      r.getAttribute("data-reference"),
      r.textContent,
    ]),
    [
      ["file", "Uploads· 12"],
      ["provider", "Google Drive· 3"],
      ["provider", "BigQuery· 4"],
    ],
  );
  // A provider's chip carries its own logo.
  for (const chip of corpusCard.querySelectorAll('[data-reference="provider"]'))
    assert.ok(chip.querySelector("svg"), chip.textContent ?? "");
  list.unmount();
  const sources = render(page());
  const member = shared(await screen.findByRole("listitem", { name: "handbook" }));
  assert.equal(member.querySelector("[data-reference]")?.textContent, "Files· 12");
  sources.unmount();
  render(page({ tab: "search", q: "leave" }));
  const hit = shared(await screen.findByRole("listitem", { name: "Parental leave" }));
  assert.deepEqual(
    [...hit.querySelectorAll("[data-reference]")].map((r) => r.textContent),
    ["handbook", "drive-hr"],
  );
});
