/**
 * The @ typeahead: what is being typed at the caret, what it could tag, and
 * the text once one is picked. The platform reads the tags (pkg/threads);
 * this only helps a person write them, so it follows the same rules: a tag
 * starts a word, and nothing inside code is a tag.
 */

/** A name a message can tag, as the typeahead offers it. */
export interface TagOption {
  kind: "agent" | "auto" | "person" | "sme";
  /** What is written after the @: an agent's name, auto, a person's handle, sme or sme:<topic>. */
  token: string;
  /** What the list shows. */
  label: string;
  /** A second line: a person's name, what an agent is for. */
  hint?: string;
}

/** The tag being typed at the caret. */
export interface Typing {
  /** Where its @ is. */
  start: number;
  /** What follows the @, up to the caret. */
  query: string;
}

const TAG_CHAR = /[A-Za-z0-9._:-]/;

/** The tag being typed at the caret, or null when the caret is not in one. */
export function tagAt(text: string, caret: number): Typing | null {
  let i = caret;
  while (i > 0 && TAG_CHAR.test(text[i - 1] ?? "")) i--;
  if (i === 0 || text[i - 1] !== "@") return null;
  const start = i - 1;
  // A tag starts a word: an @ inside one, as in an email address, is not one.
  if (start > 0 && !/[\s(]/.test(text[start - 1] ?? "")) return null;
  // Inside inline code or a code block, nothing is a tag.
  const ticks = (text.slice(0, start).match(/`/g) ?? []).length;
  if (ticks % 2 === 1) return null;
  return { start, query: text.slice(i, caret) };
}

// A person is found by their name as well as their handle; an agent by its
// name alone, since what it is for is not what it is called.
const rank = (o: TagOption, q: string): number => {
  const token = o.token.toLowerCase();
  const words = [o.label, ...(o.kind === "person" && o.hint ? [o.hint] : [])].join(" ").toLowerCase();
  if (token.startsWith(q)) return 0;
  if (words.split(/\s+/).some((w) => w.startsWith(q))) return 1;
  if (token.includes(q) || words.includes(q)) return 2;
  return -1;
};

/** The options that match what is typed: closest first, then in the order given, at most limit. */
export function matching(options: TagOption[], query: string, limit = 8): TagOption[] {
  const q = query.toLowerCase();
  return options
    .map((o, i) => ({ o, i, r: rank(o, q) }))
    .filter((x) => x.r >= 0)
    .sort((a, b) => a.r - b.r || a.i - b.i)
    .slice(0, limit)
    .map((x) => x.o);
}

/** The text with the tag being typed replaced by a picked one, and where the caret goes after it. */
export function insertTag(text: string, at: Typing, option: TagOption): { text: string; caret: number } {
  const end = at.start + 1 + at.query.length;
  const tag = `@${option.token} `;
  const rest = text.slice(end).replace(/^ /, "");
  return { text: text.slice(0, at.start) + tag + rest, caret: at.start + tag.length };
}

/**
 * What a thread offers to tag: @auto first, then the agents that can answer
 * there, then people, then experts by topic.
 */
export function tagOptions(
  agents: { name: string; description?: string }[],
  people: { handle: string; name?: string }[],
  topics: string[],
  words: { auto: string; experts: (topic: string) => string },
): TagOption[] {
  return [
    { kind: "auto" as const, token: "auto", label: "auto", hint: words.auto },
    ...agents.map((a) => ({ kind: "agent" as const, token: a.name, label: a.name, ...(a.description ? { hint: a.description } : {}) })),
    ...people.map((p) => ({ kind: "person" as const, token: p.handle, label: p.handle, ...(p.name ? { hint: p.name } : {}) })),
    ...(topics.length > 0 ? [{ kind: "sme" as const, token: "sme", label: "sme", hint: words.experts("") }] : []),
    ...topics.map((t) => ({ kind: "sme" as const, token: `sme:${t}`, label: `sme:${t}`, hint: words.experts(t) })),
  ];
}
