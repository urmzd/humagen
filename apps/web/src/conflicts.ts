import { ApiError } from "./api.ts";

/**
 * What a 409 from a thread route is about, from the platform's words for it
 * (internal/httpapi threadProblem): the thread changed since it was read, or
 * a conversation an agent has run in stays in its project or its channel.
 * Anything else is not one of these. The console says each in its own words
 * and never shows the server's, which name ids and versions.
 */
export function conflictOf(e: unknown): "version" | "project" | "channel" | null {
  if (!(e instanceof ApiError) || e.status !== 409) return null;
  const m = e.message;
  if (m.includes("changed since it was read")) return "version";
  if (m.includes("stays in its project")) return "project";
  if (m.includes("stays in its channel")) return "channel";
  return null;
}
