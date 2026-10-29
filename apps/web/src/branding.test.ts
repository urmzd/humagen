import assert from "node:assert/strict";
import { test } from "node:test";
import { accentAttribute, applyCopy, brandCopy, servedBranding } from "./branding.ts";
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
    starters: null,
    copy: {},
  });
  const acme = servedBranding("Acme");
  assert.equal(acme.wordmark, "Acme");
  assert.equal(acme.title, "Acme");
});

test("the tab title ends with the branded product's name", () => {
  assert.equal(documentTitle({ page: "inbox" }, 0, "Acme"), "Notifications · Acme");
  assert.equal(documentTitle({ page: "inbox" }, 2, "Acme"), "(2) Notifications · Acme");
});

test("the workspace's text replaces the console's by key, and nothing else", () => {
  const copy = {
    navigation: { agents: "Agents", projects: "Projects" },
    chat: { placeholderOne: (agent: string) => `Message ${agent}` },
  };
  applyCopy(copy, {
    "navigation.agents": "Assistants",
    "chat.placeholderOne": "not a function",
    "navigation.missing": "ignored",
    "nowhere.at.all": "ignored",
  });
  assert.equal(copy.navigation.agents, "Assistants");
  assert.equal(copy.navigation.projects, "Projects");
  assert.equal(typeof copy.chat.placeholderOne, "function");
  assert.equal("missing" in copy.navigation, false);
});

test("the product's own sender takes the product's name, unless the workspace words it", () => {
  const copy = { product: "Humagen", navigation: { agents: "Agents" } };
  brandCopy(copy, { ...servedBranding("Acme"), copy: { "navigation.agents": "Assistants" } });
  assert.equal(copy.product, "Acme");
  assert.equal(copy.navigation.agents, "Assistants");
  brandCopy(copy, { ...servedBranding("Acme"), copy: { product: "Acme HQ" } });
  assert.equal(copy.product, "Acme HQ");
});

test("a tab title follows the workspace's words for the page", async () => {
  const { defaultCopy } = await import("@humagen/components/copy");
  const was = defaultCopy.navigation.agents;
  defaultCopy.navigation.agents = "Assistants";
  try {
    assert.equal(documentTitle({ page: "agents" }, 0, "Acme"), "Assistants · Acme");
  } finally {
    defaultCopy.navigation.agents = was;
  }
});
