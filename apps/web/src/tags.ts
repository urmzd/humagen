/**
 * Completion in the chat bar: what is being typed at the caret, what it could
 * complete to, and the text once one is picked. `@` names people, agents,
 * @auto and the kinds of `@kind:name` mention (`@sme:billing`); `$` names a
 * skill. The platform reads them (pkg/threads); this only helps a person
 * write them, so it follows the same rules: a tag starts a word, and nothing
 * inside code is a tag.
 */

/** What starts a tag: `@` for people, agents and mention kinds, `$` for a skill. */
export type Sigil = "@" | "$";

/** A name a message can tag, as completion offers it. */
export interface TagOption {
  /**
   * agent and auto: who runs. person: someone to bring in. kind: a mention
   * kind, written `@kind:` and completed further. item: one item of a
   * mention kind, `@kind:name` (and `@sme`, every expert). skill: `$name`.
   */
  kind: "agent" | "auto" | "person" | "kind" | "item" | "skill";
  /** What is written after the sigil: an agent's name, auto, a handle, `sme:`, `sme:billing`, a skill's name. */
  token: string;
  /** What the list shows. */
  label: string;
  /** A second line: a person's name, what an agent or a skill is for. */
  hint?: string;
}

/** The sigil an option is written after. */
export const sigilOf = (o: TagOption): Sigil => (o.kind === "skill" ? "$" : "@");

/** The tag being typed at the caret. */
export interface Typing {
  /** Where its sigil is. */
  start: number;
  /** What follows the sigil, up to the caret. */
  query: string;
  sigil: Sigil;
}

const TAG_CHAR = /[A-Za-z0-9._:-]/;

/** The tag being typed at the caret, or null when the caret is not in one. */
export function tagAt(text: string, caret: number): Typing | null {
  let i = caret;
  while (i > 0 && TAG_CHAR.test(text[i - 1] ?? "")) i--;
  const sigil = text[i - 1];
  if (i === 0 || (sigil !== "@" && sigil !== "$")) return null;
  const start = i - 1;
  // A tag starts a word: an @ inside one, as in an email address, is not one.
  if (start > 0 && !/[\s(]/.test(text[start - 1] ?? "")) return null;
  // Inside inline code or a code block, nothing is a tag.
  const ticks = (text.slice(0, start).match(/`/g) ?? []).length;
  if (ticks % 2 === 1) return null;
  return { start, query: text.slice(i, caret), sigil };
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

/** Which share of the list an option takes: agents with @auto, people, mention kinds with their items, skills. */
const shareOf = (o: TagOption): string => (o.kind === "auto" ? "agent" : o.kind === "item" ? "kind" : o.kind);

/**
 * The options that match what is typed after its sigil: closest first, then
 * in the order given, at most limit. When more match than that, each share
 * (agents, people, mention kinds, skills) takes the places in turn, its best
 * first, so a kind later in the order is never crowded out of the list by
 * an earlier one. A mention kind already written out in full is not offered
 * again: its items are.
 */
export function matching(options: TagOption[], query: string, limit = 8, sigil: Sigil = "@"): TagOption[] {
  const q = query.toLowerCase();
  const ranked = options
    .filter((o) => sigilOf(o) === sigil && !(o.kind === "kind" && o.token.toLowerCase() === q))
    .map((o, i) => ({ o, i, r: rank(o, q) }))
    .filter((x) => x.r >= 0)
    .sort((a, b) => a.r - b.r || a.i - b.i);
  if (ranked.length <= limit) return ranked.map((x) => x.o);
  const shares = new Map<string, typeof ranked>();
  for (const x of ranked) shares.set(shareOf(x.o), [...(shares.get(shareOf(x.o)) ?? []), x]);
  const taken = new Set<(typeof ranked)[number]>();
  for (let round = 0; taken.size < limit; round++) {
    const next = [...shares.values()].map((share) => share[round]).filter((x) => x !== undefined);
    if (next.length === 0) break;
    for (const x of next.slice(0, limit - taken.size)) taken.add(x);
  }
  return ranked.filter((x) => taken.has(x)).map((x) => x.o);
}

/**
 * The text with the tag being typed replaced by a picked one, and where the
 * caret goes after it. A mention kind is written `@kind:` with no space, so
 * its items can be completed next; anything else ends with a space.
 */
export function insertTag(text: string, at: Typing, option: TagOption): { text: string; caret: number } {
  const end = at.start + 1 + at.query.length;
  const open = option.kind === "kind";
  const tag = `${sigilOf(option)}${option.token}${open ? "" : " "}`;
  const rest = open ? text.slice(end) : text.slice(end).replace(/^ /, "");
  return { text: text.slice(0, at.start) + tag + rest, caret: at.start + tag.length };
}

/** A mention kind, as completion offers it. */
export interface KindOption {
  kind: string;
  title?: string;
 resolves?: string;
}

/**
 * What a message can tag: @auto first, then the agents that can answer
 * there, then people, then each mention kind (`@sme:`) and the items of it
 * read so far (topics are sme's), then skills for `$`. Experts on every
 * topic, `@sme`, are offered when sme is a kind here.
 */
export function tagOptions(
  agents: { name: string; description?: string }[],
  people: { handle: string; name?: string }[],
  _topics: string[],
  words: { auto: string; experts: (topic: string) => string; kind?: (kind: string, title?: string) => string },
  extra: { kinds?: KindOption[]; items?: { kind: string; name: string; description?: string }[]; skills?: { name: string; description?: string }[] } = {},
): TagOption[] {
  // Old expert metadata can still be returned by a cached or older host, but
  // discovery replaces routing and those choices are never offered again.
  const retired = new Set((extra.kinds ?? []).filter((k) => k.kind === "sme" || k.resolves === "experts").map((k) => k.kind));
  const kinds = (extra.kinds ?? []).filter((k) => !retired.has(k.kind));
  const items = (extra.items ?? []).filter((i) => i.kind !== "sme" && !retired.has(i.kind));
  return [
    { kind: "auto" as const, token: "auto", label: "auto", hint: words.auto },
    ...agents.map((a) => ({ kind: "agent" as const, token: a.name, label: a.name, ...(a.description ? { hint: a.description } : {}) })),
    ...people.map((p) => ({ kind: "person" as const, token: p.handle, label: p.handle, ...(p.name ? { hint: p.name } : {}) })),
    ...kinds.map((k) => ({
      kind: "kind" as const,
      token: `${k.kind}:`,
      label: `${k.kind}:`,
      hint: words.kind ? words.kind(k.kind, k.title) : (k.title ?? k.kind),
    })),
    ...items.map((i) => ({
      kind: "item" as const,
      token: `${i.kind}:${i.name}`,
      label: `${i.kind}:${i.name}`,
      ...(i.description ? { hint: i.description } : {}),
    })),
    ...(extra.skills ?? []).map((s) => ({ kind: "skill" as const, token: s.name, label: s.name, ...(s.description ? { hint: s.description } : {}) })),
  ];
}
