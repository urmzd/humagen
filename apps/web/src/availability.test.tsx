import assert from "node:assert/strict";
import { test } from "node:test";
import type { ProjectAgentsView } from "./api.ts";
import { answersLine, availableOf, defaultFrom, losing, offerWords } from "./availability.ts";
import { east, field, globex, renewals, tree } from "./projects.fixtures.ts";

const view = (all: boolean, names: string[]): ProjectAgentsView => ({
  version: 1,
  enable: [],
  available: { all, names },
  offers: [],
  homes: [],
  default_resolved: null,
});

test("what may answer: with every platform agent, those with no home and the project agents the platform listed", () => {
  const all = ["lead", "helper", "globex-helper", "initech-helper"];
  const homes = { "globex-helper": "Globex", "initech-helper": "Initech" };
  assert.deepEqual(availableOf(all, view(true, ["globex-helper"]), homes), ["lead", "helper", "globex-helper"]);
  // Narrowed by an allow list, only the names listed.
  assert.deepEqual(availableOf(all, view(false, ["helper"]), homes), ["helper"]);
  // Not yet read: nothing is hidden, the platform still refuses what is not available.
  assert.deepEqual(availableOf(all, null, homes), all);
});

test("explicit Company homes remain platform-wide while scoped homes require the server's named offer", () => {
  const names = ["helper", "humagen-analyst", "older-root", "scoped"];
  const homes = { helper: "~", "humagen-analyst": "~", "older-root": "", scoped: "Vault" };
  assert.deepEqual(availableOf(names, view(true, []), homes), ["helper", "humagen-analyst", "older-root"]);
  assert.deepEqual(availableOf(names, view(false, ["helper"]), homes), ["helper"]);
  assert.deepEqual(availableOf(names, view(true, ["scoped"]), homes), names);
});

test("where an agent comes from reads in words, and where a default is set is a project", () => {
  const title = (n: string) => (n === "" ? "Acme" : n);
  assert.equal(offerWords({ agent: "globex-helper", from: "Globex", home: true }, "Globex", title), "Lives here");
  assert.equal(offerWords({ agent: "globex-helper", from: "Globex", home: true }, "Globex renewals", title), "Lives in Globex");
  assert.equal(offerWords({ agent: "lead", from: "~" }, "Field", title), "from Acme");
  assert.equal(offerWords({ agent: "lead", from: "Field" }, "Field", title), "");
  assert.equal(defaultFrom({ agent: "lead", from: "project:~" }), "");
  assert.equal(defaultFrom({ agent: "lead", from: "project:Field" }), "Field");
});

test("who answers an untagged message: the default, agents deciding for themselves and who joined, or nobody", () => {
  assert.equal(answersLine("ignite"), "@ignite answers");
  assert.equal(answersLine("*"), "Agents decide for themselves who answers");
  assert.equal(answersLine("*", "ignite"), "@ignite joined on its own");
  assert.equal(answersLine(undefined), "Nobody answers; tag an agent");
});

test("a home counts the places outside it that lose the agent, the workspace included", () => {
  // Enabled in the workspace: everything outside Globex's subtree loses it, and so does the workspace.
  const outside = tree.filter((p) => p.name !== globex.name && p.name !== renewals.name).length;
  assert.equal(losing("Globex", ["~"], tree), outside + 1);
  // Enabled only in Field: what sits under Field and outside Globex.
  const underField = tree.filter((p) => p.path.some((s) => s.name === field.name) && p.name !== globex.name && p.name !== renewals.name).length;
  assert.equal(losing("Globex", ["Field"], tree), underField);
  assert.ok(underField > 0 && tree.includes(east));
  // Back to being a platform agent loses nothing.
  assert.equal(losing("", ["~"], tree), 0);
});
