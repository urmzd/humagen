import {
  fromServer,
  notificationsOf,
  type PlatformMessage,
  projectsOf,
  updatesOf,
  type WireProject,
} from "@warp/adapter";
import type { HomeModel, HomeStart } from "@warp/core";

/** How many that need the person the Home tab shows. The rest are counted. */
export const HOME_NEEDS = 5;

/** What the Home tab is made of, all of it read for the person through the client. */
export interface HomeReads {
  /** The Slack person, and what Slack calls them. */
  person: { id: string; name: string };
  assistant: { name: string; handle?: string };
  intro: string;
  /** A page of their open inbox, and how many in it may need them. */
  inbox: { messages: Record<string, unknown>[]; open: number };
  /** A page of their open announcements, already made unable to ping anyone. */
  announcements: Record<string, unknown>[];
  /** Every project they have a role in, or null when it could not be read. */
  projects: WireProject[] | null;
  /** The workspace's name, for what is announced in the workspace itself. */
  workspace: string;
  /** Whether Build an Agent is offered: this app may reach the builder. */
  build: boolean;
  /** A console address, or nothing when the app has no public console address. */
  href: (path: string) => string | undefined;
}

const enc = encodeURIComponent;
const projectPath = (name: string) => `/projects/${name === "" ? "~" : enc(name)}`;

/**
 * The shared Home for a person the client maps, as the Slack app shows it:
 * what needs them (five at most, the rest counted), what's new in their
 * projects, and their projects, each opening its page in the console. It
 * knows nothing of their access or connections, so it shows neither, and it
 * starts only what it can answer here: a question, an agent when the builder
 * is reachable, and the console's search when it has the console's address.
 * The model is the console's: the same adapter turns the same reads into it.
 */
export function homeModel(r: HomeReads): HomeModel {
  const titles = new Map((r.projects ?? []).map((p) => [p.name, p.title || p.name]));
  const read = (raw: Record<string, unknown>[]): PlatformMessage[] => raw.map(fromServer);
  const needs = notificationsOf(read(r.inbox.messages), (id) =>
    r.href(`/inbox?message=${enc(id)}`),
  );
  const search = r.href("/context");
  const starts: HomeStart[] = [
    { id: "talk" },
    ...(r.build ? [{ id: "build" as const }] : []),
    ...(search ? [{ id: "search" as const, url: search }] : []),
  ];
  const inboxUrl = r.href("/inbox");
  const projectsUrl = r.href("/projects");
  const consoleUrl = r.href("");
  return {
    person: r.person,
    assistant: r.assistant,
    intro: r.intro,
    starts,
    notifications: {
      items: needs.slice(0, HOME_NEEDS),
      inbox: {
        total: Math.max(r.inbox.open, needs.length),
        ...(inboxUrl ? { url: inboxUrl } : {}),
      },
    },
    updates: updatesOf(
      read(r.announcements),
      (name) => (name === "" ? r.workspace : (titles.get(name) ?? name)),
      (name, id) => r.href(`${projectPath(name)}?message=${enc(id)}`),
    ),
    ...(r.projects
      ? { projects: projectsOf(r.projects, (name) => r.href(projectPath(name))) }
      : {}),
    ...(projectsUrl ? { projectsUrl } : {}),
    connections: [],
    ...(consoleUrl ? { consoleUrl } : {}),
    links: [],
  };
}
