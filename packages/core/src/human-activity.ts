/**
 * What a person did by hand in a session's workbench since their last
 * message: a command they ran, something they did in the browser, a file
 * they edited, a change they kept or discarded. Each is recorded here, shown
 * above the composer where it can be removed, and folded into the next
 * message they send, so the agent reads one message that covers all of it.
 * Nothing here is sent anywhere until the person submits.
 *
 * The API has no field for it (SessionPostInput is text and uploads), so it
 * rides in the message's text as a section at its end, between two fixed
 * lines. `foldHumanActivity` writes the section and `splitHumanActivity`
 * reads it back, so a thread can draw it collapsed.
 *
 * The platform reads a message's text for tags (@agent, /skill, $skill) and
 * refuses control characters (pkg/threads). A command's output is full of
 * both, so the section's entries sit in a code block, which is never read
 * for tags, and a record is kept as plain text: no control characters, and
 * no backticks, which would end the block.
 */

/** Where in the workbench a person did something. */
export type HumanActivitySource = "terminal" | "browser" | "files" | "changes" | "other";

export const HUMAN_ACTIVITY_SOURCES: readonly HumanActivitySource[] = [
  "terminal",
  "browser",
  "files",
  "changes",
  "other",
];

/** One thing a person did by hand. */
export interface HumanActivityRecord {
  id: string;
  source: HumanActivitySource;
  /** One line that says what was done and how it came out. */
  summary: string;
  /** More for the agent to read: a command's output, a diff, an address. */
  detail?: string;
  /** When it was done, as an ISO time. */
  at: string;
}

/** What a tab gives to record something. The store fills in the rest. */
export interface HumanActivityInput {
  source: HumanActivitySource;
  summary: string;
  detail?: string;
  /** Defaults to now. */
  at?: string;
  /**
   * Replaces the record with the same source and key instead of adding
   * another, for something a person does over and over (saving one file).
   */
  key?: string;
}

/** The most records one conversation holds. Recording another drops the oldest. */
export const MAX_HUMAN_ACTIVITY = 50;
/** The most characters of a summary and of a detail that are kept. */
export const MAX_HUMAN_SUMMARY = 200;
export const MAX_HUMAN_DETAIL = 600;
/** The most characters the section adds to a message. */
export const MAX_HUMAN_SECTION = 4000;
/** The most characters the API takes in one message (pkg/threads MaxText). */
export const MAX_MESSAGE_TEXT = 8000;

const ELLIPSIS = "…";

function clip(text: string, max: number): string {
  return text.length > max ? `${text.slice(0, max - 1)}${ELLIPSIS}` : text;
}

/**
 * Text the platform takes inside a code block: escape sequences and other
 * control characters are dropped (a terminal's colors), line endings are
 * `\n`, and a backtick is written as an apostrophe.
 */
function plain(text: string): string {
  return (
    text
      .replace(/\r\n?/g, "\n")
      // biome-ignore lint/suspicious/noControlCharactersInRegex: control characters are what is being removed
      .replace(/\u001b\[[0-9;?]*[ -/]*[@-~]/g, "")
      // biome-ignore lint/suspicious/noControlCharactersInRegex: control characters are what is being removed
      .replace(/[\u0000-\u0008\u000b-\u001f\u007f-\u009f]/g, "")
      .replace(/`/g, "'")
  );
}

/** One line: whitespace runs, line breaks among them, become one space. */
function oneLine(text: string): string {
  return plain(text).replace(/\s+/g, " ").trim();
}

export interface HumanActivityStore {
  /** The records of a conversation, oldest first. The same array until they change. */
  list(conversation: string): readonly HumanActivityRecord[];
  /** Records something, and returns the record, or undefined when it says nothing. */
  add(conversation: string, input: HumanActivityInput): HumanActivityRecord | undefined;
  remove(conversation: string, id: string): void;
  /** Removes the records named, or every record of the conversation. */
  clear(conversation: string, ids?: readonly string[]): void;
  /** Calls back when a conversation's records change. Returns a way to stop. */
  subscribe(conversation: string, changed: () => void): () => void;
}

const NONE: readonly HumanActivityRecord[] = Object.freeze([]);

/** A store of its own, for a test or a host that keeps more than one. */
export function createHumanActivityStore(now: () => Date = () => new Date()): HumanActivityStore {
  const records = new Map<string, readonly HumanActivityRecord[]>();
  const keys = new Map<string, string>();
  const watchers = new Map<string, Set<() => void>>();
  let seq = 0;
  const set = (conversation: string, next: readonly HumanActivityRecord[]) => {
    if (next.length) records.set(conversation, next);
    else records.delete(conversation);
    for (const changed of [...(watchers.get(conversation) ?? [])]) changed();
  };
  return {
    list: (conversation) => records.get(conversation) ?? NONE,
    add(conversation, input) {
      const summary = clip(oneLine(input.summary), MAX_HUMAN_SUMMARY);
      if (!conversation || !summary) return undefined;
      const detail = input.detail === undefined ? "" : plain(input.detail).trim();
      seq += 1;
      const record: HumanActivityRecord = {
        id: `h${seq.toString(36)}`,
        source: HUMAN_ACTIVITY_SOURCES.includes(input.source) ? input.source : "other",
        summary,
        ...(detail ? { detail: clip(detail, MAX_HUMAN_DETAIL) } : {}),
        at: input.at ?? now().toISOString(),
      };
      let rest = records.get(conversation) ?? NONE;
      if (input.key) {
        const slot = `${conversation}\n${record.source}\n${input.key}`;
        const earlier = keys.get(slot);
        if (earlier) rest = rest.filter((r) => r.id !== earlier);
        keys.set(slot, record.id);
      }
      set(conversation, [...rest, record].slice(-MAX_HUMAN_ACTIVITY));
      return record;
    },
    remove(conversation, id) {
      const rest = records.get(conversation) ?? NONE;
      if (rest.some((r) => r.id === id))
        set(
          conversation,
          rest.filter((r) => r.id !== id),
        );
    },
    clear(conversation, ids) {
      const rest = records.get(conversation) ?? NONE;
      if (!rest.length) return;
      set(conversation, ids ? rest.filter((r) => !ids.includes(r.id)) : NONE);
    },
    subscribe(conversation, changed) {
      const watching = watchers.get(conversation) ?? new Set();
      watchers.set(conversation, watching);
      watching.add(changed);
      return () => {
        watching.delete(changed);
        if (!watching.size) watchers.delete(conversation);
      };
    },
  };
}

/** The store the console's workbench tabs and composer share. */
export const humanActivity: HumanActivityStore = createHumanActivityStore();

// The section's fixed lines. They are part of stored messages: changing one
// leaves older messages drawn as plain text, so add a reader before changing.
const OPEN = /^\[Done by hand since my last message: (\d+)\]$/;
const openLine = (n: number) => `[Done by hand since my last message: ${n}]`;
const CLOSE = "[End of done by hand]";
const MORE = /^\((\d+) more did not fit in this message\)$/;
const moreLine = (n: number) => `(${n} more did not fit in this message)`;
const ENTRY = /^(\d+)\. (Terminal|Browser|Files|Changes|Other) (\S+): (.*)$/;
const INDENT = "    ";
const FENCE = /^`{3,}$/;

/**
 * The run of backticks that opens and closes the section's code block: longer
 * than any run in the person's own words, so nothing they typed pairs with it.
 */
function fenceFor(text: string): string {
  const longest = Math.max(2, ...(text.match(/`+/g) ?? []).map((run) => run.length));
  return "`".repeat(longest + 1);
}

const LABEL: Record<HumanActivitySource, string> = {
  terminal: "Terminal",
  browser: "Browser",
  files: "Files",
  changes: "Changes",
  other: "Other",
};
const SOURCE_OF: Record<string, HumanActivitySource> = Object.fromEntries(
  HUMAN_ACTIVITY_SOURCES.map((s) => [LABEL[s], s]),
);

/** A time to the minute in UTC, which holds no spaces: 2026-11-19T14:02Z. */
function stamp(at: string): string {
  const time = new Date(at);
  return Number.isNaN(time.getTime()) ? "unknown" : `${time.toISOString().slice(0, 16)}Z`;
}

function entry(n: number, r: HumanActivityRecord, withDetail: boolean): string {
  const head = `${n}. ${LABEL[r.source]} ${stamp(r.at)}: ${oneLine(r.summary)}`;
  if (!withDetail || !r.detail) return head;
  // Every line of a detail is indented, so none can read as one of the section's own lines.
  const lines = plain(r.detail)
    .split("\n")
    .map((line) => `${INDENT}${line}`.trimEnd() || INDENT);
  return `${head}\n${lines.join("\n")}`;
}

export interface FoldedHumanActivity {
  /** The message to send: the person's words, then the section. */
  text: string;
  /** The records the section lists, to clear once the message is posted. */
  included: string[];
  /** How many records did not fit. The section says how many; they stay for the next message. */
  omitted: number;
  /** How many records were written without their detail to make room. */
  trimmed: number;
}

/**
 * The message a person sends, with what they did by hand since their last
 * one as a section at its end. With nothing recorded the text is unchanged.
 *
 * The section never makes a message longer than the API takes: it has
 * `MAX_HUMAN_SECTION` characters, or what the person's own words leave of
 * `max` when that is less. Records are written oldest first. One that does
 * not fit with its detail is written without it; one that does not fit at
 * all is left out with every record after it, and a last line says how many
 * were. Those stay recorded, so they go with the message after. A message
 * already at the limit is sent as typed, with nothing folded in and
 * `omitted` saying so.
 */
export function foldHumanActivity(
  text: string,
  records: readonly HumanActivityRecord[],
  max: number = MAX_MESSAGE_TEXT,
): FoldedHumanActivity {
  if (!records.length) return { text, included: [], omitted: 0, trimmed: 0 };
  const total = records.length;
  const fence = fenceFor(text);
  const frame = `\n\n${openLine(total)}\n${fence}\n${fence}\n${CLOSE}`.length;
  // Room is kept for the line that counts what did not fit.
  const reserve = moreLine(total).length + 1;
  const budget = Math.min(MAX_HUMAN_SECTION, max - text.length) - frame - reserve;
  const lines: string[] = [];
  const included: string[] = [];
  let used = 0;
  let trimmed = 0;
  for (const [index, record] of records.entries()) {
    const full = entry(index + 1, record, true);
    const bare = entry(index + 1, record, false);
    const pick = used + full.length + 1 <= budget ? full : bare;
    if (used + pick.length + 1 > budget) break;
    if (pick !== full) trimmed += 1;
    lines.push(pick);
    included.push(record.id);
    used += pick.length + 1;
  }
  if (!included.length) return { text, included: [], omitted: total, trimmed: 0 };
  const omitted = total - included.length;
  if (omitted) lines.push(moreLine(omitted));
  return {
    text: `${text}\n\n${openLine(included.length)}\n${fence}\n${lines.join("\n")}\n${fence}\n${CLOSE}`,
    included,
    omitted,
    trimmed,
  };
}

/** One entry of a message's section, as it was written. */
export interface FoldedEntry {
  source: HumanActivitySource;
  /** The time as the section holds it, to the minute in UTC. */
  at: string;
  summary: string;
  detail?: string;
}

export interface SplitHumanActivity {
  /** The person's own words, without the section. */
  text: string;
  activity?: {
    /** How many things the section lists. */
    count: number;
    entries: FoldedEntry[];
    /** How many more the message had no room for. */
    omitted: number;
  };
}

/**
 * A message's own words and the section folded into it, if it has one. The
 * section is read only when it is whole and ends the message, exactly as
 * `foldHumanActivity` writes it; anything else is the message's text.
 */
export function splitHumanActivity(text: string): SplitHumanActivity {
  const whole: SplitHumanActivity = { text };
  if (!text.endsWith(`\n${CLOSE}`)) return whole;
  const lines = text.split("\n");
  let open = -1;
  for (let i = lines.length - 2; i >= 0; i--)
    if (OPEN.test(lines[i] as string)) {
      open = i;
      break;
    }
  // The section follows a blank line, as it was written.
  if (open < 1 || lines[open - 1] !== "") return whole;
  const count = Number((lines[open] as string).match(OPEN)?.[1]);
  // The entries sit in a code block: one fence under the opening line, the same above the closing one.
  const fence = lines[open + 1] as string;
  if (lines.length - open < 5 || !FENCE.test(fence) || lines.at(-2) !== fence) return whole;
  const entries: FoldedEntry[] = [];
  let omitted = 0;
  for (const line of lines.slice(open + 2, -2)) {
    const head = line.match(ENTRY);
    const last = entries.at(-1);
    if (head && Number(head[1]) === entries.length + 1 && !omitted) {
      entries.push({
        source: SOURCE_OF[head[2] as string] ?? "other",
        at: head[3] as string,
        summary: head[4] as string,
      });
    } else if (line.startsWith(INDENT) && last && !omitted) {
      const more = line.slice(INDENT.length);
      last.detail = last.detail === undefined ? more : `${last.detail}\n${more}`;
    } else if (MORE.test(line) && !omitted) {
      omitted = Number(line.match(MORE)?.[1]);
    } else return whole;
  }
  if (!entries.length || entries.length !== count) return whole;
  return {
    text: lines.slice(0, open - 1).join("\n"),
    activity: { count, entries, omitted },
  };
}

/**
 * Posts a message with what its conversation's person did by hand folded in,
 * and clears what the message carried once it is posted. A post that fails
 * clears nothing, so a retry carries it again. What is folded in is what is
 * recorded when the message is posted, which for a message queued behind a
 * reply is when its turn comes.
 */
export async function postWithHumanActivity<T>(
  conversation: string,
  text: string,
  post: (text: string) => Promise<T>,
  store: HumanActivityStore = humanActivity,
): Promise<T> {
  const folded = foldHumanActivity(text, store.list(conversation));
  const posted = await post(folded.text);
  if (folded.included.length) store.clear(conversation, folded.included);
  return posted;
}
