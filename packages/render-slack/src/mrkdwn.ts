/**
 * Convert the standard-markdown subset used by Text/Context/Callout into Slack
 * mrkdwn. Long-form model output skips this and goes in a `markdown` block.
 */

import { TIME_TOKEN } from "@agent-ui/core";

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
