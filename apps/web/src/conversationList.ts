import type { Marks, ThreadSummary } from "./api.ts";

/** How the sidebar lists conversations: newest first, by project, or the ones put away. */
export type ListView = "recents" | "projects" | "archived";

/** What a row's dot says, most urgent first. */
export type RowStanding = "needs" | "running" | "failed" | "unread" | "";

/** A project group shows this many rows before "See All (N)". */
export const GROUP_ROWS = 5;

/** No marks yet: nothing is unread. */
export const NO_MARKS: Marks = { archived: [], read: {}, tips: [] };

/**
 * Whether an agent said something in a conversation since its reader last
 * opened it. Before the reader's first mark nothing reads as unread, and
 * what the reader said themselves never does.
 */
export function isUnread(t: ThreadSummary, marks: Marks): boolean {
  if (!marks.since || !t.last_agent) return false;
  const seen = marks.read[t.id] ?? marks.since;
  return Date.parse(t.updated_at) > Date.parse(seen);
}

/**
 * The one dot a row shows. Waiting on a person comes first, since it is the
 * next human decision; running next; then a failed reply; then unread.
 */
export function standingOf(t: ThreadSummary, marks: Marks): RowStanding {
  if ((t.waiting ?? 0) > 0) return "needs";
  if (t.active > 0) return "running";
  if (t.failed > 0) return "failed";
  return isUnread(t, marks) ? "unread" : "";
}

/** How long ago, in the fewest characters: now, 5m, 3h, 2d, 3w, 9mo, 2y. */
export function age(iso: string, now: number): string {
  const s = Math.max(0, (now - Date.parse(iso)) / 1000);
  if (!Number.isFinite(s) || s < 60) return "now";
  const m = s / 60;
  if (m < 60) return `${Math.floor(m)}m`;
  const h = m / 60;
  if (h < 24) return `${Math.floor(h)}h`;
  const d = h / 24;
  if (d < 7) return `${Math.floor(d)}d`;
  if (d < 30) return `${Math.floor(d / 7)}w`;
  if (d < 365) return `${Math.floor(d / 30)}mo`;
  return `${Math.floor(d / 365)}y`;
}

/**
 * Splits what is loaded into what each view lists. Archived conversations
 * leave Pinned and Recents and are listed only under Archived; pinned ones
 * head the sidebar and leave Recents. Order is kept: newest first.
 */
export function partition(
  rows: ThreadSummary[],
  pinned: string[],
  marks: Marks,
): { pinned: ThreadSummary[]; recents: ThreadSummary[]; archived: ThreadSummary[] } {
  const away = new Set(marks.archived);
  const pins = new Set(pinned);
  const byId = new Map(rows.map((t) => [t.id, t]));
  return {
    pinned: pinned
      .filter((id) => !away.has(id))
      .map((id) => byId.get(id))
      .filter((t): t is ThreadSummary => t !== undefined),
    recents: rows.filter((t) => !away.has(t.id) && !pins.has(t.id)),
    archived: marks.archived
      .map((id) => byId.get(id))
      .filter((t): t is ThreadSummary => t !== undefined),
  };
}

/**
 * Projects in the order the Projects view lists them: those with a loaded
 * conversation first, newest first, then the person's other projects, which
 * open to "No conversations".
 */
export function projectOrder(rows: ThreadSummary[], others: string[]): string[] {
  const seen = [...new Set(rows.map((t) => t.project || "~"))];
  return [...seen, ...others.filter((p) => !seen.includes(p))];
}

/**
 * The title the system gives a conversation from its first message until it
 * is named, the same as the server's runs.DeriveTitle: the first line without
 * the agents it tags at the start, capitalized, cut at a word under 60
 * characters.
 */
export function deriveTitle(first: string): string {
  const line = first.trim().split("\n")[0] ?? "";
  let words = line.split(/\s+/).filter(Boolean);
  while (words.length > 1 && words[0]!.startsWith("@")) words = words.slice(1);
  if (words.length === 0) return "";
  if (words.length === 1 && words[0]!.startsWith("@")) return words[0]!;
  let title = words.join(" ").replace(/[ ,;:-]+$/, "");
  const runes = [...title];
  if (runes.length > 60) {
    let cut = runes.slice(0, 60).join("");
    const i = cut.lastIndexOf(" ");
    if (i > 30) cut = cut.slice(0, i);
    title = `${cut.replace(/[ ,;:.-]+$/, "")}…`;
  }
  return title.charAt(0).toUpperCase() + title.slice(1);
}
