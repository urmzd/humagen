import assert from "node:assert/strict";
import { test } from "node:test";
import {
  insertTag,
  matching,
  mentionsIn,
  type TagOption,
  tagAt,
  tagOptions,
  withMention,
  withoutMention,
} from "./tags.ts";

const words = { experts: (t: string) => (t ? `Experts on ${t}` : "Every expert") };

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
    [
      { handle: "dana", name: "Dana Scully" },
      { handle: "ignacio", name: "Ignacio" },
    ],
    ["billing"],
    words,
  );
  assert.deepEqual(
    matching(options, "ig").map((o) => o.token),
    ["ignite", "ignacio"],
  );
  assert.deepEqual(
    matching(options, "sc").map((o) => o.token),
    ["dana"],
  );
  assert.deepEqual(
    matching(options, "sme").map((o) => o.token),
    [],
  );
  assert.equal(matching(options, "").length, 4);
  assert.equal(matching(options, "", 2).length, 2);
  // `@auto` is no tag: nobody goes by it, so it is never offered.
  assert.equal(options[0]?.kind, "agent");
  assert.ok(!options.some((o) => o.token === "auto"));
});

test("when more match than the list holds, agents and people each keep places in it", () => {
  const options = tagOptions(
    ["globex-helper", "helper", "lead", "ledger", "recap-writer"].map((name) => ({ name })),
    ["ada-okafor", "lee-wong", "priya-nair", "sam-ortiz", "zoe-quinn"].map((handle) => ({
      handle,
    })),
    ["billing", "renewals"],
    words,
  );
  const shown = matching(options, "");
  assert.equal(shown.length, 8);
  // The first agents, then the first people.
  assert.deepEqual(
    shown.map((o) => o.token),
    [
      "globex-helper",
      "helper",
      "lead",
      "ledger",
      "ada-okafor",
      "lee-wong",
      "priya-nair",
      "sam-ortiz",
    ],
  );
  // A short list still holds one of each, in the order given.
  assert.deepEqual(
    matching(options, "", 4).map((o) => o.token),
    ["globex-helper", "helper", "ada-okafor", "lee-wong"],
  );
  // A kind with fewer matches than its share gives the rest to the others.
  assert.deepEqual(
    matching(options, "r", 7).map((o) => o.token),
    ["recap-writer", "globex-helper", "helper", "ledger", "ada-okafor", "priya-nair", "sam-ortiz"],
  );
});

test("picking an option writes the tag and a space, and puts the caret after it", () => {
  const option: TagOption = { kind: "agent", token: "ignite", label: "ignite" };
  assert.deepEqual(insertTag("ask @ig please", { start: 4, query: "ig", sigil: "@" }, option), {
    text: "ask @ignite please",
    caret: 12,
  });
  assert.deepEqual(insertTag("@", { start: 0, query: "", sigil: "@" }, option), {
    text: "@ignite ",
    caret: 8,
  });
});

test("$ completes nothing; @ never offers a skill or a kind over skills; a skill is written /name", () => {
  assert.equal(tagAt("@lead $rev", 10), null);
  assert.equal(tagAt("costs a$5", 9), null);
  const options = tagOptions([{ name: "lead" }], [{ handle: "review-ann" }], [], words, {
    kinds: [
      { kind: "skill", title: "Skills", resolves: "skills" },
      { kind: "howto", title: "How-tos", resolves: "skills" },
      { kind: "team", title: "Teams", resolves: "role" },
    ],
    items: [
      { kind: "skill", name: "review" },
      { kind: "howto", name: "review" },
      { kind: "team", name: "sre" },
    ],
    skills: [{ name: "review", description: "Reviews a change" }, { name: "triage" }],
  });
  assert.deepEqual(
    matching(options, "", 20, "@").map((o) => o.token),
    ["lead", "review-ann", "team:", "team:sre"],
  );
  assert.deepEqual(
    matching(options, "re", 8, "@").map((o) => o.token),
    ["review-ann", "team:sre"],
  );
  const skill = options.find((o) => o.kind === "skill");
  assert.ok(skill);
  assert.deepEqual(insertTag("@lead /re now", { start: 6, query: "re", sigil: "/" }, skill), {
    text: "@lead /review now",
    caret: 14,
  });
});

test("a mention kind completes to @kind: and stays open for its items, which complete as @kind:name", () => {
  const options = tagOptions(
    [],
    [],
    [],
    { ...words, kind: (k, t) => t ?? k },
    {
      kinds: [
        { kind: "sme", title: "Experts" },
        { kind: "team", title: "Teams", resolves: "role" },
      ],
      items: [
        { kind: "team", name: "sre" },
        { kind: "team", name: "support" },
      ],
    },
  );
  // The kinds are offered by their own names, and a kind written in full is not offered again.
  assert.deepEqual(
    matching(options, "te").map((o) => o.token),
    ["team:", "team:sre", "team:support"],
  );
  assert.deepEqual(
    matching(options, "team:").map((o) => o.token),
    ["team:sre", "team:support"],
  );
  assert.deepEqual(
    matching(options, "team:su").map((o) => o.token),
    ["team:support"],
  );
  const kind = options.find((o) => o.token === "team:");
  assert.equal(kind?.kind, "kind");
  assert.equal(kind?.hint, "Teams");
  // No space after a kind: what is typed next is its item.
  assert.deepEqual(insertTag("ask @te", { start: 4, query: "te", sigil: "@" }, kind as TagOption), {
    text: "ask @team:",
    caret: 10,
  });
  const item = options.find((o) => o.token === "team:sre") as TagOption;
  assert.deepEqual(insertTag("ask @team:s", { start: 4, query: "team:s", sigil: "@" }, item), {
    text: "ask @team:sre ",
    caret: 14,
  });
});

test("retired routing never appears even with cached expert kinds and topics", () => {
  const options = tagOptions([], [], ["billing"], words, {
    kinds: [{ kind: "sme" }, { kind: "expert", resolves: "experts" }],
    items: [
      { kind: "sme", name: "billing" },
      { kind: "expert", name: "billing" },
    ],
  });
  assert.deepEqual(
    options.map((o) => o.token),
    [],
  );
});

test("a draft's agents are its mentions: once each, in order, starting a word, outside code", () => {
  const agents = ["lead", "helper", "writer"];
  assert.deepEqual(mentionsIn("@lead and (@helper) then @lead again", agents), ["lead", "helper"]);
  assert.deepEqual(mentionsIn("mail ada@lead.com about `@writer` and @nobody", agents), []);
  assert.deepEqual(mentionsIn("@leader is not @lead-ish", agents), []);
  assert.deepEqual(mentionsIn("", agents), []);
});

test("a picked agent is mentioned at the caret as the @ completion writes it, and only once", () => {
  assert.deepEqual(withMention("", 0, "lead"), { text: "@lead ", caret: 6 });
  assert.deepEqual(withMention("ask about launch", 3, "lead"), {
    text: "ask @lead about launch",
    caret: 10,
  });
  assert.deepEqual(withMention("plan it", 7, "helper"), { text: "plan it @helper ", caret: 16 });
  assert.deepEqual(withMention("@lead plan", 10, "lead"), { text: "@lead plan", caret: 10 });
  // As many as are picked: no cap.
  let draft = "";
  for (const n of ["a1", "a2", "a3", "a4", "a5", "a6", "a7"])
    draft = withMention(draft, draft.length, n).text;
  assert.deepEqual(mentionsIn(draft, ["a1", "a2", "a3", "a4", "a5", "a6", "a7"]).length, 7);
});

test("taking a mention out removes its words and the space after, and nothing else", () => {
  assert.equal(withoutMention("@lead @helper plan it", "lead"), "@helper plan it");
  assert.equal(withoutMention("ask @helper now and @helper later", "helper"), "ask now and later");
  assert.equal(withoutMention("@leader stays", "lead"), "@leader stays");
});
