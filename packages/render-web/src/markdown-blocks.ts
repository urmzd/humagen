/**
 * Markdown cut into pieces that parse to the same thing apart as together, so
 * an answer still being written parses only its last piece on each token
 * instead of all of it.
 *
 * A cut is made only where nothing can carry across it: after a blank line,
 * outside a code fence, before a line that starts at the margin and is not a
 * list item. Such a line closes every list and quote above it. Text whose
 * meaning reaches across the whole document (link or footnote definitions, an
 * HTML block that runs past blank lines) is left whole.
 */
export function markdownBlocks(text: string): string[] {
  if (WHOLE.test(text)) return [text];
  const blocks: string[] = [];
  let start = 0;
  let fence: { mark: string; size: number } | null = null;
  let blankBefore = false;
  let at = 0;
  while (at < text.length) {
    const end = text.indexOf("\n", at);
    const line = end === -1 ? text.slice(at) : text.slice(at, end);
    if (fence) {
      const close = FENCE_CLOSE.exec(line);
      if (close?.[1]?.[0] === fence.mark && close[1].length >= fence.size) fence = null;
      blankBefore = false;
    } else {
      if (blankBefore && at > start && startsBlock(line)) {
        blocks.push(text.slice(start, at));
        start = at;
      }
      const open = FENCE_OPEN.exec(line);
      // A backtick fence's info string holds no backtick; such a line is inline code.
      if (open?.[1] && !(open[1][0] === "`" && open[2]?.includes("`")))
        fence = { mark: open[1][0] as string, size: open[1].length };
      blankBefore = line.trim() === "";
    }
    if (end === -1) break;
    at = end + 1;
  }
  blocks.push(text.slice(start));
  return blocks;
}

// A definition anywhere changes how a link or footnote elsewhere reads, and
// these HTML blocks end at their own closing mark, not at a blank line.
const WHOLE =
  /^ {0,3}(?:\[[^\]\n]+\]:|<(?:pre|script|style|textarea|!--|\?|!\[CDATA\[|![A-Za-z]))/im;
// Indented four or more, a fence is inside a list item or is code already; its
// lines are indented too, and no cut is made before an indented line.
const FENCE_OPEN = /^ {0,3}(`{3,}|~{3,})(.*)$/;
const FENCE_CLOSE = /^\s*(`{3,}|~{3,})\s*$/;
const LIST_ITEM = /^(?:[-*+]|\d{1,9}[.)])(?:[ \t]|$)/;

/** A line at the margin that cannot continue or join what is above it. */
function startsBlock(line: string): boolean {
  if (line === "" || line[0] === " " || line[0] === "\t") return false;
  return !LIST_ITEM.test(line);
}
