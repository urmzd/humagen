import assert from "node:assert/strict";
import { test } from "node:test";
import { githubRef } from "./github-ref.ts";
import { openTab, parseTab, tabKey } from "./pane.ts";
import { href, parse } from "./route.ts";

test("GitHub URLs and numbers retain explicit repository and item kind", () => {
  assert.deepEqual(githubRef("https://github.com/acme/api/pull/12#discussion"), {
    repo: "acme/api",
    number: 12,
    kind: "pull",
  });
  assert.deepEqual(githubRef("#7", "acme/api"), { repo: "acme/api", number: 7, kind: "issue" });
  for (const invalid of [
    "#0",
    "#7",
    "https://evil.test/acme/api/pull/1",
    "https://github.com/acme/api/pull/999999999999",
    "javascript:alert(1)",
  ])
    assert.equal(githubRef(invalid), undefined);
});
test("GitHub pane tabs round trip through a conversation address", () => {
  for (const kind of ["pull", "issue", "review"] as const) {
    const tab = { kind, ref: "acme/api#12" };
    assert.deepEqual(parseTab(tabKey(tab)), tab);
    const route = { page: "conversation" as const, id: "thread", ...openTab({}, tab) };
    const url = new URL(href(route), "https://example.test");
    assert.equal(parse(url.pathname, url.search).page, "conversation");
    assert.equal(href(parse(url.pathname, url.search)), href(route));
  }
});
