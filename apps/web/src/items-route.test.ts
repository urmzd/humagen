import assert from "node:assert/strict";
import { test } from "node:test";
import { documentTitle, href, parse, type Route } from "./route.ts";

const at = (url: string) => {
  const u = new URL(url, "http://x");
  return parse(u.pathname, u.search);
};

test("an item has an address of its own, and Sessions filters by place and item", () => {
  const address = "github://urmzd/humagen/pull/5";
  const item: Route = { page: "item", address };
  assert.equal(href(item), "/items/github%3A%2F%2Furmzd%2Fhumagen%2Fpull%2F5");
  assert.deepEqual(at(href(item)), item);
  assert.deepEqual(at("/items/slack%3A%2F%2FT1%2FC1%2F1.2"), {
    page: "item",
    address: "slack://T1/C1/1.2",
  });
  assert.deepEqual(at("/items"), { page: "sessions" });
  const filtered: Route = { page: "sessions", place: "slack", item: address };
  assert.deepEqual(at(href(filtered)), filtered);
  assert.equal(at("/sessions?place=fax").page, "sessions");
  assert.equal((at("/sessions?place=fax") as { place?: string }).place, undefined);
  assert.equal(documentTitle(item), "Item - Humagen");
});
