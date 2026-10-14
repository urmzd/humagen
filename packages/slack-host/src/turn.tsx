import { type Copy, defaultCopy, type TurnCapabilities, TurnView } from "@vultus/components";
import { emptyTurn, reduce, type Turn, type TurnEvent, type TurnStatus } from "@vultus/core";
import {
  type Block,
  emptyBudget,
  LIMITS,
  lifecycle,
  reactionChanges,
  renderToSlack,
  type StreamChunk,
  streamHeader,
  streamPages,
  streamStart,
  streamStopTitle,
  streamTrailer,
  streamUpdate,
} from "@vultus/render-slack";
import { type SlackCaller, slackCode } from "./client";

/**
 * One turn on Slack, kept in step with the turn as it changes: a reaction on
 * the message that asked, the session's status, `chat.startStream`, ordered
 * `chat.appendStream` calls, and `chat.stopStream` with the final blocks. A
 * turn that outgrows one message continues in the next, in the same thread.
 * Every block and chunk comes from `@vultus/render-slack`.
 *
 * Where native streaming is refused, the turn is posted once and edited in
 * place, from the same components.
 */

export interface StreamTarget {
  channel: string;
  /** The thread the turn answers in. Streaming in channels must be threaded. */
  threadTs: string;
  /** The message that asked. Lifecycle reactions go on it. */
  triggerTs: string;
  recipientUser: string;
  recipientTeam: string;
}

export interface Presented {
  /** The message that holds the final blocks, for a later `chat.update` (an approval decided). */
  ts: string;
  /** Every message the turn used, in order. More than one when it overflowed. */
  messages: string[];
}

export interface TurnStreamOptions {
  /** Start with native streaming. Pass false once a workspace has refused it. Default true. */
  native?: boolean;
  log?: (line: string) => void;
  /** A status the message that asked already shows, such as the reaction for a queued message. */
  shown?: TurnStatus;
  copy?: Copy;
  /** What the host can act on: false hides feedback or Report Issue. */
  capabilities?: TurnCapabilities;
  /** Who is looking, for approval gating. */
  viewer?: string;
}

/** Codes that mean the place itself is unusable, so falling back to a plain post would fail too. */
const NO_PLACE =
  /^(not_in_channel|channel_not_found|is_archived|invalid_auth|not_authed|account_inactive|token_revoked|token_expired|missing_scope|ratelimited)$/;

export class TurnStream {
  private sent: Turn | null = null;
  private ts = "";
  private readonly messages: string[] = [];
  private page = 1;
  private budget = emptyBudget();
  /** The status already shown as a reaction and a session status. */
  private shown: TurnStatus | null;
  private native: boolean;
  private closed = false;
  /** A write failed mid-turn. Nothing more is written until the turn ends. */
  private degraded = false;
  /** The session status Slack was last told. */
  private session: string | null = null;
  /** A session in a code channel is the channel itself, with no thread. */
  private threadless = false;
  private readonly copy: Copy;
  /** Pages of the final layout Slack already has. */
  private delivered = 0;
  /** Every call runs after the one before it, so concurrent callers can't open two messages. */
  private queue: Promise<unknown> = Promise.resolve();

  constructor(
    private readonly slack: SlackCaller,
    private readonly target: StreamTarget,
    private readonly opts: TurnStreamOptions = {},
  ) {
    this.native = opts.native ?? true;
    this.shown = opts.shown ?? null;
    this.copy = opts.copy ?? defaultCopy;
  }

  /** Whether native streaming is still in use, so the host can remember a refusal. */
  get streaming(): boolean {
    return this.native;
  }

  private log(line: string) {
    this.opts.log?.(line);
  }

  private view(turn: Turn, streamed: boolean) {
    return renderToSlack(
      <TurnView
        turn={turn}
        streamed={streamed}
        copy={this.copy}
        capabilities={this.opts.capabilities}
        viewer={this.opts.viewer}
      />,
    );
  }

  private async react(next: TurnStatus) {
    const { add, remove } = reactionChanges(this.shown, next);
    const at = { channel: this.target.channel, timestamp: this.target.triggerTs };
    for (const name of remove) {
      await this.slack.call("reactions.remove", { ...at, name }).catch(() => {});
    }
    for (const name of add) {
      await this.slack.call("reactions.add", { ...at, name }).catch(() => {});
    }
  }

  private async status(turn: Turn) {
    const { sessionStatus } = lifecycle(turn);
    // Queued and working are both "processing" to Slack: it is told once.
    if (sessionStatus === this.session) return;
    this.session = sessionStatus;
    const args = { channel_id: this.target.channel, status: sessionStatus };
    try {
      await this.slack.call(
        "agents.sessions.setStatus",
        this.threadless ? args : { ...args, thread_ts: this.target.threadTs },
      );
    } catch (e) {
      if (slackCode(e) === "thread_ts_not_allowed" && !this.threadless) {
        this.threadless = true;
        await this.slack
          .call("agents.sessions.setStatus", args)
          .catch((again: Error) => this.log(`session status skipped: ${again.message}`));
        return;
      }
      // A refusal here never costs the answer: it is already in the thread.
      this.log(`session status skipped: ${(e as Error).message}`);
    }
  }

  /** Show a status change once: the reaction, then the session. */
  private async sync(turn: Turn) {
    if (this.shown === turn.status) return;
    await this.react(turn.status);
    await this.status(turn);
    this.shown = turn.status;
  }

  private async open(turn: Turn) {
    const args = {
      channel: this.target.channel,
      thread_ts: this.target.threadTs,
      recipient_user_id: this.target.recipientUser,
      recipient_team_id: this.target.recipientTeam,
      ...streamStart(turn),
    };
    const header = streamHeader(turn, { page: this.page, labels: { part: this.copy.part } });
    let started: { ts: string };
    try {
      started = await this.slack.call("chat.startStream", {
        ...args,
        ...(header.length > 0 ? { chunks: header } : {}),
      });
    } catch (e) {
      const rejected = /^invalid_(chunks|blocks)$/.test(slackCode(e) ?? "");
      if (!rejected || header.length === 0) throw e;
      // The workspace does not take block chunks yet: say the same thing as a line of text.
      this.log(`continuation header sent as text: ${(e as Error).message}`);
      started = await this.slack.call("chat.startStream", {
        ...args,
        markdown_text: `_${this.copy.part(this.page)}_\n\n`,
      });
    }
    this.ts = started.ts;
    this.messages.push(this.ts);
  }

  /** Close the current message and continue in a new one. */
  private async turnPage(turn: Turn, trailer: boolean) {
    const at = { channel: this.target.channel, ts: this.ts };
    if (trailer) {
      await this.slack
        .call("chat.appendStream", { ...at, chunks: [streamTrailer()] })
        .catch(() => {});
    }
    await this.slack.call("chat.stopStream", at);
    this.page += 1;
    await this.open(turn);
  }

  private async append(turn: Turn, chunks: StreamChunk[]) {
    const plan = streamPages(this.budget, chunks);
    for (const [i, part] of plan.pages.entries()) {
      if (i > 0) await this.turnPage(turn, true);
      if (part.length === 0) continue;
      if (!this.ts) await this.open(turn);
      try {
        // Each append is awaited in order, so no task finishes after the stream stops.
        await this.slack.call("chat.appendStream", {
          channel: this.target.channel,
          ts: this.ts,
          chunks: part,
        });
      } catch (e) {
        if (slackCode(e) !== "msg_too_long") throw e;
        // The estimate was off and the message is full: it cannot take a trailer either.
        await this.turnPage(turn, false);
        await this.slack.call("chat.appendStream", {
          channel: this.target.channel,
          ts: this.ts,
          chunks: part,
        });
        plan.budget = streamPages(emptyBudget(), part).budget;
      }
    }
    this.budget = plan.budget;
  }

  private async post(blocks: Block[], text: string) {
    const posted = await this.slack.call<{ ts: string }>("chat.postMessage", {
      channel: this.target.channel,
      thread_ts: this.target.threadTs,
      text: text.slice(0, LIMITS.fallbackText),
      blocks,
    });
    this.ts = posted.ts;
    this.messages.push(this.ts);
  }

  private async edit(blocks: Block[], text: string) {
    await this.slack.call("chat.update", {
      channel: this.target.channel,
      ts: this.ts,
      text: text.slice(0, LIMITS.fallbackText),
      blocks,
    });
  }

  /** Streaming is not available here: carry on by posting and editing. Nothing was streamed yet. */
  private refused(e: unknown): boolean {
    const code = slackCode(e);
    if (code === undefined || NO_PLACE.test(code) || this.ts) return false;
    this.log(`native streaming is not available (${code}), posting and updating instead`);
    this.native = false;
    return true;
  }

  /**
   * Bring Slack up to a turn that is still going. A write Slack refuses does
   * not end the turn: writing stops for now, and `finish` still ends it.
   */
  update(turn: Turn): Promise<void> {
    return this.serial(() => this.updateNow(turn));
  }

  /** Run after everything already asked of this turn, whatever became of it. */
  private serial<T>(work: () => Promise<T>): Promise<T> {
    const next = this.queue.then(work, work);
    this.queue = next.catch(() => {});
    return next;
  }

  private async updateNow(turn: Turn): Promise<void> {
    if (this.closed || this.degraded) return;
    await this.sync(turn);
    try {
      if (this.native) {
        try {
          if (!this.ts) await this.open(turn);
          const { chunks } = streamUpdate(this.sent, turn);
          if (chunks.length > 0) await this.append(turn, chunks);
          this.sent = turn;
          return;
        } catch (e) {
          if (!this.refused(e)) throw e;
        }
      }
      const view = this.view(turn, false);
      const [first = []] = view.pages;
      if (first.length === 0) return;
      if (!this.ts) await this.post(first, view.text);
      else await this.edit(first, view.text);
      this.sent = turn;
    } catch (e) {
      // The rest is written once, whole, when the turn ends.
      this.degraded = true;
      this.log(`writing the turn failed, it will be posted when it ends: ${(e as Error).message}`);
    }
  }

  /**
   * End the turn on Slack with its final layout, and settle the reaction and
   * the session. It always gets as far as the session: when the message being
   * written can't take the end of the turn, the stream is closed and the whole
   * turn is posted again as a new message.
   */
  finish(turn: Turn): Promise<Presented> {
    return this.serial(() => this.finishNow(turn));
  }

  private async finishNow(turn: Turn): Promise<Presented> {
    if (this.closed) return { ts: this.ts, messages: this.messages };
    this.closed = true;
    /** The final layout. `this.delivered` counts how many of its pages Slack has. */
    let final: ReturnType<TurnStream["view"]> | null = null;
    this.delivered = 0;
    try {
      if (this.degraded) throw new Error("an earlier write failed");
      if (this.native && this.ts) {
        // Flush what was not yet appended, then stop with the final layout.
        const { chunks, rewritten } = streamUpdate(this.sent, turn);
        // Words Slack already has were changed: a stream can't take that back.
        if (rewritten) throw new Error("the answer was rewritten after it streamed");
        if (chunks.length > 0) await this.append(turn, chunks);
        const title = streamStopTitle(turn);
        final = this.view(turn, true);
        const [first = []] = final.pages;
        await this.slack.call("chat.stopStream", {
          channel: this.target.channel,
          ts: this.ts,
          ...(title ? { chunks: [{ type: "plan_update", title }] } : {}),
          blocks: first,
        });
        this.delivered = 1;
        await this.overflow(final);
        for (const w of final.warnings) this.log(`render warning: ${w}`);
      } else {
        // Nothing was streamed, so the message carries the whole turn.
        final = this.view(turn, false);
        const [first = []] = final.pages;
        if (!this.ts) await this.post(first, final.text);
        else await this.edit(first, final.text);
        this.delivered = 1;
        await this.overflow(final);
        for (const w of final.warnings) this.log(`render warning: ${w}`);
      }
    } catch (e) {
      if (final && this.delivered > 0) {
        // The turn's first message is in place: only what didn't arrive is sent again.
        this.log(`a continuation failed, sending the rest again: ${(e as Error).message}`);
        await this.overflow(final).catch((again: Error) =>
          this.log(`the rest of the turn could not be posted: ${again.message}`),
        );
      } else {
        this.log(`ending the turn in place failed, posting it whole: ${(e as Error).message}`);
        if (this.native && this.ts) {
          await this.slack
            .call("chat.stopStream", { channel: this.target.channel, ts: this.ts })
            .catch(() => {});
        }
        const whole = this.view(turn, false);
        try {
          const [first = []] = whole.pages;
          await this.post(first, whole.text);
          this.delivered = 1;
          await this.overflow(whole);
        } catch (again) {
          this.log(`the turn could not be posted: ${(again as Error).message}`);
        }
      }
    }
    // Reactions and the session status are each tried, whatever happened above.
    await this.sync(turn);
    return { ts: this.ts, messages: this.messages };
  }

  /**
   * Final blocks that outgrew one message follow it, each with its own
   * fallback text: every page Slack doesn't have yet, counted as it lands.
   */
  private async overflow(final: { pages: Block[][]; text: string }) {
    while (this.delivered < final.pages.length) {
      const blocks = final.pages[this.delivered] as Block[];
      const page = this.delivered + 1;
      await this.post(blocks, this.copy.continued(page, final.pages.length, final.text));
      this.delivered = page;
    }
  }
}

export interface StreamResult extends Presented {
  turn: Turn;
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

/**
 * Drive one turn from a list of events, as a host does with a live run:
 * fold each event into the turn and keep Slack in step, until the turn
 * finishes, fails or waits on a person. Token-sized text events are batched,
 * since Slack prefers fewer, larger appends. An aborted signal stops the turn.
 */
export async function streamTurn(
  slack: SlackCaller,
  target: StreamTarget,
  events: TurnEvent[],
  {
    tickMs = 120,
    signal,
    ...opts
  }: TurnStreamOptions & { tickMs?: number; signal?: AbortSignal } = {},
): Promise<StreamResult> {
  const stream = new TurnStream(slack, target, opts);
  let turn: Turn = emptyTurn("", { name: "" });
  for (let i = 0; i < events.length; i++) {
    const event = events[i] as TurnEvent;
    turn = reduce(turn, event);
    if (signal?.aborted) {
      turn = reduce(turn, { type: "turn.stopped" });
      break;
    }
    if (turn.status === "waiting" || turn.status === "failed" || turn.status === "done") break;
    const next = events[i + 1];
    if (next?.type === "text.delta" && event.type === "text.delta" && i % 6 !== 0) continue;
    await stream.update(turn);
    await sleep(tickMs);
  }
  const presented = await stream.finish(turn);
  return { turn, ...presented };
}
