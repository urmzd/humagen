import type { defaultCopy } from "@humagen/components";
import type { Marks, ThreadSummary } from "./api.ts";

/**
 * How the sidebar lists conversations: newest first ("flat", the default),
 * under Today, Yesterday and Earlier ("recents"), by project, or the ones
 * put away.
 */
export type ListView = "flat" | "recents" | "projects" | "archived";

/** What a row's state mark says, most urgent first. */
export type RowStanding = "needs" | "running" | "failed" | "unread" | "";

/** Where a session started, as its row's leading mark shows it. */
export type RowSource = "console" | "slack" | "slack_dm" | "github" | "scheduled" | "assignment";

/**
 * Where a session started. The summary's own `source` names a Slack thread
 * or direct message, a pull request or a schedule; a run's origin is the
 * fallback, and anything else was started in the console or through the API.
 */
export function rowSource(t: Pick<ThreadSummary, "origin" | "source">): RowSource {
  if (t.source) return t.source;
  switch (t.origin) {
    case "slack":
      return "slack";
    case "github":
      return "github";
    case "cron":
    case "manual":
      return "scheduled";
    case "assignment":
      return "assignment";
  }
  return "console";
}

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

/** The words a row's state is said in (defaultCopy.sidebar.state). */
export type StateWords = Pick<
  typeof defaultCopy.sidebar.state,
  "waitingYou" | "waitingOther" | "waitingUnknown" | "failed" | "working" | "alert"
>;

/** The row's most pressing status and the detail behind its badge. */
export type RowState = {
  kind: "approval" | "question" | "waiting" | "failed" | "working" | "alert";
  tone: "warn" | "bad" | "neutral";
  text: string;
};

/** Session listings report a failure only while the latest turn failed. */
export function rowState(
  t: ThreadSummary,
  /** Passed in so this module stays plain TypeScript. */
  words: StateWords,
  opts: {
    session?: boolean;
    alert?: { priority: "urgent" | "normal" | "low"; title: string };
  } = {},
): RowState | undefined {
  // Something in it is parked. Whom on is the listing's to say, from the
  // reader's own inbox; without that it waits on someone.
  if ((t.waiting ?? 0) > 0) {
    // Only a listing that says the reader is asked makes it theirs: a missing
    // answer is no one's, never the reader's by default.
    const you = t.waiting_on_you === true;
    const detail = t.waiting_for ?? "";
    return {
      kind: t.waiting_kind ?? "waiting",
      tone: you ? "warn" : "neutral",
      text: you
        ? words.waitingYou(detail, t.waiting_kind)
        : t.waiting_unknown === true
          ? // The listing could not tell whom: said without naming anyone.
            words.waitingUnknown(detail, t.waiting_kind)
          : words.waitingOther(detail, t.waiting_kind),
    };
  }
  const failure = t.failure ?? (!opts.session && t.failed > 0 ? "" : undefined);
  if (failure !== undefined) return { kind: "failed", tone: "bad", text: words.failed(failure) };
  if (t.active > 0) return { kind: "working", tone: "neutral", text: words.working };
  if (opts.alert)
    return {
      kind: "alert",
      tone: opts.alert.priority === "urgent" ? "warn" : "neutral",
      text: words.alert(opts.alert.title),
    };
  return undefined;
}

// One rule for every age and relative time in the console: format.ts.
export { age } from "./format.ts";

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
export function deriveTitle(first: string, limit = 60): string {
  const line = first.trim().split("\n")[0] ?? "";
  let words = line.split(/\s+/).filter(Boolean);
  while (words.length > 1 && words[0]!.startsWith("@")) words = words.slice(1);
  if (words.length === 0) return "";
  if (words.length === 1 && words[0]!.startsWith("@")) return words[0]!;
  let title = words.join(" ").replace(/[ ,;:-]+$/, "");
  const runes = [...title];
  if (runes.length > limit) {
    let cut = runes.slice(0, limit).join("");
    const i = cut.lastIndexOf(" ");
    if (i > limit / 2) cut = cut.slice(0, i);
    title = `${cut.replace(/[ ,;:.-]+$/, "")}…`;
  }
  return title.charAt(0).toUpperCase() + title.slice(1);
}

/**
 * A session's placeholder title, the same as the server's runs.SessionTitle:
 * deriveTitle cut at a word under 40 characters, so a sidebar row shows it
 * whole until the generated title replaces it.
 */
export function placeholderTitle(first: string): string {
  return deriveTitle(first, 39);
}
