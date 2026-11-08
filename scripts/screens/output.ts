import { resolve } from "node:path";

/** CI uploads from the repository root even when pnpm runs this package elsewhere. */
export function screenOutput(repo: string, configured?: string): string {
  return resolve(repo, configured ?? ".screens");
}
