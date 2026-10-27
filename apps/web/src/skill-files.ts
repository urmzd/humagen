import type {
  LibrarySkillDefinition,
  LibrarySkillFile,
} from "../../../packages/client/src/generated/types.ts";

export const MAX_SKILL_FILES = 64;
export const MAX_SKILL_FILE_BYTES = 128 * 1024;
export const MAX_SKILL_BYTES = 512 * 1024;

export function encodeSkillFile(path: string, bytes: Uint8Array): LibrarySkillFile {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return { path, data: btoa(binary) };
}
export const textSkillFile = (path: string, text: string) =>
  encodeSkillFile(path, new TextEncoder().encode(text));

export function skillFileBytes(file: LibrarySkillFile): Uint8Array {
  return Uint8Array.from(atob(file.data), (char) => char.charCodeAt(0));
}
export function skillFileText(file: LibrarySkillFile): string | null {
  try {
    const text = new TextDecoder("utf-8", { fatal: true }).decode(skillFileBytes(file));
    return text.includes("\0") ? null : text;
  } catch {
    return null;
  }
}
export function validSkillPath(path: string): boolean {
  return (
    path.length > 0 &&
    path.length <= 256 &&
    !path.includes("\\") &&
    !path.includes("\0") &&
    path.split("/").every((part) => part !== "" && part !== "." && part !== "..")
  );
}

export function skillFiles(definition: LibrarySkillDefinition): LibrarySkillFile[] {
  if (definition.files) return definition.files.map((file) => ({ ...file }));
  return [
    textSkillFile(
      "SKILL.md",
      `---\nname: ${definition.name}\ndescription: ${JSON.stringify(definition.description)}\n${definition.tools.length ? `allowed-tools: ${JSON.stringify(definition.tools.join(" "))}\n` : ""}---\n\n${definition.instructions ?? ""}\n`,
    ),
  ];
}

/** The selected directory is a single skill; paths are relative to that root. */
export async function importSkillFolder(
  files: File[],
): Promise<{ name: string; files: LibrarySkillFile[] }> {
  if (files.length === 0 || files.length > MAX_SKILL_FILES)
    throw new Error(`Choose one skill folder with 1 to ${MAX_SKILL_FILES} files.`);
  const root = files[0]!.webkitRelativePath.split("/")[0]!;
  let total = 0;
  const seen = new Set<string>();
  for (const file of files) {
    if (!file.webkitRelativePath.startsWith(`${root}/`))
      throw new Error("Choose a single skill folder.");
    const path = file.webkitRelativePath.slice(root.length + 1);
    if (!validSkillPath(path) || seen.has(path))
      throw new Error(`Invalid or duplicate file path: ${path}`);
    seen.add(path);
    total += file.size;
    if (file.size > MAX_SKILL_FILE_BYTES || total > MAX_SKILL_BYTES)
      throw new Error("A skill can contain up to 512 KiB, with 128 KiB per file.");
  }
  if (!seen.has("SKILL.md"))
    throw new Error("The selected folder needs a SKILL.md file at its root.");
  return {
    name: root,
    files: await Promise.all(
      files.map(async (file) =>
        encodeSkillFile(
          file.webkitRelativePath.slice(root.length + 1),
          new Uint8Array(await file.arrayBuffer()),
        ),
      ),
    ),
  };
}
