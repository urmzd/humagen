import { type Names, readThread, sinceLastTurn, withHistory } from "./history.ts";
import { words } from "./host.ts";
import { eventKey, type Session, sessionName, sessionOf, tagId } from "./keys.ts";
import { type SlackApi, SlackError } from "./slack.ts";

/**
 * Moving a conversation into a Slack code channel of its own. Off unless the
 * Slack surface sets `code_channels: true` (`SLACK_CODE_CHANNELS`), and the
 * app never builds this class when it is off.
 *
 * Nothing here decides that a request is coding work. The app knows an agent
 * by its name and description only, and a run names no repository, so there
 * is no signal to trust: a person presses Continue in a Code Channel on an
 * answer instead.
 *
 * One conversation, two places. A humagen conversation is named by the Slack
 * thread it started in (`keys.ts`), and that does not change when it moves:
 * a message in the code channel is keyed by the thread the channel was opened
 * from, so it continues that conversation. The link from a code channel back
 * to its thread is read from Slack (`conversations.info`, the channel's
 * `agent_session.origin_link`), which is what `agents.conversations.create`
 * was given, so it is derived, never stored, and a restart loses nothing.
 *
 * Every refusal leaves things as they are: the conversation carries on in its
 * thread, and a channel that cannot be read is treated as an ordinary one.
 */

export const codeChannelWords = {
  /** Posted in the thread the conversation started in, once its channel exists. */
  opened: (channel: string) => `Continuing in <#${channel}>. Follow along there.`,
  already: (channel: string) => `This conversation already continues in <#${channel}>.`,
  refused: "A code channel could not be opened here. Carry on in this thread.",
  notYours: "Only the person who asked can move this conversation to a code channel.",
  elsewhere: "This conversation cannot be moved to a code channel.",
  /** What the agent is told about the thread, on its first turn in the channel. */
  originOpen:
    "This code channel was opened from a Slack thread. Messages in that thread since you were last asked, oldest first:",
  /** The channel's name when the request has no words to take one from. */
  untitled: "Coding work",
};

/** The longest name `agents.conversations.create` is given. */
export const TITLE_MAX = 80;

/** Later than any message, so a whole thread counts as before it. */
const END = "9999999999.999999";

/** Refusals that say nothing about whether code channels work here: the next press may succeed. */
const PASSING =
  /^(ratelimited|rate_limited|internal_error|fatal_error|service_unavailable|request_timeout|name_taken|invalid_name|invalid_arguments|invalid_args|message_not_found|thread_not_found|channel_not_found)$/;

/**
 * The session id Slack is given for a thread's code channel. It comes from the
 * thread alone, so a second press, a redelivery or a restart asks for the
 * same channel, never a second one.
 */
export const sessionId = (origin: Session) => `${origin.channel}-${origin.threadTs}`;

/** A name for the channel, from the words that started the conversation. */
export function channelTitle(text: string): string {
  // A mention deep in a thread carries the thread ahead of the question.
  const cut = text.lastIndexOf(words.historyClose);
  const said = cut < 0 ? text : text.slice(cut + words.historyClose.length);
  const plain = said
    .replace(/<[^>]*>/g, " ")
    .replace(/[`*_~]/g, "")
    .replace(/\s+/g, " ")
    .trim();
  if (!plain) return codeChannelWords.untitled;
  if (plain.length <= TITLE_MAX) return plain;
  const head = plain.slice(0, TITLE_MAX);
  const space = head.lastIndexOf(" ");
  return (space > TITLE_MAX / 2 ? head.slice(0, space) : head).trim();
}

/** What Slack says a channel is: an ordinary one, or a code channel and the thread it came from. */
export type Place = { code: false } | { code: true; origin: Session | null };

interface ChannelInfo {
  channel?: {
    properties?: {
      record_channel?: { record_type?: string };
      agent_session?: { origin_link?: { channel_id?: string; ts?: string } };
    };
  };
}

/** The part of a run this reads. */
export interface RunRef {
  id: string;
  thread: string;
  tag_id?: string;
  text?: string;
}

/** A press of Continue in a Code Channel. */
export interface Press {
  /** The answered run the button was under. */
  run: string;
  user: string;
  /** Where the button was pressed. */
  channel?: string;
}

/** What a press needs of the app: its records and its rules. */
export interface PressHost {
  run(id: string): Promise<RunRef | null>;
  /** Whether this person asked for the run, or may act for anyone. */
  mayMove(run: string, user: string): boolean;
  /** Whether a channel is bound to a humagen channel, whose threads stay where they are. */
  bound(channel: string): Promise<boolean>;
  /** Tell only the person who pressed. */
  whisper(text: string): Promise<void>;
}

export type Opened = { channel: string; again: boolean } | { refused: string };

export class CodeChannels {
  private readonly places = new Map<string, Place>();
  /** The channel each thread was moved to by this process, by session name. */
  private readonly moved = new Map<string, string>();
  /** Slack refused in a way that will not change by itself: the button is no longer offered. */
  private unavailable = false;
  /** Slack will not say what a channel is: every channel is treated as an ordinary one. */
  private blind = false;

  constructor(
    private readonly slack: SlackApi,
    private readonly opts: {
      team: string;
      botUser: string;
      /** Who humagen says this app is. Run trigger ids are made from it. */
      subject: string;
      names?: Names;
      log?: (line: string) => void;
    },
  ) {}

  private log(line: string) {
    this.opts.log?.(line);
  }

  /** Whether the button is worth showing: not once Slack has said code channels are not here. */
  get available(): boolean {
    return !this.unavailable;
  }

  /**
   * What a channel is. A direct message is never asked about. A channel Slack
   * will not describe is an ordinary one, and after a missing scope Slack is
   * not asked again.
   */
  async place(channel: string): Promise<Place> {
    if (channel.startsWith("D") || this.blind) return { code: false };
    const known = this.places.get(channel);
    if (known) return known;
    let info: ChannelInfo;
    try {
      info = await this.slack.call<ChannelInfo>("conversations.info", { channel });
    } catch (e) {
      const code = e instanceof SlackError ? e.code : (e as Error).message;
      if (code === "missing_scope") {
        this.blind = true;
        this.log(
          "conversations.info needs channels:read and groups:read; code channels are answered as ordinary channels",
        );
      } else {
        this.log(`channel ${channel} could not be read (${code}); answered as an ordinary channel`);
      }
      return { code: false };
    }
    const properties = info.channel?.properties;
    let place: Place = { code: false };
    if (properties?.record_channel?.record_type === "agent_channel") {
      const link = properties.agent_session?.origin_link;
      const linked =
        /^[A-Za-z0-9]+$/.test(link?.channel_id ?? "") && /^\d+\.\d+$/.test(link?.ts ?? "");
      place = {
        code: true,
        origin: linked
          ? { team: this.opts.team, channel: String(link?.channel_id), threadTs: String(link?.ts) }
          : null,
      };
    }
    this.remember(channel, place);
    return place;
  }

  private remember(channel: string, place: Place) {
    if (this.places.size >= 4096) this.places.clear();
    this.places.set(channel, place);
  }

  /** The thread a code channel was opened from, or null for any other place. */
  async originOf(channel: string): Promise<Session | null> {
    const place = await this.place(channel);
    return place.code ? place.origin : null;
  }

  /** Whether an answer in this channel offers the button: never in a code channel. */
  async offers(channel: string): Promise<boolean> {
    return this.available && !(await this.place(channel)).code;
  }

  /**
   * Where a click on a message of a moved conversation was made: the code
   * channel, when the click came from one opened from this thread. Null means
   * the thread itself.
   */
  async there(
    origin: Session,
    channel: string | undefined,
    message: { ts?: string; thread_ts?: string } | undefined,
  ): Promise<Session | null> {
    if (!channel || channel === origin.channel || !message?.ts) return null;
    const from = await this.originOf(channel);
    if (!from || sessionName(from) !== sessionName(origin)) return null;
    return { team: origin.team, channel, threadTs: message.thread_ts ?? message.ts };
  }

  /**
   * Open the code channel of a thread. Slack is asked with a session id made
   * from the thread, so asking twice gives the same channel. A refusal is
   * returned, never thrown: the caller carries on in the thread.
   */
  async open(origin: Session, title: string): Promise<Opened> {
    const name = sessionName(origin);
    const before = this.moved.get(name);
    if (before) return { channel: before, again: true };
    let channel: string | undefined;
    try {
      const made = await this.slack.call<{ channel_id?: string; channel?: { id?: string } }>(
        "agents.conversations.create",
        {
          name: title.slice(0, TITLE_MAX),
          session_id: sessionId(origin),
          origin_channel_id: origin.channel,
          origin_message_ts: origin.threadTs,
        },
      );
      channel = made.channel_id ?? made.channel?.id;
    } catch (e) {
      const code = e instanceof SlackError ? e.code : "unreachable";
      // A missing scope, a plan without code channels, an app without the feature:
      // none of them changes until someone changes the app, so the button goes.
      if (e instanceof SlackError && !PASSING.test(code)) {
        this.unavailable = true;
        this.log(
          `code channels are not available (${code}); conversations stay in their threads until the app restarts`,
        );
      } else {
        this.log(`a code channel could not be opened (${code}); the thread carries on`);
      }
      return { refused: code };
    }
    if (!channel) {
      this.log("agents.conversations.create named no channel; the thread carries on");
      return { refused: "no_channel" };
    }
    if (this.moved.size >= 4096) this.moved.clear();
    this.moved.set(name, channel);
    this.remember(channel, { code: true, origin });
    return { channel, again: false };
  }

  /**
   * Continue in a Code Channel was pressed under an answer. The conversation
   * must be one this app named from the thread the button is in, and the
   * person the one who asked (or an operator); in a direct message it is
   * theirs already. The acknowledgement is posted in the thread once.
   */
  async press(p: Press, host: PressHost): Promise<void> {
    const run = await host.run(p.run).catch(() => null);
    const first =
      run && (run.id === run.thread ? run : await host.run(run.thread).catch(() => null));
    const origin = first?.tag_id ? sessionOf(this.opts.subject, first.tag_id) : null;
    if (
      !run ||
      !origin ||
      origin.team !== this.opts.team ||
      origin.channel !== p.channel ||
      (await host.bound(origin.channel))
    ) {
      return host.whisper(codeChannelWords.elsewhere);
    }
    if (!origin.channel.startsWith("D") && !host.mayMove(run.id, p.user)) {
      return host.whisper(codeChannelWords.notYours);
    }
    const got = await this.open(origin, channelTitle(first?.text ?? ""));
    if ("refused" in got) return host.whisper(codeChannelWords.refused);
    if (got.again) return host.whisper(codeChannelWords.already(got.channel));
    await this.slack
      .call("chat.postMessage", {
        channel: origin.channel,
        thread_ts: origin.threadTs,
        text: codeChannelWords.opened(got.channel),
      })
      .catch((e: Error) => this.log(`the code channel was not announced: ${e.message}`));
  }

  /**
   * The first message answered in a code channel, with what people said in
   * the thread it came from since the app was last asked there. The
   * conversation already holds every turn the app took in the thread, so only
   * what was said around them is added, and only once: a conversation with a
   * turn asked from the channel already has it. A direct message has nothing
   * the app was not shown. A thread Slack will not give is no reason to fail
   * the turn: the message goes alone.
   */
  async withOrigin(
    text: string,
    at: { origin: Session; there: Session; turns: { tag_id?: string }[] },
  ): Promise<string> {
    const { origin } = at;
    if (origin.channel.startsWith("D")) return text;
    const asked = tagId(this.opts.subject, eventKey(at.there, ""));
    if (at.turns.some((t) => t.tag_id?.startsWith(asked))) return text;
    try {
      const said = sinceLastTurn(
        await readThread(this.slack, { channel: origin.channel, threadTs: origin.threadTs }),
        { botUser: this.opts.botUser, threadTs: origin.threadTs, triggerTs: END },
      );
      if (said.length === 0) return text;
      const names = await this.opts.names?.of(said.map((m) => m.user ?? ""));
      const told = withHistory(text, said, names);
      return told.startsWith(words.historyOpen)
        ? `${codeChannelWords.originOpen}${told.slice(words.historyOpen.length)}`
        : told;
    } catch (e) {
      this.log(`the thread the code channel came from was skipped: ${(e as Error).message}`);
      return text;
    }
  }
}
