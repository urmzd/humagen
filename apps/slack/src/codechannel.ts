/**
 * What a Slack code channel shows around a session, besides its messages: the
 * context bar at the top of the channel, the session's name, and the summary
 * a closed channel shares back to the message that started it.
 *
 * A session in a code channel is the channel itself. The presenter learns
 * that a channel is one when Slack refuses a thread for the session's status
 * (`thread_ts_not_allowed`), and only then is anything here called, so an
 * ordinary thread or a direct message makes none of these calls. Each call
 * is tried on its own and a refusal is remembered for the channel: the turn's
 * messages never depend on any of it.
 */
import { type Copy, defaultCopy } from "@humagen/components";
import type { Run } from "./humagen.ts";
import { type SlackApi, SlackError } from "./slack.ts";

/** The icons Slack draws beside a context bar item. */
export type BarIcon =
  | "branch"
  | "folder"
  | "hierarchy"
  | "life-ring"
  | "link"
  | "globe"
  | "terminal"
  | "code"
  | "search"
  | "lock";

export interface BarItem {
  key: string;
  label: string;
  icon?: BarIcon;
  /** Makes the item a link. */
  url?: string;
}

/** Slack shows at most this many items. */
export const MAX_BAR_ITEMS = 5;
/** A session's name is one short line. */
export const MAX_TITLE = 80;
/** `chat.postMessage` takes at most this much `markdown_text`. */
const MAX_SUMMARY = 12_000;

type Href = (path: string) => string | undefined;

/** One line, clipped. */
function line(text: string, max: number): string {
  const one = text.replace(/\s+/g, " ").trim();
  return one.length > max ? `${one.slice(0, max - 1).trimEnd()}…` : one;
}

/**
 * The context bar for a run: only what the run itself says. Its project and
 * the agent answering, and the conversation in the console when the console
 * has an address. A run names no repository, branch, pull request or check,
 * so none is shown.
 */
export function contextBar(
  run: Pick<Run, "id" | "agent" | "project" | "thread">,
  opts: { href?: Href; copy?: Copy } = {},
): BarItem[] {
  const copy = opts.copy ?? defaultCopy;
  const items: BarItem[] = [];
  if (run.project) {
    const url = opts.href?.(`/projects/${encodeURIComponent(run.project)}`);
    items.push({
      key: "project",
      label: line(run.project, 60),
      icon: "folder",
      ...(url && { url }),
    });
  }
  if (run.agent) items.push({ key: "agent", label: line(run.agent, 60), icon: "terminal" });
  const open = opts.href?.(`/conversations/${encodeURIComponent(run.thread || run.id)}`);
  if (open)
    items.push({ key: "conversation", label: copy.openConversation, icon: "link", url: open });
  return items.slice(0, MAX_BAR_ITEMS);
}

/** What a closing session says last: the run's own summary of its outcome, else its answer. */
export function summaryOf(run: Pick<Run, "outcome" | "answer">): string {
  return (run.outcome?.summary || run.answer || "").trim();
}

/** Refusals that may pass: the call is tried again the next time. Any other is remembered. */
const PASSING = /^(ratelimited|internal_error|fatal_error|service_unavailable|request_timeout)$/;

interface Chrome {
  /** The context bar Slack was last given, as sent. */
  bar?: string;
  /** The name the session was given. Set once. */
  title?: string;
  /** Slack refused the context bar here. */
  noBar?: boolean;
  /** Slack refused the name here. */
  noTitle?: boolean;
}

export interface CodeChannelOptions {
  /** The title humagen has for a conversation, or null while it has none. */
  title: (thread: string) => Promise<string | null>;
  href?: Href;
  copy?: Copy;
  log?: (line: string) => void;
  /** How long to wait before each further look for a title once a turn has ended. */
  titleWaits?: number[];
  sleep?: (ms: number) => Promise<void>;
}

/** Waits that never hold the process open. */
const nap = (ms: number) =>
  new Promise<void>((done) => {
    setTimeout(done, ms).unref();
  });

export class CodeChannels {
  private readonly known = new Map<string, Chrome>();

  constructor(
    private readonly slack: SlackApi,
    private readonly opts: CodeChannelOptions,
  ) {}

  private log(text: string) {
    this.opts.log?.(text);
  }

  /** Whether a channel was seen to be a code channel since this process started. */
  has(channel: string): boolean {
    return this.known.has(channel);
  }

  private of(channel: string): Chrome {
    let chrome = this.known.get(channel);
    if (!chrome) {
      if (this.known.size >= 4096) this.known.clear();
      chrome = {};
      this.known.set(channel, chrome);
    }
    return chrome;
  }

  /** A channel that was archived or is gone: nothing is kept for it. */
  forget(channel: string): void {
    this.known.delete(channel);
  }

  /** Whether a refusal settles the matter for this channel. */
  private final(what: string, e: unknown): boolean {
    this.log(`${what} skipped: ${(e as Error).message}`);
    return e instanceof SlackError && !PASSING.test(e.code);
  }

  /** A turn began in a code channel: the context bar, and the session's name if humagen has one. */
  async sync(channel: string, run: Run): Promise<void> {
    await this.show(channel, run);
    await this.name(channel, run.thread || run.id);
  }

  /** Keep the context bar to what the run says. Slack is told only when it changes. */
  async show(channel: string, run: Run): Promise<void> {
    const chrome = this.of(channel);
    if (chrome.noBar) return;
    const items = contextBar(run, this.opts);
    const sent = JSON.stringify(items);
    if (chrome.bar === sent) return;
    try {
      await this.slack.call("agents.conversations.setProperties", {
        channel_id: channel,
        code_channel: { context_bar_items: items },
      });
      chrome.bar = sent;
    } catch (e) {
      if (this.final("context bar", e)) chrome.noBar = true;
    }
  }

  /**
   * Name the session after its conversation's title, once. Until humagen has
   * a title nothing is renamed, and once the session is named it is not
   * asked about again. True when the session has its name.
   */
  async name(channel: string, thread: string): Promise<boolean> {
    const chrome = this.of(channel);
    if (chrome.title !== undefined || chrome.noTitle) return true;
    let title: string;
    try {
      title = line((await this.opts.title(thread)) ?? "", MAX_TITLE);
    } catch (e) {
      this.log(`session name skipped: ${(e as Error).message}`);
      return false;
    }
    if (!title) return false;
    // A turn that began meanwhile may have named it already.
    if (chrome.title !== undefined) return true;
    chrome.title = title;
    try {
      await this.slack.call("agents.sessions.rename", { channel_id: channel, title });
    } catch (e) {
      if (this.final("session name", e)) chrome.noTitle = true;
      else chrome.title = undefined;
    }
    return chrome.title !== undefined || chrome.noTitle === true;
  }

  /**
   * A turn ended. A conversation is titled after its first turn, so the name
   * is looked for now and, while there is none, a few times more.
   */
  async settle(channel: string, run: Run): Promise<void> {
    const thread = run.thread || run.id;
    if (await this.name(channel, thread)) return;
    for (const ms of this.opts.titleWaits ?? [5_000, 20_000]) {
      await (this.opts.sleep ?? nap)(ms);
      // Archived or forgotten while waiting.
      if (!this.known.has(channel)) return;
      if (await this.name(channel, thread)) return;
    }
  }

  /**
   * End a code channel's session: post its summary, close the session and
   * archive the channel, sharing the summary back to the message the session
   * started from. A channel started from Slack's own controls has no such
   * message and is archived without one.
   *
   * Nothing calls this on its own: it is for a person's request to wrap up.
   * A channel not seen to be a code channel is left alone, so an ordinary
   * channel is never posted to or archived from here.
   */
  async close(channel: string, summary: string): Promise<"closed" | "skipped" | "failed"> {
    const text = summary.trim().slice(0, MAX_SUMMARY);
    if (!this.known.has(channel) || !text) return "skipped";
    try {
      const posted = await this.slack.call<{ ts: string }>("chat.postMessage", {
        channel,
        markdown_text: text,
      });
      // The session is closed while the channel can still be written to.
      await this.slack.call("agents.sessions.setStatus", { channel_id: channel, status: "closed" });
      try {
        await this.slack.call("agents.conversations.archive", {
          channel_id: channel,
          summary_message_ts: posted.ts,
        });
      } catch (e) {
        if (!(e instanceof SlackError) || e.code !== "no_origin_link") throw e;
        await this.slack.call("agents.conversations.archive", { channel_id: channel });
      }
    } catch (e) {
      this.log(`closing the code channel ${channel} failed: ${(e as Error).message}`);
      return "failed";
    }
    this.forget(channel);
    return "closed";
  }
}
