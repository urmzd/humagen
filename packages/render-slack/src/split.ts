/**
 * Break markdown that has outgrown one Slack message so both halves still
 * render: an open code fence is closed and reopened, a table's header rows are
 * repeated on the continuation, and a table never passes Slack's row limit.
 */

const FENCE = /^\s*(`{3,}|~{3,})/;
const TABLE_ROW = /^\s*\|.*\|\s*$/;
const TABLE_SEPARATOR = /^\s*\|[\s:|-]*-[\s:|-]*\|\s*$/;
/** Room kept back for the fence closer a break may add. */
const CLOSER_ROOM = 8;
/** Slack renders 100 rows of a table and silently drops the rest. */
const TABLE_ROWS = 100;

interface Scan {
  fence: { open: string; close: string } | null;
  /** The table the text ends in: its lines so far and where it starts. */
  rows: string[];
  rowsStart: number;
  /** Where a row would pass the table limit, or -1. */
  overflow: number;
}

/** Walk the lines of `text`. A last line with no newline yet only counts if it looks like a row. */
function scan(text: string): Scan {
  const s: Scan = { fence: null, rows: [], rowsStart: 0, overflow: -1 };
  const lines = text.split("\n");
  let offset = 0;
  lines.forEach((line, i) => {
    const partial = i === lines.length - 1;
    const marker = FENCE.exec(line)?.[1];
    if (partial) {
      // The separator isn't a rendered row, so a full table is the limit plus one line.
      const row = !s.fence && line.trimStart().startsWith("|");
      if (row && s.rows.length > TABLE_ROWS && s.overflow === -1) s.overflow = offset;
    } else if (marker) {
      s.fence = s.fence ? null : { open: line.trim(), close: marker };
      s.rows = [];
    } else if (!s.fence && TABLE_ROW.test(line)) {
      if (s.rows.length === 0) s.rowsStart = offset;
      if (s.rows.length > TABLE_ROWS && s.overflow === -1) s.overflow = offset;
      s.rows.push(line);
    } else if (!s.fence) {
      s.rows = [];
    }
    offset += line.length + 1;
  });
  return s;
}

/** Prefer a paragraph, then a line, then a word; never cut before `from`. */
function cutPoint(text: string, room: number, from: number): number {
  const window = text.slice(0, Math.max(room, 1));
  const paragraph = window.lastIndexOf("\n\n");
  // A paragraph break is only worth it when it doesn't waste most of the message.
  if (paragraph >= 0 && paragraph + 2 > from && paragraph + 2 >= from + (room - from) / 2) {
    return paragraph + 2;
  }
  const line = window.lastIndexOf("\n") + 1;
  if (line > from) return line;
  if (from > 0) return from;
  const space = window.lastIndexOf(" ") + 1;
  return space > 0 ? space : window.length;
}

function breakAt(text: string, at: number, from: number, room: number) {
  let cut = at;
  const { fence, rows, rowsStart } = scan(text.slice(0, cut));
  if (fence) {
    return {
      head: `${text.slice(from, cut).replace(/\n$/, "")}\n${fence.close}`,
      tail: `${fence.open}\n${text.slice(cut)}`,
    };
  }
  let lead = "";
  if (rows.length > 0 && text.slice(cut).trimStart().startsWith("|")) {
    const [header, separator] = rows;
    if (rows.length <= 2 && rowsStart > from) {
      // Only the header made it: move the whole table to the next message.
      cut = rowsStart;
    } else if (
      header &&
      separator &&
      TABLE_SEPARATOR.test(separator) &&
      header.length + separator.length < room / 4
    ) {
      lead = `${header}\n${separator}\n`;
    }
  }
  return { head: text.slice(from, cut), tail: lead + text.slice(cut) };
}

/**
 * Split `text` so the first part fits in `room` characters. `from` marks text
 * already sent and unchangeable (a live stream): the cut never lands before it,
 * and `head` holds only what still has to be sent. `tail` starts the next message.
 */
export function breakMarkdown(
  text: string,
  room: number,
  from = 0,
): { head: string; tail: string } {
  return breakAt(text, cutPoint(text, room - CLOSER_ROOM, from), from, room);
}

/**
 * Where `text` must continue in a new message, or null while it still fits.
 * Past `soft` it breaks at the first line end it sees, so a live stream is cut
 * between lines rather than wherever the hard limit happens to fall.
 */
export function nextBreak(
  text: string,
  room: number,
  { from = 0, soft = room }: { from?: number; soft?: number } = {},
): { head: string; tail: string } | null {
  const { overflow } = scan(text);
  if (overflow >= from && overflow <= room) return breakAt(text, overflow, from, room);
  if (text.length > room) return breakMarkdown(text, room, from);
  if (text.length > soft) {
    const line = text.indexOf("\n", Math.max(soft, from) - 1) + 1;
    // Wait for what follows the line end: it decides whether a table continues.
    if (line > 0 && line < text.length) return breakAt(text, line, from, room);
  }
  return null;
}
