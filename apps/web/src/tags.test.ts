import assert from "node:assert/strict";
import { test } from "node:test";
import { insertTag, matching, tagAt, tagOptions, type TagOption } from "./tags.ts";

const words = { auto: "Picks the agent that fits", experts: (t: string) => (t ? `Experts on ${t}` : "Every expert") };

test("a tag is typed where a word starts with @, never inside a word or inside code", () => {
  assert.deepEqual(tagAt("ask @ig", 7), { start: 4, query: "ig", sigil: "@" });
  assert.deepEqual(tagAt("@", 1), { start: 0, query: "", sigil: "@" });
  assert.deepEqual(tagAt("(@sme:bil", 9), { start: 1, query: "sme:bil", sigil: "@" });
  assert.equal(tagAt("mail dana@acme", 14), null);
  assert.equal(tagAt("run `@lead", 10), null);
  assert.deepEqual(tagAt("run `x` @lead", 13), { start: 8, query: "lead", sigil: "@" });
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
  assert.deepEqual(matching(options, "sme").map((o) => o.token), ["sme", "sme:", "sme:billing"]);
  assert.equal(matching(options, "").length, 8);
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
    ["auto", "globex-helper", "helper", "ada-okafor", "lee-wong", "priya-nair", "sme", "sme:"],
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
  assert.deepEqual(insertTag("ask @ig please", { start: 4, query: "ig", sigil: "@" }, option), { text: "ask @ignite please", caret: 12 });
  assert.deepEqual(insertTag("@", { start: 0, query: "", sigil: "@" }, option), { text: "@ignite ", caret: 8 });
});

test("$ starts a skill, and only skills complete after it; @ never offers a skill", () => {
  assert.deepEqual(tagAt("@lead $rev", 10), { start: 6, query: "rev", sigil: "$" });
  assert.equal(tagAt("costs a$5", 9), null);
  assert.equal(tagAt("run `$review", 12), null);
  const options = tagOptions([{ name: "lead" }], [{ handle: "review-ann" }], [], words, {
    skills: [{ name: "review", description: "Reviews a change" }, { name: "triage" }],
  });
  assert.deepEqual(matching(options, "re", 8, "$").map((o) => o.token), ["review"]);
  assert.deepEqual(matching(options, "", 8, "$").map((o) => o.token), ["review", "triage"]);
  assert.ok(!matching(options, "re", 8, "@").some((o) => o.kind === "skill"));
  const skill = options.find((o) => o.kind === "skill");
  assert.ok(skill);
  assert.deepEqual(insertTag("@lead $re now", { start: 6, query: "re", sigil: "$" }, skill), { text: "@lead $review now", caret: 14 });
});

test("a mention kind completes to @kind: and stays open for its items, which complete as @kind:name", () => {
  const options = tagOptions([], [], [], { ...words, kind: (k, t) => t ?? k }, {
    kinds: [{ kind: "sme", title: "Experts" }, { kind: "skill", title: "Skills" }],
    items: [{ kind: "skill", name: "review" }, { kind: "skill", name: "release-notes" }],
  });
  // The kinds are offered by their own names, and a kind written in full is not offered again.
  assert.deepEqual(matching(options, "sk").map((o) => o.token), ["skill:", "skill:review", "skill:release-notes"]);
  assert.deepEqual(matching(options, "skill:").map((o) => o.token), ["skill:review", "skill:release-notes"]);
  assert.deepEqual(matching(options, "skill:rel").map((o) => o.token), ["skill:release-notes"]);
  const kind = options.find((o) => o.token === "skill:");
  assert.equal(kind?.kind, "kind");
  assert.equal(kind?.hint, "Skills");
  // No space after a kind: what is typed next is its item.
  assert.deepEqual(insertTag("ask @sk", { start: 4, query: "sk", sigil: "@" }, kind as TagOption), { text: "ask @skill:", caret: 11 });
  const item = options.find((o) => o.token === "skill:review") as TagOption;
  assert.deepEqual(insertTag("ask @skill:re", { start: 4, query: "skill:re", sigil: "@" }, item), { text: "ask @skill:review ", caret: 18 });
});
