import assert from "node:assert/strict";
import { test } from "node:test";
import { accentAttribute, servedBranding } from "./branding.ts";
import { documentTitle } from "./route.ts";

test("the workspace accent, and no choice yet, draw with the branded accent", () => {
  assert.equal(accentAttribute("workspace"), "brand");
  assert.equal(accentAttribute(undefined), "brand");
  assert.equal(accentAttribute(""), "brand");
  assert.equal(accentAttribute("teal"), "teal");
});

test("before the workspace's branding is read, the served name brands the console", () => {
  assert.deepEqual(servedBranding("Humagen"), {
    name: "Humagen",
    wordmark: "humagen",
    title: "Humagen",
    logo: null,
    logo_alt: "Humagen",
    style: null,
  });
  const acme = servedBranding("Acme");
  assert.equal(acme.wordmark, "Acme");
  assert.equal(acme.title, "Acme");
});

test("the tab title ends with the branded product's name", () => {
  assert.equal(documentTitle({ page: "inbox" }, 0, "Acme"), "Notifications · Acme");
  assert.equal(documentTitle({ page: "inbox" }, 2, "Acme"), "(2) Notifications · Acme");
});
