import type { Schedule } from "./api.ts";

/**
 * A project's weekly recap is an ordinary schedule: nothing about it is stored
 * as a recap. These build the one "Add Weekly Recap" creates.
 */

/** FNV-1a over the name's code points: stable across browsers and runs. */
function hash(text: string): number {
  let h = 0x811c9dc5;
  for (const ch of text) {
    h ^= ch.codePointAt(0) ?? 0;
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h;
}

/**
 * When a project's recap runs: Monday morning between 08:01 and 08:59 UTC.
 * The minute comes from the project's name, so a workspace's recaps do not all
 * fire at once, and is never :00 or :30, when most schedules run.
 */
export function recapCron(project: string): string {
  // 58 minutes: 1 to 59 without 30.
  let minute = 1 + (hash(project) % 58);
  if (minute >= 30) minute += 1;
  return `${minute} 8 * * 1`;
}

/** A schedule's name for a project's recap: letters, digits and dashes, as schedule names are. "~" and "" are the workspace. */
export function recapName(project: string): string {
  if (project === "" || project === "~") return "workspace-recap";
  const slug = project
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 48)
    .replace(/-+$/, "");
  return `${slug || "project"}-recap`;
}

/**
 * The schedule "Add Weekly Recap" creates in a project: its agent asked, each
 * Monday morning, what changed since the last recap, with the answer posted
 * to the project. `text` and `description` are the host's copy.
 */
export function weeklyRecap(project: string, agent: string, text: string, description: string): Schedule {
  return {
    name: recapName(project),
    expression: recapCron(project === "" ? "~" : project),
    agent,
    text,
    project: project === "" ? "~" : project,
    description,
    deliver: "project",
  };
}

/** Where a schedule's answers may go: the project only when it is filed in one. */
export type Deliver = "" | "inbox" | "project";

export function deliverOptions(
  project: string | undefined,
  words: { deliverNone: string; deliverInbox: (mine: boolean) => string; deliverProject: string },
  mine: boolean,
): { value: Deliver; label: string }[] {
  const out: { value: Deliver; label: string }[] = [
    { value: "", label: words.deliverNone },
    { value: "inbox", label: words.deliverInbox(mine) },
  ];
  if (project !== undefined) out.push({ value: "project", label: words.deliverProject });
  return out;
}

/** What deliver becomes when the project changes: a schedule filed nowhere cannot post to a project. */
export function deliverFor(project: string | undefined, deliver: Schedule["deliver"]): Schedule["deliver"] {
  return deliver === "project" && project === undefined ? undefined : deliver;
}

/** The agent a recap asks: warp-analyst when it is there, else the first agent offered. */
export function recapAgent(agents: string[]): string | undefined {
  return agents.includes("warp-analyst") ? "warp-analyst" : agents[0];
}
