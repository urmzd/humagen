import {
  type Copy,
  defaultCopy,
  linkCitations,
  type TurnCapabilities,
  TurnView,
} from "@humagen/components";
import { ActionId, formatTokens, formatUsd, type Task, type Turn, type Usage } from "@humagen/core";
import { Markdown } from "@humagen/primitives";
import {
  ActionContext,
  FeedbackPromptContext,
  GaugeIcon,
  type ItemActions,
  ItemActionsContext,
  Rating,
  RedirectIcon,
  RefreshIcon,
  RequestBubble,
  ShimmerVerb,
  SuppressContext,
  THINKING_VERBS,
  TurnMetaContext,
  useVerbSequence,
} from "@humagen/render-web";
import {
  type CSSProperties,
  type ReactNode,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { AgentInitials, AgentOrb, type OrbState, turnOrbState } from "./AgentOrb";
import { StopBanner } from "./Alerts";
import { MessageActionRow, messageActionButton } from "./MessageActionRow";
import { Reasoning } from "./Reasoning";
import { type Block, interleave, StepGroup, WorkedFor } from "./WorkSteps";

function seconds(ms?: number) {
  if (ms === undefined) return null;
  const s = Math.round(ms / 1000);
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}

/** Agent mark: its initials ringed in its color, or the host's avatar, with a small orb while it works. */
export function AgentAvatar({ agent, state = "idle" }: { agent: Turn["agent"]; state?: OrbState }) {
  if (!agent.avatarUrl) return <AgentInitials name={agent.name} state={state} className="mt-px" />;
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

/** How a host stops the run behind a mark. `pending` while the ledger has not recorded the end. */
export interface MarkStop {
  onStop: () => void;
  pending?: boolean;
  /** Why the last request did not take, said under the message. */
  problem?: string;
}

const working = (s: OrbState) => !["idle", "done", "error", "stopped", "waiting"].includes(s);

/**
 * The mark of an agent at work, as the way to stop it. It moves while the
 * agent works; hover or keyboard focus turns it into a stop square. A touch
 * shows the square first and stops on the second tap. A run waiting on a
 * person is not working, so it is a plain, still mark.
 */
export function StoppableMark({
  agent,
  state,
  stop,
  children,
  copy = defaultCopy,
}: {
  copy?: Copy;
  agent: string;
  state: OrbState;
  stop?: MarkStop;
  children: ReactNode;
}) {
  const [armed, setArmed] = useState(false);
  const touch = useRef(false);
  if (!stop || (!working(state) && !stop.pending)) return <>{children}</>;
  const label = stop.pending
    ? copy.conversation.stoppingAgent(agent)
    : copy.conversation.stopAgent(agent);
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      disabled={stop.pending}
      data-armed={armed || undefined}
      onPointerDown={(e) => {
        touch.current = e.pointerType === "touch";
      }}
      onBlur={() => setArmed(false)}
      onClick={() => {
        if (touch.current && !armed) return setArmed(true);
        setArmed(false);
        stop.onStop();
      }}
      className="group/stop relative inline-flex size-7 shrink-0 items-center justify-center rounded-full focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-aui-accent disabled:cursor-progress"
    >
      <span
        className={`flex items-center justify-center ${stop.pending ? "opacity-50" : "group-hover/stop:opacity-0 group-focus-visible/stop:opacity-0 group-data-[armed]/stop:opacity-0"}`}
      >
        {children}
      </span>
      {!stop.pending && (
        <span
          aria-hidden
          className="absolute inset-0 flex items-center justify-center rounded-full bg-aui-hover opacity-0 group-hover/stop:opacity-100 group-focus-visible/stop:opacity-100 group-data-[armed]/stop:opacity-100"
        >
          <span className="size-2.5 rounded-[2px] bg-aui-fg" />
        </span>
      )}
    </button>
  );
}

// A finished turn needs no word: a still mark and its answer say it.
const dot: Record<Exclude<Turn["status"], "done">, string> = {
  queued: "bg-aui-muted",
  working: "bg-aui-accent",
  waiting: "bg-aui-accent",
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
        <GaugeIcon size={16} />
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

const actionBtn = messageActionButton;

/** What a call gave the other agent: the prompt in its arguments, else the arguments. */
export function callPrompt(task: Task): string {
  const raw = task.input?.trim() ?? "";
  if (!raw) return "";
  try {
    const args = JSON.parse(raw) as Record<string, unknown>;
    for (const key of ["task", "prompt", "message", "input", "text", "query", "instructions"]) {
      const v = args[key];
      if (typeof v === "string" && v.trim()) return v.trim();
    }
    // A call in chat form: the text of its messages, in order.
    if (Array.isArray(args.messages)) {
      const said = (args.messages as { content?: unknown }[])
        .flatMap((m) =>
          typeof m.content === "string"
            ? [m.content]
            : Array.isArray(m.content)
              ? (m.content as { text?: unknown }[]).flatMap((c) =>
                  typeof c.text === "string" ? [c.text] : [],
                )
              : [],
        )
        .join("\n")
        .trim();
      if (said) return said;
    }
  } catch {
    // Not JSON: the arguments are the prompt.
  }
  return raw;
}

/**
 * A call this turn made to another agent, as a message from the caller to
 * it: both marks, what it was asked and what it answered. A call at work is
 * open; one that ended folds to a line that opens again.
 */
function AgentCall({
  task,
  caller,
  turnId,
  copy,
}: {
  task: Task;
  caller: string;
  turnId: string;
  copy: Copy;
}) {
  const onAction = useContext(ActionContext);
  const sub = task.subagent as NonNullable<Task["subagent"]>;
  const live = task.status === "in_progress" || task.status === "waiting";
  const [open, setOpen] = useState(live);
  useEffect(() => {
    if (live) setOpen(true);
  }, [live]);
  const prompt = callPrompt(task);
  const answer = task.status === "complete" ? task.output?.trim() : undefined;
  const word = copy.conversation.callStatus[task.status];
  const tint =
    sub.hue === undefined
      ? {}
      : { "data-accent": "agent", style: { "--aui-accent-h": sub.hue } as CSSProperties };
  const runId = sub.runId;
  return (
    <li className="min-w-0 border-aui-border border-l-2 pl-3" {...tint}>
      <details open={open} onToggle={(e) => setOpen(e.currentTarget.open)} className="min-w-0">
        <summary className="flex min-h-7 cursor-pointer list-none items-center gap-1.5 text-aui-xs text-aui-muted [&::-webkit-details-marker]:hidden">
          <span className="sr-only">{copy.conversation.callAsked(caller)} </span>
          <span aria-hidden>→</span>
          <AgentOrb state={live ? "thinking" : "idle"} size={14} label="" />
          <span className="font-medium text-aui-fg">{sub.name}</span>
          {word && (
            <span className={task.status === "error" ? "text-aui-bad" : "text-aui-accent"}>
              {word}
            </span>
          )}
          {!open && prompt && <span className="min-w-0 flex-1 truncate">{prompt}</span>}
        </summary>
        <div className="flex min-w-0 flex-col gap-2 pt-1 pb-2">
          {prompt && (
            <p className="w-fit max-w-full whitespace-pre-wrap rounded-aui bg-aui-hover px-3 py-1.5 text-aui-sm text-aui-fg">
              {prompt}
            </p>
          )}
          {answer && <p className="whitespace-pre-wrap text-aui-sm text-aui-fg">{answer}</p>}
          {task.status === "error" && task.output ? (
            <p className="whitespace-pre-wrap text-aui-bad text-aui-xs">{task.output}</p>
          ) : (
            !answer && sub.summary && <p className="text-aui-xs text-aui-muted">{sub.summary}</p>
          )}
          {runId && (
            <button
              type="button"
              aria-haspopup="dialog"
              onClick={() => onAction({ id: ActionId.openRun, payload: { turnId, target: runId } })}
              className="w-fit rounded-aui text-aui-xs text-aui-muted underline-offset-2 hover:text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
            >
              {copy.conversation.openCall(sub.name)}
            </button>
          )}
        </div>
      </details>
    </li>
  );
}

/** Copy, regenerate, rate (then an optional comment), and usage. */
function MessageActions({
  turn,
  copy,
  rate,
  regenerate,
  onReply,
  actions,
  onRedirect,
}: {
  actions?: ReactNode;
  onRedirect?: () => void;
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
    <MessageActionRow
      text={text}
      copy={copy}
      copyLabel={copy.conversation.copyAnswer}
      copyControl={hostCopy}
      onReply={onReply}
    >
      {turn.outcome && onRedirect && (
        <button
          type="button"
          className={actionBtn}
          aria-label={copy.conversation.redirect}
          title={copy.conversation.redirect}
          onClick={onRedirect}
        >
          <RedirectIcon size={16} />
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
          <RefreshIcon size={16} />
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
      {actions}
    </MessageActionRow>
  );
}

/**
 * One agent turn in a web chat: the activity orb, name and live status in the
 * header, the reasoning trace, then the shared TurnView. Render inside a WebSurface.
 *
 * The header replaces the "Name: Status" context line TurnView emits for the
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
  timestamp,
  actions,
  onRedirect,
  stop,
  disclaimer = true,
}: {
  turn: Turn;
  /** Stop the run from its mark while it works. Leave out where it cannot be stopped. */
  stop?: MarkStop;
  /**
   * Say the AI disclaimer under the answer. A host that says it once, under
   * its prompt bar, turns it off.
   */
  disclaimer?: boolean;
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
  timestamp?: ReactNode;
  actions?: ReactNode;
  onRedirect?: () => void;
  onAskAgent?: () => void;
  onReply?: () => void;
}) {
  const inherited = useContext(ItemActionsContext);
  const statusLine = `**${turn.agent.name}**: ${copy.status[turn.status]}`;
  const final = turn.status === "done";
  const settledTurn = final || turn.status === "failed" || turn.status === "stopped";
  const suppressed = useMemo(() => {
    const set = new Set([statusLine]);
    if (!disclaimer) set.add(turn.disclaimer ?? copy.disclaimer(turn.agent.name));
    if (final) set.add("block:feedback");
    if (turn.status === "stopped" && turn.stop) {
      set.add(copy.stopped);
      set.add(copy.stopPhase[turn.stop]);
      set.add("block:resume");
    }
    return set;
  }, [
    statusLine,
    final,
    turn.status,
    turn.stop,
    turn.disclaimer,
    turn.agent.name,
    disclaimer,
    copy,
  ]);
  const activity = useActivity(turn);
  const orb = turn.detached ? "idle" : turnOrbState(turn);
  const busy = turn.status === "working" || turn.status === "queued";
  const reasoningLive =
    busy && !turn.detached && !turn.text && !turn.tasks.some((t) => t.status !== "pending");
  // Thinking, steps, calls and words in the order they came; the last words
  // after every step are the answer, drawn with the rest of the turn.
  const { blocks, tail, rest } = useMemo(
    () => interleave(turn.text, turn.tasks, turn.thoughts),
    [turn.text, turn.tasks, turn.thoughts],
  );
  const lastThought = turn.thoughts?.[turn.thoughts.length - 1];
  const thinkingNow =
    busy &&
    !turn.detached &&
    lastThought?.at === turn.text.length &&
    lastThought.after === turn.tasks.length;
  const [traceOpen, setTraceOpen] = useState(true);
  const block = (b: Block) =>
    b.kind === "text" ? (
      <Markdown key={b.key} text={linkCitations(b.text, turn.citations)} />
    ) : b.kind === "thought" ? (
      <Reasoning key={b.key} text={b.text} streaming={thinkingNow && b.last} />
    ) : b.kind === "steps" ? (
      <StepGroup key={b.key} turn={turn} tasks={b.tasks} copy={copy} />
    ) : (
      <ul key={b.key} aria-label="Agent calls" className="flex min-w-0 flex-col">
        <AgentCall task={b.task} caller={turn.agent.name} turnId={turn.id} copy={copy} />
      </ul>
    );

  return (
    <article
      aria-label={`${turn.agent.name} message`}
      aria-busy={busy || undefined}
      data-turn-id={turn.id}
      data-aui-still={turn.detached || undefined}
      className="group/message aui-fade-up grid min-w-0 scroll-mt-4 grid-cols-[1.75rem_minmax(0,1fr)] gap-x-2.5"
    >
      <StoppableMark agent={turn.agent.name} state={orb} stop={stop} copy={copy}>
        <AgentAvatar agent={turn.agent} state={orb} />
      </StoppableMark>
      <div className="flex min-w-0 flex-col gap-2">
        <header className="flex min-h-7 min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 text-aui-sm">
          {onAskAgent ? (
            <button
              type="button"
              className="min-h-6 pointer-coarse:min-h-11 max-w-full break-words rounded-aui text-left font-semibold text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
              aria-label={copy.conversation.askAgent(turn.agent.name)}
              onClick={onAskAgent}
            >
              {turn.agent.name}
            </button>
          ) : (
            <span className="max-w-full break-words font-semibold text-aui-fg">
              {turn.agent.name}
            </span>
          )}
          <span className="flex min-w-0 items-center gap-1.5 text-aui-2xs text-aui-muted">
            {activity ? (
              <ShimmerVerb className="truncate">{activity}</ShimmerVerb>
            ) : turn.status !== "done" ? (
              <>
                <span aria-hidden className={`size-1.5 rounded-full ${dot[turn.status]}`} />
                <span>{copy.status[turn.status]}</span>
              </>
            ) : null}
          </span>
          {timestamp && <span className="text-aui-xs text-aui-muted">{timestamp}</span>}
          {/* Announce state changes, not every streamed token. */}
          <output className="sr-only">{copy.status[turn.status]}</output>
        </header>
        {turn.reasoning && !turn.thoughts?.length && (
          <Reasoning text={turn.reasoning} streaming={reasoningLive} />
        )}
        <ItemActionsContext.Provider value={itemActions ?? inherited}>
          <TurnMetaContext.Provider value={turn}>
            <SuppressContext.Provider value={suppressed}>
              <div className="aui-stack">
                {settledTurn && blocks.length > 0 && (
                  <WorkedFor
                    ms={turn.durationMs}
                    open={traceOpen}
                    onToggle={() => setTraceOpen((o) => !o)}
                    copy={copy}
                  />
                )}
                {(!settledTurn || traceOpen) && blocks.map(block)}
                <TurnView
                  turn={{ ...turn, text: tail }}
                  copy={copy}
                  capabilities={capabilities}
                  hideTasks
                />
                {rest.map(block)}
                {turn.status === "stopped" && turn.stop && (
                  <StopBanner turnId={turn.id} stop={turn.stop} label={copy.stopPhase[turn.stop]} />
                )}
              </div>
            </SuppressContext.Provider>
          </TurnMetaContext.Provider>
          {(settledTurn || actions) && (
            <FeedbackPromptContext.Provider value={feedbackPrompt}>
              <MessageActions
                turn={turn}
                copy={copy}
                rate={final && capabilities?.feedback !== false}
                regenerate={final && capabilities?.regenerate !== false}
                onReply={settledTurn ? onReply : undefined}
                actions={actions}
                onRedirect={final ? onRedirect : undefined}
              />
            </FeedbackPromptContext.Provider>
          )}
        </ItemActionsContext.Provider>
        {stop?.problem && (
          <p role="alert" className="text-aui-xs text-aui-bad">
            {stop.problem}
          </p>
        )}
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
