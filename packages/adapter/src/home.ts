/**
 * The platform's projects as the shared Home shows them, from
 * `GET /api/projects`. The Slack app reads the list for one of its people,
 * with `act_as_client` and `act_as_user`, and maps it here. The console's
 * Home has no Your projects.
 */
import { type HomeProject, orderProjects } from "@warp/core";

/** A project view as `GET /api/projects` lists it: only the fields Home reads. */
export interface WireProject {
  name: string;
  title?: string;
  kind: string;
  archived?: boolean;
  /** The projects from the top level down to this one, the workspace left out. */
  path?: { name: string; title?: string }[];
  active?: number;
  active_at?: string;
}

/**
 * Your projects on Home, from the projects the reader has a role in: every
 * one that is not archived, where something happened most recently first.
 * `url` is where one opens, when it opens anywhere.
 */
export function projectsOf(views: WireProject[], url?: (name: string) => string | undefined): HomeProject[] {
  return orderProjects(
    views
      .filter((v) => !v.archived)
      .map((v) => {
        const at = url?.(v.name);
        return {
          name: v.name,
          title: v.title || v.name,
          // The path ends at the project itself.
          within: (v.path ?? []).slice(0, -1).map((s) => s.title || s.name),
          kind: v.kind,
          active: v.active ?? 0,
          ...(v.active_at ? { activeAt: v.active_at } : {}),
          ...(at ? { url: at } : {}),
        };
      }),
  );
}
