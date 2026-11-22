// Checks the built site (run `pnpm build` first): every page carries its
// social preview and structured data, and each preview image exists at the
// size its tags declare.
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { pages, site } from "../src/content/site.ts";
import { terms, termsDate } from "../src/content/terms.ts";

const dist = new URL("../dist", import.meta.url).pathname;
const built = [
  { file: "index.html", page: pages.home, ld: "SoftwareApplication" },
  { file: "deploy/index.html", page: pages.deploy, ld: "TechArticle" },
  { file: "terms/index.html", page: pages.terms },
  { file: "404.html" },
];

const meta = (html: string, key: string): string | undefined =>
  html.match(new RegExp(`<meta (?:property|name)="${key}" content="([^"]*)"`))?.[1];

/** A PNG's width and height, from its IHDR chunk. */
const pngSize = (buf: Buffer) => {
  assert.equal(buf.subarray(1, 4).toString("latin1"), "PNG", "not a PNG");
  return { width: buf.readUInt32BE(16), height: buf.readUInt32BE(20) };
};

test("the site is built", () => {
  assert.ok(existsSync(join(dist, "index.html")), "run pnpm build before the tests");
});

for (const b of built) {
  test(`${b.file} has a 1200x630 social preview`, () => {
    const html = readFileSync(join(dist, b.file), "utf8");
    for (const key of [
      "og:title",
      "og:description",
      "og:url",
      "og:type",
      "og:site_name",
      "og:image:alt",
      "twitter:image",
    ]) {
      assert.ok(meta(html, key), `${b.file} has no ${key}`);
    }
    assert.equal(meta(html, "twitter:card"), "summary_large_image");
    const image = meta(html, "og:image") ?? "";
    assert.ok(
      image.startsWith(`${site.url}/og/`),
      `og:image ${image} is not under ${site.url}/og/`,
    );
    assert.equal(meta(html, "twitter:image"), image);
    const file = join(dist, new URL(image).pathname);
    assert.ok(existsSync(file), `${file} is missing`);
    assert.deepEqual(pngSize(readFileSync(file)), { width: 1200, height: 630 });
    assert.equal(meta(html, "og:image:width"), "1200");
    assert.equal(meta(html, "og:image:height"), "630");
    if (b.page) {
      const canonical = new URL(b.page.path, site.url).href;
      assert.equal(meta(html, "og:url"), canonical);
      assert.equal(meta(html, "og:title"), b.page.title);
      assert.ok(html.includes(`<link rel="canonical" href="${canonical}">`), "canonical");
      assert.ok(
        readFileSync(join(dist, "sitemap.xml"), "utf8").includes(`<loc>${canonical}</loc>`),
      );
    }
  });

  const { page, ld } = b;
  if (page && ld) {
    test(`${b.file} has ${ld} structured data that matches the page`, () => {
      const html = readFileSync(join(dist, b.file), "utf8");
      const raw = html.match(/<script type="application\/ld\+json">([^<]*)<\/script>/)?.[1];
      assert.ok(raw, `${b.file} has no JSON-LD`);
      const data = JSON.parse(raw);
      assert.equal(data["@type"], ld);
      assert.equal(data.image, meta(html, "og:image"));
      for (const banned of ["aggregateRating", "review", "offers"]) {
        assert.ok(!(banned in data), `${b.file} JSON-LD claims ${banned}`);
      }
      if (ld === "TechArticle") assert.equal(data.headline, page.title);
    });
  }
}

/** Text as Astro writes it into HTML. */
const escaped = (text: string) =>
  text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");

test("the terms page shows the whole of its text, its links and the day it changed", () => {
  const html = readFileSync(join(dist, "terms/index.html"), "utf8");
  assert.ok(html.includes(`<title>${escaped(pages.terms.title)}</title>`), "title");
  assert.ok(html.includes(`<time datetime="${terms.updated}">${termsDate()}</time>`));
  const shows = (text: string) =>
    assert.ok(html.includes(escaped(text)) || html.includes(text), `the page lacks: ${text}`);
  for (const p of terms.intro) shows(p);
  for (const s of terms.sections) {
    assert.ok(html.includes(`id="${s.id}"`), s.id);
    shows(s.title);
    for (const p of s.body) shows(p);
    for (const l of s.links ?? []) {
      assert.ok(html.includes(`href="${l.href}"`), l.href);
      shows(l.label);
    }
  }
});
