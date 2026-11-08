import type { MessagePick, Revision } from "./api.ts";

/** What the next message may pick: the models and efforts any agent it may start offers. */
export interface Offers {
  models: string[];
  efforts: string[];
}

/** The order efforts are shown in, lightest first. Unknown ones follow, by name. */
const effortOrder = ["none", "minimal", "low", "medium", "high", "xhigh", "max"];

/**
 * What the next message may pick. The workspace's offer applies to every agent; until an
 * administrator sets one, the picks the named agents' own specs offer, together, as before.
 */
export function offersFor(
  agents: Revision[],
  names: string[],
  workspace?: { models?: string[]; efforts?: string[] },
): Offers {
  if (workspace && ((workspace.models?.length ?? 0) > 0 || (workspace.efforts?.length ?? 0) > 0))
    return offersOf([...(workspace.models ?? [])], [...(workspace.efforts ?? [])]);
  const models = new Set<string>();
  const efforts = new Set<string>();
  for (const a of agents) {
    if (!names.includes(a.spec.name)) continue;
    for (const m of a.spec.choices?.models ?? []) models.add(m);
    for (const e of a.spec.choices?.efforts ?? []) efforts.add(e);
  }
  return offersOf([...models], [...efforts]);
}

/** Offers in the order the control shows them: models by name, efforts lightest first. */
function offersOf(models: string[], efforts: string[]): Offers {
  const rank = (e: string) => {
    const i = effortOrder.indexOf(e);
    return i < 0 ? effortOrder.length : i;
  };
  return {
    models: [...new Set(models)].sort(),
    efforts: [...new Set(efforts)].sort((a, b) => rank(a) - rank(b) || a.localeCompare(b)),
  };
}

/** The agents a draft names with `@`, among those known. */
export function namedIn(draft: string, agents: Revision[]): string[] {
  const names = new Set(agents.map((a) => a.spec.name));
  const out: string[] = [];
  for (const m of draft.matchAll(/(?:^|[^\w@])@([a-z][a-z0-9-]*)/gi)) {
    const name = (m[1] ?? "").toLowerCase();
    if (names.has(name) && !out.includes(name)) out.push(name);
  }
  return out;
}

/** The pick kept to what is offered: a choice no longer offered is dropped. */
export function keptPick(pick: MessagePick, offers: Offers): MessagePick {
  const out: MessagePick = {};
  if (pick.model && offers.models.includes(pick.model)) out.model = pick.model;
  if (pick.effort && offers.efforts.includes(pick.effort)) out.effort = pick.effort;
  return out;
}

/** The model's name without its provider, as the control shows it. */
export function modelName(ref: string): string {
  const slash = ref.indexOf("/");
  return slash < 0 ? ref : ref.slice(slash + 1);
}
