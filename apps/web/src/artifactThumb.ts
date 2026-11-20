import type { Stored } from "./storage.ts";

const MAX_CHARS = 420;

const ENTITIES: Record<string, string> = {
  amp: "&",
  lt: "<",
  gt: ">",
  quot: '"',
  apos: "'",
  nbsp: " ",
};

/** The few named entities and the numbered ones, as the characters they stand for. The result is only ever shown as text. */
function decode(text: string): string {
  return text.replace(/&(#x[0-9a-f]+|#\d+|[a-z]+);/gi, (whole, name: string) => {
    if (name[0] !== "#") return ENTITIES[name.toLowerCase()] ?? whole;
    const code = /^#x/i.test(name) ? Number.parseInt(name.slice(2), 16) : Number(name.slice(1));
    return code > 0 && code <= 0x10ffff ? String.fromCodePoint(code) : whole;
  });
}

const isHtml = (file: Stored) => file.kind === "html" || file.mime === "text/html";
const isMarkdown = (file: Stored) =>
  file.mime.includes("markdown") || /\.(md|markdown)$/i.test(file.title);

/**
 * The start of a text file as plain words: an HTML file without its tags, a
 * Markdown file without its marks, and anything else (code, data, plain
 * text) as it is written, with its indentation.
 */
export function previewText(file: Stored): string {
  const raw = file.preview?.trim();
  if (!raw) return "";
  const verbatim = !isHtml(file) && !isMarkdown(file);
  const plain = isHtml(file)
    ? decode(
        raw
          // The preview is the start of the file, so a script, style, comment or tag may be cut off.
          // The head goes with them: its title is not the page's words, and the card has its own.
          .replace(/<(head|title|script|style)\b[\s\S]*?(<\/\1\s*>|$)/gi, " ")
          .replace(/<!--[\s\S]*?(-->|$)/g, " ")
          .replace(/<\/(h[1-6]|p|li|div|tr)>|<br\s*\/?>/gi, "\n")
          .replace(/<[^>]*(>|$)/g, " "),
      )
    : isMarkdown(file)
      ? raw
          .replace(/^#{1,6}\s+/gm, "")
          .replace(/[*`]{1,3}/g, "")
          // An underscore inside a word is part of a name, not emphasis.
          .replace(/(?<![A-Za-z0-9])_{1,3}|_{1,3}(?![A-Za-z0-9])/g, "")
      : raw;
  return plain
    .split("\n")
    .map((line) =>
      verbatim ? line.replace(/\t/g, "  ").trimEnd() : line.replace(/[ \t]+/g, " ").trim(),
    )
    .filter((line) => line.trim())
    .join("\n")
    .slice(0, MAX_CHARS);
}
