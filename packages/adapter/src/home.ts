/**
 * The platform's projects as the shared Home shows them, from
 * `GET /api/projects`. The Slack app reads the list for one of its people,
 * with `act_as_client` and `act_as_user`, and maps it here. The list holds
 * every project they may see, which names where What's new was posted, and
 * Your projects are the ones it marks `mine`. The console's Home has no Your
 * projects; one that shows them makes them here.
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
  /**
   * Whether the project names the reader: they are a member or an owner
   * through an entry for their subject or a role they hold, there or as an
   * owner above. Access only through everyone's entry or projects:govern
   * does not count.
   */
  mine: boolean;
}

/**
 * Your projects on Home: the projects that name the reader (`mine`), every one
 * that is not archived, where something happened most recently first and then
 * in the tree's order. A project they see only as one of everyone, or only
 * through projects:govern, is not theirs. `url` is where one opens, when it
 * opens anywhere.
 */
export function projectsOf(views: WireProject[], url?: (name: string) => string | undefined): HomeProject[] {
  return orderProjects(
    views
      .filter((v) => v.mine && !v.archived)
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
