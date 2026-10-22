import assert from "node:assert/strict";
import { test } from "node:test";
import { insertTag, matching, tagAt, tagOptions, type TagOption } from "./tags.ts";

const words = { auto: "Picks the agent that fits", experts: (t: string) => (t ? `Experts on ${t}` : "Every expert") };

test("a tag is typed where a word starts with @, never inside a word or inside code", () => {
  assert.deepEqual(tagAt("ask @ig", 7), { start: 4, query: "ig" });
  assert.deepEqual(tagAt("@", 1), { start: 0, query: "" });
  assert.deepEqual(tagAt("(@sme:bil", 9), { start: 1, query: "sme:bil" });
  assert.equal(tagAt("mail dana@acme", 14), null);
  assert.equal(tagAt("run `@lead", 10), null);
  assert.deepEqual(tagAt("run `x` @lead", 13), { start: 8, query: "lead" });
  assert.equal(tagAt("no tag here", 5), null);
  // The caret decides: past the tag and a space, nothing is being typed.
  assert.equal(tagAt("@ignite then", 12), null);
});

test("options that start with what is typed come first, then the rest that hold it, at most the limit", () => {
  const options: TagOption[] = tagOptions(
    [{ name: "globex-helper", description: "Globex renewals" }, { name: "ignite" }],
    [{ handle: "dana", name: "Dana Scully" }, { handle: "ignacio", name: "Ignacio" }],
    ["billing"],
    words,
  );
  assert.deepEqual(matching(options, "ig").map((o) => o.token), ["ignite", "ignacio"]);
  assert.deepEqual(matching(options, "sc").map((o) => o.token), ["dana"]);
  assert.deepEqual(matching(options, "sme").map((o) => o.token), ["sme", "sme:billing"]);
  assert.equal(matching(options, "").length, 7);
  assert.equal(matching(options, "", 2).length, 2);
  assert.equal(options[0]?.kind, "auto");
});

test("when more match than the list holds, agents, people and expert topics each keep places in it", () => {
  const options = tagOptions(
    ["globex-helper", "helper", "lead", "ledger", "recap-writer"].map((name) => ({ name })),
    ["ada-okafor", "lee-wong", "priya-nair", "sam-ortiz", "zoe-quinn"].map((handle) => ({ handle })),
    ["billing", "renewals"],
    words,
  );
  const shown = matching(options, "");
  assert.equal(shown.length, 8);
  // @auto and the first agents, the first people, then every expert and the first topic.
  assert.deepEqual(
    shown.map((o) => o.token),
    ["auto", "globex-helper", "helper", "ada-okafor", "lee-wong", "priya-nair", "sme", "sme:billing"],
  );
  // A short list still holds one of each, in the order given.
  assert.deepEqual(
    matching(options, "", 4).map((o) => o.token),
    ["auto", "globex-helper", "ada-okafor", "sme"],
  );
  // A kind with fewer matches than its share gives the rest to the others.
  assert.deepEqual(
    matching(options, "r", 7).map((o) => o.token),
    ["recap-writer", "globex-helper", "helper", "ada-okafor", "priya-nair", "sam-ortiz", "sme:renewals"],
  );
});

test("picking an option writes the tag and a space, and puts the caret after it", () => {
  const option: TagOption = { kind: "agent", token: "ignite", label: "ignite" };
  assert.deepEqual(insertTag("ask @ig please", { start: 4, query: "ig" }, option), { text: "ask @ignite please", caret: 12 });
  assert.deepEqual(insertTag("@", { start: 0, query: "" }, option), { text: "@ignite ", caret: 8 });
});
