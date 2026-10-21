import type { Agent, ClientRecord } from "./warp.ts";

/**
 * Who answers a message. In order:
 *
 * 1. An agent named in the message as `@name`, when that agent exists and this
 *    surface may route to it. The name is taken out of the message.
 * 2. The default agent: the client's, or the one configured for the app.
 * 3. Nobody, when the surface may reach any agent: the app sends no agent,
 *    and the default the platform's projects set decides. With none set, the
 *    platform refuses, and the person is asked to tag an agent.
 *
 * Nothing is picked here. With a client that lists its agents and names no
 * default, nothing is chosen: the platform's default is not known here, so it
 * could be one the list leaves out.
 */
export type Choice =
  | { agent: string; text: string; how: "named" | "default" }
  | { agent: ""; text: string; how: "inherited" }
  | { agent: null; text: string; how: "none" };

export interface Routing {
  /** The client this app is, when it is one. Its list is default deny. */
  client?: ClientRecord | null;
  /** The app's own default, used when it is not a client or the client has none. */
  defaultAgent?: string;
}

/** Whether this surface may route to an agent. Without a client, any agent in the workspace. */
export function mayRoute(routing: Routing, name: string): boolean {
  const client = routing.client;
  if (!client) return true;
  if (!client.enabled) return false;
  return (
    client.default_agent === name ||
    (client.agents ?? []).includes("*") ||
    (client.agents ?? []).includes(name)
  );
}

const NAMED = /(^|\s)@([A-Za-z0-9][A-Za-z0-9_-]*)(?=$|[\s,.:;!?])/g;

export function choose(text: string, agents: Agent[], routing: Routing): Choice {
  const said = text.trim();
  const known = new Set(agents.map((a) => a.name));
  for (const m of said.matchAll(NAMED)) {
    const name = m[2] as string;
    if (!known.has(name) || !mayRoute(routing, name)) continue;
    const at = (m.index ?? 0) + (m[1] ?? "").length;
    const rest = `${said.slice(0, at)}${said.slice(at + name.length + 1)}`
      .replace(/\s{2,}/g, " ")
      .replace(/^[\s,:]+/, "")
      .trim();
    return { agent: name, text: rest || said, how: "named" };
  }
  if (routing.client && !routing.client.enabled) return { agent: null, text: said, how: "none" };
  const fallback = routing.client?.default_agent || routing.defaultAgent;
  if (fallback && mayRoute(routing, fallback))
    return { agent: fallback, text: said, how: "default" };
  const any = !routing.client || (routing.client.agents ?? []).includes("*");
  return any
    ? { agent: "", text: said, how: "inherited" }
    : { agent: null, text: said, how: "none" };
}
