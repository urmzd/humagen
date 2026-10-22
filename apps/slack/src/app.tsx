import "./react.ts";
import { createHash } from "node:crypto";
import {
  actedFor,
  answerOfAction,
  answers,
  ended,
  type Follower,
  follow,
  fromServer,
  type PlatformMessage,
  retryOf,
  toRequest,
} from "@warp/adapter";
import {
  AnnouncementPost,
  type Copy,
  defaultCopy,
  Home,
  MailboxView,
  MessageView,
  TurnView,
} from "@warp/components";
import {
  ActionId,
  emptyQueue,
  emptyTurn,
  type Failure,
  feedbackEvent,
  HOME_ID,
  messageState,
  partMatches,
  type QueueState,
  queueReduce,
  reduce,
  type Turn,
  type UiAction,
} from "@warp/core";
import {
  CONNECTIONS_MODAL,
  connectionsModal,
  decodeFeedbackModal,
  decodeReplyModal,
  decodeSlackAction,
  escapeMentions,
  feedbackModal,
  homeView,
  LIMITS,
  type ReplyTarget,
  reactionChanges,
  renderToSlack,
  replyModal,
  type SerializeResult,
  suggestedPrompts,
} from "@warp/render-slack";
import { BUILDER, choose, mayRoute, type Routing } from "./agents.ts";
import { type Branding, brandedCopy, brandedWords, DEFAULT_BRANDING } from "./branding.ts";
import { inThread, Names, readThread, sinceLastTurn, withHistory } from "./history.ts";
import { homeModel } from "./home.ts";
import { askForm, buildForm, ENTRY, formKind, type words } from "./host.ts";
import { eventKey, runId, type Session, sessionName, sessionOf, threadKey } from "./keys.ts";
import { Presenter } from "./present.tsx";
import { type Envelope, type SlackApi, SlackError } from "./slack.ts";
import {
  type ActAs,
  type Agent,
  type Binding,
  type ClientRecord,
  type InboxNotice,
  type Receipt,
  type Run,
  type StreamItem,
  type SurfaceRef,
  type ThreadPosted,
  type Warp,
  WarpError,
  type WireMessage,
  type WireProject,
} from "./warp.ts";

/**
 * The Slack app: Slack deliveries in, warp calls and Slack calls out.
 *
 * It holds no rendering of its own. A run's events go through
 * `@warp/adapter` into one shared UI turn, and that turn goes through
 * `@warp/render-slack`. It holds no record of its own either: which
 * conversation a thread is, and whether a message was already answered, are
 * asked of warp. What it keeps in memory is listed in the README, with
 * what a restart costs.
 */

export interface AppOptions {
  /** The one Slack team this instance serves. */
  team: string;
  /** This app's own bot user, whose messages never start a run. */
  botUser: string;
  /** Who warp says this app is. Run ids are made from it. */
  subject: string;
  /** The workspace warp says this app is in. Run ids are made from it too. */
  tenant: string;
  operators?: string[];
  /** The client record this app is, when it is one. */
  clientId?: string;
  defaultAgent?: string;
  /** Where people reach the console, for links to files. */
  publicUrl?: string;
  /** Name, tagline, disclaimer and suggested prompts. Left out, the defaults. */
  branding?: Branding;
  /** The least time between two writes of one turn to Slack. */
  flushMs?: number;
  /** How often a run's record is read while it is being followed. */
  pollMs?: number;
  /** How long a run may go without an event or a change to its record before it is followed slowly. */
  stallMs?: number;
  /** The longest wait between two reads of a quiet run's record. */
  slowPollMs?: number;
  /**
   * How long the stop button waits for a run this process is not following to
   * end before it settles the session anyway. The runtime gives a turn ten
   * seconds to wrap up by default.
   */
  stopWaitMs?: number;
  /** What time it is, in milliseconds since the epoch. Left out, the system's clock. */
  now?: () => number;
  /**
   * How long to wait before trying a catch-up that did not finish again,
   * doubled after each failure up to five minutes. 5 seconds by default.
   */
  catchUpRetryMs?: number;
  log?: (line: string) => void;
}

interface Job {
  files?: boolean;
  session: Session;
  text: string;
  /** The message that asked. Lifecycle reactions go on it. */
  triggerTs: string;
  user: string;
  /** Names the Slack event, so the same event is one run. */
  key: string;
  /**
   * Set when a turn is asked again: who answers, where it continues, and whom
   * it acts for are already known. `as` is the person who pressed, asked as
   * themselves, when the turn acted for them; null when it acted for nobody.
   */
  again?: { agent: string; after?: string; as: ActAs | null };
  /**
   * Who may settle what the run raises, when that is not the person who sent
   * this; null when nobody here is known to have asked.
   */
  requester?: string | null;
  /**
   * The words are someone else's, not the person's who sent this: a message
   * of another's they asked about. Asked through the app, for nobody, so
   * nobody's words run with their credentials without their typing them.
   */
  notTheirs?: boolean;
  /** A mention in a channel thread: what people said there since the app was last asked goes with it. */
  history?: boolean;
}

type Item =
  | StreamItem
  | { type: "record"; run: Run }
  | { type: "stopped"; run?: Run }
  | { type: "broken"; error: Error }
  | { type: "tick" };

/** A run being followed right now. */
interface Live {
  run?: Run;
  /** Stop was pressed. The run is followed until warp says how it ended. */
  stopped: boolean;
  wake(item: Item): void;
}

interface Thread {
  queue: QueueState;
  jobs: Map<string, Job>;
  live?: Live;
}

const terminal = (s: Run["status"]) => s === "completed" || s === "failed" || s === "stopped";

/** A turn waiting on a person's answer to a question. */
const waitsOnPerson = (turn: Turn) =>
  turn.status === "waiting" &&
  turn.interrupts.some((i) =>
    i.type === "approval" ? i.decision.kind === "pending" : i.answer.kind === "pending",
  );

/** A small wait queue: what the stream, the poll and the stop button put in, the pump takes out. */
class Mailbox<T> {
  private readonly items: T[] = [];
  private waiter: ((item: T) => void) | null = null;
  push(item: T) {
    if (this.waiter) {
      const w = this.waiter;
      this.waiter = null;
      w(item);
    } else this.items.push(item);
  }
  get empty() {
    return this.items.length === 0;
  }
  next(): Promise<T> {
    const item = this.items.shift();
    if (item !== undefined) return Promise.resolve(item);
    return new Promise((resolve) => {
      this.waiter = resolve;
    });
  }
}

/** Remembers the most recent `max` entries and forgets the oldest. */
class Recent<V> extends Map<string, V> {
  constructor(private readonly max: number) {
    super();
  }
  override set(key: string, value: V) {
    super.delete(key);
    super.set(key, value);
    if (this.size > this.max) super.delete(this.keys().next().value as string);
    return this;
  }
}

/**
 * An inbox message as Slack can show it. warp's shared budget part offers
 * only fixed amounts to raise a limit to, and the platform names none, so on
 * Slack a reached limit is something to read: it is raised in the console.
 */
// warp: MessagePartView's budget part needs a raise control that takes an amount (not only
// fixed `raiseTo` buttons) and an Acknowledge button, and render-slack must drop an empty actions
// block. Until then a limit is read-only here.
/**
 * Text a person asked with, made safe to repeat as the app. The app posts it
 * as itself, so a `<!channel>`, `<!here>`, `<!everyone>` or `<!subteam^…>` it
 * passed through would notify everyone with the app's rights, and a
 * `<url|label>` would show a link under a label the person chose. Only a
 * person (`<@U…>`) or channel (`<#C…>`) reference is kept; everything else is
 * shown as typed. Slack sends command text entity-encoded and form values raw,
 * so the text is decoded first and then encoded once.
 */
export function echoed(text: string): string {
  const raw = text.replace(/&lt;|&gt;|&amp;/g, (e) =>
    e === "&lt;" ? "<" : e === "&gt;" ? ">" : "&",
  );
  const kept = /<@[UW][A-Z0-9]+(?:\|[^<>]*)?>|<#C[A-Z0-9]+(?:\|[^<>]*)?>/g;
  const encode = (s: string) =>
    s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  let out = "";
  let at = 0;
  for (const m of raw.matchAll(kept)) {
    out += encode(raw.slice(at, m.index)) + m[0];
    at = (m.index ?? 0) + m[0].length;
  }
  return out + encode(raw.slice(at));
}

/**
 * An announcement's words, which an agent may have written, made unable to
 * ping anyone: its subject and every part's text go through escapeMentions
 * before rendering, so no `<@U…>`, `<!here>`, `<!channel>`, `<!everyone>` or
 * `<!subteam^…>` reaches the channel, in the blocks or in the fallback text.
 */
export function untrustedAnnouncement(raw: WireMessage): WireMessage {
  return {
    ...raw,
    subject: escapeMentions(raw.subject),
    parts: raw.parts.map((p) => {
      const part = p as { type: string; text?: unknown };
      return typeof part.text === "string" ? { ...part, text: escapeMentions(part.text) } : p;
    }),
  };
}

export function forSlack(message: PlatformMessage): PlatformMessage {
  return {
    ...message,
    parts: message.parts.map((p) =>
      p.type === "budget"
        ? { ...p, allowed: [] }
        : // Retry on a failure is the app's own here (again()), decided for whoever
          // presses it, not by what warp offers the app's credential.
          p.type === "failure" && p.allowed?.length && !p.allowed.includes(ActionId.retry)
          ? { ...p, allowed: [...p.allowed, ActionId.retry] }
          : p,
    ),
  };
}

/**
 * A refusal or an error from warp, as warp's failure copy. The
 * platform's own words go to the log, never to Slack: they can name internals.
 */
function publicFailure(e: unknown): Failure {
  const status = e instanceof WarpError ? e.status : 0;
  if (status === 429 || status === 402) {
    const resets = e instanceof WarpError ? e.refusal?.resets_at : undefined;
    const wait = resets ? Math.max(0, Math.round((Date.parse(resets) - Date.now()) / 1000)) : 0;
    return {
      kind: "rate_limited",
      message: defaultCopy.budgetTitle,
      httpStatus: status,
      ...(wait ? { retryAfterSeconds: wait } : {}),
    };
  }
  // warp: failure copy has titles only (copy.failure[kind]); a body per kind, such as
  // copy.failureBody[kind], would stop the title being repeated as the message here.
  if (status === 401 || status === 403) {
    return { kind: "forbidden", message: defaultCopy.failure.forbidden(), httpStatus: status };
  }
  if (status === 0 || status >= 500) {
    return {
      kind: "unavailable",
      service: "warp",
      message: defaultCopy.failure.unavailable("warp"),
      ...(status ? { httpStatus: status } : {}),
    };
  }
  return { kind: "internal", message: defaultCopy.failure.internal(), httpStatus: status };
}

/** Nobody this app may reach can answer. */
// warp: needs copy for this, such as copy.noAgent(how to name one).
const NO_AGENT: Failure = {
  kind: "forbidden",
  message: "No agent answers here yet. Name one this app may reach, as @name.",
};

/** A retry of a turn that acted for nobody, by someone who did not ask, and is not an operator. */
// warp: needs copy for this, such as copy.notYoursRetry; copy.notYours speaks of approving.
const RETRY_NOT_YOURS = "Only the person who asked can retry this.";

/** Wait, or stop waiting when the signal aborts. */
const sleep = (ms: number, signal: AbortSignal) =>
  new Promise<void>((resolve) => {
    if (signal.aborted) return resolve();
    const done = () => {
      clearTimeout(timer);
      signal.removeEventListener("abort", done);
      resolve();
    };
    const timer = setTimeout(done, ms);
    signal.addEventListener("abort", done);
  });

const short = (s: string) => createHash("sha256").update(s).digest("hex").slice(0, 20);

/**
 * How far back a catch-up posts an announcement this app has no receipt for:
 * a restart, a deploy or a night without the app. An older one stays in
 * warp's inbox and the Home tab, as a post days late would read as news.
 */
export const CATCH_UP_WINDOW_MS = 24 * 60 * 60_000;

/** How many of a bound project's newest announcements a catch-up reads: one page. */
export const CATCH_UP_PAGE = 50;

/** Why an announcement is looked at: a notice says it changed, or a catch-up found it behind. */
type Look = "notice" | "catch-up";

const isDraft = (m: WireMessage) => m.parts.some((p) => p.type === "draft");

/** Whether a message changed after its receipt last said the post shows it. */
const changedSince = (m: WireMessage, r: Receipt) =>
  Date.parse(m.updated_at ?? "") > Date.parse(r.at ?? "");

/**
 * What a catch-up brings up to date, of one page of a bound project's
 * announcements as this client's machine reads them, newest first: their
 * ids, oldest first, so the channel reads in order. One with this client's
 * receipt is edited when it changed since the receipt, as when it was
 * withdrawn. One without is posted when it is open, from the last
 * CATCH_UP_WINDOW_MS, and announced no earlier than the Slack channel was
 * bound (`boundAt`): what came before never reached the channel, and is not
 * news there now. A binding that does not say when it was made posts
 * nothing new. A draft never reaches a channel.
 */
export function catchUpOn(
  page: WireMessage[],
  client: string,
  now: number,
  boundAt: string | undefined,
): string[] {
  const news = page.filter((m) => m.kind === "announcement" && !isDraft(m));
  const due: string[] = [];
  for (const m of news.toReversed()) {
    const receipt = m.deliveries?.[client];
    if (receipt ? changedSince(m, receipt) : isNews(m, now, boundAt)) due.push(m.id);
  }
  return due;
}

/**
 * Whether an announcement with no post yet is news in a channel bound at
 * `boundAt`: open, from the last CATCH_UP_WINDOW_MS, and announced no
 * earlier than the binding. A binding that does not say when it was made,
 * or says a time before any announcement could be, makes nothing news.
 */
export function isNews(m: WireMessage, now: number, boundAt: string | undefined): boolean {
  const bound = Date.parse(boundAt ?? "");
  if (Number.isNaN(bound) || bound < Date.parse("2000-01-01T00:00:00Z")) return false;
  const since = Math.max(now - CATCH_UP_WINDOW_MS, bound);
  return m.state === "open" && Date.parse(m.at ?? "") >= since;
}

/** What asking a turn again came to. */
type Again = "started" | "not-theirs" | "not-yours" | "elsewhere" | "not-routed";

/** What a direct message to one person about one inbox message is kept by. */
const toldKey = (as: ActAs, message: string) => `${as.user}\u0000${message}`;

/**
 * Whether a press is on a message the app sent the person who pressed as
 * theirs, by tell or by Review: those are posted top-level in their messages
 * with the app, whose Slack ids start with D. An inbox message shown on a run
 * is always in the run's thread.
 */
const sentToThem = (p: ActionPayload): boolean => {
  const at = p.message;
  const channel = p.channel?.id ?? p.container?.channel_id ?? "";
  return !!at && channel.startsWith("D") && (!at.thread_ts || at.thread_ts === at.ts);
};

export class App {
  private readonly threads = new Map<string, Thread>();
  /** Slack deliveries already taken, so a redelivery in this process does nothing. */
  private readonly seen = new Recent<true>(10_000);
  /** Who asked for each run, by Slack user id. */
  private readonly requesters = new Recent<string>(10_000);
  /** Inbox messages posted to a thread, so a change edits the post instead of adding one. */
  private readonly mail = new Recent<{ channel: string; ts: string; threadTs?: string }>(2_000);
  private readonly work = new Set<Promise<unknown>>();
  private native = true;
  private agentList: { at: number; agents: Agent[] } | null = null;
  private clientRecord: { at: number; client: ClientRecord | null } | null = null;
  private bindings: { at: number; list: Binding[] } | null = null;
  /**
   * Announcements being brought up to date right now, and why to look again
   * once that is done: a notice or a catch-up that comes meanwhile waits its
   * turn, so two never post one twice.
   */
  private readonly announcing = new Map<string, { again: Look | null; done: Promise<void> }>();
  /**
   * Announcements this process posted whose receipt warp did not take, so
   * the next look records it instead of posting again.
   */
  private readonly unrecorded = new Recent<{ channel: string; ts: string }>(2_000);
  /**
   * Announcements whose post could not be checked after its receipt was
   * recorded, because warp was away: a change made then reads older than
   * the receipt, so the next look edits the post whatever the receipt says.
   */
  private readonly unchecked = new Recent<true>(2_000);
  /** Whether a catch-up is running, and whether another is due once it ends. */
  private catching: { again: boolean } | null = null;
  /** Whether catch-ups are being run until one finishes, and whether another open came meanwhile. */
  private catchingUp: { again: boolean } | null = null;
  /**
   * Where each mention or question sent to a person's direct messages was
   * posted, by person and message, to keep it current. A question to a role
   * is one message sent to each person who holds it.
   */
  private readonly told = new Recent<{ channel: string; ts: string }>(2_000);
  private readonly flushMs: number;
  private readonly pollMs: number;
  private readonly stallMs: number;
  private readonly slowPollMs: number;
  private readonly stopWaitMs: number;
  /** Ends every slow follow on shutdown. */
  private readonly closing = new AbortController();
  /**
   * Runs whose end has been posted in their thread, by the slow follow or by
   * the stop button. Whichever posts first claims it, so an end is posted once.
   */
  private readonly confirmed = new Recent<true>(10_000);
  /** Runs followed slowly right now, and how to make each one look again at once. */
  private readonly slow = new Map<string, () => void>();
  private readonly branding: Branding;
  /** The shared copy, with this deployment's disclaimer. */
  private readonly copy: Copy;
  /** The host's own words, with this deployment's name. */
  private readonly words: typeof words;
  /** Display names for thread history, when the app may read them. */
  private readonly names: Names;

  constructor(
    private readonly slack: SlackApi,
    private readonly warp: Warp,
    private readonly opts: AppOptions,
  ) {
    this.flushMs = opts.flushMs ?? 700;
    this.pollMs = opts.pollMs ?? 5_000;
    this.stallMs = opts.stallMs ?? 15 * 60_000;
    this.slowPollMs = opts.slowPollMs ?? 3 * 60_000;
    this.stopWaitMs = opts.stopWaitMs ?? 30_000;
    this.branding = opts.branding ?? DEFAULT_BRANDING;
    this.copy = brandedCopy(this.branding);
    this.words = brandedWords(this.branding);
    this.names = new Names(slack, (line) => this.log(line));
  }

  private log(line: string) {
    this.opts.log?.(line);
  }

  /** Stop following quiet runs. Turns in flight still finish. */
  close(): void {
    this.closing.abort();
  }

  /** Resolves when everything started so far has finished. Tests wait on it. */
  async idle(): Promise<void> {
    while (this.work.size > 0) await Promise.allSettled([...this.work]);
  }

  private track<T>(work: Promise<T>): Promise<T> {
    this.work.add(work);
    const done = () => this.work.delete(work);
    work.then(done, done);
    return work;
  }

  /** One delivery from Slack. Anything from another team is dropped before it is read. */
  async handle(envelope: Envelope): Promise<void> {
    const p = envelope.payload ?? {};
    const team = teamOf(envelope.type, p);
    if (team !== this.opts.team) {
      if (envelope.type !== "hello") this.log(`ignored a ${envelope.type} for another team`);
      return;
    }
    const work =
      envelope.type === "events_api"
        ? this.onEvent(p.event as SlackEvent)
        : envelope.type === "interactive" && p.type === "block_actions"
          ? this.onAction(p as unknown as ActionPayload)
          : envelope.type === "interactive"
            ? this.onInteractive(p as unknown as InteractivePayload)
            : envelope.type === "slash_commands"
              ? this.onCommand(p as unknown as CommandPayload)
              : Promise.resolve();
    await this.track(work);
  }

  /**
   * Follow the inbox for as long as the signal allows, and show what concerns
   * this app's threads. The stream does not replay what it missed, so each
   * time it opens, at the start and after every reconnect, the app catches up
   * on the announcements of its bound channels.
   */
  async watchInbox(signal: AbortSignal): Promise<void> {
    const opened = () => {
      this.track(this.catchUpUntilDone(signal));
    };
    for await (const notice of this.warp.notices({ signal, opened })) {
      await this.track(
        this.onNotice(notice).catch((e: Error) => this.log(`inbox notice failed: ${e.message}`)),
      );
    }
  }

  /**
   * Catch up until a catch-up finishes, waiting longer after each one that
   * did not: a missed announcement is news for a day, and the next reconnect
   * may be days away. An open that comes meanwhile is caught up on once the
   * one under way ends.
   */
  private async catchUpUntilDone(signal: AbortSignal): Promise<void> {
    if (this.catchingUp) {
      this.catchingUp.again = true;
      return;
    }
    const run = { again: true };
    this.catchingUp = run;
    const base = this.opts.catchUpRetryMs ?? 5_000;
    let wait = base;
    try {
      while (run.again && !signal.aborted) {
        run.again = false;
        const done = await this.catchUp().catch((e: Error) => {
          this.log(`catch-up failed: ${e.message}`);
          return false;
        });
        if (done) {
          wait = base;
          continue;
        }
        this.log(`catch-up did not finish; trying again in ${Math.round(wait / 1000)}s`);
        await sleep(wait, signal);
        wait = Math.min(wait * 2, 5 * 60_000);
        run.again = true;
      }
    } finally {
      this.catchingUp = null;
    }
  }

  /**
   * Bring the posts of every project whose general channel is bound here up
   * to date with what was announced, edited or withdrawn while the app was
   * not listening: each project's newest CATCH_UP_PAGE announcements are read
   * as the app's machine reads them, and compared with this client's
   * receipts (catchUpOn). The receipt is the record, so a notice that comes
   * meanwhile changes nothing it does. One catch-up runs at a time, and one
   * asked for meanwhile runs once it ends. It answers whether it finished:
   * false when warp could not be read, or a post was not brought up to date
   * for a reason other than Slack refusing it, such as a post deleted there.
   * An app whose client does not name its credential as the client's
   * machine catches up on nothing: warp shows it no receipts, so it would
   * post everything again.
   */
  async catchUp(): Promise<boolean> {
    const client = this.opts.clientId;
    if (!client) return true;
    if (this.catching) {
      this.catching.again = true;
      return true;
    }
    const run = { again: true };
    this.catching = run;
    let done = true;
    try {
      while (run.again) {
        run.again = false;
        // Read afresh: a channel bound while the app was away counts now, and so does a new machine.
        this.clientRecord = null;
        this.bindings = null;
        if (!(await this.isMachine())) {
          this.log(
            `catch-up skipped: client ${client} is disabled or does not name this app's credential as its machine`,
          );
          return done;
        }
        const generals = (await this.bound()).filter((b) => b.channel === "general");
        for (const b of generals) {
          let page: WireMessage[];
          try {
            page = await this.warp.announcements(b.project, CATCH_UP_PAGE);
          } catch (e) {
            this.log(`announcements of ${b.project || "~"} were not read: ${(e as Error).message}`);
            // A project the machine may not read, or a token without
            // projects:read, answers the same next time: only warp being
            // away is tried again.
            if (!(e instanceof WarpError && (e.status === 403 || e.status === 404))) done = false;
            continue;
          }
          const due = catchUpOn(page, client, (this.opts.now ?? Date.now)(), b.bound_at);
          // A post whose receipt warp did not take is looked at whatever became of it.
          for (const m of page)
            if ((this.unrecorded.has(m.id) || this.unchecked.has(m.id)) && !due.includes(m.id)) due.push(m.id);
          for (const id of due) {
            // One post Slack refuses to edit, such as one deleted there, holds up no other.
            await this.announce(id, "catch-up").catch((e: Error) => {
              this.log(`announcement ${id} was not brought up to date: ${e.message}`);
              // Slack refusing an edit, or warp refusing this machine (403,
              // 404), answers the same next time: only warp being away is
              // tried again.
              const lasting =
                e instanceof SlackError ||
                (e instanceof WarpError && (e.status === 403 || e.status === 404));
              if (!lasting) done = false;
            });
          }
        }
      }
      return done;
    } finally {
      this.catching = null;
    }
  }

  /**
   * Whether warp names this app's credential as its enabled client's
   * machine. Only then does warp show it the client's receipts and take new
   * ones, so only then does it post announcements.
   */
  private async isMachine(): Promise<boolean> {
    const client = (await this.routing()).client;
    return (
      !!client &&
      client.enabled &&
      client.surface === "slack" &&
      client.slack?.machine === this.opts.subject
    );
  }

  // Routing

  private async agents(): Promise<Agent[]> {
    if (this.agentList && Date.now() - this.agentList.at < 30_000) return this.agentList.agents;
    const agents = (await this.warp.listAgents()).agents.map((r) => r.spec);
    this.agentList = { at: Date.now(), agents };
    return agents;
  }

  /** Nothing answers by default: say how to tag an agent this app may reach, naming one. */
  private async tagOne(routing: Routing | undefined): Promise<Failure> {
    const known = await this.agents().catch(() => [] as Agent[]);
    const example = known
      .map((a) => a.name)
      .filter((name) => !routing || mayRoute(routing, name))
      .sort()[0];
    return { kind: "forbidden", message: this.copy.auto.tagOne(example) };
  }

  /** The client record is read again every half minute, so a change in the console takes effect. */
  private async routing(): Promise<Routing> {
    if (!this.opts.clientId) return { defaultAgent: this.opts.defaultAgent };
    if (!this.clientRecord || Date.now() - this.clientRecord.at >= 30_000) {
      const found = await this.warp.getClient(this.opts.clientId);
      // A client that is gone, is for another surface, or does not name this team routes to nothing.
      const ours = found && found.surface === "slack" && found.identity?.team === this.opts.team;
      this.clientRecord = {
        at: Date.now(),
        client: ours
          ? found
          : { id: this.opts.clientId, surface: "slack", enabled: false, agents: [] },
      };
    }
    return { client: this.clientRecord.client, defaultAgent: this.opts.defaultAgent };
  }

  private href = (path: string) =>
    this.opts.publicUrl ? `${this.opts.publicUrl}${path}` : undefined;

  // Events

  private async onEvent(event: SlackEvent): Promise<void> {
    if (!event) return;
    if (event.type === "app_home_opened") {
      if (event.tab === "home" && event.user) await this.publishHome(event.user);
      // The app's messages: Slack offers the suggested prompts above the composer.
      if (event.tab === "messages" && event.channel) await this.suggest(event.channel);
      return;
    }
    if (event.type === "assistant_thread_started") {
      const at = event.assistant_thread;
      if (at?.channel_id) await this.suggest(at.channel_id, at.thread_ts);
      return;
    }
    if (event.type === "agent_session_stopped") {
      const channel = event.channel ?? event.channel_id;
      if (!channel || !event.thread_ts) return;
      await this.stop({ team: this.opts.team, channel, threadTs: event.thread_ts });
      return;
    }
    const asked =
      event.type === "app_mention" || (event.type === "message" && event.channel_type === "im");
    // Loop prevention: a bot's message, this app's own, an edit or a join never starts a run.
    if (!asked || (event.subtype && event.subtype !== "file_share") || event.bot_id || !event.user)
      return;
    if (event.user === this.opts.botUser || !event.channel || !event.ts) return;
    const session: Session = {
      team: this.opts.team,
      channel: event.channel,
      threadTs: event.thread_ts ?? event.ts,
    };
    const text = (event.text ?? "").replaceAll(`<@${this.opts.botUser}>`, " ").trim();
    if (!text && !event.files?.length) return;
    await this.offerSkills(event.channel);
    this.enqueue({
      files: Boolean(event.files?.length),
      session,
      text: text || "Please review the attached files.",
      triggerTs: event.ts,
      user: event.user,
      key: eventKey(session, event.ts),
      // A direct message is always addressed to the app, so only a channel thread has more to read.
      history: event.type === "app_mention" && session.threadTs !== event.ts,
    });
  }

  private readonly skillChannels = new Map<string, string>();

  /** Slack supports dynamic commands in code channels. Other channels use /skill. */
  private async offerSkills(channel: string) {
    const commands = (await this.routing()).client?.slack?.commands ?? [];
    if (!commands.length && !this.skillChannels.has(channel)) return;
    const signature = JSON.stringify(commands);
    if (this.skillChannels.get(channel) === signature) return;
    try {
      await this.slack.call("agents.conversations.setCommands", { channel_id: channel, commands });
    } catch (error) {
      if (!(error instanceof SlackError) || error.code !== "not_agent_channel") {
        this.log("Skill commands could not be registered; /skill remains available.");
        return;
      }
    }
    if (this.skillChannels.size >= 4096) this.skillChannels.clear();
    this.skillChannels.set(channel, signature);
  }

  /** Offer this deployment's suggested prompts. Without any, Slack is not asked. */
  private async suggest(channel: string, threadTs?: string): Promise<void> {
    if (this.branding.prompts.length === 0) return;
    await this.slack
      .call("assistant.threads.setSuggestedPrompts", {
        channel_id: channel,
        ...(threadTs ? { thread_ts: threadTs } : {}),
        ...suggestedPrompts({ suggestions: this.branding.prompts }, this.branding.tagline),
      })
      .catch((e: Error) => this.log(`suggested prompts skipped: ${e.message}`));
  }

  // The queue: one turn at a time per thread, as in the console's composer.

  private thread(session: Session): Thread {
    const name = sessionName(session);
    let thread = this.threads.get(name);
    if (!thread) {
      thread = { queue: emptyQueue("auto"), jobs: new Map() };
      this.threads.set(name, thread);
    }
    return thread;
  }

  private enqueue(job: Job) {
    if (this.seen.has(job.key)) return;
    this.seen.set(job.key, true);
    const thread = this.thread(job.session);
    thread.jobs.set(job.key, job);
    const [queue, effect] = queueReduce(thread.queue, {
      type: "send",
      message: { id: job.key, text: job.text, queuedAt: new Date().toISOString() },
    });
    thread.queue = queue;
    if (effect.type === "start") {
      this.track(this.drive(thread, job, false));
      return;
    }
    // It waits behind the turn in flight. The reaction says it was seen.
    this.track(this.react(job, null, "queued"));
  }

  private async react(job: Job, prev: Turn["status"] | null, next: Turn["status"]) {
    const { add, remove } = reactionChanges(prev, next);
    const at = { channel: job.session.channel, timestamp: job.triggerTs };
    for (const name of remove)
      await this.slack.call("reactions.remove", { ...at, name }).catch(() => {});
    for (const name of add) await this.slack.call("reactions.add", { ...at, name }).catch(() => {});
  }

  /** Answer one message, then whatever the queue releases next. */
  private async drive(thread: Thread, first: Job, waited: boolean): Promise<void> {
    let job: Job | undefined = first;
    let queued = waited;
    while (job) {
      let outcome: "done" | "failed" | "stopped" = "failed";
      try {
        outcome = await this.answer(thread, job, queued);
      } catch (e) {
        this.log(`turn failed: ${(e as Error).message}`);
      }
      thread.live = undefined;
      thread.jobs.delete(job.key);
      let [queue, effect] = queueReduce(thread.queue, { type: "turnEnded", outcome });
      if (queue.status === "held" && queue.heldReason === "stopped") {
        // Stop means stop: what was waiting behind the stopped turn is not started.
        for (const m of queue.pending) {
          const waiting = thread.jobs.get(m.id);
          if (waiting) await this.react(waiting, "queued", "stopped");
          thread.jobs.delete(m.id);
        }
        [queue, effect] = queueReduce(queue, { type: "clear" });
      } else if (queue.status === "held") {
        // Slack has no control to release a held queue, and each message is its own question.
        [queue, effect] = queueReduce(queue, { type: "resume" });
      }
      thread.queue = queue;
      job = effect.type === "start" ? thread.jobs.get(effect.message.id) : undefined;
      queued = true;
    }
    if (thread.queue.status === "idle" && thread.jobs.size === 0) {
      this.threads.delete(sessionName(first.session));
    }
  }

  /**
   * Where a message sits in its conversation. The conversation's id follows
   * from the thread, so warp is asked rather than a local record.
   */
  private async place(job: Job): Promise<{ key: string; after?: string } | "duplicate"> {
    // A turn asked again continues where again() found it, after the turn,
    // in its conversation: one this app started, or the warp thread a mapped
    // person's Slack thread is. Asked again before by the same person, it is
    // that run.
    if (job.again) {
      const again = await this.warp
        .getRun(runId(this.opts.tenant, this.opts.subject, job.key))
        .catch(() => null);
      return again ? "duplicate" : { key: job.key, after: job.again.after };
    }
    const first = threadKey(job.session);
    const turns = (await this.warp.getThread(runId(this.opts.tenant, this.opts.subject, first)))
      ?.turns;
    // The first run in a thread is keyed by the thread, which is what names the conversation.
    if (!turns || turns.length === 0) return { key: first };
    if (job.key === first) return "duplicate";
    if (turns.some((t) => t.id === runId(this.opts.tenant, this.opts.subject, job.key)))
      return "duplicate";
    const latest = turns
      .filter((t) => !t.parent)
      .sort((a, b) => a.created_at.localeCompare(b.created_at))
      .at(-1);
    return { key: job.key, after: latest?.id };
  }

  private target(job: Job) {
    return {
      channel: job.session.channel,
      threadTs: job.session.threadTs,
      triggerTs: job.triggerTs,
      recipientUser: job.user,
      recipientTeam: job.session.team,
    };
  }

  /** A turn that ended before any run existed: nothing to retry, only something to read. */
  private async refuse(job: Job, presenter: Presenter, agent: string, failure: Failure) {
    const turn = reduce(emptyTurn(job.key, { name: agent || "warp" }), {
      type: "turn.failed",
      failure: { ...failure, retryable: false },
    });
    await presenter.finish(turn);
    this.native = this.native && presenter.streaming;
    return "failed" as const;
  }

  private async answer(thread: Thread, job: Job, queued: boolean) {
    const items = new Mailbox<Item>();
    const live: Live = { stopped: false, wake: (item) => items.push(item) };
    thread.live = live;
    const presenter = new Presenter(this.slack, this.target(job), {
      native: this.native,
      copy: this.copy,
      log: (line) => this.log(line),
      ...(queued ? { shown: "queued" as const } : {}),
    });

    let placed: Awaited<ReturnType<App["place"]>>;
    let agent = job.again?.agent ?? "";
    let text = job.text;
    let run: Run;
    let routing: Routing | undefined;
    try {
      routing = await this.routing();
      // A person the client maps posts as themselves into the warp thread this
      // Slack thread is: the platform reads the tags, files it from the binding,
      // and starts what it calls for. Anyone else asks through the app. A turn
      // asked again acts for whom again() decided, never for whoever else is
      // known, and someone else's words are asked for nobody.
      const asking = job.again ? job.again.as : job.notTheirs ? null : await this.actAs(job.user);
      if (asking && !job.again) {
        if (live.stopped) {
          await presenter.finish(
            reduce(emptyTurn(job.key, { name: "warp" }), { type: "turn.stopped" }),
          );
          return "stopped" as const;
        }
        const posted = await this.cowork(job, asking);
        // Warp already has this message: it was answered before a restart.
        if (posted.repeated) return "done" as const;
        const first = posted.runs[0];
        if (!first) return this.unanswered(job, presenter, posted, routing);
        if (posted.runs.length > 1) {
          this.log(
            `message ${posted.message.id} also started ${posted.runs
              .slice(1)
              .map((r) => r.id)
              .join(", ")}`,
          );
        }
        run = first;
        agent = run.agent;
      } else {
        // A bound channel is a warp channel: its thread, its default, its
        // allow list and its limits are the platform's, and only a person
        // the client maps posts there, in their own words. Anyone else is
        // told so, never answered around it, and so is someone asking about
        // another's message there, which nobody may post as them.
        if (!asking && !job.again && (await this.isBound(job.session.channel)))
          return this.refuse(job, presenter, "", {
            kind: "forbidden",
            message: job.notTheirs
              ? this.copy.threads.notTheirsInBound
              : this.copy.threads.unmappedInBound,
          });
        if (job.files)
          throw new WarpError(403, "Files need a connected workspace account and client.");
        placed = await this.place(job);
        // Warp already has a run for this Slack event: it was answered before a restart.
        if (placed === "duplicate") return "done" as const;
        if (!job.again) {
          const choice = choose(job.text, await this.agents(), routing);
          if (choice.agent === null) return this.refuse(job, presenter, "", NO_AGENT);
          agent = choice.agent;
          text = choice.text;
        } else if (!mayRoute(routing, agent)) {
          // Asked again after the client was disabled or the agent left its list.
          return this.refuse(job, presenter, agent, NO_AGENT);
        }
        if (job.history) text = await this.withThread(job, text);
        // Stop was pressed before there was a run to stop: say so and settle the session.
        if (live.stopped) {
          await presenter.finish(
            reduce(emptyTurn(job.key, { name: agent || "warp" }), { type: "turn.stopped" }),
          );
          return "stopped" as const;
        }
        // Who asked, the client and the thread are known here and have nowhere to go:
        // the route takes none of them. See the README.
        // A person the client maps asks as themselves, so they do not approve
        // their own run's calls.
        run = await this.warp.createRun(
          {
            agent,
            text,
            ...(placed.after ? { after: placed.after } : {}),
            ...(asking ? { act_as: asking } : {}),
          },
          placed.key,
        );
      }
    } catch (e) {
      this.log(`warp refused the turn: ${(e as Error).message}`);
      // A reply under an announcement that was withdrawn starts nothing.
      if (e instanceof WarpError && e.status === 410)
        return this.refuse(job, presenter, "", {
          kind: "forbidden",
          message: this.copy.announcements.replyGone,
        });
      // No agent was named, and nothing answers here by default.
      if (agent === "" && e instanceof WarpError && e.status === 409)
        return this.refuse(job, presenter, "", await this.tagOne(routing));
      return this.refuse(job, presenter, agent, publicFailure(e));
    }

    live.run = run;
    if (live.stopped) {
      // Stop was pressed while the run was being started, so it is told now.
      const told = await this.warp.stopRun(run.id).catch(() => undefined);
      items.push({ type: "stopped", run: told });
    }
    const owner = job.requester === undefined ? job.user : job.requester;
    if (owner) this.requesters.set(run.id, owner);
    this.log(`run ${run.id} for ${job.user} in ${job.session.channel}, answered by ${run.agent}`);
    const following = follow(run, { href: this.href });
    const abort = new AbortController();
    const pump = this.pump(run, following, presenter, items, abort.signal);
    let turn: Turn;
    let stalled: boolean;
    try {
      ({ turn, stalled } = await pump);
    } finally {
      abort.abort();
    }
    // A quiet run's stream is closed as it stands: still working, so the session
    // stays processing and Stop stays available. Its end is posted when it comes.
    await presenter.finish(turn);
    this.native = this.native && presenter.streaming;
    if (stalled) {
      this.track(this.later(run, following, presenter));
      // The thread is not held for it. A later message follows it in the conversation,
      // as warp orders any turn sent after one that is still running.
      return "done" as const;
    }
    this.log(`run ${run.id} ended ${turn.status}`);
    return turn.status === "done" ? "done" : turn.status === "stopped" ? "stopped" : "failed";
  }

  /** Where on Slack a message of this job's thread was posted. */
  private surfaceOf(job: Job, messageTs: string): SurfaceRef {
    return {
      client: this.opts.clientId ?? "",
      channel: job.session.channel,
      ts: job.session.threadTs,
      message: messageTs,
      app_user: this.opts.botUser,
      ...(job.files ? { files: true } : {}),
    };
  }

  /**
   * Post a mapped person's message into the warp thread this Slack thread
   * is. In a channel thread, what people said there since the app was last
   * asked goes first, each as its person and posted only, so the room reads
   * as it did in Slack; someone the client does not map has no voice there.
   * Under an announcement this app posted, the thread is the
   * announcement's: warp finds it by the post's receipt and starts it with
   * the announcement, ahead of these messages.
   */
  private async cowork(job: Job, asking: ActAs): Promise<ThreadPosted> {
    if (job.history) {
      const at = {
        channel: job.session.channel,
        threadTs: job.session.threadTs,
        triggerTs: job.triggerTs,
      };
      try {
        const said = sinceLastTurn(await readThread(this.slack, at), {
          botUser: this.opts.botUser,
          ...at,
        });
        for (const m of said) {
          const as = m.user && m.ts ? await this.actAs(m.user) : null;
          if (!as || !m.ts || !(m.text ?? "").trim()) continue;
          await this.warp.postThread({
            text: m.text ?? "",
            act_as: as,
            surface: this.surfaceOf(job, m.ts),
            post_only: true,
          });
        }
      } catch (e) {
        this.log(`thread history skipped: ${(e as Error).message}`);
      }
    }
    return this.warp.postThread({
      text: job.text,
      act_as: asking,
      surface: this.surfaceOf(job, job.triggerTs),
    });
  }

  /**
   * A message that started nothing: it told people, was posted only, or
   * nobody answers here. Only the last is a failure.
   */
  private async unanswered(
    job: Job,
    presenter: Presenter,
    posted: ThreadPosted,
    routing: Routing | undefined,
  ) {
    if (posted.rule === "none" || posted.reason) {
      return this.refuse(
        job,
        presenter,
        "",
        posted.reason ? { kind: "forbidden", message: posted.reason } : await this.tagOne(routing),
      );
    }
    const said =
      posted.notified.length > 0
        ? this.copy.threads.notified(posted.notified)
        : this.copy.threads.postOnlyHint;
    let turn = reduce(emptyTurn(job.key, { name: "warp" }), {
      type: "turn.started",
      turnId: job.key,
      agent: { name: "warp" },
    });
    turn = reduce(turn, { type: "text.delta", text: said });
    await presenter.finish(reduce(turn, { type: "turn.completed" }));
    this.native = this.native && presenter.streaming;
    return "done" as const;
  }

  /**
   * The question with what people said in its thread since the app was last
   * asked there. The run takes only text, so the messages go ahead of it, each
   * with who wrote it. A thread Slack will not give is no reason to fail the
   * turn: the question goes alone.
   */
  private async withThread(job: Job, text: string): Promise<string> {
    const at = {
      channel: job.session.channel,
      threadTs: job.session.threadTs,
      triggerTs: job.triggerTs,
    };
    try {
      const said = sinceLastTurn(await readThread(this.slack, at), {
        botUser: this.opts.botUser,
        ...at,
      });
      if (said.length === 0) return text;
      const names = await this.names.of(said.map((m) => m.user ?? ""));
      return withHistory(text, said, names);
    } catch (e) {
      this.log(`thread history skipped: ${(e as Error).message}`);
      return text;
    }
  }

  /**
   * Follow a quiet run by its record, less and less often, until it ends, then
   * post its end in the thread. A stop pressed meanwhile confirms it instead.
   */
  private async later(run: Run, following: Follower, presenter: Presenter): Promise<void> {
    let wait = this.pollMs;
    try {
      while (!this.closing.signal.aborted) {
        const nudged = await this.nap(run.id, wait);
        // A stop was pressed: the run will end soon, so it is read often again.
        wait = nudged ? Math.min(this.pollMs, 500) : Math.min(wait * 2, this.slowPollMs);
        if (this.closing.signal.aborted) return;
        const record = await this.warp.getRun(run.id).catch(() => null);
        if (!record || !terminal(record.status)) continue;
        if (!this.claim(run.id)) return;
        following.settle(record);
        await presenter.conclude(following.turn);
        this.log(`run ${run.id} ended ${following.turn.status}, after it went quiet`);
        return;
      }
    } finally {
      this.slow.delete(run.id);
    }
  }

  /** Wait, until the time is up, the app closes, or the run is nudged. Reports whether it was nudged. */
  private nap(id: string, ms: number): Promise<boolean> {
    return new Promise((resolve) => {
      const ctl = new AbortController();
      const stopWaiting = () => ctl.abort();
      this.closing.signal.addEventListener("abort", stopWaiting, { once: true });
      let nudged = false;
      this.slow.set(id, () => {
        nudged = true;
        ctl.abort();
      });
      sleep(ms, ctl.signal).then(() => {
        this.closing.signal.removeEventListener("abort", stopWaiting);
        resolve(nudged);
      });
    });
  }

  /** Claim the posting of a run's end. False when it has been posted already. */
  private claim(id: string): boolean {
    if (this.confirmed.has(id)) return false;
    this.confirmed.set(id, true);
    return true;
  }

  /**
   * Fold a run's events into its turn and keep Slack in step, until the turn
   * ends. The record is read beside the stream, because the ledger decides
   * whether a run is over, and a stream can miss the event that says so.
   */
  private async pump(
    run: Run,
    following: Follower,
    presenter: Presenter,
    items: Mailbox<Item>,
    signal: AbortSignal,
  ): Promise<{ turn: Turn; stalled: boolean }> {
    (async () => {
      try {
        for await (const item of this.warp.runEvents(run, { signal })) items.push(item);
      } catch (e) {
        if (!signal.aborted) items.push({ type: "broken", error: e as Error });
      }
    })();
    const read = async () => {
      const record = await this.warp.getRun(run.id).catch(() => null);
      if (record && !signal.aborted) items.push({ type: "record", run: record });
    };
    const poll = setInterval(read, this.pollMs);
    let tick: ReturnType<typeof setTimeout> | undefined;
    signal.addEventListener("abort", () => {
      clearInterval(poll);
      clearTimeout(tick);
    });

    // The session shows work has begun before the first event arrives.
    await presenter.update(following.turn);
    let dirty = false;
    let last = 0;
    let over = 0;
    let progress = Date.now();
    let seen = run.updated_at;
    for (;;) {
      const item = await items.next();
      if (item.type === "stopped") {
        if (item.run && terminal(item.run.status)) {
          following.settle(item.run);
          return { turn: following.turn, stalled: false };
        }
        if (!item.run) {
          // Warp did not take the stop: the turn stops here, and says it may still finish elsewhere.
          if (!ended(following.turn)) {
            following.push({ type: "turn.stopped" }, { type: "stop.updated", stop: "failed" });
          }
          return { turn: following.turn, stalled: false };
        }
        // Warp took it and the run is stopping. It is followed on until
        // lifecycle.stopped, or its record, says how it ended: stopped with
        // what it had said, or completed if it finished first.
        following.push({ type: "stop.updated", stop: "acknowledged" });
        dirty = true;
      }
      if (item.type === "record" && item.run.updated_at !== seen) {
        seen = item.run.updated_at;
        progress = Date.now();
      }
      if (item.type === "event" && item.event.run_id === run.id) {
        progress = Date.now();
        const before = following.turn;
        dirty = following.apply(item.event) !== before || dirty;
        // A question shows with its buttons at once, and its card settles
        // where it was shown when it is answered, whoever answered it.
        if (waitsOnPerson(following.turn)) {
          await presenter.pause(following.turn);
          dirty = false;
        } else if (following.turn.interrupts !== before.interrupts) {
          await presenter.settleQuestion(following.turn);
        }
      } else if (item.type === "record" && terminal(item.run.status)) {
        // An ended run is given one more read for its final event to arrive, so
        // the streamed answer is not cut short. The record carries the answer too.
        over += 1;
        if (over > 1) following.settle(item.run);
      } else if (item.type === "record" && Date.now() - progress > this.stallMs) {
        // Nothing has been heard for a long time. That is not a failure: a long tool
        // call, or a run waiting on other work, says nothing until it moves. The
        // stream is closed and the run is followed slowly from here.
        this.log(`run ${run.id} quiet for ${this.stallMs}ms, following it slowly`);
        return { turn: following.turn, stalled: true };
      } else if (item.type === "broken") {
        this.log(`event stream for ${run.id} ended: ${item.error.message}`);
        await read();
      }
      if (ended(following.turn)) return { turn: following.turn, stalled: false };
      if (!dirty || !items.empty) continue;
      const wait = this.flushMs - (Date.now() - last);
      if (wait > 0) {
        // Slack prefers fewer, larger appends: what arrives meanwhile goes out together.
        clearTimeout(tick);
        tick = setTimeout(() => items.push({ type: "tick" }), wait);
        continue;
      }
      await presenter.update(following.turn);
      dirty = false;
      last = Date.now();
    }
  }

  // Stop

  /**
   * The stop button, in the order Slack asks for: stop the work, confirm in
   * the thread, then move the session off processing. Slack changes nothing by
   * itself. Which runs to stop is asked of warp as well as remembered, so
   * the button works after a restart.
   */
  private async stop(session: Session): Promise<void> {
    const thread = this.threads.get(sessionName(session));
    const live = thread?.live;
    // A turn whose run is still being started stops itself once it has one.
    const following = live?.run;
    if (live) live.stopped = true;
    const ids = new Set<string>();
    if (following) ids.add(following.id);
    const conversation = runId(this.opts.tenant, this.opts.subject, threadKey(session));
    const turns = (await this.warp.getThread(conversation).catch(() => null))?.turns;
    for (const t of turns ?? []) if (!t.parent && !terminal(t.status)) ids.add(t.id);

    const stopped: Run[] = [];
    for (const id of ids) {
      try {
        stopped.push(await this.warp.stopRun(id));
      } catch (e) {
        this.log(`stop of ${id} failed: ${(e as Error).message}`);
      }
    }
    if (live && following) {
      // The turn in flight confirms in its own message and settles the session.
      live.wake({ type: "stopped", run: stopped.find((r) => r.id === following.id) });
    }
    // Runs this process is not following live. One it follows slowly is
    // looked at again at once, and its slow follow posts its end, stopped or
    // not. Any other is read until it ends and its end is posted here, in
    // Slack's order: the end, then the status. One that has not ended by
    // stopWaitMs is left to a slow follow, and the session stays working
    // until it has.
    const deadline = Date.now() + this.stopWaitMs;
    const ends = await Promise.all(
      stopped
        .filter((run) => run.id !== following?.id)
        .map((run) => {
          const nudge = this.slow.get(run.id);
          if (nudge) {
            nudge();
            return Promise.resolve(false);
          }
          return this.endOf(session, run, deadline);
        }),
    );
    if (!live && ends.every(Boolean)) await this.settleSession(session);
  }

  /** Set a session's status to active: nothing is working in it any more. */
  private async settleSession(session: Session): Promise<void> {
    await this.slack
      .call("agents.sessions.setStatus", {
        channel_id: session.channel,
        thread_ts: session.threadTs,
        status: "active",
      })
      .catch((e: Error) => this.log(`session status skipped: ${e.message}`));
  }

  /**
   * Post a run's end in its thread once: what it had said, marked stopped, or
   * its answer when it finished before the stop reached it.
   */
  private async postEnd(session: Session, run: Run): Promise<void> {
    if (!this.claim(run.id)) return;
    if (run.status !== "stopped")
      this.log(`run ${run.id} ended ${run.status} before the stop reached it`);
    const turn = follow(run, { href: this.href }).settle(run);
    await this.postView(session, renderToSlack(<TurnView copy={this.copy} turn={turn} />)).catch(
      (e: Error) => this.log(`the end of run ${run.id} was not posted: ${e.message}`),
    );
  }

  /**
   * Read a run that was asked to stop until it ends, and post its end. Reports
   * whether it ended by the deadline. One that has not is followed on slowly,
   * and its end is posted, and the session settled, when it comes.
   */
  private async endOf(session: Session, run: Run, deadline: number): Promise<boolean> {
    // A stop takes seconds, not minutes, so it is read more often than a quiet run.
    let wait = Math.min(this.pollMs, 500);
    let record: Run | null = run;
    while (
      !(record && terminal(record.status)) &&
      !this.closing.signal.aborted &&
      Date.now() < deadline
    ) {
      await sleep(Math.min(wait, Math.max(deadline - Date.now(), 0)), this.closing.signal);
      wait = Math.min(wait * 2, 5_000);
      record = await this.warp.getRun(run.id).catch(() => null);
    }
    if (record && terminal(record.status)) {
      await this.postEnd(session, record);
      return true;
    }
    if (this.closing.signal.aborted) return false;
    this.log(
      `run ${run.id} was asked to stop and had not ended after ${this.stopWaitMs}ms; following it slowly`,
    );
    this.track(this.endLater(session, run));
    return false;
  }

  /** Follow a stopping run by its record, less and less often, then post its end and settle the session. */
  private async endLater(session: Session, run: Run): Promise<void> {
    let wait = Math.min(this.pollMs, 500);
    while (!this.closing.signal.aborted) {
      await sleep(wait, this.closing.signal);
      wait = Math.min(wait * 2, this.slowPollMs);
      if (this.closing.signal.aborted) return;
      const record = await this.warp.getRun(run.id).catch(() => null);
      if (!record || !terminal(record.status)) continue;
      await this.postEnd(session, record);
      if (!this.threads.get(sessionName(session))?.live) await this.settleSession(session);
      return;
    }
  }

  // Clicks

  private async whisper(p: ActionPayload, text: string) {
    const channel = p.channel?.id ?? p.container?.channel_id;
    if (!channel) return;
    await this.slack
      .call("chat.postEphemeral", {
        channel,
        user: p.user.id,
        // Reply where the button was clicked, not in the channel above the thread.
        ...(p.message?.thread_ts ? { thread_ts: p.message.thread_ts } : {}),
        text,
      })
      .catch(() => {});
  }

  private async onAction(p: ActionPayload): Promise<void> {
    const action = p.actions?.[0];
    const ui = action ? decodeSlackAction(action, p.state) : null;
    // Ticking a checkbox is not a decision. The buttons read the ticks.
    if (!ui) return;
    const dedupe = `action-${p.trigger_id ?? ""}`;
    if (p.trigger_id && this.seen.has(dedupe)) return;
    this.seen.set(dedupe, true);

    if (ui.id === ActionId.feedback) return this.onFeedback(ui, p);
    // The Home tab's own controls, and Review on an item it shows.
    if (ui.payload.turnId === HOME_ID) return this.onHome(p, ui);
    if (ui.id === ActionId.open && (p.view?.type === "home" || p.container?.type === "view"))
      return this.review(p, ui.payload.turnId);
    const as = await this.actAs(p.user.id);
    const readFor = (who: ActAs) =>
      this.warp.inboxMessageFor(ui.payload.turnId, who).catch(() => null);
    // A message the app sent the person as theirs is theirs to act on, even
    // where the app may read it too: it is read and done for them.
    const sent = sentToThem(p);
    let mine = as && sent ? await readFor(as) : null;
    if (mine && as) return this.onPersonal(p, ui, mine, as);
    // A click on an inbox message names the message. Anything else names a run.
    const message = await this.warp.inbox.get(ui.payload.turnId).catch(() => null);
    if (message) return this.onMail(p, ui, message);
    // One addressed to the person who clicked is read for them, through the client.
    mine = as && !sent ? await readFor(as) : null;
    if (mine && as) return this.onPersonal(p, ui, mine, as);
    if (answers(ui)) return this.onAnswer(p, ui);
    if (ui.id === ActionId.retry || ui.id === ActionId.resume) {
      const run = await this.warp.getRun(ui.payload.turnId);
      const outcome = run ? await this.again(run, p.user.id, p.message?.ts) : "elsewhere";
      if (outcome !== "started") await this.refuseRetry(p, outcome);
    }
    // Opening a called run, reporting and reconnecting have nowhere to go from Slack yet.
  }

  /** Say why a turn was not asked again. */
  private refuseRetry(p: ActionPayload, outcome: Exclude<Again, "started">) {
    const text =
      outcome === "not-theirs"
        ? defaultCopy.retryOnlyTheirs
        : outcome === "not-yours"
          ? RETRY_NOT_YOURS
          : defaultCopy.cantRetry;
    return this.whisper(p, text);
  }

  /**
   * Ask a turn again, in the thread it was asked in, for whoever the turn
   * acted for, as Retry in warp's inbox does. Retry and Resume both come here.
   *
   * A turn that acted for a person is theirs to ask again, so only a Slack
   * person this app's client maps to that same person may, and the new run is
   * asked as them. Anyone else, an operator included, is refused: they would
   * either run that person's words with their own credentials, or that
   * person's credentials on a choice the person never made. A turn that acted
   * for nobody is asked again for nobody, through the app, by the person who
   * asked for it or an operator, so nobody's credentials are put behind words
   * that were not theirs. Warp's record says whom a turn acted for, so a
   * restart, which forgets who asked, changes none of this, except that only
   * an operator may then ask again for nobody.
   *
   * A turn this app asked as itself acted for its own credential: nobody when
   * that is a machine, and the person it is when it is one, as under dev
   * sign-in, where every request is that person's. Either way it is asked
   * again as it was asked, through the app, so it acts for the same one, and
   * whoever may ask again for nobody may ask it.
   *
   * The key names who pressed, so two people's presses are two runs and one
   * person pressing twice joins their first. The routing rules are checked
   * again.
   */
  private async again(run: Run, user: string, clickedTs?: string): Promise<Again> {
    const session = await this.sessionOfRun(run);
    if (!session || session.team !== this.opts.team) return "elsewhere";
    const owner = this.requesters.get(run.id);
    const acted = actedFor(run);
    // Asked through the app again, a turn the app asked as itself acts for the same one.
    const person = acted === this.opts.subject ? "" : acted;
    let as: ActAs | null = null;
    if (person) {
      const mapped = await this.mapped(user);
      if (!mapped || mapped.person !== person) return "not-theirs";
      as = mapped.as;
    } else {
      const operator = (this.opts.operators ?? []).includes(user);
      if (!operator && (!owner || owner !== user)) return "not-yours";
    }
    if (!mayRoute(await this.routing(), run.agent)) return "not-routed";
    const { agent, text, after } = retryOf(run);
    this.enqueue({
      session,
      text,
      triggerTs: clickedTs ?? session.threadTs,
      user,
      // A turn asked again for its person is theirs to settle; one asked again
      // for nobody stays its first asker's.
      requester: person ? user : (owner ?? null),
      key: `slack-again-${short(`${user}\n${run.id}`)}`,
      // The first turn of a conversation is asked again after itself, so the thread stays one conversation.
      again: { agent, after: after ?? run.id, as },
    });
    return "started";
  }

  /** Open while the trigger is fresh; a skipped modal still keeps the rating. */
  private async onFeedback(ui: UiAction, p: ActionPayload) {
    const event = feedbackEvent(ui);
    if (event?.type !== "feedback" || !event.rating) return;
    const modal = p.trigger_id
      ? this.slack.call("views.open", {
          trigger_id: p.trigger_id,
          view: feedbackModal({ turnId: ui.payload.turnId, rating: event.rating }),
        })
      : Promise.resolve();
    const saved = this.saveFeedback(ui.payload.turnId, p.user.id, event.rating);
    const results = await Promise.allSettled([modal, saved]);
    if (results.some((r) => r.status === "rejected")) {
      this.log(`feedback could not be completed for ${ui.payload.turnId}`);
      await this.whisper(p, "Your feedback could not be saved or opened. Please try again.");
    }
  }

  private readonly feedbackSaves = new Map<string, Promise<unknown>>();

  private async saveFeedback(
    run: string,
    user: string,
    rating: "up" | "down",
    comment?: string,
    reason?: string,
  ) {
    const key = `${run}/${user}`;
    const previous = this.feedbackSaves.get(key) ?? Promise.resolve();
    const saved = previous
      .catch(() => {})
      .then(async () => {
        const as = await this.actAs(user);
        if (!as) throw new Error("Feedback needs a connected workspace account");
        return this.warp.saveFeedback(run, {
          rating,
          ...(comment ? { comment } : {}),
          ...(reason ? { reason } : {}),
          act_as: as,
        });
      });
    this.feedbackSaves.set(key, saved);
    try {
      return await saved;
    } finally {
      if (this.feedbackSaves.get(key) === saved) this.feedbackSaves.delete(key);
    }
  }

  // Questions a run waits on

  /**
   * Who a Slack person is in the workspace, as this app's client maps them,
   * or null. Only the client this app is maps people, and only for the
   * credential it names, which must be this app's own.
   */
  private async actAs(user: string): Promise<ActAs | null> {
    return (await this.mapped(user))?.as ?? null;
  }

  /** How to ask as a Slack person, and whom the client maps them to, or null (see actAs). */
  private async mapped(user: string): Promise<{ as: ActAs; person: string } | null> {
    const routing = await this.routing().catch(() => null);
    const client = routing?.client;
    if (!client || !this.opts.clientId || !client.enabled) return null;
    const person = client.slack?.machine === this.opts.subject ? client.slack?.people?.[user] : "";
    return person ? { as: { client: this.opts.clientId, user }, person } : null;
  }

  /**
   * Answer a question shown on a run, for the person who clicked. Warp
   * decides whether they may: it is asked as them, through the client that
   * maps them, and a person it does not map is never asked for at all. The
   * card settles from the run's own events.
   */
  private async onAnswer(p: ActionPayload, ui: UiAction): Promise<void> {
    const answer = answerOfAction(ui);
    if (!answer) {
      // warp: a modal that collects a reply to a question (as feedbackModal does for a
      // rating), for Something Else and Reply. Until then a reply is typed in the console.
      if (ui.id === ActionId.clarifyOther) return this.whisper(p, this.words.replyInConsole);
      return;
    }
    const as = await this.actAs(p.user.id);
    if (!as) {
      this.log(`an answer from ${p.user.id} was not sent: this client does not map them to anyone`);
      return this.whisper(p, defaultCopy.noPermission);
    }
    try {
      await this.warp.answerQuestion(answer, as);
      this.log(`question ${answer.question} on ${answer.run} answered for ${p.user.id}`);
    } catch (e) {
      this.log(
        `question ${answer.question} on ${answer.run} was not answered: ${(e as Error).message}`,
      );
      const status = e instanceof WarpError ? e.status : 0;
      return this.whisper(
        p,
        status === 403 ? defaultCopy.noPermission : defaultCopy.failure.internal(),
      );
    }
  }

  // The inbox

  /**
   * Retry on an inbox failure. A retry is always this app's, so its answer
   * shows in the run's thread and the client's agents are checked: warp is
   * never asked to retry on its own, and the message is only acknowledged.
   * Only a failed run can be asked again, and a Retry on anything else is
   * refused. The part to acknowledge, or null when nothing was asked.
   */
  private async retried(
    p: ActionPayload,
    part: PlatformMessage["parts"][number] | undefined,
    clickedTs?: string,
  ): Promise<string | null> {
    if (part?.type !== "failure") {
      await this.refuseRetry(p, "elsewhere");
      return null;
    }
    const run = part.failure.reference ? await this.warp.getRun(part.failure.reference) : null;
    const outcome = run ? await this.again(run, p.user.id, clickedTs) : "elsewhere";
    if (outcome !== "started") {
      await this.refuseRetry(p, outcome);
      return null;
    }
    return part.id;
  }

  /** Whether a Slack person may settle an inbox message here: they asked for the run, or they are an operator. */
  private maySettle(user: string, message: PlatformMessage): boolean {
    if ((this.opts.operators ?? []).includes(user)) return true;
    return message.about.some((s) => s.kind === "run" && this.requesters.get(s.id) === user);
  }

  private async onMail(p: ActionPayload, ui: UiAction, message: PlatformMessage) {
    const part = message.parts.find((x) => partMatches(x, ui.payload.target));
    const question = part?.type === "approval" || part?.type === "choice" || part?.type === "reply";
    // Who may answer a question is warp's to decide, for the person as it knows them.
    const as = question ? await this.actAs(p.user.id) : null;
    if (question && !as) return this.whisper(p, defaultCopy.noPermission);
    // Who may ask a failed turn again is again()'s to decide, as in the thread.
    if (!question && ui.id !== ActionId.retry && !this.maySettle(p.user.id, message)) {
      return this.whisper(p, defaultCopy.notYours);
    }
    if (part?.type === "reply" && ui.id === ActionId.reply && !ui.payload.text) {
      return this.askForReply(
        p,
        await this.warp.getInboxMessage(message.id).catch(() => null),
        part.id,
      );
    }
    // A failed turn is asked again here, where its answer can be shown, when the thread is this app's.
    let request = toRequest(message, ui);
    if (request?.call === "resolve" && as)
      request = { ...request, values: { ...request.values, act_as: as } };
    if (ui.id === ActionId.retry) {
      const partId = await this.retried(p, part, p.message?.ts);
      if (!partId) return;
      request = { call: "resolve", partId, action: "acknowledge" };
    }
    if (!request) return;
    let updated: PlatformMessage;
    try {
      updated = await this.warp.inbox.send(message, request);
    } catch (e) {
      // The platform's words stay in the log. The person is told in warp's.
      this.log(`inbox ${message.id} was not settled: ${(e as Error).message}`);
      const status = e instanceof WarpError ? e.status : 0;
      return this.whisper(
        p,
        status === 403 ? defaultCopy.noPermission : defaultCopy.failure.internal(),
      );
    }
    const channel = p.channel?.id ?? p.container?.channel_id;
    if (p.container?.type === "view" || p.view?.type === "home") {
      await this.publishHome(p.user.id);
    } else if (channel && p.message?.ts) {
      await this.showMail(updated, {
        channel,
        ts: p.message.ts,
        ...(p.message.thread_ts ? { threadTs: p.message.thread_ts } : {}),
      });
    }
  }

  /**
   * The reply modal was sent: the words reply to the person's mention, as
   * them, through the client, which posts a human message in the thread that
   * starts no run and settles the mention; or they answer the question a run
   * waits on. Someone the client does not map is not asked for at all.
   */
  private async onReply(user: string, text: string, target: ReplyTarget): Promise<void> {
    const as = await this.actAs(user);
    if (!as) {
      this.log(`a reply from ${user} was not sent: this client does not map them to anyone`);
      return;
    }
    try {
      if ("inbox" in target) {
        const updated = await this.warp.resolveFor(
          target.inbox,
          target.part,
          { action: "reply", text },
          as,
        );
        const told = this.told.get(toldKey(as, updated.id));
        if (told)
          await this.slack.call("chat.update", {
            channel: told.channel,
            ts: told.ts,
            ...this.personalView(updated),
          });
      } else {
        await this.warp.answerQuestion(
          { run: target.run, question: target.question, body: { action: "reply", text } },
          as,
        );
      }
    } catch (e) {
      this.log(`a reply from ${user} was refused: ${(e as Error).message}`);
    }
  }

  /** Post a rendered view in a thread, every page of it. Returns the first message. */
  private async postView(
    at: { channel: string; threadTs: string },
    view: SerializeResult,
  ): Promise<string> {
    let first = "";
    for (const [i, blocks] of view.pages.entries()) {
      const sent = await this.slack.call<{ ts: string }>("chat.postMessage", {
        channel: at.channel,
        thread_ts: at.threadTs,
        // warp: needs a fallback-text helper for continued pages, as for turns.
        text: (i === 0 ? view.text : `(${i + 1}/${view.pages.length}) ${view.text}`).slice(
          0,
          LIMITS.fallbackText,
        ),
        blocks,
      });
      first ||= sent.ts;
    }
    return first;
  }

  /** Edit a posted view. Pages beyond the first follow it in the thread. */
  private async showMail(
    message: PlatformMessage,
    at: { channel: string; ts: string; threadTs?: string },
  ) {
    const view = renderToSlack(<MessageView message={forSlack(message)} />);
    const [first = [], ...rest] = view.pages;
    await this.slack.call("chat.update", {
      channel: at.channel,
      ts: at.ts,
      text: view.text.slice(0, LIMITS.fallbackText),
      blocks: first,
    });
    if (rest.length > 0 && at.threadTs) {
      await this.postView(
        { channel: at.channel, threadTs: at.threadTs },
        { ...view, pages: rest, blocks: rest[0] ?? [] },
      );
    }
  }

  /** The Slack thread a run's conversation lives in, read from its first run's trigger id. */
  private async sessionOfRun(run: Run): Promise<Session | null> {
    const first =
      run.id === run.thread ? run : await this.warp.getRun(run.thread).catch(() => null);
    const asked = first?.tag_id ? sessionOf(this.opts.subject, first.tag_id) : null;
    if (asked) return asked;
    // A Slack thread a mapped person posts in is a warp thread with its
    // surface, and its runs answer their messages: the session is where it is.
    const surface = await this.warp.threadSurface(run.thread).catch(() => null);
    if (!surface || !this.opts.clientId || surface.client !== this.opts.clientId) return null;
    return { team: this.opts.team, channel: surface.channel, threadTs: surface.ts };
  }

  /** Whether a Slack channel is bound to a channel in warp, through this client. */
  private async isBound(channel: string): Promise<boolean> {
    return (await this.bound()).some((b) => b.slack_channel === channel);
  }

  /**
   * The Slack channels this client is bound to, read again every half minute.
   * A read that fails throws and is not kept, so nobody is answered around a
   * channel's rules because warp could not say it has them: the turn is
   * refused, an announcement waits, and the next one reads again.
   */
  private async bound(): Promise<Binding[]> {
    if (!this.opts.clientId) return [];
    if (!this.bindings || Date.now() - this.bindings.at >= 30_000) {
      const list = await this.warp.bindings(this.opts.clientId);
      this.bindings = { at: Date.now(), list };
    }
    return this.bindings.list;
  }

  /**
   * An announcement of a project whose general channel is bound here is
   * posted there, top-level, once. Where it went is recorded in warp, so a
   * restart, an edit or a withdrawal updates that post instead of posting
   * again. A draft never reaches a channel, and one withdrawn before it was
   * posted is not posted. A notice and a catch-up both come here, one look
   * at a time for each announcement, and each look reads it afresh, receipt
   * and all: a look asked for meanwhile waits for the one under way.
   */
  private announce(id: string, why: Look = "notice"): Promise<void> {
    const client = this.opts.clientId;
    if (!client) return Promise.resolve();
    const busy = this.announcing.get(id);
    if (busy) {
      if (busy.again !== "notice") busy.again = why;
      // The look asked for is the one under way's to make: its outcome is this one's.
      return busy.done;
    }
    const turn: { again: Look | null; done: Promise<void> } = {
      again: why,
      done: Promise.resolve(),
    };
    this.announcing.set(id, turn);
    turn.done = this.looks(id, client, turn).finally(() => this.announcing.delete(id));
    return turn.done;
  }

  /** The looks at one announcement, one after another, for as long as another is asked for. */
  private async looks(id: string, client: string, turn: { again: Look | null }): Promise<void> {
    // What the post shows after each look, so a look asked for meanwhile
    // leaves alone a post that already shows the announcement as it stands.
    let shown: string | undefined;
    // A look that fails does not drop one asked for meanwhile: that one
    // reads everything afresh, and only the last look's failure is told.
    let failed: unknown = null;
    while (turn.again) {
      const look = turn.again;
      turn.again = null;
      try {
        shown = await this.announceOnce(id, client, look, shown);
        failed = null;
      } catch (e) {
        failed = e;
      }
    }
    if (failed) throw failed;
  }

  /**
   * One look at an announcement, which answers the `updated_at` its post
   * shows after it. With this client's receipt, the post is edited: on a
   * notice unless the last look already showed it as it stands, on a
   * catch-up when it changed since the receipt. Once the post shows a
   * change, the receipt is recorded again, so it says so. Without one, an
   * open announcement is posted and its receipt recorded, then read once
   * more: a withdrawal between the first read and the receipt is older than
   * the receipt, and the post is edited for it now.
   */
  private async announceOnce(
    id: string,
    client: string,
    look: Look,
    shown: string | undefined,
  ): Promise<string | undefined> {
    // Only the client's machine is shown the client's receipts: any other
    // credential would post every announcement again.
    if (!(await this.isMachine())) return shown;
    // A message gone or hidden from this machine (403, 404) has nothing to
    // post. Any other failure is warp being away: it is thrown, so the
    // catch-up is tried again instead of taking the announcement as done.
    const raw = await this.warp.getInboxMessage(id).catch((e: unknown) => {
      if (e instanceof WarpError && (e.status === 403 || e.status === 404)) return null;
      throw e;
    });
    if (!raw || raw.kind !== "announcement" || isDraft(raw)) return shown;
    const project = raw.source?.project ?? "";
    const at = (await this.bound()).find((b) => b.project === project && b.channel === "general");
    if (!at) return shown;
    const receipt = raw.deliveries?.[client];
    // A receipt warp took though its answer was lost needs no remembering.
    if (receipt) this.unrecorded.delete(id);
    const posted = this.unrecorded.get(id);
    if (!receipt && posted) {
      // Posted by this process, and warp did not take the receipt then.
      await this.warp.recordDelivery(id, client, posted);
      this.unrecorded.delete(id);
      await this.slack.call("chat.update", { ...posted, ...this.newsView(raw) });
      return this.keepUp(id, client, posted, raw.updated_at);
    }
    if (receipt) {
      const changed = changedSince(raw, receipt) || this.unchecked.has(id);
      if (look === "catch-up" ? !changed : raw.updated_at === shown) return shown;
      const where = { channel: receipt.channel, ts: receipt.ts };
      await this.slack.call("chat.update", { ...where, ...this.newsView(raw) });
      if (!changed) return raw.updated_at;
      await this.warp.recordDelivery(id, client, where);
      return this.keepUp(id, client, where, raw.updated_at);
    }
    // Only what a catch-up would post is posted: an old announcement a
    // notice names is not news in the channel now.
    if (!isNews(raw, (this.opts.now ?? Date.now)(), at.bound_at)) return shown;
    const sent = await this.slack.call<{ ts: string; channel?: string }>("chat.postMessage", {
      channel: at.slack_channel,
      ...this.newsView(raw),
    });
    const where = { channel: sent.channel ?? at.slack_channel, ts: sent.ts };
    this.unrecorded.set(id, where);
    await this.warp.recordDelivery(id, client, where);
    this.unrecorded.delete(id);
    return this.keepUp(id, client, where, raw.updated_at);
  }

  /**
   * After a receipt is recorded, read the announcement once more: warp
   * dates a receipt when it takes it, so a change between the read the post
   * shows and the receipt would read as older than the receipt, and no
   * catch-up would see it. A change found is shown and recorded, at most
   * three times over.
   */
  private async keepUp(
    id: string,
    client: string,
    where: { channel: string; ts: string },
    shown: string | undefined,
  ): Promise<string | undefined> {
    for (let i = 0; i < 3; i++) {
      // A read warp could not answer leaves the post unchecked: it is thrown,
      // so the catch-up is tried again, and that look edits the post.
      const now = await this.warp.getInboxMessage(id).catch((e: unknown) => {
        if (e instanceof WarpError && (e.status === 403 || e.status === 404)) return null;
        this.unchecked.set(id, true);
        throw e;
      });
      if (!now || now.updated_at === shown) {
        this.unchecked.delete(id);
        return shown;
      }
      await this.slack.call("chat.update", { ...where, ...this.newsView(now) });
      await this.warp.recordDelivery(id, client, where);
      shown = now.updated_at;
    }
    return shown;
  }

  /**
   * An announcement as its channel post shows it: its words made unable to
   * ping anyone, or once withdrawn only that it was.
   */
  private newsView(raw: WireMessage): { text: string; blocks: unknown[] } {
    const safe = untrustedAnnouncement(raw);
    const view = renderToSlack(
      <AnnouncementPost message={forSlack(fromServer(safe))} copy={this.copy} />,
    );
    return { text: (view.text || safe.subject).slice(0, LIMITS.fallbackText), blocks: view.blocks };
  }

  /**
   * A person's message as a direct message with the app: the message, and
   * where it was said. Warp snoozes and dismisses only as the caller, so here
   * they would be the app's: they are not offered.
   */
  private personalView(raw: WireMessage) {
    const view = renderToSlack(
      <MessageView message={forSlack(fromServer(raw))} setAside={false} />,
    );
    const thread = raw.source?.thread;
    const url = thread ? this.href(`/tasks/${encodeURIComponent(thread)}`) : undefined;
    const blocks = url
      ? [
          ...view.blocks,
          {
            type: "context",
            elements: [{ type: "mrkdwn", text: `<${url}|${this.words.openThread}>` }],
          },
        ]
      : view.blocks;
    return { text: (view.text || raw.subject).slice(0, LIMITS.fallbackText), blocks };
  }

  /**
   * A mention or a question addressed to a person the client maps reaches
   * them in a direct message with the app, with Reply, and is kept current
   * as it is settled. One raised in a Slack thread of this app's that the
   * person is already in is not: Slack told them there.
   */
  private async tell(notice: InboxNotice): Promise<void> {
    const as = notice.for;
    if (!as || as.client !== this.opts.clientId) return;
    const raw = await this.warp.inboxMessageFor(notice.id, as).catch(() => null);
    if (!raw) return;
    const view = this.personalView(raw);
    const key = toldKey(as, raw.id);
    const told = this.told.get(key);
    if (told) {
      await this.slack.call("chat.update", { channel: told.channel, ts: told.ts, ...view });
      return;
    }
    if (raw.state !== "open") return;
    const thread = raw.source?.thread;
    const surface = thread ? await this.warp.threadSurface(thread).catch(() => null) : null;
    if (surface?.client === this.opts.clientId && (await this.isIn(surface, as.user))) return;
    const sent = await this.slack.call<{ ts: string; channel?: string }>("chat.postMessage", {
      channel: as.user,
      ...view,
    });
    this.told.set(key, { channel: sent.channel ?? as.user, ts: sent.ts });
  }

  /**
   * Whether a Slack person is already in a Slack thread of this app's: they
   * wrote in it or were mentioned there the Slack way. A tag by handle, an
   * invitation into someone's private thread or an expert asked by a run
   * pings nobody in Slack, so they are told in their direct messages. A
   * thread that cannot be read counts as one they are not in: told twice is
   * better than never.
   */
  private async isIn(at: { channel: string; ts: string }, user: string): Promise<boolean> {
    try {
      return inThread(await readThread(this.slack, { channel: at.channel, threadTs: at.ts }), user);
    } catch (e) {
      this.log(`thread ${at.channel}/${at.ts} was not read: ${(e as Error).message}`);
      return false;
    }
  }

  /**
   * What a Reply on a message writes to: the mention, replied to through the
   * person's inbox, or the question a run waits on.
   */
  private replyTarget(raw: WireMessage, partId: string): ReplyTarget | null {
    const part = raw.parts.find((x) => (x as { id?: string }).id === partId) as
      | { type: string; question?: { id?: string } }
      | undefined;
    if (part?.type === "mention") return { inbox: raw.id, part: partId };
    const run = (raw.source as { run?: string } | undefined)?.run;
    if (part?.type === "reply" && run && part.question?.id)
      return { run, question: part.question.id };
    return null;
  }

  /** Open the reply modal for a Reply that carries no words, or say where to reply instead. */
  private async askForReply(p: ActionPayload, raw: WireMessage | null, partId: string) {
    const target = raw ? this.replyTarget(raw, partId) : null;
    if (!target || !p.trigger_id) return this.whisper(p, this.words.replyInConsole);
    const excerpt = (
      raw?.parts.find((x) => (x as { id?: string }).id === partId) as
        | { mention?: { excerpt?: string } }
        | undefined
    )?.mention?.excerpt;
    await this.slack.call("views.open", {
      trigger_id: p.trigger_id,
      view: replyModal(target, excerpt ? { context: excerpt } : {}),
    });
  }

  /**
   * A click on a person's own message, read for them through the client:
   * Reply opens the reply modal, and anything else is done for them.
   */
  private async onPersonal(p: ActionPayload, ui: UiAction, raw: WireMessage, as: ActAs) {
    const message = fromServer(raw);
    const part = message.parts.find((x) => partMatches(x, ui.payload.target));
    if (part && ui.id === ActionId.reply && !ui.payload.text)
      return this.askForReply(p, raw, part.id);
    let request = toRequest(message, ui);
    if (ui.id === ActionId.retry) {
      // Pressed in their messages with the app, so the turn asked again is shown in the run's thread.
      const partId = await this.retried(p, part);
      if (!partId) return;
      request = { call: "resolve", partId, action: "acknowledge" };
    }
    if (request?.call !== "resolve") return;
    let updated: WireMessage;
    try {
      updated = await this.warp.resolveFor(
        message.id,
        request.partId,
        { action: request.action, ...request.values },
        as,
      );
    } catch (e) {
      this.log(`inbox ${message.id} was not settled for ${as.user}: ${(e as Error).message}`);
      const status = e instanceof WarpError ? e.status : 0;
      return this.whisper(
        p,
        status === 403 ? defaultCopy.noPermission : defaultCopy.failure.internal(),
      );
    }
    const channel = p.channel?.id ?? p.container?.channel_id;
    if (channel && p.message?.ts) {
      await this.slack.call("chat.update", {
        channel,
        ts: p.message.ts,
        ...this.personalView(updated),
      });
    }
  }

  /** An inbox message changed. One about a run in this app's thread is shown there, once, and kept current. */
  private async onNotice(notice: InboxNotice): Promise<void> {
    if (notice.for) return this.tell(notice);
    if (notice.kind === "announcement") return this.announce(notice.id);
    const message = await this.warp.inbox.get(notice.id).catch(() => null);
    if (!message) return;
    const posted = this.mail.get(message.id);
    if (posted) return this.showMail(message, posted);
    if (messageState(message) !== "open") return;
    // A failed turn already says so in its own message, with Retry, and a
    // question is shown on the run it holds up.
    if (message.kind === "failure" || message.parts.some((p) => p.type === "failure")) return;
    if (message.kind === "approval" || message.kind === "clarification") return;
    const about = message.about.find((s) => s.kind === "run");
    const run = about ? await this.warp.getRun(about.id) : null;
    const session = run ? await this.sessionOfRun(run) : null;
    if (!session || session.team !== this.opts.team) return;
    const ts = await this.postView(
      session,
      renderToSlack(<MessageView message={forSlack(message)} />),
    );
    this.mail.set(message.id, { channel: session.channel, ts, threadTs: session.threadTs });
  }

  /**
   * The Home tab. A person the client maps sees the shared Home, read as
   * them through the client (act_as): at most five that need them, what's
   * new in their projects and the projects that name them, each opening in
   * the console, and ways to start. Their projects are left out when they
   * cannot be read.
   * Without a mapping the tab is the mailbox, as it always was: an operator
   * sees the whole inbox the app may read, and anyone else what they may
   * settle.
   */
  private async publishHome(user: string): Promise<void> {
    const mapped = await this.mapped(user);
    if (mapped) {
      const view = homeView(<Home home={await this.homeFor(user, mapped)} copy={this.copy} />).view;
      await this.slack.call("views.publish", { user_id: user, view });
      return;
    }
    const page = await this.warp.inbox.list({ limit: 30, state: "open" });
    const operator = (this.opts.operators ?? []).includes(user);
    const messages = operator
      ? page.messages
      : page.messages.filter((m) => this.maySettle(user, m));
    const view = renderToSlack(<MailboxView messages={messages.map(forSlack)} />, {
      placement: "home",
    });
    await this.slack.call("views.publish", {
      user_id: user,
      view: { type: "home", blocks: view.blocks },
    });
  }

  /** The shared Home's model for a person the client maps, everything in it read as them. */
  private async homeFor(user: string, mapped: { as: ActAs; person: string }) {
    const [mine, news, projects, names, build] = await Promise.all([
      // What needs them, posted announcements left out before the page is
      // cut, so however much news is newer the rows agree with the total.
      this.warp.inboxFor(mapped.as, { state: "open", needs: true, limit: 30 }),
      // Posted announcements only, so drafts never crowd them off the page.
      this.warp.inboxFor(mapped.as, { state: "open", news: true, limit: 20 }),
      this.warp.projectsFor(mapped.as).catch((e: Error): WireProject[] | null => {
        this.log(`Home shows no projects for ${user}: ${e.message}`);
        return null;
      }),
      this.names.of([user, this.opts.botUser]),
      this.builds().catch(() => false),
    ]);
    const name = this.branding.name ?? "Warp";
    return homeModel({
      person: { id: user, name: names.get(user) ?? mapped.person },
      assistant: { name, handle: names.get(this.opts.botUser) ?? name },
      intro: this.words.homeIntro,
      // An agent may have written what either list holds, a draft, a
      // question or an announcement: its words ping nobody here either.
      inbox: {
        ...mine,
        messages: mine.messages.map((m) => untrustedAnnouncement(m as WireMessage)),
      },
      announcements: news.messages.map((m) => untrustedAnnouncement(m as WireMessage)),
      projects,
      workspace: this.opts.tenant,
      build,
      href: this.href,
    });
  }

  /** Whether Build an Agent is offered: the builder is there, and this app may reach it. */
  private async builds(): Promise<boolean> {
    const known = (await this.agents()).some((a) => a.name === BUILDER);
    return known && mayRoute(await this.routing(), BUILDER);
  }

  /**
   * A control on the Home tab. Ask a Question and Build an Agent open a form,
   * whose words are asked in the app's messages with the person, as the
   * shortcut's are; Build an Agent asks the builder. Search Context is a
   * link Slack opens itself, so its click asks nothing here.
   */
  private async onHome(p: ActionPayload, ui: UiAction): Promise<void> {
    if (!p.trigger_id) return;
    if (ui.id === ActionId.homeAsk) {
      await this.slack.call("views.open", { trigger_id: p.trigger_id, view: askForm(this.words) });
      return;
    }
    if (ui.id === ActionId.homeStart && ui.payload.target === "build" && (await this.builds())) {
      await this.slack.call("views.open", {
        trigger_id: p.trigger_id,
        view: buildForm(this.words),
      });
    }
  }

  /**
   * Review on an item the Home tab shows without a console address: it is
   * sent to the person's messages with the app, read for them through the
   * client, where they can answer it, as a mention or a question addressed
   * to them is. Someone the client maps to nobody sees no such item.
   */
  private async review(p: ActionPayload, id: string): Promise<void> {
    const as = await this.actAs(p.user.id);
    const raw = as ? await this.warp.inboxMessageFor(id, as).catch(() => null) : null;
    if (!as || !raw) return;
    const sent = await this.slack.call<{ ts: string; channel?: string }>("chat.postMessage", {
      channel: as.user,
      ...this.personalView(raw),
    });
    this.told.set(toldKey(as, raw.id), { channel: sent.channel ?? as.user, ts: sent.ts });
  }

  // Shortcuts and the command

  /**
   * Start a turn somewhere that has no message to answer yet: say who asked
   * what, then answer in that message's thread.
   */
  private async start(
    where: { channel: string; user: string },
    text: string,
    responseUrl?: string,
  ): Promise<void> {
    let root: { ts: string; channel: string };
    try {
      root = await this.slack.call("chat.postMessage", {
        channel: where.channel,
        // warp: needs a component for the asking message itself, such as RequestView({by, text}),
        // which the web shows as the user's bubble. Until then it is the person and their words.
        text: `<@${where.user}>: ${echoed(text)}`,
      });
    } catch (e) {
      const away = e instanceof SlackError && /not_in_channel|channel_not_found/.test(e.code);
      if (!away || !responseUrl) throw e;
      // The app can only post where it is a member. Say so instead of failing silently.
      await this.slack.respond(responseUrl, {
        response_type: "ephemeral",
        text: this.words.notInChannel,
      });
      return;
    }
    const session: Session = { team: this.opts.team, channel: root.channel, threadTs: root.ts };
    this.enqueue({ session, text, triggerTs: root.ts, user: where.user, key: threadKey(session) });
  }

  private async onCommand(c: CommandPayload): Promise<void> {
    if (c.trigger_id) {
      if (this.seen.has(`command-${c.trigger_id}`)) return;
      this.seen.set(`command-${c.trigger_id}`, true);
    }
    if (c.command !== ENTRY.command && c.command !== "/skill") {
      const skills = (await this.routing()).client?.slack?.commands ?? [];
      const skill = skills.find((skill) => `/${skill.name}` === c.command);
      if (!skill) return;
      return this.start(
        { channel: c.channel_id, user: c.user_id },
        `/${skill.name} ${(c.text ?? "").trim()}`,
        c.response_url,
      );
    }
    const text = (c.text ?? "").trim();
    if (c.command === "/skill" || text === "skills" || text.startsWith("skill ")) {
      const skills = (await this.routing()).client?.slack?.commands ?? [];
      const args = c.command === "/skill" ? text : text === "skills" ? "" : text.slice(6).trim();
      const [name, ...rest] = args.split(/\s+/);
      if (!name || args === "skills" || args === "help") {
        await this.slack.respond(c.response_url, {
          response_type: "ephemeral",
          text: skills.length
            ? skills.map((skill) => `/${skill.name}: ${skill.description}`).join("\n")
            : "No skills are configured for this app.",
        });
        return;
      }
      if (!skills.some((skill) => skill.name === name)) {
        await this.slack.respond(c.response_url, {
          response_type: "ephemeral",
          text: "That skill is not available in this app. Use /skill to see available skills.",
        });
        return;
      }
      return this.start(
        { channel: c.channel_id, user: c.user_id },
        `/${name} ${rest.join(" ")}`.trim(),
        c.response_url,
      );
    }
    if (text === "connections") return this.openConnections(c);
    if (text.startsWith("canvas ")) return this.publishCanvas(c, text.slice(7).trim());
    if (!text || text === "help") {
      // Who can be asked, in the platform's own words.
      // warp: needs an AgentList component (name, description), as the console's agent menu shows.
      const routing = await this.routing();
      const lines = (await this.agents())
        .filter((a) => choose(`@${a.name}`, [a], routing).how === "named")
        .map((a) => (a.description ? `\`@${a.name}\`: ${a.description}` : `\`@${a.name}\``));
      if (lines.length > 0) {
        await this.slack.respond(c.response_url, {
          response_type: "ephemeral",
          text: lines.join("\n"),
        });
      }
      return;
    }
    await this.start({ channel: c.channel_id, user: c.user_id }, text, c.response_url);
  }

  private async openConnections(c: CommandPayload) {
    const opened = await this.slack.call<{ view: { id: string } }>("views.open", {
      trigger_id: c.trigger_id,
      view: connectionsModal(undefined, c.channel_id),
    });
    const as = await this.actAs(c.user_id);
    if (!as) {
      await this.slack.call("views.update", {
        view_id: opened.view.id,
        view: connectionsModal(
          [],
          c.channel_id,
          "Ask a workspace administrator to connect your Slack identity to a workspace account.",
        ),
      });
      return;
    }
    const status = await this.warp.connections(as);
    await this.slack.call("views.update", {
      view_id: opened.view.id,
      view: connectionsModal(status.providers, c.channel_id),
    });
  }

  private async publishCanvas(c: CommandPayload, id: string) {
    const as = await this.actAs(c.user_id);
    const run = as ? await this.warp.getRunFor(id, as) : null;
    const session = run && (await this.sessionOfRun(run));
    if (
      !run ||
      !session ||
      session.team !== this.opts.team ||
      session.channel !== c.channel_id ||
      !terminal(run.status) ||
      !run.answer
    ) {
      await this.slack.respond(c.response_url, {
        response_type: "ephemeral",
        text: "Choose a finished answer from this channel: /agent canvas RUN_ID.",
      });
      return;
    }
    const canvas = await this.slack.call<{ canvas_id: string }>("canvases.create", {
      title: `${run.agent}: ${run.text.slice(0, 100)}`,
      document_content: { type: "markdown", markdown: run.answer },
    });
    await this.slack.call("canvases.access.set", {
      canvas_id: canvas.canvas_id,
      ...(c.channel_id.startsWith("D")
        ? { user_ids: [c.user_id] }
        : { channel_ids: [c.channel_id] }),
      access_level: "read",
    });
    await this.slack.respond(c.response_url, {
      response_type: "ephemeral",
      text: `Canvas: https://app.slack.com/docs/${this.opts.team}/${canvas.canvas_id}`,
    });
  }

  private async onInteractive(p: InteractivePayload): Promise<void> {
    if (p.trigger_id) {
      const once = `${p.type}-${p.trigger_id}`;
      if (this.seen.has(once)) return;
      this.seen.set(once, true);
    }
    if (p.type === "shortcut" && p.callback_id === ENTRY.ask) {
      await this.slack.call("views.open", { trigger_id: p.trigger_id, view: askForm(this.words) });
      return;
    }
    if (p.type === "message_action" && p.callback_id === ENTRY.askAbout) {
      const { channel, message } = p;
      const text = (message?.text ?? "").trim();
      if (!channel || !message || !text) return;
      // The message is the question, so the answer belongs in its thread.
      const session: Session = {
        team: this.opts.team,
        channel: channel.id,
        threadTs: message.thread_ts ?? message.ts,
      };
      this.enqueue({
        session,
        text,
        triggerTs: message.ts,
        user: p.user.id,
        key: eventKey(session, message.ts, p.trigger_id),
        // Someone else's message, or a bot's, is not the presser's to run as them.
        ...(message.user !== p.user.id ? { notTheirs: true } : {}),
      });
      return;
    }
    if (p.type === "view_submission" && p.view?.callback_id === CONNECTIONS_MODAL) {
      const as = await this.actAs(p.user.id);
      if (!as) throw new Error("Connections need a mapped workspace account");
      const values = p.view.state?.values;
      const provider = (
        values?.provider?.value?.selected_option?.value ?? values?.provider?.value?.value
      )?.trim();
      const operation = values?.operation?.value?.selected_option?.value;
      if (!provider) return;
      let text = "Connection updated.";
      try {
        if (operation === "disconnect") await this.warp.disconnect(provider, as);
        else {
          const ref = values?.reference?.value?.value?.trim();
          if (!ref) throw new Error("A credential reference is required");
          await this.warp.connect(provider, ref, as);
        }
      } catch {
        text =
          "Your connection could not be updated. Open /agent connections and check the provider and your allowed credential reference.";
      }
      if (p.view.private_metadata)
        await this.slack.call("chat.postEphemeral", {
          channel: p.view.private_metadata,
          user: p.user.id,
          text,
        });
      return;
    }
    const feedback = p.type === "view_submission" && p.view ? decodeFeedbackModal(p.view) : null;
    if (feedback) {
      const { turnId, target, text, option } = feedback.payload;
      if (target === "up" || target === "down")
        await this.saveFeedback(turnId, p.user.id, target, text, option);
      return;
    }
    const reply = p.type === "view_submission" && p.view ? decodeReplyModal(p.view) : null;
    if (reply) return this.onReply(p.user.id, reply.text, reply.target);
    if (p.type !== "view_submission" || p.view?.callback_id !== ENTRY.form) return;
    const said = p.view.state?.values?.text?.value?.value?.trim() ?? "";
    if (!said) return;
    // Build an Agent's words go to the builder, tagged as anyone would tag it.
    const text = formKind(p.view.private_metadata) === "build" ? `@${BUILDER} ${said}` : said;
    // A global shortcut or the Home tab has no channel: answer in the app's own messages with this person.
    await this.start({ channel: p.user.id, user: p.user.id }, text);
  }
}

/** The team a delivery says it is from. Each kind of delivery carries it somewhere else. */
function teamOf(type: string, p: Record<string, unknown>): string | undefined {
  const at = (v: unknown, k: string) =>
    v && typeof v === "object" ? (v as Record<string, unknown>)[k] : undefined;
  if (type === "events_api" || type === "slash_commands") return p.team_id as string | undefined;
  if (type === "interactive") {
    return (at(p.team, "id") ?? at(p.user, "team_id")) as string | undefined;
  }
  return undefined;
}

interface SlackEvent {
  type: string;
  files?: { id: string }[];
  subtype?: string;
  channel?: string;
  channel_id?: string;
  channel_type?: string;
  user?: string;
  bot_id?: string;
  text?: string;
  ts?: string;
  thread_ts?: string;
  tab?: string;
  assistant_thread?: { channel_id?: string; thread_ts?: string };
}

interface ActionPayload {
  user: { id: string };
  channel?: { id: string };
  container?: { type?: string; channel_id?: string };
  view?: { type?: string };
  trigger_id?: string;
  message?: { ts: string; thread_ts?: string };
  actions?: { action_id: string; value?: string; selected_option?: { value: string } }[];
  state?: Parameters<typeof decodeSlackAction>[1];
}

interface InteractivePayload {
  type: string;
  callback_id?: string;
  trigger_id: string;
  user: { id: string };
  channel?: { id: string };
  message?: { ts: string; thread_ts?: string; text?: string; user?: string };
  view?: {
    callback_id: string;
    private_metadata?: string;
    state?: {
      values?: Record<
        string,
        Record<string, { value?: string | null; selected_option?: { value: string } | null }>
      >;
    };
  };
}

interface CommandPayload {
  command: string;
  text?: string;
  user_id: string;
  channel_id: string;
  trigger_id?: string;
  response_url: string;
}
