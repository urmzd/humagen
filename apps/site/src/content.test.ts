// Checks the site's content: no em dashes anywhere in the copy (AGENTS.md),
// and llms.txt carries every page and every guide step.
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { pages, steps } from "./content/site.ts";
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
