import {
  AnswerText,
  type Copy,
  defaultCopy,
  linkCitations,
  reviewMarkdown,
  type TurnCapabilities,
  TurnView,
} from "@humagen/components";
import {
  ActionId,
  type ConnectionNeed,
  formatTokens,
  formatUsd,
  readReview,
  type Task,
  type Turn,
  type Usage,
} from "@humagen/core";
import {
  ActionContext,
  ChevronRightIcon,
  FeedbackPromptContext,
  GaugeIcon,
  GitHubIcon,
  GitHubRefKindsContext,
  GitHubRefWordsContext,
  githubRefKinds,
  type ItemActions,
  ItemActionsContext,
  ItemStopControl,
  type ItemStops,
  ItemStopsContext,
  Rating,
  RedirectIcon,
  RefreshIcon,
  RequestBubble,
  Shimmer,
  ShimmerVerb,
  SuppressContext,
  THINKING_VERBS,
  TurnMetaContext,
  useVerbSequence,
} from "@humagen/render-web";
import {
  Children,
  type CSSProperties,
  createContext,
  Fragment,
  type ReactNode,
  useContext,
  useMemo,
  useRef,
  useState,
} from "react";
import { AgentChangeCards } from "./AgentChangeCard.tsx";
import { AgentInitials, AgentOrb, type OrbState, turnOrbState } from "./AgentOrb";
import { StopBanner } from "./Alerts";
import { MessageActionRow, messageActionButton } from "./MessageActionRow";
import { MessageMore } from "./MessageMore";
import { Reasoning } from "./Reasoning";
import { TurnChanges } from "./TurnChanges";
import { type Block, Elapsed, failureLine, interleave, StepGroup } from "./WorkSteps";

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

/** The words of every item's Stop, from the copy. */
export function stopWords(copy: Copy) {
  const d = copy.dock;
  return {
    stop: d.stop,
    stopping: d.stopping,
    stopped: d.stopped,
    stopLabel: d.stopLabel,
    stoppingLabel: d.stoppingLabel,
    stoppedLabel: d.stoppedLabel,
  };
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

// A finished turn needs no word: a still mark and its answer say it. Waiting on
// a person is the one amber the sidebar uses, never the agent's own color.
const dot: Record<Exclude<Turn["status"], "done">, string> = {
  queued: "bg-aui-muted",
  working: "bg-aui-accent",
  waiting: "bg-aui-warn",
  failed: "bg-aui-bad",
  stopped: "bg-aui-muted",
};

/** What the agent is doing right now, as a verb: the running task, else a thinking sequence. */
/** What a turn at work is doing now, in a few words that turn while it only thinks; null once it is not working. */
export function useTurnActivity(turn: Turn): string | null {
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
    for (const key of [
      "brief",
      "task",
      "prompt",
      "message",
      "input",
      "text",
      "query",
      "instructions",
    ]) {
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

/** One line for a call: the title the caller gave it, else the first line of what it asked. */
export function callTitle(task: Task): string {
  try {
    const args = JSON.parse(task.input?.trim() || "{}") as Record<string, unknown>;
    if (typeof args.title === "string" && args.title.trim()) return args.title.trim();
  } catch {
    // Not JSON: the arguments are the prompt.
  }
  return callPrompt(task).split("\n", 1)[0] ?? "";
}

/**
 * A called agent's own thread, drawn where the call happened: what that agent
 * said and did, in its own order, under its own line. A run is several
 * threads, the caller's and each agent's it called, and a host that holds
 * them gives each call its thread here. Null, or a call with no thread yet,
 * leaves the call as one line.
 */
export const CallThreadContext = createContext<((task: Task) => ReactNode) | null>(null);

/**
 * A call this turn made to another agent, as one item where it happened:
 * the agent's mark, its name, where it stands and one line of what it was
 * asked. Its name shimmers while it works. The item opens the run it
 * started, with every step that run took (`ActionId.openRun`). A call that
 * started no run opens in place, to what it was asked and what came back.
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
  const stops = useContext(ItemStopsContext);
  const turn = useContext(TurnMetaContext);
  const sub = task.subagent as NonNullable<Task["subagent"]>;
  const stop = stops && turn ? stops.of({ kind: "subagent", turn, task }) : null;
  const stopped = Boolean(stop?.stopped);
  const live = task.status === "in_progress" && !stopped;
  const [open, setOpen] = useState(false);
  const retryCalls = useContext(RetryCallContext);
  // Asking again is in flight, or was accepted; a refusal makes Retry usable again.
  const [retry, setRetry] = useState<"idle" | "pending" | "sent">("idle");
  const failed = task.status === "error" && !stopped;
  const reason = failed ? (failureLine(task.output) ?? sub.summary) : undefined;
  const prompt = callPrompt(task);
  const call = prompt ? { turnId, agent: sub.name, text: prompt } : undefined;
  const canRetry = !!call && !!retryCalls && (retryCalls.allows?.(call) ?? true);
  const title = callTitle(task);
  const answer = task.status === "complete" ? task.output?.trim() : undefined;
  // A call someone stopped reads as stopped, calmly, not as a failure.
  const word = stopped ? undefined : copy.conversation.callStatus[task.status];
  const ms = sub.durationMs ?? task.durationMs;
  const tint =
    sub.hue === undefined
      ? {}
      : { "data-accent": "agent", style: { "--aui-accent-h": sub.hue } as CSSProperties };
  const runId = sub.runId;
  return (
    <div
      data-aui="agent-call"
      data-status={stopped ? "stopped" : task.status}
      className="min-w-0"
      {...tint}
    >
      <div className="flex min-w-0 items-center gap-1 rounded-aui border border-aui-border pr-1">
        <button
          type="button"
          {...(runId ? { "aria-haspopup": "dialog" as const } : { "aria-expanded": open })}
          onClick={() =>
            runId
              ? onAction({ id: ActionId.openRun, payload: { turnId, target: runId } })
              : setOpen(!open)
          }
          className="group/call flex min-w-0 flex-1 pointer-coarse:min-h-11 items-center gap-2 rounded-aui px-2.5 py-1.5 text-left text-aui-sm transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent motion-reduce:transition-none"
        >
          <span className="sr-only">{copy.conversation.callAsked(caller)} </span>
          <AgentInitials name={sub.name} size={18} />
          <span className="shrink-0 font-medium text-aui-fg">
            {live ? <Shimmer>{sub.name}</Shimmer> : sub.name}
          </span>
          {word && (
            <span
              className={`shrink-0 text-aui-xs ${task.status === "error" || task.status === "denied" ? "text-aui-bad" : task.status === "waiting" ? "text-aui-warn" : "text-aui-accent"}`}
            >
              {word}
            </span>
          )}
          <span className="min-w-0 flex-1 truncate text-aui-xs text-aui-muted">{title}</span>
          {live && task.startedAt && (
            <span className="text-aui-2xs">
              <Elapsed since={task.startedAt} copy={copy} />
            </span>
          )}
          {!live && ms !== undefined && ms > 0 && (
            <span className="shrink-0 text-aui-2xs text-aui-muted tabular-nums">{seconds(ms)}</span>
          )}
          <ChevronRightIcon
            size={12}
            strokeWidth={2.5}
            className={`shrink-0 text-aui-muted opacity-60 group-hover/call:opacity-100 ${!runId && open ? "rotate-90" : ""}`}
          />
        </button>
        {stop && stops && <ItemStopControl stop={stop} words={stops.words} />}
      </div>
      {reason && (
        <div
          data-testid="call-failure"
          className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1 px-2.5 pt-1.5 text-aui-xs"
        >
          <span className="min-w-0 flex-1 basis-60 text-aui-bad [overflow-wrap:anywhere]">
            {reason}
          </span>
          <span className="flex shrink-0 items-center gap-1">
            <button
              type="button"
              {...(runId ? { "aria-haspopup": "dialog" as const } : { "aria-expanded": open })}
              onClick={() =>
                runId
                  ? onAction({ id: ActionId.openRun, payload: { turnId, target: runId } })
                  : setOpen(!open)
              }
              className={callAction}
            >
              {copy.conversation.callFailure.details}
            </button>
            {canRetry && retryCalls && call && (
              <button
                type="button"
                disabled={retry !== "idle"}
                onClick={() => {
                  setRetry("pending");
                  retryCalls.ask(call).then(
                    () => setRetry("sent"),
                    () => setRetry("idle"),
                  );
                }}
                className={`${callAction} gap-1 text-aui-fg`}
              >
                <RefreshIcon size={12} aria-hidden />
                {retry === "idle"
                  ? copy.conversation.callFailure.retry
                  : retry === "pending"
                    ? copy.conversation.callFailure.retrying
                    : copy.conversation.callFailure.retried}
                <span className="sr-only"> {sub.name}</span>
              </button>
            )}
          </span>
        </div>
      )}
      {!runId && open && (
        <div className="flex min-w-0 flex-col gap-2 px-2.5 pt-2 pb-1">
          {prompt && (
            <p className="w-fit max-w-full whitespace-pre-wrap rounded-aui bg-aui-hover px-3 py-1.5 text-aui-sm text-aui-fg">
              {prompt}
            </p>
          )}
          {answer && <p className="whitespace-pre-wrap text-aui-sm text-aui-fg">{answer}</p>}
          {task.status === "error" && task.output ? (
            <pre className="m-0 max-h-64 overflow-auto whitespace-pre-wrap rounded-aui-sm bg-aui-hover px-2.5 py-1.5 font-mono text-aui-2xs text-aui-fg/85">
              {task.output}
            </pre>
          ) : (
            !answer && sub.summary && <p className="text-aui-xs text-aui-muted">{sub.summary}</p>
          )}
        </div>
      )}
    </div>
  );
}

const callAction =
  "inline-flex min-h-6 pointer-coarse:min-h-11 items-center rounded-aui-sm px-1.5 text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-60";

/** One delegated call to ask again: the turn it was made from, the agent, and what it was asked. */
export interface RetryCall {
  turnId: string;
  agent: string;
  text: string;
}

/**
 * How a host asks an agent again what a delegated call that failed asked it,
 * as the conversation's next message. `ask` settles when the host has posted,
 * or refused: a refusal leaves Retry ready to press again, and the host says
 * why. `allows` leaves Retry off a call the host could not post (one too long
 * for a message). Without a host, or where a host provides `null` (a private
 * side chat, another person's answer), a failed call offers no Retry.
 */
export interface RetryCalls {
  ask: (call: RetryCall) => Promise<void>;
  allows?: (call: RetryCall) => boolean;
}

export const RetryCallContext = createContext<RetryCalls | null>(null);

/**
 * A connection the run needed and the person has not made, as a card under
 * the answer: what is missing, and Connect, which opens that connection.
 */
export const ConnectLinkContext = createContext<(provider: string) => string>(
  (provider) => `/settings/plugins/${encodeURIComponent(provider)}`,
);

function ConnectCards({ needs, copy }: { needs: ConnectionNeed[]; copy: Copy }) {
  const link = useContext(ConnectLinkContext);
  const w = copy.conversation.connect;
  return (
    <ul className="m-0 flex list-none flex-col gap-2 p-0">
      {needs.map((n) => (
        <li
          key={n.provider}
          data-testid="connect-card"
          data-provider={n.provider}
          className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 rounded-aui border border-aui-border bg-aui-surface px-3 py-2.5"
        >
          <span
            aria-hidden
            className="flex size-8 shrink-0 items-center justify-center rounded-aui-sm border border-aui-border text-aui-fg"
          >
            {n.provider === "github" ? <GitHubIcon size={16} /> : <PlugIcon />}
          </span>
          <span className="flex min-w-0 flex-1 basis-48 flex-col">
            <span className="font-medium text-aui-fg text-aui-sm">{w.title(n.name)}</span>
            <span className="text-aui-muted text-aui-xs">{w.body(n.name)}</span>
          </span>
          <a
            href={link(n.provider)}
            className="inline-flex h-8 pointer-coarse:h-11 shrink-0 items-center rounded-aui bg-aui-fg px-3 font-medium text-aui-bg text-aui-xs hover:opacity-90 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-aui-accent"
          >
            {w.action(n.name)}
          </a>
        </li>
      ))}
    </ul>
  );
}

function PlugIcon() {
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      width={16}
      height={16}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M9 2v6M15 2v6M6 8h12v4a6 6 0 0 1-12 0zM12 18v4" />
    </svg>
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
  // A review copies as the Markdown its card offers, not as the JSON it came as.
  const review = readReview(turn.text);
  const text =
    review.kind === "review"
      ? reviewMarkdown(review.report, copy)
      : turn.text.replace(/\[\[[^\]]+\]\]/g, "").trim();
  const hostCopy = text ? (itemActions?.({ kind: "answer", turn }) ?? null) : null;
  return (
    <MessageActionRow
      text={text}
      copy={copy}
      copyLabel={copy.conversation.copyAnswer}
      copyControl={hostCopy}
      onReply={onReply}
    >
      {rate && (
        <Rating
          up={{ id: ActionId.feedback, payload: { turnId: turn.id, target: "up" } }}
          down={{ id: ActionId.feedback, payload: { turnId: turn.id, target: "down" } }}
          selected={turn.feedback}
          labels={{ up: copy.helpful, down: copy.notHelpful }}
          prompt={copy.feedbackPrompt}
        />
      )}
      {/* The host's actions (Fork, and Make a skill or Remember after an outcome) sit in the row. */}
      {actions}
      <Less>
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
        {turn.usage && <UsageChip usage={turn.usage} />}
      </Less>
    </MessageActionRow>
  );
}

/** Less frequent actions: in the row when there is one, behind More actions when there are several. */
function Less({ children }: { children: ReactNode }) {
  const shown = Children.toArray(children);
  if (shown.length === 0) return null;
  if (shown.length === 1) return <>{shown}</>;
  return <MessageMore>{shown}</MessageMore>;
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
  itemStops,
  disclaimer = true,
  activity: sayActivity = true,
  embedded = false,
}: {
  turn: Turn;
  /**
   * Each running item's own Stop, at the top of its row: a call to another
   * agent, a step, background work a step started. The agent's own Stop is
   * `stop`. Leave it out to inherit one from an `ItemStopsContext` further up.
   */
  itemStops?: ItemStops;
  /**
   * Say what the agent is doing beside its name while it works. A host that
   * says it in its prompt, for every thread at work, turns it off.
   */
  activity?: boolean;
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
  /**
   * The stream alone, inside something that already names the agent and says
   * its state: a called agent's thread in another answer. No header, no
   * action row, and it is not a message of the conversation (the wave and
   * the keys that step through messages pass it by).
   */
  embedded?: boolean;
}) {
  const inherited = useContext(ItemActionsContext);
  const inheritedStops = useContext(ItemStopsContext);
  const stops = itemStops ?? inheritedStops;
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
  // A number reads one way in the whole answer, and in the session when the host says more.
  const sessionKinds = useContext(GitHubRefKindsContext);
  const refKinds = useMemo(() => {
    const kinds = new Map(sessionKinds);
    for (const [label, kind] of githubRefKinds(turn.text))
      if (kind === "pull" || !kinds.has(label)) kinds.set(label, kind);
    return kinds;
  }, [sessionKinds, turn.text]);
  const callThread = useContext(CallThreadContext);
  const doing = useTurnActivity(turn);
  const activity = sayActivity ? doing : null;
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
  // A review's Ask Follow-up writes in the composer, which a host that may ask this agent has.
  const followUp = capabilities?.followUp ?? onAskAgent !== undefined;
  const block = (b: Block) =>
    b.kind === "text" ? (
      <AnswerText
        key={b.key}
        turnId={turn.id}
        agent={turn.agent.name}
        text={b.text}
        markdown={linkCitations(b.text, turn.citations)}
        pullRequestHint={turn.source?.url}
        followUp={followUp}
        copy={copy}
      />
    ) : b.kind === "thought" ? (
      <Reasoning key={b.key} text={b.text} streaming={thinkingNow && b.last} />
    ) : b.kind === "tools" ? (
      <StepGroup key={b.key} turn={turn} tasks={b.tasks} copy={copy} />
    ) : (
      // The agent's own thread, where the host has it; else the call as one line.
      <Fragment key={b.key}>
        {callThread?.(b.task) ?? (
          <AgentCall task={b.task} caller={turn.agent.name} turnId={turn.id} copy={copy} />
        )}
      </Fragment>
    );

  const Frame = embedded ? "div" : "article";
  return (
    <Frame
      {...(embedded
        ? { "data-aui": "thread-stream", className: "flex min-w-0 flex-col gap-2" }
        : {
            "aria-label": `${turn.agent.name} message`,
            "aria-busy": busy || undefined,
            "data-turn-id": turn.id,
            "data-aui-still": turn.detached || undefined,
            // A tap focuses the message, which shows its actions (theme.css).
            tabIndex: -1,
            // Flat: the reply runs the full width of the column, under one quiet line naming who said it.
            className: "group/message aui-fade-up flex min-w-0 scroll-mt-4 flex-col gap-2",
          })}
    >
      <div className="flex min-w-0 flex-col gap-2">
        {!embedded && (
          <header className="flex min-h-7 min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 text-aui-sm">
            <StoppableMark agent={turn.agent.name} state={orb} stop={stop} copy={copy}>
              <AgentAvatar agent={turn.agent} state={orb} />
            </StoppableMark>
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
              ) : turn.status !== "done" && (sayActivity || !busy) ? (
                <>
                  <span aria-hidden className={`size-1.5 rounded-full ${dot[turn.status]}`} />
                  <span>{copy.status[turn.status]}</span>
                </>
              ) : null}
            </span>
            {timestamp && (
              <span className="text-aui-xs text-aui-muted opacity-0 transition-opacity group-hover/message:opacity-100 group-focus-within/message:opacity-100 motion-reduce:transition-none pointer-coarse:opacity-100">
                {timestamp}
              </span>
            )}
            {/* The agent's own Stop, in words at the top of its answer, beside the mark that also stops it. */}
            {stop && (working(orb) || stop.pending) && (
              <span className="ml-auto">
                <ItemStopControl
                  stop={{ what: turn.agent.name, onStop: stop.onStop, pending: stop.pending }}
                  words={stopWords(copy)}
                />
              </span>
            )}
            {/* Announce state changes, not every streamed token. */}
            <output className="sr-only">{copy.status[turn.status]}</output>
          </header>
        )}
        {turn.reasoning && !turn.thoughts?.length && (
          <Reasoning text={turn.reasoning} streaming={reasoningLive} />
        )}
        <ItemActionsContext.Provider value={itemActions ?? inherited}>
          <ItemStopsContext.Provider value={stops}>
            <GitHubRefWordsContext.Provider value={copy.conversation.githubRef}>
              <GitHubRefKindsContext.Provider value={refKinds}>
                <TurnMetaContext.Provider value={turn}>
                  <SuppressContext.Provider value={suppressed}>
                    <div className="aui-stack">
                      {/* What happened, in the order it happened, before the last words. */}
                      {blocks.map(block)}
                      <TurnView
                        turn={{ ...turn, text: tail }}
                        copy={copy}
                        capabilities={{ ...capabilities, followUp }}
                        hideTasks
                      />
                      <AgentChangeCards tasks={turn.tasks} />
                      {settledTurn && turn.needsConnection?.length ? (
                        <ConnectCards needs={turn.needsConnection} copy={copy} />
                      ) : null}
                      {rest.map(block)}
                      {turn.status === "stopped" && turn.stop && (
                        <StopBanner
                          turnId={turn.id}
                          stop={turn.stop}
                          label={copy.stopPhase[turn.stop]}
                        />
                      )}
                      {/* What the turn changed, once it is over: a card that opens each file. */}
                      {settledTurn && <TurnChanges tasks={turn.tasks} copy={copy} />}
                    </div>
                  </SuppressContext.Provider>
                </TurnMetaContext.Provider>
              </GitHubRefKindsContext.Provider>
            </GitHubRefWordsContext.Provider>
          </ItemStopsContext.Provider>
          {!embedded && (settledTurn || actions) && (
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
    </Frame>
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
