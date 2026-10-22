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

/**
 * What a message did, kept for the conversation it started: Home and a
 * channel start one and then open it at its address, where the line about
 * the first message is said as a later post's is.
 */
const said = new Map<string, string>();

export function rememberSaid(thread: string, line: string): void {
  if (line) said.set(thread, line);
  else said.delete(thread);
}

export function saidFor(thread: string): string {
  return said.get(thread) ?? "";
}
