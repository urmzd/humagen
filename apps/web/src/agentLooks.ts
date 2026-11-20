import { type BotLook, configureBotLooks } from "@humagen/web";
import { type AgentLook, agentLook, subscribeHues } from "./agentHues.ts";

const drawn = new WeakMap<AgentLook, BotLook>();

/**
 * An agent's look as a mark draws it. The same kept look is always the same
 * object, so a mark reading it re-renders only when the look changes.
 */
export function botLook(look: AgentLook | undefined): BotLook | undefined {
  if (!look) return undefined;
  const seen = drawn.get(look);
  if (seen) return seen;
  const out: BotLook = {
    shape: look.shape as BotLook["shape"],
    face: look.face as BotLook["face"],
    finish: look.finish as BotLook["finish"],
    hat: look.hat as BotLook["hat"],
    glasses: look.glasses as BotLook["glasses"],
    headphones: look.headphones,
    bowTie: look.bow_tie,
    label: look.display_name,
  };
  drawn.set(look, out);
  return out;
}

/** Every agent mark, in the console and in the shared components, draws the looks the API serves. */
export function drawAgentLooks(): void {
  configureBotLooks({ get: (name) => botLook(agentLook(name)), subscribe: subscribeHues });
}
