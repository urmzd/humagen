import type { QueuedUpload } from "@humagen/core";

const MAX_FILES = 5;
const MAX_TEXT_BYTES = 200 * 1024;
const MAX_BYTES = 25 * 1024 * 1024;
const TEXT_EXT =
  /\.(txt|md|markdown|csv|tsv|json|ya?ml|toml|xml|html?|css|log|sql|sh|py|go|rs|ts|tsx|js|jsx|java|rb|c|h|cpp)$/i;
const MEDIA_EXT = /\.(png|jpe?g|gif|webp|wav|mp3|m4a|ogg|webm|mp4|mov|pdf|docx|pptx|xlsx)$/i;
export interface Attached {
  id: string;
  name: string;
  text: string;
  data?: string;
  kind?: string;
  bytes?: number;
}

export async function readAttached(
  picked: FileList | File[],
  already: Attached[],
): Promise<{ files: Attached[]; skipped: string[] }> {
  const files = [...already];
  const skipped: string[] = [];
  let total = files.reduce((n, f) => n + (f.bytes ?? f.text.length), 0);
  for (const file of Array.from(picked)) {
    const textual =
      file.type.startsWith("text/") ||
      /json|xml|yaml|javascript/.test(file.type) ||
      TEXT_EXT.test(file.name);
    const media = /^(image|audio|video)\//.test(file.type) || MEDIA_EXT.test(file.name);
    const id = `${file.name}:${file.size}:${file.lastModified}`;
    if (files.length >= MAX_FILES) skipped.push(`${file.name}: at most ${MAX_FILES} files`);
    else if (!textual && !media) skipped.push(`${file.name}: unsupported file type`);
    else if (total + file.size > MAX_BYTES)
      skipped.push(`${file.name}: attachments exceed 25 MB in total`);
    else if (textual && file.size > MAX_TEXT_BYTES)
      skipped.push(`${file.name}: text files are limited to 200 KB`);
    else if (textual) {
      const text = await file.text();
      if (text.includes("\u0000")) skipped.push(`${file.name}: invalid text file`);
      else {
        files.push({
          id,
          name: file.name,
          text,
          data: encodeBytes(new Uint8Array(await file.arrayBuffer())),
          bytes: file.size,
        });
        total += file.size;
      }
    } else {
      const bytes = new Uint8Array(await file.arrayBuffer());
      files.push({ id, name: file.name, text: "", data: encodeBytes(bytes), bytes: file.size });
      total += file.size;
    }
  }
  return { files, skipped };
}
export function withAttached(text: string, files: Attached[]): string {
  return [
    text.trim(),
    ...files
      .filter((f) => f.data === undefined)
      .map((f) => `Attached file: ${f.name}\n\`\`\`\n${f.text}\n\`\`\``),
  ]
    .filter(Boolean)
    .join("\n\n");
}
export function uploadsOf(files: Attached[]): QueuedUpload[] {
  return files
    .filter((f): f is Attached & { data: string } => f.data !== undefined)
    .map((f) => ({ name: f.name, data: f.data }));
}

function encodeBytes(bytes: Uint8Array): string {
  let binary = "";
  for (let i = 0; i < bytes.length; i += 32768)
    binary += String.fromCharCode(...bytes.subarray(i, i + 32768));
  return btoa(binary);
}
