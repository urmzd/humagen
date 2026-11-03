import assert from "node:assert/strict";
import { test } from "node:test";
import {
  commandsFor,
  commandTokens,
  fuzzy,
  MAX_SKILLS,
  nextEnabled,
  segments,
  skillRefs,
  skillsIn,
} from "./commands.ts";
import { insertTag, matching, tagAt, tagOptions } from "./tags.ts";

const words = { experts: (t: string) => (t ? `Experts on ${t}` : "Every expert") };
const reasons = { named: "Already in this message", limit: (n: number) => `At most ${n}` };
const options = tagOptions([{ name: "review-bot" }], [{ handle: "rev" }], [], words, {
  skills: [
    { name: "security-review", description: "Find vulnerabilities in a change" },
    { name: "review", description: "Review changes in the current session." },
    { name: "rubber-duck", description: "Talk a problem through" },
    { name: "pr-resolve-comments", description: "Resolve review comments on a PR" },
    { name: "triage" },
  ],
});

test("/ starts a command at the start of a message or after a space, never in a path, a word or code", () => {
  assert.deepEqual(tagAt("/rev", 4), { start: 0, query: "rev", sigil: "/" });
  assert.deepEqual(tagAt("please /", 8), { start: 7, query: "", sigil: "/" });
  assert.deepEqual(tagAt("one\n/tri", 8), { start: 4, query: "tri", sigil: "/" });
  assert.equal(tagAt("and/or", 4), null);
  assert.equal(tagAt("see /tmp/x", 10), null);
  assert.equal(tagAt("(/rev", 5), null);
  assert.equal(tagAt("https://x", 8), null);
  assert.equal(tagAt("run `/rev", 9), null);
  // `@` completion never offers a skill, and `$` opens nothing.
  assert.ok(!matching(options, "rev", 8, "@").some((o) => o.kind === "skill"));
  assert.equal(tagAt("$rev", 4), null);
});

test("fuzzy matches a prefix first, then a word start, then inside a word, then letters in order", () => {
  assert.deepEqual(fuzzy("review", "rev"), { score: 0, hits: [[0, 3]] });
  assert.deepEqual(fuzzy("security-review", "rev"), { score: 1, hits: [[9, 12]] });
  assert.deepEqual(fuzzy("preview", "rev"), { score: 2, hits: [[1, 4]] });
  assert.deepEqual(fuzzy("pr-resolve-comments", "prc")?.hits, [
    [0, 2],
    [11, 12],
  ]);
  assert.equal(fuzzy("triage", "rev"), null);
  assert.deepEqual(fuzzy("Review", ""), { score: 0, hits: [] });
  assert.deepEqual(segments("security-review", [[9, 12]]), [
    { text: "security-", hit: false },
    { text: "rev", hit: true },
    { text: "iew", hit: false },
  ]);
});

test("the command menu lists skills only, closest name first, then by description, with the matches", () => {
  const at = tagAt("/review", 7);
  assert.ok(at);
  const got = commandsFor(options, "/review", at, reasons);
  assert.deepEqual(
    got.map((c) => c.option.token),
    ["review", "security-review", "pr-resolve-comments"],
  );
  assert.deepEqual(got[0]?.hintHits, [[0, 6]]);
  assert.deepEqual(got[1]?.nameHits, [[9, 15]]);
  // Matched by its description alone, so no letter of its name is marked.
  assert.deepEqual(got[2]?.nameHits, []);
  assert.deepEqual(got[2]?.hintHits, [[8, 14]]);
  // An empty query lists every skill in the order given; agents and people never.
  const all = commandsFor(options, "/", { start: 0, query: "", sigil: "/" }, reasons);
  assert.equal(all.length, 5);
  assert.ok(all.every((c) => c.option.kind === "skill" && c.hintHits.length === 0));
});

test("a skill the message names is greyed, and every skill once it names as many as it may", () => {
  const text = "$review @skill:triage ok /";
  const at = tagAt(text, text.length);
  assert.ok(at);
  const got = commandsFor(options, text, at, reasons);
  assert.equal(got.find((c) => c.option.token === "review")?.disabled, reasons.named);
  assert.equal(got.find((c) => c.option.token === "triage")?.disabled, reasons.named);
  assert.equal(got.find((c) => c.option.token === "rubber-duck")?.disabled, undefined);
  // The command being typed is not counted as named.
  const typing = commandsFor(
    options,
    "/review",
    { start: 0, query: "review", sigil: "/" },
    reasons,
  );
  assert.equal(typing[0]?.disabled, undefined);
  const full = "/review /triage /rubber-duck /security-review /";
  const last = tagAt(full, full.length);
  assert.ok(last);
  const capped = commandsFor(options, full, last, reasons);
  assert.equal(skillsIn(full, ["review", "triage", "rubber-duck", "security-review"]).length, 4);
  assert.equal(
    capped.find((c) => c.option.token === "pr-resolve-comments")?.disabled,
    reasons.limit(MAX_SKILLS),
  );
  assert.equal(nextEnabled(capped, 0, 1), -1);
});

test("arrow keys skip a greyed command, wrapping round the list", () => {
  const list = [{ disabled: "x" }, {}, { disabled: "x" }, {}].map((c, i) => ({
    option: { kind: "skill" as const, token: `s${i}`, label: `s${i}` },
    nameHits: [],
    hintHits: [],
    ...c,
  }));
  assert.equal(nextEnabled(list, 1, 1), 3);
  assert.equal(nextEnabled(list, 3, 1), 1);
  assert.equal(nextEnabled(list, 1, -1), 3);
});

test("a picked command is written /name with a space, and the box draws known skills as tokens", () => {
  const skill = options.find((o) => o.token === "review");
  assert.ok(skill);
  assert.deepEqual(insertTag("/rev now", { start: 0, query: "rev", sigil: "/" }, skill), {
    text: "/review now",
    caret: 8,
  });
  const names = ["review", "triage"];
  assert.deepEqual(commandTokens("/review this, then /triage.", names), [
    { start: 0, end: 7, name: "review" },
    { start: 19, end: 26, name: "triage" },
  ]);
  // The aliases are left as text in the box.
  assert.deepEqual(commandTokens("$review @skill:triage", names), []);
  // A path, an unknown name, a slash in a word, and code are text.
  assert.deepEqual(commandTokens("/review/x /ghost a/review `/review`", names), []);
});

test("a posted message shows every way of naming a skill as /name, and /name only for its resolved skills", () => {
  const text = "$review then @Skill:triage and /draft, see /tmp/x, $5 `$review`";
  assert.deepEqual(skillRefs(text, ["review", "triage", "draft"]), [
    { start: 0, end: 7, name: "review" },
    { start: 13, end: 26, name: "triage" },
    { start: 31, end: 37, name: "draft" },
  ]);
  // Without the resolved names a /name is text, since a path reads the same.
  assert.deepEqual(
    skillRefs(text).map((r) => r.name),
    ["review", "triage"],
  );
});
