import { TurnView } from "@agent-ui/components";
import { emptyTurn, reduce, type Turn, type TurnEvent, type TurnStatus } from "@agent-ui/core";
import {
  lifecycle,
  reactionChanges,
  renderToSlack,
  streamStart,
  streamStopTitle,
  streamUpdate,
} from "@agent-ui/render-slack";
import type { SlackClient } from "./slack";

/**
 * Drive one turn through real Slack exactly as a host would:
 * reactions on the trigger, session status, chat.startStream, ordered
 * appendStream calls, and chat.stopStream with the final blocks.
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
  /** ts of the streamed message, for later chat.update (e.g. approval decisions). */
  ts: string;
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
  // Status already reflected in reactions and the session, so each change is sent once.
  let shown: TurnStatus | null = null;
  const sync = async () => {
    if (shown === turn.status) return;
    await react(slack, target, shown, turn.status);
    await setStatus(slack, target, turn);
    shown = turn.status;
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

    if (!ts) {
      const started = await slack.call<{ ts: string }>("chat.startStream", {
        channel: target.channel,
        thread_ts: target.threadTs,
        recipient_user_id: target.recipientUser,
        recipient_team_id: target.recipientTeam,
        ...streamStart(turn),
      });
      ts = started.ts;
    }
    const { chunks } = streamUpdate(sent, turn);
    if (chunks.length > 0) {
      // Await each append in order, so no task finishes after the stream stops.
      await slack.call("chat.appendStream", { channel: target.channel, ts, chunks });
      sent = turn;
    }
    await sleep(tickMs);
  }

  // Flush anything not yet appended, then stop with the final layout.
  const { chunks } = streamUpdate(sent, turn);
  const title = streamStopTitle(turn);
  const final = renderToSlack(<TurnView turn={turn} streamed />);
  if (!ts) {
    const posted = await slack.call<{ ts: string }>("chat.postMessage", {
      channel: target.channel,
      thread_ts: target.threadTs,
      text: final.text,
      blocks: final.blocks,
    });
    ts = posted.ts;
  } else {
    await slack.call("chat.stopStream", {
      channel: target.channel,
      ts,
      chunks: [...chunks, ...(title ? [{ type: "plan_update", title }] : [])],
      blocks: final.blocks,
    });
  }
  for (const w of final.warnings) console.warn(`  render warning: ${w}`);
  await sync();
  return { turn, ts };
}
