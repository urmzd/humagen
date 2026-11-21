// Checks the site's content: no em dashes anywhere in the copy (AGENTS.md),
// and llms.txt carries every page and every guide step.
import assert from "node:assert/strict";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { pages, site, steps } from "./content/site.ts";
import { terms, termsDate } from "./content/terms.ts";
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

test("the terms name the license, say an AI answer can be wrong, and add no condition by use", () => {
  const text = [...terms.intro, ...terms.sections.flatMap((s) => [s.title, ...s.body])].join(" ");
  assert.match(text, /Apache License 2\.0/);
  assert.match(text, /is AI and can make mistakes/);
  // The license is the agreement for the software: the page binds nobody by use.
  assert.doesNotMatch(text, /you agree|by using/i);
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

test("every link in the terms goes into the repository, and the history link names the terms' own file", () => {
  const links = terms.sections.flatMap((s) => s.links ?? []);
  assert.ok(links.length >= 4);
  for (const l of links) assert.ok(l.href.startsWith(`${site.repo}/`), l.href);
  const history = links.find((l) => l.href.includes("/commits/"));
  assert.ok(history);
  const file = history.href.split("/commits/main/")[1] ?? "";
  assert.ok(existsSync(join(new URL("../../..", import.meta.url).pathname, file)), file);
  for (const name of ["LICENSE", "SECURITY.md"]) {
    assert.ok(
      links.some((l) => l.href.endsWith(`/blob/main/${name}`)),
      name,
    );
    assert.ok(existsSync(join(new URL("../../..", import.meta.url).pathname, name)), name);
  }
});

test("llms.txt carries the terms' text", () => {
  const txt = llmsTxt();
  for (const s of terms.sections) {
    assert.ok(txt.includes(s.title), s.title);
    for (const p of s.body) assert.ok(txt.includes(p), p);
  }
});
