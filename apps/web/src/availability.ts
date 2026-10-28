/**
 * Which agents may answer where, in words. Pure: the platform decides what is
 * available (GET /api/projects/{name}/agents), this only reads its answer for
 * the composer, a project's Agents tab, an agent's Access tab and the
 * Directory.
 */
import { defaultCopy } from "@humagen/components";
import type { AgentOffer, DefaultResolved, ProjectAgentsView, ProjectView } from "./api.ts";

const place = defaultCopy.agentPlace;
const auto = defaultCopy.auto;

/** The default that lets every agent available decide for itself whether to answer. It is never an agent. */
export const AUTO = "*";
/** Every platform agent, alone in an enable list. */
export const EVERYONE = "*";

/** The agents of `all` that may run where `view` was read, in the order given. */
export function availableOf(
  all: string[],
  view: Pick<ProjectAgentsView, "available"> | null,
  homes: Record<string, string>,
): string[] {
  if (!view) return all;
  const names = new Set(view.available.names);
  // Company homes remain platform-wide. Scoped homes are offered only when
  // the server names them on this project's available chain.
  return all.filter(
    (a) => names.has(a) || (view.available.all && nameOfAddress(homes[a] ?? "") === ""),
  );
}

/** "~" in an answer is the workspace, "" here. */
export function nameOfAddress(address: string): string {
  return address === "~" ? "" : address;
}

/** Where a default is set, as the platform says it ("project:Field", "project:~"), as a project name. */
export function defaultFrom(d: DefaultResolved): string {
  return nameOfAddress(d.from.replace(/^project:/, ""));
}

/**
 * Where an agent available in a project comes from, in words: "Lives here",
 * "Lives in Globex", "from Field", or nothing when this project enables it.
 */
export function offerWords(
  offer: AgentOffer,
  here: string,
  titleOf: (name: string) => string,
): string {
  const from = nameOfAddress(offer.from);
  if (offer.home) return from === here ? place.livesHere : place.livesIn(titleOf(from));
  return from === here ? "" : place.fromProject(titleOf(from));
}

/** Who answers a message that tags no agent, in the line under a composer. */
export function answersLine(agent: string | undefined, picked?: string): string {
  if (!agent) return auto.nobody;
  if (agent === AUTO) return picked ? auto.picks(picked) : auto.picksWho;
  return auto.answers(agent);
}

/** Whether `name` is `project` or sits below it, by the paths the tree lists. */
export function inSubtree(p: ProjectView | undefined, project: string): boolean {
  if (project === "") return true;
  if (!p) return false;
  return p.name === project || p.path.some((s) => s.name === project);
}

/**
 * How many places (the workspace counts as one) would lose an agent if it
 * came to live in `home`: those it is available in through an enable list
 * now, outside the new home's subtree. Allow lists are not read here, so it
 * can count a place an allow list already keeps the agent out of.
 */
export function losing(home: string, enabledIn: string[], tree: ProjectView[]): number {
  if (home === "") return 0;
  const enabled = enabledIn.map(nameOfAddress);
  let n = 0;
  // The workspace itself, which a project agent never answers in.
  if (enabled.includes("")) n++;
  for (const p of tree) {
    if (!inSubtree(p, home) && enabled.some((e) => inSubtree(p, e))) n++;
  }
  return n;
}
