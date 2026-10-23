import {
  type Copy,
  defaultCopy,
  TaskList,
  type TurnCapabilities,
  TurnView,
} from "@humagen/components";
import { ActionId, formatTokens, formatUsd, type Turn, type Usage } from "@humagen/core";
import {
  ActionContext,
  CopyButton,
  FeedbackPromptContext,
  GaugeIcon,
  type ItemActions,
  ItemActionsContext,
  MessageIcon,
  Rating,
  RefreshIcon,
  RequestBubble,
  ShimmerVerb,
  SuppressContext,
  THINKING_VERBS,
  TurnMetaContext,
  useVerbSequence,
} from "@humagen/render-web";
import { useContext, useEffect, useMemo, useState } from "react";
import { AgentOrb, type OrbState, turnOrbState } from "./AgentOrb";
import { StopBanner } from "./Alerts";
import { Reasoning } from "./Reasoning";

function seconds(ms?: number) {
  if (ms === undefined) return null;
  const s = Math.round(ms / 1000);
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}

/** Agent mark: the activity orb, or the host's avatar with a small orb while it works. */
export function AgentAvatar({ agent, state = "idle" }: { agent: Turn["agent"]; state?: OrbState }) {
  if (!agent.avatarUrl) return <AgentOrb state={state} size="md" label="" className="mt-px" />;
  const live = state !== "idle" && state !== "done";
  return (
    <span aria-hidden className="relative flex size-7 shrink-0 items-center justify-center">
      <img src={agent.avatarUrl} alt="" className="size-7 rounded-full object-cover" />
      {live && (
        <AgentOrb state={state} size="xs" label="" className="absolute -right-0.5 -bottom-0.5" />
      )}
    </span>
  );
}

const dot: Record<Turn["status"], string> = {
  queued: "bg-aui-muted",
  working: "bg-aui-accent",
  waiting: "bg-aui-accent",
  done: "bg-aui-ok",
  failed: "bg-aui-bad",
  stopped: "bg-aui-muted",
};

/** What the agent is doing right now, as a verb: the running task, else a thinking sequence. */
function useActivity(turn: Turn): string | null {
  // A run followed quietly shows its status, not motion.
  const busy = (turn.status === "working" || turn.status === "queued") && !turn.detached;
  const nothingYet = !turn.text && turn.tasks.length === 0;
  const verb = useVerbSequence(busy && nothingYet, THINKING_VERBS);
  if (!busy) return null;
  const running = [...turn.tasks].reverse().find((t) => t.status === "in_progress");
  if (running) return running.title;
  if (turn.status === "queued") return "Starting…";
  if (turn.text) return "Writing…";
  return verb;
}

function UsageChip({ usage }: { usage: Usage }) {
  const [open, setOpen] = useState(false);
  const rows: [string, string][] = [];
  if (usage.model) rows.push(["Model", usage.model]);
  if (usage.inputTokens !== undefined)
    rows.push(["Input", `${formatTokens(usage.inputTokens)} tokens`]);
  if (usage.outputTokens !== undefined)
    rows.push(["Output", `${formatTokens(usage.outputTokens)} tokens`]);
  if (usage.cacheReadTokens)
    rows.push(["Cache read", `${formatTokens(usage.cacheReadTokens)} tokens`]);
  if (usage.cacheWriteTokens)
    rows.push(["Cache write", `${formatTokens(usage.cacheWriteTokens)} tokens`]);
  // Unpriced models omit cost rather than show $0.00.
  if (usage.costUsd !== undefined) rows.push(["Cost", formatUsd(usage.costUsd)]);
  if (usage.latencyMs !== undefined) rows.push(["Latency", seconds(usage.latencyMs) ?? ""]);
  if (rows.length === 0) return null;
  return (
    <span className="relative">
      <button
        type="button"
        aria-label="Usage details"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        onMouseEnter={() => setOpen(true)}
        onMouseLeave={() => setOpen(false)}
        onBlur={() => setOpen(false)}
        className={actionBtn}
      >
        <GaugeIcon size={13} />
      </button>
      {open && (
        <span
          role="tooltip"
          className="aui-pop-in aui-glass absolute bottom-full left-0 z-40 mb-1 grid w-52 grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 rounded-aui p-2 text-aui-2xs"
        >
          {rows.map(([k, v]) => (
            <span key={k} className="contents">
              <span className="text-aui-muted">{k}</span>
              <span className="text-right text-aui-fg tabular-nums">{v}</span>
            </span>
          ))}
        </span>
      )}
    </span>
  );
}

const actionBtn =
  "inline-flex size-7 items-center justify-center rounded-aui-sm text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:text-aui-accent";

function WorkSummary({ turn, copy }: { turn: Turn; copy: Copy }) {
  const attention = turn.tasks.some((task) => task.status === "waiting" || task.status === "error");
  const [open, setOpen] = useState(attention);
  useEffect(() => {
    if (attention) setOpen(true);
  }, [attention]);
  const completed = turn.tasks.filter((task) => task.status === "complete").length;
  const blocked = turn.tasks.filter(
    (task) => task.status === "waiting" || task.status === "error",
  ).length;
  if (turn.tasks.length === 0) return null;
  return (
    <details
      open={open}
      onToggle={(event) => setOpen(event.currentTarget.open)}
      className="min-w-0 rounded-aui border border-aui-border px-3 py-2"
    >
      <summary className="min-h-6 cursor-pointer text-aui-xs text-aui-muted">
        {[
          copy.conversation.tools(turn.tasks.length),
          completed ? copy.conversation.complete(completed) : "",
          blocked ? copy.conversation.blocked(blocked) : "",
        ]
          .filter(Boolean)
          .join(" · ")}
      </summary>
      <div className="mt-3 flex min-w-0 flex-col gap-1">
        <TaskList turn={turn} copy={copy} />
      </div>
    </details>
  );
}

/** Copy, regenerate, rate (then an optional comment), and usage. */
function MessageActions({
  turn,
  copy,
  rate,
  regenerate,
  onReply,
}: {
  turn: Turn;
  copy: Copy;
  rate: boolean;
  regenerate: boolean;
  onReply?: () => void;
}) {
  const onAction = useContext(ActionContext);
  const itemActions = useContext(ItemActionsContext);
  const text = turn.text.replace(/\[\[[^\]]+\]\]/g, "").trim();
  const hostCopy = text ? (itemActions?.({ kind: "answer", turn }) ?? null) : null;
  return (
    <div role="toolbar" aria-label="Message actions" className="-ml-1.5 flex items-center gap-0.5">
      {/* One copy control, never two: the host's replaces the built-in when it draws one. */}
      {text && (hostCopy ?? <CopyButton text={text} label="Copy answer" note="above" />)}
      {onReply && (
        <button
          type="button"
          className={actionBtn}
          aria-label={copy.conversation.reply}
          title={copy.conversation.reply}
          onClick={onReply}
        >
          <MessageIcon size={13} />
        </button>
      )}
      {regenerate && (
        <button
          type="button"
          aria-label="Regenerate"
          title="Regenerate"
          className={actionBtn}
          onClick={() =>
            onAction({ id: ActionId.retry, payload: { turnId: turn.id, target: "regenerate" } })
          }
        >
          <RefreshIcon size={13} />
        </button>
      )}
      {rate && (
        <Rating
          up={{ id: ActionId.feedback, payload: { turnId: turn.id, target: "up" } }}
          down={{ id: ActionId.feedback, payload: { turnId: turn.id, target: "down" } }}
          selected={turn.feedback}
          labels={{ up: copy.helpful, down: copy.notHelpful }}
          prompt={copy.feedbackPrompt}
        />
      )}
      {turn.usage && <UsageChip usage={turn.usage} />}
    </div>
  );
}

/**
 * One agent turn in a web chat: the activity orb, name and live status in the
 * header, the reasoning trace, then the shared TurnView. Render inside a WebSurface.
 *
 * The header replaces the "Name · Status" context line TurnView emits for the
 * web, so the status is said once. Slack shows status as a reaction instead.
 * Finished turns get a compact action row (copy, regenerate, rate, usage).
 */
export function AgentMessage({
  turn,
  copy = defaultCopy,
  itemActions,
  feedbackPrompt = true,
  capabilities,
  onAskAgent,
  onReply,
}: {
  turn: Turn;
  copy?: Copy;
  /**
   * The host's own controls for each item this turn draws: every tool call,
   * every step another agent did, and the answer. Use it for a "Copy as" menu.
   * What it returns for the answer replaces the built-in Copy answer button;
   * return null there to keep the built-in one.
   * Leave it out to inherit one from an `ItemActionsContext` further up.
   */
  itemActions?: ItemActions;
  /**
   * Ask for an optional comment after a rating. On by default. The rating is
   * always sent on the click; turning this off only skips the question.
   */
  feedbackPrompt?: boolean;
  /**
   * What the host can act on. `feedback: false` hides the rating; `report: false` Report Issue;
   * `regenerate: false` Regenerate.
   */
  capabilities?: TurnCapabilities;
  onAskAgent?: () => void;
  onReply?: () => void;
}) {
  const inherited = useContext(ItemActionsContext);
  const statusLine = `**${turn.agent.name}** · ${copy.status[turn.status]}`;
  const final = turn.status === "done";
  const suppressed = useMemo(() => {
    const set = new Set([statusLine]);
    if (final) set.add("block:feedback");
    if (turn.status === "stopped" && turn.stop) {
      set.add(copy.stopped);
      set.add(copy.stopPhase[turn.stop]);
      set.add("block:resume");
    }
    return set;
  }, [statusLine, final, turn.status, turn.stop, copy]);
  const activity = useActivity(turn);
  const busy = turn.status === "working" || turn.status === "queued";
  const reasoningLive =
    busy && !turn.detached && !turn.text && !turn.tasks.some((t) => t.status !== "pending");

  const settled =
    turn.status === "done" && turn.durationMs !== undefined
      ? `${copy.status.done} in ${seconds(turn.durationMs)}`
      : copy.status[turn.status];

  return (
    <article
      aria-label={`${turn.agent.name} message`}
      aria-busy={busy || undefined}
      data-turn-id={turn.id}
      data-aui-still={turn.detached || undefined}
      className="aui-fade-up grid min-w-0 scroll-mt-4 grid-cols-[1.75rem_minmax(0,1fr)] gap-x-2.5"
    >
      <AgentAvatar agent={turn.agent} state={turn.detached ? "idle" : turnOrbState(turn)} />
      <div className="flex min-w-0 flex-col gap-2">
        <header className="flex h-7 min-w-0 items-center gap-2 text-aui-sm">
          {onAskAgent ? (
            <button
              type="button"
              className="min-h-6 rounded-aui font-semibold text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
              aria-label={copy.conversation.askAgent(turn.agent.name)}
              onClick={onAskAgent}
            >
              {turn.agent.name}
            </button>
          ) : (
            <span className="font-semibold text-aui-fg">{turn.agent.name}</span>
          )}
          <span className="flex min-w-0 items-center gap-1.5 text-aui-2xs text-aui-muted">
            {activity ? (
              <ShimmerVerb className="truncate">{activity}</ShimmerVerb>
            ) : (
              <>
                <span aria-hidden className={`size-1.5 rounded-full ${dot[turn.status]}`} />
                <span>{settled}</span>
              </>
            )}
          </span>
          {/* Announce state changes, not every streamed token. */}
          <output className="sr-only">{copy.status[turn.status]}</output>
        </header>
        {turn.reasoning && <Reasoning text={turn.reasoning} streaming={reasoningLive} />}
        <ItemActionsContext.Provider value={itemActions ?? inherited}>
          <TurnMetaContext.Provider value={turn}>
            <SuppressContext.Provider value={suppressed}>
              <div className="aui-stack">
                <WorkSummary turn={turn} copy={copy} />
                <TurnView turn={turn} copy={copy} capabilities={capabilities} hideTasks />
                {turn.status === "stopped" && turn.stop && (
                  <StopBanner turnId={turn.id} stop={turn.stop} label={copy.stopPhase[turn.stop]} />
                )}
              </div>
            </SuppressContext.Provider>
          </TurnMetaContext.Provider>
          {final && (
            <FeedbackPromptContext.Provider value={feedbackPrompt}>
              <MessageActions
                turn={turn}
                copy={copy}
                rate={capabilities?.feedback !== false}
                regenerate={capabilities?.regenerate !== false}
                onReply={onReply}
              />
            </FeedbackPromptContext.Provider>
          )}
        </ItemActionsContext.Provider>
      </div>
    </article>
  );
}

/**
 * The person's message in a chat: a right-aligned bubble. The same bubble
 * `RequestView` draws inside a surface (which Slack posts as the thread's
 * first message), without the name, since a chat's own speaker is "You".
 */
export function UserMessage({ text, name = "You" }: { text: string; name?: string }) {
  return <RequestBubble by={name} text={text} showBy={false} />;
}
