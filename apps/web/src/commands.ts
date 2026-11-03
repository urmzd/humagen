/**
 * Slash commands in the chat bar: `/` at the start of a message or after a
 * space opens a menu of the skills a message here can name, filtered as the
 * name is typed. A picked command is written `/name `, which the platform
 * reads as `$name` (pkg/threads), so the same skills, permissions and limits
 * apply. This file holds the pure parts: matching with the letters to
 * highlight, why a command cannot be picked, and where the box shows a
 * command as a token.
 */
import type { TagOption, Typing } from "./tags.ts";

/** How many skills one message names (`threads.MaxSkills`). */
export const MAX_SKILLS = 4;

/** A run of matched characters, [start, end). */
export type Hit = [number, number];

/** Where a query matches text, and how well: lower is closer. */
export interface Fuzzy {
  score: number;
  hits: Hit[];
}

const isWordStart = (text: string, i: number) => i === 0 || /[\s_.:/-]/.test(text[i - 1] ?? "");

/**
 * Whether query matches text, ignoring case: as a prefix (0), at the start
 * of a word (1), anywhere in it (2), or as letters in order with gaps (3 and
 * up, fewer runs first). Null when it does not.
 */
export function fuzzy(text: string, query: string): Fuzzy | null {
  const t = text.toLowerCase();
  const q = query.toLowerCase();
  if (!q) return { score: 0, hits: [] };
  let at = t.indexOf(q);
  if (at === 0) return { score: 0, hits: [[0, q.length]] };
  // A later word that starts with the query beats a match inside a word.
  for (let i = at; i >= 0; i = t.indexOf(q, i + 1))
    if (isWordStart(t, i)) return { score: 1, hits: [[i, i + q.length]] };
  if (at > 0) return { score: 2, hits: [[at, at + q.length]] };
  const hits: Hit[] = [];
  at = 0;
  for (const c of q) {
    const i = t.indexOf(c, at);
    if (i < 0) return null;
    const last = hits[hits.length - 1];
    if (last && last[1] === i) last[1] = i + 1;
    else hits.push([i, i + 1]);
    at = i + 1;
  }
  return { score: 3 + hits.length / 100, hits };
}

/** Text in runs, each marked when a hit covers it, for highlighting. */
export function segments(text: string, hits: Hit[]): { text: string; hit: boolean }[] {
  const out: { text: string; hit: boolean }[] = [];
  let at = 0;
  for (const [start, end] of hits) {
    if (start > at) out.push({ text: text.slice(at, start), hit: false });
    out.push({ text: text.slice(start, end), hit: true });
    at = end;
  }
  if (at < text.length) out.push({ text: text.slice(at), hit: false });
  return out;
}

/** A command as the `/` menu shows it. */
export interface Command {
  option: TagOption;
  /** The letters of its name the query matched. */
  nameHits: Hit[];
  /** The letters of its description the query matched. */
  hintHits: Hit[];
  /** Why it cannot be picked here, shown in place of its description. */
  disabled?: string;
}

const outsideCode = (text: string, at: number) =>
  ((text.slice(0, at).match(/`/g) ?? []).length & 1) === 0;

const NAME = "[a-z][a-z0-9]*(?:-[a-z0-9]+)*";
// A skill as a message names it: `/name` after a space, `$name` starting a
// word, and `@skill:name`.
const SLASH = new RegExp(`(^|\\s)/(${NAME})(?=$|\\s|[.,;:!?)](?:$|\\s))`, "g");
const DOLLAR = new RegExp(`(^|[\\s(])\\$(${NAME})(?![A-Za-z0-9_-])`, "g");
const AT_SKILL = new RegExp(`(^|[\\s(])@skill:(${NAME})(?![A-Za-z0-9_-])`, "gi");

/**
 * Where a message names one of the skills given, in order: each `/name` or
 * `$name` outside code. The box draws these as tokens; `@skill:name` is
 * left as text, as `@` completion writes it.
 */
export function commandTokens(
  text: string,
  skills: Iterable<string>,
): { start: number; end: number; name: string }[] {
  const known = new Set(skills);
  const out: { start: number; end: number; name: string }[] = [];
  for (const re of [SLASH, DOLLAR])
    for (const m of text.matchAll(re)) {
      const name = m[2] ?? "";
      const start = (m.index ?? 0) + (m[1] ?? "").length;
      if (known.has(name) && outsideCode(text, start))
        out.push({ start, end: start + 1 + name.length, name });
    }
  return out.sort((a, b) => a.start - b.start);
}

/** The skills a message names, once each in the order written, outside code. */
export function skillsIn(text: string, skills: Iterable<string>): string[] {
  const known = new Set(skills);
  const found: { at: number; name: string }[] = [];
  for (const re of [SLASH, DOLLAR, AT_SKILL])
    for (const m of text.matchAll(re)) {
      const name = (m[2] ?? "").toLowerCase();
      const at = (m.index ?? 0) + (m[1] ?? "").length;
      if (known.has(name) && outsideCode(text, at)) found.push({ at, name });
    }
  const out: string[] = [];
  for (const f of found.sort((a, b) => a.at - b.at)) if (!out.includes(f.name)) out.push(f.name);
  return out;
}

/**
 * The commands that match what is typed after `/`: the skills among the
 * options, closest name first, then those whose description holds the
 * query, in the order given, at most limit. A skill the rest of the message
 * already names is greyed, and once it names MAX_SKILLS, so is every other.
 */
export function commandsFor(
  options: TagOption[],
  text: string,
  at: Typing,
  words: { named: string; limit: (max: number) => string },
  limit = 12,
): Command[] {
  const skills = options.filter((o) => o.kind === "skill");
  const rest = text.slice(0, at.start) + text.slice(at.start + 1 + at.query.length);
  const named = skillsIn(
    rest,
    skills.map((s) => s.token),
  );
  const ranked: { c: Command; r: number; i: number }[] = [];
  skills.forEach((option, i) => {
    const name = fuzzy(option.token, at.query);
    const hint =
      option.hint && at.query ? option.hint.toLowerCase().indexOf(at.query.toLowerCase()) : -1;
    if (!name && hint < 0) return;
    const disabled = named.includes(option.token)
      ? words.named
      : named.length >= MAX_SKILLS
        ? words.limit(MAX_SKILLS)
        : undefined;
    ranked.push({
      c: {
        option,
        nameHits: name?.hits ?? [],
        hintHits: hint >= 0 ? [[hint, hint + at.query.length]] : [],
        ...(disabled ? { disabled } : {}),
      },
      r: name ? name.score : 10,
      i,
    });
  });
  return ranked
    .sort((a, b) => a.r - b.r || a.i - b.i)
    .slice(0, limit)
    .map((x) => x.c);
}

/** The next command that can be picked from from, moving by step (1 or -1), or -1. */
export function nextEnabled(commands: Command[], from: number, step: 1 | -1): number {
  const n = commands.length;
  for (let k = 1; k <= n; k++) {
    const i = (((from + step * k) % n) + n) % n;
    if (!commands[i]?.disabled) return i;
  }
  return -1;
}
