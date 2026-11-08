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

async function openMenu(name: string) {
  const more = await screen.findByRole("button", { name: words.more(name) });
  more.focus();
  fireEvent.keyDown(more, { key: "ArrowDown" });
}
async function choose(item: string) {
  const entry = await screen.findByRole("menuitem", { name: item });
  entry.focus();
  fireEvent.keyDown(entry, { key: "Enter" });
}

test("Knowledge lists corpora as cards: providers, items, one pill, Open and an owner's menu", async () => {
  net = stubFetch({
    "GET /api/corpora": () => ({
      corpora: [corpus(), corpus({ name: "finance", mine: false, links: "shared", members: [] })],
    }),
  });
  render(<KnowledgePage sources={[]} canWrite onOpen={() => {}} />);
  const hr = await screen.findByRole("listitem", { name: "hr-policies" });
  assert.ok(within(hr).getByText("Uploads"));
  assert.ok(within(hr).getByText("Google Drive"));
  assert.ok(within(hr).getByText("BigQuery"));
  assert.ok(within(hr).getByText(words.items(19)));
  assert.ok(within(hr).getByText(words.isolated));
  assert.equal(
    within(hr).getByRole("link", { name: words.open }).getAttribute("href"),
    "/knowledge/hr-policies",
  );
  assert.ok(within(hr).getByRole("button", { name: words.more("hr-policies") }));
  const finance = screen.getByRole("listitem", { name: "finance" });
  assert.ok(within(finance).getByText(words.shared));
  // Only the owner changes a corpus.
  assert.equal(within(finance).queryByRole("button", { name: words.more("finance") }), null);
  // With corpora shown, New Corpus is the page's one action.
  assert.equal(screen.getAllByRole("button", { name: words.newCorpus }).length, 1);
});

test("an empty Knowledge is a title and New Corpus", async () => {
  net = stubFetch({ "GET /api/corpora": () => ({ corpora: [] }) });
  render(<KnowledgePage sources={[]} canWrite onOpen={() => {}} />);
  await screen.findByText(words.empty);
  assert.equal(screen.getAllByRole("button", { name: words.newCorpus }).length, 1);
});

test("New Corpus names it as the API takes it, sets who reads it, and opens it", async () => {
  let opened = "";
  net = stubFetch({
    "GET /api/corpora": () => ({ corpora: [] }),
    "POST /api/corpora": (body) => corpus({ ...(body as Partial<Corpus>), members: [] }),
    ...directory,
  });
  render(<KnowledgePage sources={[]} canWrite onOpen={(name) => (opened = name)} />);
  fireEvent.click(await screen.findByRole("button", { name: words.newCorpus }));
  const form = await screen.findByRole("form", { name: words.form.newTitle });
  const name = within(form).getByRole("textbox", {
    name: new RegExp(`^${words.form.name}`),
  }) as HTMLInputElement;
  fireEvent.change(name, { target: { value: "HR policies" } });
  assert.equal(name.value, "hr-policies");
  fireEvent.change(within(form).getByRole("textbox", { name: words.form.description }), {
    target: { value: "Leave and benefits" },
  });
  fireEvent.click(within(form).getByRole("radio", { name: words.form.workspace }));
  fireEvent.click(within(form).getByRole("button", { name: words.form.create }));
  await waitFor(() => assert.equal(opened, "hr-policies"));
  assert.deepEqual(net.seen.find((r) => r.method === "POST")?.body, {
    name: "hr-policies",
    description: "Leave and benefits",
    readers: [{ kind: "workspace" }],
  });
});

test("Delete asks first, then deletes at the version read", async () => {
  net = stubFetch({
    "GET /api/corpora": () => ({ corpora: [corpus()] }),
    "DELETE /api/corpora/hr-policies": () => undefined,
  });
  render(<KnowledgePage sources={[]} canWrite onOpen={() => {}} />);
  await openMenu("hr-policies");
  await choose(words.delete);
  const ask = await screen.findByRole("group", { name: words.deleteAsk("hr-policies") });
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

test("a corpus page has Sources and Access tabs, members with what they hold and their state", async () => {
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
  assert.ok(within(handbook).getByText("Files | 12 items"));
  assert.ok(within(handbook).getByText(words.status.ready));
  assert.equal(
    within(handbook)
      .getByRole("link", { name: `${words.open} handbook` })
      .getAttribute("href"),
    "/knowledge/hr-policies/sources/handbook",
  );
  const warehouse = screen.getByRole("listitem", { name: "people-warehouse" });
  assert.ok(within(warehouse).getByText("Records | 4 items"));
  assert.ok(within(warehouse).getByText(words.status.failed));
  assert.ok(screen.getByText("What HR has written down"));
  fireEvent.click(screen.getByRole("tab", { name: words.tabs.access }));
  assert.deepEqual(tabs, ["access"]);
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
  assert.ok(
    within(sheet)
      .getByRole("button", { name: /Snowflake/ })
      .hasAttribute("disabled"),
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

test("a member the reader cannot see is listed without its name, and nobody can act on it", async () => {
  net = stubFetch({
    "GET /api/corpora/hr-policies": () =>
      corpus({ members: [{ kind: "source", name: "", items: 0, status: "hidden" }] }),
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
  assert.equal(screen.queryByRole("button", { name: words.more("handbook") }), null);
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
