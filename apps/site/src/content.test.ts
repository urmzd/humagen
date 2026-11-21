// Checks the site's content: no em dashes anywhere in the copy (AGENTS.md),
// and llms.txt carries every page and every guide step.
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { pages, site, steps } from "./content/site.ts";
import { terms, termsDate, termsFile } from "./content/terms.ts";
import { llmsTxt } from "./lib/llms.ts";

const files = (dir: string): string[] =>
  readdirSync(dir, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory()
      ? files(join(dir, e.name))
      : /\.(ts|astro|css|md)$/.test(e.name)
        ? [join(dir, e.name)]
        : [],
  );

test("no em dashes in the site's sources", () => {
  const root = new URL(".", import.meta.url).pathname;
  for (const f of [...files(root), join(root, "../README.md")]) {
    assert.ok(
      !readFileSync(f, "utf8").includes(String.fromCharCode(0x2014)),
      `${f} has an em dash`,
    );
  }
});

test("llms.txt names every page and every step", () => {
  const txt = llmsTxt();
  for (const p of Object.values(pages)) assert.ok(txt.includes(p.title), p.title);
  for (const s of steps) assert.ok(txt.includes(s.title), s.title);
});

test("every page has its own title and description", () => {
  const all = Object.values(pages);
  assert.equal(new Set(all.map((p) => p.title)).size, all.length);
  assert.equal(new Set(all.map((p) => p.description)).size, all.length);
});

test("app icons are the console's own files", () => {
  const root = new URL("..", import.meta.url).pathname;
  for (const f of ["favicon.svg", "favicon-32.png", "icon.svg", "apple-touch-icon.png"]) {
    const ours = readFileSync(join(root, "public", f));
    const theirs = readFileSync(join(root, "../web/public", f));
    assert.ok(ours.equals(theirs), `public/${f} differs from apps/web/public/${f}`);
  }
});

const termsText = () =>
  [...terms.intro, ...terms.sections.flatMap((s) => [s.title, ...s.body])].join(" ");

test("the terms name the license, say an AI answer can be wrong, and add no condition by use", () => {
  const text = termsText();
  assert.match(text, /Apache License 2\.0/);
  assert.match(text, /is AI and can make mistakes/);
  // The license is the agreement for the software: the page binds nobody by
  // use. These are the usual ways to say that it does.
  assert.doesNotMatch(
    text,
    /\byou (agree|accept|consent)|by (using|accessing|continuing)|your use\b.*\b(means|constitutes|is acceptance)|constitutes|\bbound by\b|must (agree|accept)/i,
  );
  // Every section is addressable, and no two share an address.
  const ids = terms.sections.map((s) => s.id);
  assert.equal(new Set(ids).size, ids.length);
});

test("the terms show a real day, not one still to come", () => {
  assert.match(terms.updated, /^\d{4}-\d{2}-\d{2}$/);
  const day = new Date(`${terms.updated}T00:00:00Z`);
  assert.equal(day.toISOString().slice(0, 10), terms.updated, "not a calendar day");
  assert.ok(day.getTime() <= Date.now(), "the day the terms changed is in the future");
  assert.equal(termsDate("2026-11-21"), "November 21, 2026");
});

test("the day the terms show is the day this text was written", () => {
  // The page says it shows the day it last changed. This holds the text to
  // that day: when a word changes, set terms.updated to the day of the
  // change, then put the new digest and that day here.
  const digest = createHash("sha256")
    .update(JSON.stringify([terms.intro, terms.sections]))
    .digest("hex")
    .slice(0, 16);
  assert.deepEqual(
    { digest, updated: terms.updated },
    { digest: "8292c06d4f02e58f", updated: "2026-11-21" },
    "the terms changed: set terms.updated to today, and this test's digest and day with it",
  );
});

test("every link in the terms goes into the repository, and the history link is the terms' own file", () => {
  const root = new URL("../../..", import.meta.url).pathname;
  const links = terms.sections.flatMap((s) => s.links ?? []);
  for (const l of links) assert.ok(l.href.startsWith(`${site.repo}/`), l.href);
  // The terms are read from the file the history link names, and no other.
  assert.equal(termsFile, "apps/site/src/content/terms.ts");
  assert.equal(
    new URL("./content/terms.ts", import.meta.url).pathname,
    join(root, termsFile),
    "termsFile is not the file the terms are in",
  );
  assert.deepEqual(
    links.filter((l) => l.href.includes("/commits/")).map((l) => l.href),
    [`${site.repo}/commits/main/${termsFile}`],
  );
  for (const name of ["LICENSE", "SECURITY.md"]) {
    assert.ok(
      links.some((l) => l.href === `${site.repo}/blob/main/${name}`),
      name,
    );
    assert.ok(existsSync(join(root, name)), name);
  }
  assert.ok(links.some((l) => l.href === `${site.repo}/issues`));
});

test("llms.txt carries the whole of the terms: the day, every paragraph and every link", () => {
  const txt = llmsTxt();
  assert.ok(txt.includes(`${terms.changed} ${termsDate()}.`));
  for (const p of terms.intro) assert.ok(txt.includes(p), p);
  for (const s of terms.sections) {
    assert.ok(txt.includes(`### ${s.title}`), s.title);
    for (const p of s.body) assert.ok(txt.includes(p), p);
    for (const l of s.links ?? []) assert.ok(txt.includes(`[${l.label}](${l.href})`), l.href);
  }
});
