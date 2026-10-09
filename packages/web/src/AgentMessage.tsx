import { type Copy, defaultCopy, TurnView } from "@agent-ui/components";
import type { Turn } from "@agent-ui/core";
import {
  ShimmerVerb,
  SparkleIcon,
  SuppressContext,
  THINKING_VERBS,
  useVerbSequence,
} from "@agent-ui/render-web";
import { useMemo } from "react";
import { Reasoning } from "./Reasoning";

function seconds(ms?: number) {
  if (ms === undefined) return null;
  const s = Math.round(ms / 1000);
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}

/** Agent mark: the host's avatar, or the accent monogram. A ring turns while the agent works. */
export function AgentAvatar({ agent, busy = false }: { agent: Turn["agent"]; busy?: boolean }) {
  return (
    <span aria-hidden className="relative flex size-7 shrink-0 items-center justify-center">
      {busy && (
        <svg
          aria-hidden="true"
          viewBox="0 0 32 32"
          className="aui-spinner absolute -inset-[3px] size-[calc(100%+6px)] text-aui-accent"
          fill="none"
        >
          <circle
            cx="16"
            cy="16"
            r="15"
            stroke="currentColor"
            strokeOpacity="0.15"
            strokeWidth="1.5"
          />
          <path
            d="M31 16A15 15 0 0 0 16 1"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
          />
        </svg>
      )}
      {agent.avatarUrl ? (
        <img src={agent.avatarUrl} alt="" className="size-7 rounded-full object-cover" />
      ) : (
        <span className="flex size-7 items-center justify-center rounded-full bg-aui-accent text-aui-accent-fg shadow-aui-sm">
          <SparkleIcon size={14} fill="currentColor" strokeWidth={1.5} />
        </span>
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
  const busy = turn.status === "working" || turn.status === "queued";
  const nothingYet = !turn.text && turn.tasks.length === 0;
  const verb = useVerbSequence(busy && nothingYet, THINKING_VERBS);
  if (!busy) return null;
  const running = [...turn.tasks].reverse().find((t) => t.status === "in_progress");
  if (running) return running.title;
  if (turn.status === "queued") return "Starting…";
  if (turn.text) return "Writing…";
  return verb;
}

/**
 * One agent turn in a web chat: avatar, name and live status in the header,
 * the reasoning trace, then the shared TurnView. Render inside a WebSurface.
 *
 * The header replaces the "Name · Status" context line TurnView emits for the
 * web, so the status is said once. Slack shows status as a reaction instead.
 */
export function AgentMessage({ turn, copy = defaultCopy }: { turn: Turn; copy?: Copy }) {
  const statusLine = `**${turn.agent.name}** · ${copy.status[turn.status]}`;
  const suppressed = useMemo(() => new Set([statusLine]), [statusLine]);
  const activity = useActivity(turn);
  const busy = turn.status === "working" || turn.status === "queued";
  const reasoningLive = busy && !turn.text && !turn.tasks.some((t) => t.status !== "pending");

  const settled =
    turn.status === "done" && turn.durationMs !== undefined
      ? `${copy.status.done} in ${seconds(turn.durationMs)}`
      : copy.status[turn.status];

  return (
    <article
      aria-label={`${turn.agent.name} message`}
      aria-busy={busy || undefined}
      className="aui-fade-up grid min-w-0 grid-cols-[1.75rem_minmax(0,1fr)] gap-x-3"
    >
      <AgentAvatar agent={turn.agent} busy={busy} />
      <div className="flex min-w-0 flex-col gap-3">
        <header className="flex h-7 min-w-0 items-center gap-2 text-aui-sm">
          <span className="font-semibold text-aui-fg">{turn.agent.name}</span>
          <span className="flex min-w-0 items-center gap-1.5 text-aui-muted text-aui-xs">
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
        <SuppressContext.Provider value={suppressed}>
          <div className="aui-stack">
            <TurnView turn={turn} copy={copy} />
          </div>
        </SuppressContext.Provider>
      </div>
    </article>
  );
}

/** The person's message: a right-aligned bubble. */
export function UserMessage({ text, name = "You" }: { text: string; name?: string }) {
  return (
    <article aria-label={`${name} message`} className="aui-fade-up flex justify-end">
      <p className="max-w-[min(80%,36rem)] whitespace-pre-wrap break-words rounded-aui-lg rounded-br-aui-sm bg-aui-subtle px-3.5 py-2 text-aui-base text-aui-fg">
        {text}
      </p>
    </article>
  );
}
