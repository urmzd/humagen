import { doing } from "./access.ts";
import type { ToolCredential, ToolCredentials } from "./api.ts";

/** How a credential can be given here. */
export type Way = "reference" | "value";

/**
 * The ways a person may give a credential in this workspace: a reference when
 * the operator listed somewhere references may start, a value when the
 * workspace keeps them. Neither means nobody can connect anything yet.
 */
export function ways(t: Pick<ToolCredentials, "values" | "references">): Way[] {
  const out: Way[] = [];
  if (t.references.length > 0) out.push("reference");
  if (t.values) out.push("value");
  return out;
}

/** How you stand with one tool, in words. A value is never shown, because it never comes back. */
export function standing(c: ToolCredential): string {
  if (!c.connected) return "Not connected. An agent cannot use these tools for you.";
  if (c.kind === "value") return "Connected. Your credential is stored with humagen.";
  return `Connected. Humagen reads ${c.ref ?? "your reference"} each time a run uses it, and stores only the reference.`;
}

/** Where your credential goes when a run uses it. */
export function destination(c: ToolCredential): string {
  if (!c.endpoint) return "It runs on the server, which is handed your credential.";
  let host = c.endpoint;
  try {
    host = new URL(c.endpoint).host;
  } catch {
    // Shown as written.
  }
  return `Your credential is sent to ${host}.`;
}

/** What it offers, in words. */
export function offers(c: ToolCredential): string {
  if (c.tools === 0) return "No tools accepted yet";
  return c.tools === 1 ? "1 tool" : `${c.tools} tools`;
}

/**
 * Whether a reference starts where the workspace allows. The server decides,
 * and says so the same way whatever the reason; this only says it sooner.
 */
export function startsAllowed(ref: string, starts: string[]): boolean {
  const r = ref.trim();
  return r !== "" && starts.some((s) => s !== "" && r.startsWith(s));
}

/** The hint under the reference field: where references may live here. */
export function whereReferences(starts: string[]): string {
  if (starts.length === 0) return "This workspace lets nobody reference a credential.";
  return `A reference here starts with ${starts.join(", ")}. Only the reference is stored.`;
}

/**
 * The permission giving or removing a credential takes: a credential is used
 * only by a run that acts for its person, so connecting one takes what
 * starting a run does.
 */
export const CONNECT_PERMISSION = "runs:write";

/** Said in place of Connect to someone who may not connect. */
export const cannotConnect = `Connecting a credential takes permission to ${doing[CONNECT_PERMISSION]}, which you don't have. An admin can give it to you.`;
