import { ActionId, type UiAction } from "./actions";
import type { Tone } from "./types";

/**
 * The Home surface: where a person lands in the console and in the Slack App
 * Home tab. One plain view-model, filled in by the host, drawn by the shared
 * `Home` component on both surfaces. Words that don't change between hosts
 * (headings, button labels, the start entries) live in the components' copy;
 * this holds only what the host knows: who, what is waiting, what is connected.
 *
 * Every control raises a `UiAction` (see `homeAction`). `turnId` is `HOME_ID`
 * for everything except Review, which names the inbox item instead.
 */

/** `turnId` of every Home action that isn't about one waiting item. */
export const HOME_ID = "home";

/** `option` of `ActionId.homeAnsweredBy` that lets every agent decide for itself whether to answer. */
export const ANY_AGENT = "*";

/** The ways to start, in the order the web shows them. Labels live in copy. */
export const HOME_STARTS = ["talk", "build", "search", "review", "runs", "schedule"] as const;
export type HomeStartId = (typeof HOME_STARTS)[number];

export interface HomeStart {
  id: HomeStartId;
  /** A number to show next to it, such as how many items need the person. */
  count?: number;
  /** Where it goes on the web. Leave out to raise `ActionId.homeStart` instead. */
  url?: string;
}

/**
 * Something an agent is waiting on this person for.
 * @deprecated Use `HomeNotification` with `kind: "inbox"`. Kept so hosts built
 * before notifications still render; `Home` reads it when `notifications` is absent.
 */
export interface HomeWaiting {
  /** The item's id: the interrupt, part or message id the host resolves. */
  id: string;
  /** The message or turn it belongs to. Defaults to `id`. */
  messageId?: string;
  /** Who is waiting: an agent's name. */
  from: string;
  /** What it wants, starting with a verb: "wants to post a reply in #support." */
  summary: string;
  /** What happens next, in a sentence: "Waiting for your approval." */
  detail?: string;
}

/** What a notification is: something in the inbox, or an alert a rule raised. */
export type HomeNotificationKind = "inbox" | "alert";

/** How serious an alert is. Inbox items leave it out. */
export type HomeSeverity = "critical" | "warning" | "info";

/** One thing for this person to look at: an inbox item waiting on them, or an open alert. */
export interface HomeNotification {
  kind: HomeNotificationKind;
  /** Stable within its kind: the inbox message id, or the alert id. */
  id: string;
  /** For inbox items, the message or turn Review opens. Defaults to `id`. */
  messageId?: string;
  /** Who or what raised it: an agent's name, or an alert rule. */
  from: string;
  /** One line, starting with a verb: "wants to post a reply in #support." */
  summary: string;
  /** What happens next, or more about it, in a sentence. */
  detail?: string;
  /** When it arrived or fired, as ISO 8601. */
  at: string;
  /** Alerts only. Said in words, never by color alone. */
  severity?: HomeSeverity;
  /**
   * Where exactly this item opens in the console. Relative on the web; for
   * Slack it must be absolute (the host's public console URL), and a relative
   * or missing one shows the item without a link rather than a broken one.
   */
  url?: string;
}

/** The whole list a kind of notification comes from, for "N more". */
export interface HomeNotificationSource {
  /** How many are open in all, including those not in `items`. Defaults to how many `items` holds. */
  total?: number;
  /** The full list in the console: the inbox, or the alerts page. */
  url?: string;
}

export interface HomeNotifications {
  /** Any order: `Home` orders them (see `orderNotifications`) and shows the first `HOME_NOTIFICATIONS`. */
  items: HomeNotification[];
  inbox?: HomeNotificationSource;
  /** Leave out when the person may not read alerts. */
  alerts?: HomeNotificationSource;
}

/** How many notifications Home shows in full; the rest are counted and linked. */
export const HOME_NOTIFICATIONS = 8;

/**
 * How many of what needs the person a host reads for Home and lists, newest
 * first: the Slack Home tab and the console's Needs you box. The rest are
 * counted and are in the inbox.
 */
export const HOME_NEEDS = 5;

/** 0 for critical alerts, 1 for inbox items, 2 for every other alert. */
export function notificationGroup(n: HomeNotification): number {
  if (n.kind === "inbox") return 1;
  return n.severity === "critical" ? 0 : 2;
}

/**
 * The order Home shows notifications in: critical alerts (something is
 * broken), then inbox items (an agent is blocked on this person), then other
 * alerts; newest first within each group, and by id when two arrive together.
 */
export function orderNotifications(items: HomeNotification[]): HomeNotification[] {
  const time = (n: HomeNotification) => {
    const t = Date.parse(n.at);
    return Number.isNaN(t) ? 0 : t;
  };
  return [...items].sort(
    (a, b) =>
      notificationGroup(a) - notificationGroup(b) ||
      time(b) - time(a) ||
      a.kind.localeCompare(b.kind) ||
      a.id.localeCompare(b.id),
  );
}

/** Notifications from the older `waiting` list: inbox items, with no time or link. */
export function notificationsFromWaiting(waiting: HomeWaiting[]): HomeNotifications {
  return {
    items: waiting.map((w) => ({ kind: "inbox" as const, at: "", ...w })),
  };
}

/** One announcement from a project the person belongs to, for What's new. */
export interface HomeUpdate {
  /** The inbox message's id. */
  id: string;
  /** The project it was posted in, by name: "" is the workspace itself. */
  project: string;
  /** The project as people see it: its title, or the workspace's name. */
  projectTitle: string;
  subject: string;
  /** Who posted it: a person, or an agent scheduled in the project. */
  from: { kind: "person" | "agent" | "system"; name: string };
  /** When it was posted, ISO. */
  at: string;
  unread: boolean;
  /** Where it opens on the web, such as the project's inbox with it selected. */
  url?: string;
}

/** How many announcements What's new shows. The rest are in the inbox. */
export const HOME_UPDATES_MAX = 5;

/**
 * What's new in the order Home shows it: unread first, then newest first,
 * and by id when two arrive together; at most `HOME_UPDATES_MAX`.
 */
export function orderUpdates(items: HomeUpdate[]): HomeUpdate[] {
  const time = (u: HomeUpdate) => {
    const t = Date.parse(u.at);
    return Number.isNaN(t) ? 0 : t;
  };
  return [...items]
    .sort(
      (a, b) =>
        Number(b.unread) - Number(a.unread) || time(b) - time(a) || a.id.localeCompare(b.id),
    )
    .slice(0, HOME_UPDATES_MAX);
}

/**
 * A project that names the person, for Your projects: they are a member or an
 * owner there through an entry for them or a role they hold, on it or as an
 * owner above it. One they see only as one of everyone, or only through
 * projects:govern, is not theirs.
 */
export interface HomeProject {
  /** Its name: the address it opens at. */
  name: string;
  /** As people see it: its title, or its name. */
  title: string;
  /** The projects above it as people see them, top level first. Empty at the top level. */
  within: string[];
  /** Its kind: company, team, account, or one of the workspace's own. */
  kind: string;
  /** How many runs in it are in flight. */
  active: number;
  /** When a run in it last changed, ISO. Leave out when nothing has been filed in it. */
  activeAt?: string;
  /** Where it opens, such as the project's page in the console. */
  url?: string;
}

/** How many projects Your projects shows. The rest are counted and linked. */
export const HOME_PROJECTS_MAX = 8;

/**
 * Your projects in the order Home shows them: where something happened most
 * recently first, then the ones where nothing has, in the order given (the
 * tree's, from the platform). All of them: Home shows the first
 * `HOME_PROJECTS_MAX` and counts the rest.
 */
export function orderProjects(items: HomeProject[]): HomeProject[] {
  const time = (p: HomeProject) => {
    const t = p.activeAt ? Date.parse(p.activeAt) : Number.NaN;
    return Number.isNaN(t) ? 0 : t;
  };
  return items
    .map((p, i) => ({ p, i }))
    .sort((a, b) => time(b.p) - time(a.p) || a.i - b.i)
    .map(({ p }) => p);
}

/** Which agent answers the person's direct messages. */
export interface HomeAnsweredBy {
  agents: { id: string; name: string }[];
  /** An agent id, or `ANY_AGENT` (the default) to let the router choose. */
  selected?: string;
}

/** A connection the whole workspace shares (Slack, GitHub, Jira). */
export interface HomeConnection {
  id: string;
  name: string;
  /** What it lets agents do: "Agents answer on issues and review pull requests." */
  description?: string;
  connected: boolean;
  /** The workspace or organization it is connected to. */
  account?: string;
  /** True when this viewer may connect it. Others are told an admin can. */
  canConnect: boolean;
}

/** The person's own accounts, which agents use to act as them. */
export interface HomePersonal {
  /** How many the person has connected. */
  connected: number;
  canConnect: boolean;
}

/** What the person may do, and why. */
export interface HomeAccess {
  /** The role a rule grants them. Leave out when no rule grants one. */
  role?: string;
  /** The group that earned the role. */
  group?: string;
  /** How they signed in: "your company's identity provider". */
  via?: string;
  /** What the role lets them do, after "You can": "run agents and manage schedules". */
  allows?: string;
  /** What it does not, after "You cannot": "change what an agent is". */
  denies?: string;
  /** The explanation of this person's access, step by step. */
  explainUrl?: string;
  policyUrl?: string;
}

export interface HomeLink {
  label: string;
  url: string;
}

export interface HomeModel {
  person: {
    id: string;
    name: string;
    /** For the greeting. Defaults to the first word of `name`. */
    firstName?: string;
  };
  assistant: {
    name: string;
    /** How people mention it in Slack, without the @. Defaults to `name`. */
    handle?: string;
  };
  /** One sentence about what the assistant does. The surface adds how to reach it. */
  intro: string;
  /** The ask box on the web. Leave out to hide it. */
  ask?: {
    placeholder?: string;
    attach?: boolean;
    /** Lines under the field: an error from the last question, the files attached to the next. */
    notices?: { tone: Tone; text: string }[];
  };
  starts: HomeStart[];
  /** What needs this person: inbox items and open alerts. */
  notifications: HomeNotifications;
  /** What's new: announcements from the person's projects. Leave out to hide it. */
  updates?: HomeUpdate[];
  /** Your projects: every project that names the person (`HomeProject`), in any order. Leave out to hide it. */
  projects?: HomeProject[];
  /** Where all of the person's projects are listed, for "N more". */
  projectsUrl?: string;
  /** @deprecated Use `notifications`. Read only when `notifications` is absent. */
  waiting?: HomeWaiting[];
  /** Leave out when the person can't choose. */
  answeredBy?: HomeAnsweredBy;
  connections: HomeConnection[];
  /** Leave out to hide "Your own accounts". */
  personal?: HomePersonal;
  /** Where all connections are managed. */
  connectionsUrl?: string;
  /**
   * What the person may do. Leave out when the host does not know, as the
   * Slack app does not for the people it reads for: Slack then shows only
   * the link to the console, and the web no access section.
   */
  access?: HomeAccess;
  /** The web console, linked from Slack. */
  consoleUrl?: string;
  links: HomeLink[];
}

/** The target a connect action names for the person's own accounts. */
export const PERSONAL_ACCOUNT = "personal";

/**
 * The action each Home control raises. One place, so a host's router and the
 * component can't disagree about ids.
 */
export const homeAction = {
  /** Ask the assistant. `text` is the question when typed; Slack sends none, so open a form. */
  ask: (text?: string): UiAction => ({
    id: ActionId.homeAsk,
    payload: { turnId: HOME_ID, target: "ask", ...(text ? { text } : {}) },
  }),
  start: (id: HomeStartId): UiAction => ({
    id: ActionId.homeStart,
    payload: { turnId: HOME_ID, target: id },
  }),
  /** Attach a file to the next question. The host opens its file picker. */
  attach: (): UiAction => ({
    id: ActionId.homeAttach,
    payload: { turnId: HOME_ID, target: "ask" },
  }),
  /** Review an inbox item: the same action that opens it from the inbox. */
  review: (item: Pick<HomeNotification, "id" | "messageId">): UiAction => ({
    id: ActionId.open,
    payload: { turnId: item.messageId ?? item.id, target: item.id },
  }),
  /** The surface fills in `option` with the agent id that was picked. */
  answeredBy: (agentId?: string): UiAction => ({
    id: ActionId.homeAnsweredBy,
    payload: {
      turnId: HOME_ID,
      target: "answered_by",
      ...(agentId ? { option: agentId } : {}),
    },
  }),
  /** `target` is a connection id, or `PERSONAL_ACCOUNT`. */
  connect: (target: string): UiAction => ({
    id: ActionId.homeConnect,
    payload: { turnId: HOME_ID, target },
  }),
};

/** "Ada" from "Ada Lovelace", unless the host says otherwise. */
export function firstName(person: HomeModel["person"]): string {
  return person.firstName ?? person.name.split(/\s+/)[0] ?? person.name;
}
