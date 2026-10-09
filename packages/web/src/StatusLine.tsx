import {
  type ApprovalMode,
  budgetPressure,
  contextPressure,
  formatTokens,
  formatUsd,
  type Pressure,
  type SessionState,
} from "@agent-ui/core";
import { AlertTriangleIcon, ListChecksIcon, ShieldIcon, ZapIcon } from "@agent-ui/render-web";
import { type ReactNode, useEffect, useId, useState } from "react";
import { AgentOrb, orbLabel, orbStateOf } from "./AgentOrb";

const pressureBar: Record<Pressure, string> = {
  ok: "bg-aui-accent",
  warn: "bg-aui-warn",
  critical: "bg-aui-bad",
};
const pressureText: Record<Pressure, string> = {
  ok: "text-aui-muted",
  warn: "text-aui-fg",
  critical: "text-aui-bad",
};

/**
 * One statusline segment. Segments are separated by a slim chevron, like an
 * editor's statusline, but flat: no filled blocks, just type and hairlines.
 */
function Segment({
  children,
  label,
  className,
  tip,
  hideBelow,
}: {
  children: ReactNode;
  label: string;
  className?: string;
  /** Hover/focus card with detail. */
  tip?: ReactNode;
  /** Collapse on narrow widths: "sm" hides below 640px, "md" below 768px. */
  hideBelow?: "sm" | "md";
}) {
  const [open, setOpen] = useState(false);
  const tipId = useId();
  const hide =
    hideBelow === "sm" ? "hidden sm:flex" : hideBelow === "md" ? "hidden md:flex" : "flex";
  return (
    <li
      aria-label={label}
      className={`group/seg relative ${hide} min-w-0 items-center gap-1.5 px-2 before:absolute before:left-0 before:h-3 before:w-px before:rotate-[20deg] before:bg-aui-border first:before:hidden ${className ?? ""}`}
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
      onFocus={() => setOpen(true)}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget)) setOpen(false);
      }}
      aria-describedby={tip && open ? tipId : undefined}
    >
      {children}
      {tip && open && (
        <div
          id={tipId}
          role="tooltip"
          className="aui-pop-in aui-glass absolute bottom-full left-0 z-40 mb-1.5 w-64 rounded-aui p-2.5 text-aui-xs"
        >
          {tip}
        </div>
      )}
    </li>
  );
}

function Meter({ ratio, pressure, tick }: { ratio: number; pressure: Pressure; tick?: number }) {
  return (
    <span aria-hidden className="relative h-1 w-12 overflow-hidden rounded-full bg-aui-subtle">
      <span
        className={`absolute inset-y-0 left-0 rounded-full transition-[width] duration-500 motion-reduce:transition-none ${pressureBar[pressure]}`}
        style={{ width: `${Math.min(100, ratio * 100)}%` }}
      />
      {tick !== undefined && (
        <span className="absolute inset-y-0 w-px bg-aui-fg/60" style={{ left: `${tick * 100}%` }} />
      )}
    </span>
  );
}

/** "freed 127k" flashes for a few seconds after a compaction, then settles. */
function useFlash(key: string | undefined, ms = 4000) {
  const [flash, setFlash] = useState(false);
  useEffect(() => {
    if (!key) return;
    setFlash(true);
    const t = setTimeout(() => setFlash(false), ms);
    return () => clearTimeout(t);
  }, [key, ms]);
  return flash;
}

/**
 * The session at a glance, under the composer: what the agent is doing, the
 * model, whether it asks before acting, how full the context is, compaction,
 * spend against the cap, and how many messages are queued. Collapses to the
 * essentials on narrow widths. Slack has no persistent bar; it gets the same
 * state as SessionNotices when it matters.
 */
export function StatusLine({
  session,
  onApprovalMode,
  onCompact,
  className,
}: {
  session: SessionState;
  onApprovalMode?: (mode: ApprovalMode) => void;
  onCompact?: () => void;
  className?: string;
}) {
  const state = orbStateOf(session.status, session.activity);
  const busy = state !== "idle" && state !== "done" && state !== "stopped" && state !== "error";
  const c = session.context ? contextPressure(session.context) : null;
  const b = session.budget ? budgetPressure(session.budget) : null;
  const compaction = session.compaction;
  const flash = useFlash(compaction?.kind === "compacted" ? compaction.at : undefined);
  const mode = session.approvalMode ?? "ask";
  const segBtn =
    "inline-flex h-5 items-center gap-1 rounded-aui-sm px-1 -mx-1 transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent";

  return (
    <ul
      aria-label="Session status"
      data-aui="statusline"
      className={`flex h-6 min-w-0 items-center overflow-visible text-aui-2xs text-aui-muted tabular-nums ${className ?? ""}`}
    >
      <Segment label={`Agent: ${orbLabel[state]}`} className={busy ? "text-aui-fg" : ""}>
        <AgentOrb state={state} size="xs" label="" />
        <span className={`font-medium ${busy ? "text-aui-accent" : ""}`}>{orbLabel[state]}</span>
      </Segment>

      {session.model && (
        <Segment label={`Model ${session.model}`} hideBelow="md">
          <span className="truncate font-aui-mono">{session.model}</span>
        </Segment>
      )}

      <Segment label="Approval mode" hideBelow="sm">
        <button
          type="button"
          className={segBtn}
          aria-pressed={mode === "auto"}
          disabled={!onApprovalMode}
          title={
            mode === "ask"
              ? "Asks before side effects. Click to act without asking."
              : "Acts without asking. Click to ask first."
          }
          onClick={() => onApprovalMode?.(mode === "ask" ? "auto" : "ask")}
        >
          {mode === "ask" ? (
            <ShieldIcon size={11} />
          ) : (
            <ZapIcon size={11} className="text-aui-warn" />
          )}
          {mode === "ask" ? "Ask before acting" : "Act without asking"}
        </button>
      </Segment>

      {c && session.context && (
        <Segment
          label={`Context ${Math.round(c.ratio * 100)}% full`}
          className={pressureText[c.pressure]}
          tip={
            <div className="flex flex-col gap-1.5">
              <p className="font-medium text-aui-fg">
                Context {formatTokens(session.context.usedTokens)} of{" "}
                {formatTokens(session.context.limitTokens)} tokens
              </p>
              <p className="text-aui-muted">
                Compacts at {Math.round((session.context.compactAt ?? 0.8) * 100)}%: older messages
                are summarized to make room.
              </p>
              {onCompact && compaction?.kind !== "compacting" && (
                <button
                  type="button"
                  onClick={onCompact}
                  className="self-start rounded-aui-sm border border-aui-border px-1.5 py-0.5 font-medium text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                >
                  Compact now
                </button>
              )}
            </div>
          }
        >
          <button type="button" className={segBtn} aria-label="Context details">
            <Meter ratio={c.ratio} pressure={c.pressure} tick={session.context.compactAt} />
            <span>{Math.round(c.ratio * 100)}%</span>
          </button>
        </Segment>
      )}

      {compaction && compaction.kind !== "idle" && (compaction.kind !== "compacted" || flash) && (
        <Segment
          label="Compaction"
          className={compaction.kind === "failed" ? "text-aui-warn" : ""}
          tip={
            compaction.kind === "failed" ? (
              <p>Couldn't compact: {compaction.reason}</p>
            ) : compaction.kind === "compacted" && compaction.summary ? (
              <p>{compaction.summary}</p>
            ) : undefined
          }
        >
          {compaction.kind === "compacting" && <span className="aui-shimmer">Compacting…</span>}
          {compaction.kind === "compacted" && (
            <span className="aui-fade-up text-aui-ok">
              freed {formatTokens(compaction.savedTokens)}
            </span>
          )}
          {compaction.kind === "failed" && (
            <span className="inline-flex items-center gap-1">
              <AlertTriangleIcon size={11} />
              Compaction failed
            </span>
          )}
        </Segment>
      )}

      {b && session.budget && (
        <Segment
          label={`Spend ${formatUsd(session.budget.spentUsd)} of ${formatUsd(session.budget.limitUsd)}`}
          className={`${pressureText[b.pressure]} ml-auto`}
          tip={
            <div className="flex flex-col gap-1">
              <p className="font-medium text-aui-fg">
                {formatUsd(session.budget.spentUsd)} of {formatUsd(session.budget.limitUsd)} this{" "}
                {session.budget.period}
              </p>
              {session.budget.nextTurnUsd !== undefined && (
                <p className="text-aui-muted">
                  Next turn ≈ {formatUsd(session.budget.nextTurnUsd)}
                </p>
              )}
              {b.blocked && (
                <p className="text-aui-bad">Blocked: the next turn would pass the cap.</p>
              )}
            </div>
          }
        >
          <button type="button" className={segBtn} aria-label="Spend details">
            <Meter ratio={b.ratio} pressure={b.pressure} />
            <span>
              {formatUsd(session.budget.spentUsd)}
              <span className="hidden sm:inline">
                {" "}
                / {formatUsd(session.budget.limitUsd)}
                <span className="opacity-70">/{session.budget.period.slice(0, 2)}</span>
              </span>
            </span>
            {b.blocked && <span className="font-medium">· blocked</span>}
          </button>
        </Segment>
      )}

      {session.queued !== undefined && session.queued > 0 && (
        <Segment label={`${session.queued} queued`} className={b ? "" : "ml-auto"}>
          <ListChecksIcon size={11} />
          <span>{session.queued} queued</span>
        </Segment>
      )}
    </ul>
  );
}
