import { TurnView } from "@vultus/components";
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
import { type SlackClient, SlackError } from "./slack";

/**
 * Drive one turn through real Slack exactly as a host would:
 * reactions on the trigger, session status, chat.startStream, ordered
 * appendStream calls, and chat.stopStream with the final blocks. A turn that
 * outgrows one message continues in the next one, in the same thread.
 */

export interface StreamTarget {
  channel: string;
  /** Thread root; streaming in channels must be threaded. */
  threadTs: string;
  /** Message that triggered the turn; lifecycle reactions go here. */
  triggerTs: string;
  recipientUser: string;
  recipientTeam: string;
}

export interface StreamResult {
  turn: Turn;
  /** ts of the message holding the final blocks, for later chat.update (e.g. approval decisions). */
  ts: string;
  /** Every message the turn used, in order. More than one when it overflowed. */
  messages: string[];
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

async function react(
  slack: SlackClient,
  target: StreamTarget,
  prev: TurnStatus | null,
  next: TurnStatus,
) {
  const { add, remove } = reactionChanges(prev, next);
  for (const name of remove) {
    await slack
      .call("reactions.remove", { channel: target.channel, timestamp: target.triggerTs, name })
      .catch(() => {});
  }
  for (const name of add) {
    await slack
      .call("reactions.add", { channel: target.channel, timestamp: target.triggerTs, name })
      .catch(() => {});
  }
}

async function setStatus(slack: SlackClient, target: StreamTarget, turn: Turn) {
  const { sessionStatus } = lifecycle(turn);
  await slack
    .call("agents.sessions.setStatus", {
      channel_id: target.channel,
      thread_ts: target.threadTs,
      status: sessionStatus,
    })
    .catch((e: Error) => console.warn(`  setStatus skipped: ${e.message}`));
}

export async function streamTurn(
  slack: SlackClient,
  target: StreamTarget,
  events: TurnEvent[],
  { tickMs = 120, signal }: { tickMs?: number; signal?: AbortSignal } = {},
): Promise<StreamResult> {
  let turn: Turn = emptyTurn("", { name: "" });
  let sent: Turn | null = null;
  let ts = "";
  const messages: string[] = [];
  let page = 1;
  let budget = emptyBudget();
  // Status already reflected in reactions and the session, so each change is sent once.
  let shown: TurnStatus | null = null;
  const sync = async () => {
    if (shown === turn.status) return;
    await react(slack, target, shown, turn.status);
    await setStatus(slack, target, turn);
    shown = turn.status;
  };

  const open = async () => {
    const args = {
      channel: target.channel,
      thread_ts: target.threadTs,
      recipient_user_id: target.recipientUser,
      recipient_team_id: target.recipientTeam,
      ...streamStart(turn),
    };
    const header = streamHeader(turn, { page });
    let started: { ts: string };
    try {
      started = await slack.call("chat.startStream", {
        ...args,
        ...(header.length > 0 ? { chunks: header } : {}),
      });
    } catch (e) {
      const rejected = e instanceof SlackError && /^invalid_(chunks|blocks)$/.test(e.code);
      if (!rejected || header.length === 0) throw e;
      // The workspace doesn't take blocks chunks yet: say the same thing as a line of text.
      console.warn(`  header sent as text: ${e.message}`);
      started = await slack.call("chat.startStream", {
        ...args,
        markdown_text: `_Part ${page}_\n\n`,
      });
    }
    ts = started.ts;
    messages.push(ts);
  };
  /** Close the current message and continue in a new one. */
  const turnPage = async (trailer: boolean) => {
    if (trailer) {
      await slack
        .call("chat.appendStream", { channel: target.channel, ts, chunks: [streamTrailer()] })
        .catch(() => {});
    }
    await slack.call("chat.stopStream", { channel: target.channel, ts });
    page += 1;
    await open();
  };
  const send = async (chunks: StreamChunk[]) => {
    const plan = streamPages(budget, chunks);
    for (const [i, part] of plan.pages.entries()) {
      if (i > 0) await turnPage(true);
      if (part.length === 0) continue;
      if (!ts) await open();
      try {
        // Await each append in order, so no task finishes after the stream stops.
        await slack.call("chat.appendStream", { channel: target.channel, ts, chunks: part });
      } catch (e) {
        if (!(e instanceof SlackError) || e.code !== "msg_too_long") throw e;
        // The estimate was off and the message is full: it can't take a trailer either.
        await turnPage(false);
        await slack.call("chat.appendStream", { channel: target.channel, ts, chunks: part });
        plan.budget = streamPages(emptyBudget(), part).budget;
      }
    }
    budget = plan.budget;
  };
  const post = async (blocks: Block[], text: string) => {
    const posted = await slack.call<{ ts: string }>("chat.postMessage", {
      channel: target.channel,
      thread_ts: target.threadTs,
      text: text.slice(0, LIMITS.fallbackText),
      blocks,
    });
    ts = posted.ts;
    messages.push(ts);
  };

  for (let i = 0; i < events.length; i++) {
    const event = events[i] as TurnEvent;
    const before = turn;
    turn = reduce(turn, event);
    if (signal?.aborted) {
      turn = reduce(turn, { type: "turn.stopped" });
      break;
    }
    if (before.status !== turn.status) await sync();
    if (turn.status === "waiting" || turn.status === "failed" || turn.status === "done") break;

    // Batch token-sized events into one append per tick; Slack prefers fewer, larger appends.
    const next = events[i + 1];
    if (next?.type === "text.delta" && event.type === "text.delta" && i % 6 !== 0) continue;

    if (!ts) await open();
    const { chunks } = streamUpdate(sent, turn);
    if (chunks.length > 0) {
      await send(chunks);
      sent = turn;
    }
    await sleep(tickMs);
  }

  // Flush anything not yet appended, then stop with the final layout.
  const { chunks } = streamUpdate(sent, turn);
  const title = streamStopTitle(turn);
  const final = renderToSlack(<TurnView turn={turn} streamed />);
  const [first = [], ...rest] = final.pages;
  if (!ts) {
    await post(first, final.text);
  } else {
    if (chunks.length > 0) await send(chunks);
    await slack.call("chat.stopStream", {
      channel: target.channel,
      ts,
      ...(title ? { chunks: [{ type: "plan_update", title }] } : {}),
      blocks: first,
    });
  }
  // Final blocks that outgrew one message follow it; each needs its own fallback text.
  for (const [i, blocks] of rest.entries()) {
    await post(blocks, `(${i + 2}/${final.pages.length}) ${final.text}`);
  }
  for (const w of final.warnings) console.warn(`  render warning: ${w}`);
  await sync();
  return { turn, ts, messages };
}
