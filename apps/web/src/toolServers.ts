import type { PersonContext, PersonContextInput, PersonToolServer } from "./api.ts";
import { startsAllowed } from "./connections.ts";

/** The most tool servers one person adds, as the server holds it. */
export const MAX_TOOL_SERVERS = 16;

const NAME = /^[a-z0-9][a-z0-9-]{0,62}$/;

/** The host a server is on, as a run's tool_servers ceiling lists it, or the address as written. */
export function hostOf(url: string): string {
  try {
    return new URL(url).hostname.toLowerCase();
  } catch {
    return url;
  }
}

/**
 * Why a server cannot be added, or null when it can. The server decides; this
 * only says it sooner, in the same terms.
 */
export function toolServerProblem(
  s: PersonToolServer,
  existing: PersonToolServer[],
  starts: string[],
): string | null {
  if (existing.length >= MAX_TOOL_SERVERS)
    return `You can add at most ${MAX_TOOL_SERVERS} tool servers.`;
  if (!NAME.test(s.name)) return "A name is lower case letters, digits and dashes, at most 63.";
  if (existing.some((o) => o.name === s.name))
    return `You already have a tool server named ${s.name}.`;
  let u: URL;
  try {
    u = new URL(s.url);
  } catch {
    return "The address is an https URL.";
  }
  if (u.protocol !== "https:" || u.username !== "" || u.password !== "" || u.hostname === "")
    return "The address is an https URL with no user or password in it.";
  const ref = s.credential?.trim() ?? "";
  if (ref !== "" && !startsAllowed(ref, starts))
    return "That is not somewhere this workspace lets references live.";
  return null;
}

/** The write that adds s, made from the context as read. Run settings are kept. */
export function withToolServer(c: PersonContext, s: PersonToolServer): PersonContextInput {
  const ref = s.credential?.trim();
  const added: PersonToolServer = { name: s.name, url: s.url.trim() };
  if (ref) added.credential = ref;
  return { run: c.run, tool_servers: [...c.tool_servers, added], version: c.version };
}

/** The write that removes the server named name, made from the context as read. */
export function withoutToolServer(c: PersonContext, name: string): PersonContextInput {
  return {
    run: c.run,
    tool_servers: c.tool_servers.filter((s) => s.name !== name),
    version: c.version,
  };
}

/** How a server's credential is reached, in words. A reference is shown; it names no secret. */
export function credentialWords(s: PersonToolServer): string {
  if (!s.credential) return "Reached without a credential.";
  return `Humagen reads ${s.credential} when a run uses it, and stores only the reference.`;
}
