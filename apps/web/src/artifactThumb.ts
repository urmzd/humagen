import type { Stored } from "./storage.ts";

const MAX_CHARS = 420;

/** The start of a text file as plain words: tags and Markdown marks are left out. */
export function previewText(file: Stored): string {
  const raw = file.preview?.trim();
  if (!raw) return "";
  const plain =
    file.kind === "html" || file.mime === "text/html"
      ? raw
          .replace(/<(script|style)[\s\S]*?<\/\1>/gi, " ")
          .replace(/<\/(h[1-6]|p|li|div|tr)>/gi, "\n")
          .replace(/<[^>]+>/g, " ")
      : raw.replace(/^#{1,6}\s+/gm, "").replace(/[*_`]{1,3}/g, "");
  return plain
    .split("\n")
    .map((line) => line.replace(/[ \t]+/g, " ").trim())
    .filter(Boolean)
    .join("\n")
    .slice(0, MAX_CHARS);
}
