import { ApiError, api } from "./api.ts";

/**
 * Adds files to a source as documents. Files are read here and sent as text,
 * so only text files up to 900 KB are added. Returns why each file that was
 * left out was left out.
 */
export async function addTextFiles(source: string, files: Iterable<File>): Promise<string[]> {
  const skipped: string[] = [];
  for (const file of files) {
    const text = await file.text();
    if (file.size > 900 * 1024) skipped.push(`${file.name} is larger than 900 KB`);
    else if (text.includes("\u0000")) skipped.push(`${file.name} is not a text file`);
    else {
      try {
        await api.addDocument(source, { title: file.name.replace(/\.[^.]+$/, ""), uri: "", text });
      } catch (e) {
        skipped.push(`${file.name}: ${e instanceof ApiError ? e.message : "could not be added"}`);
      }
    }
  }
  return skipped;
}
