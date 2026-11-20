import type { ProjectView } from "./api.ts";

/**
 * The topics someone answers for in a project, along its chain: the
 * project's, then each project's above it, then the workspace's.
 */
export function topicsOf(
  projects: ProjectView[],
  root: ProjectView | null,
  name: string,
): string[] {
  const seen = new Set<string>();
  const add = (p: ProjectView | null | undefined) => {
    for (const e of p?.experts ?? []) for (const t of e.topics) seen.add(t);
  };
  let at = projects.find((p) => p.name === name);
  for (let steps = 0; at && steps < 5; steps++) {
    add(at);
    const parent = at.parent ?? "";
    at = parent === "" ? undefined : projects.find((p) => p.name === parent);
  }
  add(root);
  return [...seen].sort().slice(0, 16);
}
