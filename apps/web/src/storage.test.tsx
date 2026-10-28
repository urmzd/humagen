import assert from "node:assert/strict";
import { test } from "node:test";
import type { ReactElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { DeleteVersion, StorageMore, StorageUsed, StoredDetail } from "./Storage.tsx";
import { memoryStorage, sampleFiles, sampleUse } from "./storage.fixtures.ts";
import {
  appendPage,
  deleteProblem,
  OVER_LIMIT,
  overLimit,
  PAGE,
  type Stored,
  showsInline,
} from "./storage.ts";

// These render the storage page's parts to markup and drive the in-memory
// transport the way the page does. The clicks are in storage.dom.test.tsx.

const html = (el: ReactElement) => renderToStaticMarkup(el);
const text = (s: string) => s.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ");
const noop = () => {};
const byId = (id: string) => sampleFiles().find((f) => f.id === id) as Stored;
const detail = (file: Stored, canDelete = true) =>
  html(
    <StoredDetail
      file={file}
      versions={[file]}
      canDelete={canDelete}
      onClose={noop}
      onDelete={noop}
      onOpenRun={noop}
      onOpenThread={noop}
    />,
  );

test("an image is previewed inline from the inline content route", () => {
  const png = byId("a3");
  const out = detail(png);
  assert.match(out, /<img[^>]+src="\/api\/artifacts\/a3\/content\?inline=1"/);
  assert.match(out, /alt="incident-timeline\.png"/);
  assert.doesNotMatch(out, /stored-preview/);
});

test("only inert image types are previewed: SVG and HTML are not", () => {
  for (const mime of ["image/png", "image/jpeg", "image/gif", "image/webp"])
    assert.ok(showsInline({ ...byId("a3"), mime }), mime);
  for (const mime of ["image/svg+xml", "text/html", "application/pdf", "text/plain"])
    assert.ok(!showsInline({ ...byId("a3"), mime }), mime);
  const svg = detail({ ...byId("a3"), mime: "image/svg+xml", title: "logo.svg" });
  assert.doesNotMatch(svg, /<img/);
  // An HTML file's preview is text, escaped, never markup on this page.
  const page = detail(byId("a4"));
  assert.doesNotMatch(page, /<img|<h1>2\.4/);
  assert.match(page, /&lt;h1&gt;2\.4&lt;\/h1&gt;/);
  assert.match(detail(byId("a2")), /No preview for this kind of artifact/);
});

test("delete is offered only to someone who may delete, and asks before it deletes", () => {
  assert.doesNotMatch(text(detail(byId("a1"), false)), /Delete/);
  assert.match(text(detail(byId("a1"))), /Delete/);
  assert.doesNotMatch(text(detail(byId("a1"))), /cannot be undone/);
  const asking = text(
    html(<DeleteVersion version={3} confirming onAsk={noop} onDelete={noop} onKeep={noop} />),
  );
  assert.match(asking, /Delete version 3 of this artifact\? This cannot be undone\./);
  assert.match(asking, /Delete Version/);
  assert.match(asking, /Keep It/);
});

test("a deleted file leaves the list and the totals", async () => {
  const via = memoryStorage();
  const before = await via.use();
  await via.remove("a3");
  const page = await via.list({ project: "", agent: "", kind: "", q: "", limit: PAGE });
  assert.ok(!page.artifacts.some((f) => f.id === "a3"));
  const after = await via.use();
  assert.equal(after.count, before.count - 1);
  assert.equal(after.used_bytes, before.used_bytes - byId("a3").size);
});

test("a refused delete says why", () => {
  assert.equal(
    deleteProblem(new Error("You may not delete files.")),
    "The artifact could not be deleted. You may not delete files.",
  );
  assert.equal(deleteProblem(new Error("")), "The artifact could not be deleted.");
  assert.equal(deleteProblem("boom"), "The artifact could not be deleted.");
});

test("over quota is more than the limit, and no limit is never over", () => {
  assert.ok(overLimit(sampleUse({ used_bytes: 6 * 1024 ** 2 })));
  assert.ok(
    !overLimit(sampleUse({ used_bytes: 5 * 1024 ** 2 })),
    "exactly at the limit is not over",
  );
  assert.ok(!overLimit(sampleUse({ used_bytes: 1 })));
  assert.ok(!overLimit(sampleUse({ used_bytes: 10 * 1024 ** 3, limit_bytes: null })));
  assert.match(OVER_LIMIT, /cannot publish new artifacts/);
  const over = text(html(<StorageUsed use={sampleUse({ used_bytes: 6 * 1024 ** 2 })} />));
  assert.match(over, /Over/);
  assert.match(text(html(<StorageUsed use={sampleUse({ limit_bytes: null })} />)), /No limit/);
});

test("See more shows while there is a next page, and says when it is loading or failed", () => {
  assert.equal(
    html(<StorageMore hasMore={false} loading={false} failed={false} onMore={noop} />),
    "",
  );
  assert.match(
    text(html(<StorageMore hasMore loading={false} failed={false} onMore={noop} />)),
    /See more/,
  );
  const loading = html(<StorageMore hasMore loading failed={false} onMore={noop} />);
  assert.match(text(loading), /Loading/);
  assert.match(loading, /disabled=""/);
  assert.match(
    text(html(<StorageMore hasMore loading={false} failed onMore={noop} />)),
    /Could not load\. Try again/,
  );
});

test("See more walks every page once, newest first, without repeats", async () => {
  const many = Array.from({ length: 7 }, (_, i) => ({
    ...byId("a1"),
    id: `f${i}`,
    title: `f${i}.md`,
    created_at: new Date(Date.UTC(2026, 0, 1, 0, i)).toISOString(),
  }));
  const via = memoryStorage(many, sampleUse(), 3);
  const filter = { project: "", agent: "", kind: "", q: "" };
  let page = await via.list({ ...filter, limit: PAGE });
  let shown = page.artifacts;
  let pages = 1;
  while (page.next) {
    page = await via.list({ ...filter, limit: PAGE, before: page.next });
    // A page that overlaps what is shown, as one can when files land between pages, adds nothing twice.
    shown = appendPage(shown, [...shown.slice(-1), ...page.artifacts]);
    pages++;
  }
  assert.equal(pages, 3);
  assert.deepEqual(
    shown.map((f) => f.id),
    ["f6", "f5", "f4", "f3", "f2", "f1", "f0"],
  );
});
