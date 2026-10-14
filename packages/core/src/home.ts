import { ActionId, type UiAction } from "./actions";

/**
 * The Home surface: where a person lands in the console and in the Slack App
 * Home tab. One plain view-model, filled in by the host, drawn by the shared
 * `Home` component on both surfaces. Words that don't change between hosts
 * (headings, button labels, the start entries) live in the components' copy;
 * this holds only what the host knows: who, what is waiting, what is connected.
 *
 * Every control raises a `UiAction` (see `homeAction`). `turnId` is `HOME_ID`
 * for everything except Review, which names the waiting item instead.
 */

/** `turnId` of every Home action that isn't about one waiting item. */
export const HOME_ID = "home";

/** `option` of `ActionId.homeAnsweredBy` that lets the router choose the agent. */
export const ANY_AGENT = "auto";

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

/** Something an agent is waiting on this person for. */
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
  /** The ask box on the web. */
  ask?: { placeholder?: string; attach?: boolean };
  starts: HomeStart[];
  waiting: HomeWaiting[];
  /** Leave out when the person can't choose. */
  answeredBy?: HomeAnsweredBy;
  connections: HomeConnection[];
  /** Leave out to hide "Your own accounts". */
  personal?: HomePersonal;
  /** Where all connections are managed. */
  connectionsUrl?: string;
  access: HomeAccess;
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
  /** Review a waiting item: the same action that opens it from the inbox. */
  review: (item: HomeWaiting): UiAction => ({
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
