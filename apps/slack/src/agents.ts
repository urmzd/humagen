import type { Agent, ClientRecord } from "./humagen.ts";
import { builtinAgent } from "./legacy.ts";

/**
 * Who answers a message. In order:
 *
 * 1. An agent named in the message as `@name`, when that agent exists and this
 *    surface may route to it. The name is taken out of the message. `@auto`
 *    names nobody.
 * 2. The default agent: the client's, or the one configured for the app.
 *    `*` asks every agent available where the message lands whether to take
 *    it, where this surface may reach any agent.
 * 3. Nobody, when the surface may reach any agent: the app sends no agent,
 *    and the default the platform's projects set decides. With none set, the
 *    platform refuses, and the person is asked to tag an agent.
 *
 * Nothing is picked here. With a client that lists its agents and names no
 * default, nothing is chosen: the platform's default is not known here, so it
 * could be one the list leaves out. For the same reason a client that lists
 * its agents never sends `*` as its default: the platform asks every agent
 * available where the run lands, and the API does not tell it the client's
 * list, so one that joins could be one the list leaves out.
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

/** The default that lets every agent available decide whether to take a message. It is no agent. */
export const AUTO = "*";

/**
 * The platform's agent that builds and changes agents with a person, each
 * change after someone approves it. Home offers Build an Agent only where
 * this app may reach it.
 */
export const BUILDER = "humagen-builder";

/**
 * Whether this surface may route to an agent. Without a client, any agent in
 * the workspace. `*` only where the surface may reach any agent, this
 * app's own choice: the platform asks only a client's agents for a run that
 * acts for its person (`act_as`).
 */
export function mayRoute(routing: Routing, name: string): boolean {
  const client = routing.client;
  if (!client) return true;
  if (!client.enabled) return false;
  const any = (client.agents ?? []).includes("*");
  if (name === AUTO) return any;
  return client.default_agent === name || any || (client.agents ?? []).includes(name);
}

const NAMED = /(^|\s)@([A-Za-z0-9][A-Za-z0-9_-]*)(?=$|[\s,.:;!?])/g;

export function choose(text: string, agents: Agent[], routing: Routing): Choice {
  const said = text.trim();
  const known = new Set(agents.map((a) => a.name));
  for (const m of said.matchAll(NAMED)) {
    const name = m[2] as string;
    if (!known.has(builtinAgent(name)) || !mayRoute(routing, name)) continue;
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
