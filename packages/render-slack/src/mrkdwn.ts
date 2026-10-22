/**
 * Convert the standard-markdown subset used by Text/Context/Callout into Slack
 * mrkdwn. Long-form model output skips this and goes in a `markdown` block.
 */

import { TIME_TOKEN } from "@humagen/core";

/** Slack date token: shown in each reader's own timezone and format. */
function slackDate(iso: string): string {
  const ms = Date.parse(iso);
  if (Number.isNaN(ms)) return iso;
  return `<!date^${Math.floor(ms / 1000)}^{date_short_pretty} at {time}|${plainDate(iso)}>`;
}

/** Locale-free fallback for places that can't localize (fallback text, plain_text). */
export function plainDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return `${d.toISOString().slice(0, 16).replace("T", " ")} UTC`;
}

/** A user or channel mention exactly as Slack writes it. Nothing else in angle brackets is kept. */
const SAFE_TOKEN = /(<@[UW][A-Z0-9]+(?:\|[^<>]*)?>|<#C[A-Z0-9]+(?:\|[^<>]*)?>)/;

/**
 * Text for Slack, escaped so it can't ping or link: `&`, `<` and `>` become
 * entities, which also defuses `<!channel>`, `<!here>`, `<!everyone>`,
 * `<!subteam^…>` and `<url|label>` links. Only user mentions (`<@U123>`) and
 * channel links (`<#C123>`) are kept. Use it for words a person typed, and for
 * every fallback `text`, which Slack parses the same way.
 */
export function escapeSlackText(text: string): string {
  return text
    .split(SAFE_TOKEN)
    .map((piece, i) => (i % 2 === 1 ? piece : escapeHtml(piece)))
    .join("");
}

/**
 * Announcement text for Slack, which pings nobody: every `&`, `<` and `>` is
 * an entity, so user mentions (`<@U123>`), `<!here>`, `<!channel>`,
 * `<!everyone>`, `<!subteam^…>` and links are shown as typed, and a bare
 * `@here`, `@channel` or `@everyone` is broken with a zero-width space.
 * Announcements reach a whole project and may be written by a model, so,
 * unlike `escapeSlackText`, not even a user mention is kept.
 */
export function escapeAnnouncement(text: string): string {
  return escapeHtml(text).replace(/@(here|channel|everyone)\b/gi, "@\u200B$1");
}

/** Whether the whole string is one user mention, such as `<@U123>`. */
export function isMention(text: string): boolean {
  return /^<@[UW][A-Z0-9]+(?:\|[^<>]*)?>$/.test(text);
}

/**
 * A name made safe to show in bold: escaped like any typed text, and with the
 * characters that would end or start formatting swapped for look-alikes.
 */
export function plainName(text: string): string {
  return escapeSlackText(text)
    .replaceAll("*", "\u2217")
    .replaceAll("_", "\uFF3F")
    .replaceAll("~", "\u223C")
    .replaceAll("`", "\u02CB");
}

function escapeHtml(text: string): string {
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

export function toMrkdwn(markdown: string): string {
  const links: string[] = [];
  const BOLD = "\uE000";
  let out = escapeHtml(markdown)
    // Links first so their text isn't reformatted. Escaping turned nothing in URLs we keep.
    .replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_m, text: string, url: string) => {
      links.push(`<${url.replace(/&amp;/g, "&")}|${text}>`);
      return `\uE001${links.length - 1}\uE001`;
    })
    .replace(/\*\*(.+?)\*\*/g, `${BOLD}$1${BOLD}`)
    .replace(/(^|[^*])\*([^*\n]+)\*(?!\*)/g, "$1_$2_")
    .replace(/~~(.+?)~~/g, "~$1~");
  out = out.replaceAll(BOLD, "*").replace(TIME_TOKEN, (_m, iso: string) => slackDate(iso));
  return out.replace(/\uE001(\d+)\uE001/g, (_m, i: string) => links[Number(i)] ?? "");
}

/** Strip markdown to plain text for top-level fallback `text` and plain_text fields. */
export function toPlain(markdown: string): string {
  return markdown
    .replace(TIME_TOKEN, (_m, iso: string) => plainDate(iso))
    .replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, "$1 ($2)")
    .replace(/\*\*(.+?)\*\*/g, "$1")
    .replace(/`([^`]+)`/g, "$1")
    .replace(/~~(.+?)~~/g, "$1");
}
