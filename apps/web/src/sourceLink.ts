import { type Reference, referenceOf } from "./trace.ts";

/**
 * The page a session's source is on, by the session's parent, when it is
 * one a person can open from the Sessions table: the GitHub issue or pull
 * request it was asked on.
 */
export function sessionSourceLink(parent: string): Reference | undefined {
  if (!parent.startsWith("github://")) return undefined;
  const ref = referenceOf(parent);
  return ref.kind === "github" && ref.url ? ref : undefined;
}
