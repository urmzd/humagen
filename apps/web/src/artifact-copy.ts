import type { Stored } from "./storage.ts";

export const MAX_EDITABLE_ARTIFACT_BYTES = 2 * 1024 * 1024;

/** Eligibility is checked again against the actual bytes before saving a copy. */
export function canEditArtifact(file: Stored): boolean {
  return (
    file.size >= 0 &&
    file.size <= MAX_EDITABLE_ARTIFACT_BYTES &&
    (file.kind === "code" ||
      file.kind === "html" ||
      file.mime.startsWith("text/") ||
      /^application\/(?:json|ld\+json|javascript|x-javascript|xml|yaml|x-yaml)(?:;|$)/i.test(
        file.mime,
      ) ||
      /\.(?:md|mdx|txt|json|jsonl|csv|tsv|html?|css|scss|[cm]?[jt]sx?|py|go|rs|java|sh|bash|zsh|sql|ya?ml|toml|xml|diff|patch|log)$/i.test(
        file.title,
      ))
  );
}

function copyTitle(title: string): string {
  const name = title.split(/[\\/]/).at(-1) ?? "";
  return (
    name
      .replace(/[^a-zA-Z0-9._-]/g, "_")
      .replace(/^\.+/, "")
      .slice(-128) || "artifact.txt"
  );
}

async function responseError(response: Response, fallback: string): Promise<Error> {
  const body = (await response.json().catch(() => null)) as { error?: string } | null;
  return new Error(body?.error || fallback);
}

/** Copy an exact immutable text artifact into a new, editable conversation file. */
export async function copyArtifactToWorkspace(
  file: Stored,
  thread: string,
  signal?: AbortSignal,
): Promise<string> {
  if (file.thread !== thread) throw new Error("The artifact is not in this conversation.");
  if (!canEditArtifact(file)) throw new Error("This file cannot be opened as an editable copy.");
  const response = await fetch(`/api/artifacts/${encodeURIComponent(file.id)}/content`, {
    credentials: "same-origin",
    signal,
  });
  if (!response.ok)
    throw await responseError(response, "The artifact content could not be loaded.");
  const chunks: Uint8Array[] = [];
  let size = 0;
  const reader = response.body?.getReader();
  if (reader) {
    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        size += value.byteLength;
        if (size > MAX_EDITABLE_ARTIFACT_BYTES)
          throw new Error("Editable copies are limited to files of 2 MB.");
        chunks.push(value);
      }
    } finally {
      await reader.cancel().catch(() => {});
      reader.releaseLock();
    }
  }
  const bytes = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  let content: string;
  try {
    // Keep a UTF-8 BOM when present so the saved copy preserves the original bytes.
    content = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(bytes);
  } catch {
    throw new Error("Only UTF-8 text files can be opened as editable copies.");
  }
  if (bytes.some((byte) => (byte < 32 && byte !== 9 && byte !== 10 && byte !== 13) || byte === 127))
    throw new Error("Binary files cannot be opened as editable copies.");
  const path = `copies/${crypto.randomUUID()}/${copyTitle(file.title)}`;
  const saved = await fetch(`/api/threads/${encodeURIComponent(thread)}/files/content`, {
    method: "PUT",
    credentials: "same-origin",
    signal,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path, content, revision: "" }),
  });
  if (!saved.ok) throw await responseError(saved, "Could not create an editable copy.");
  return path;
}
