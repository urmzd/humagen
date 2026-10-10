import type { AgentActivity, Turn, TurnStatus } from "@vultus/core";
import type { CSSProperties } from "react";

/**
 * What the orb depicts. The activities come from the runtime; `done`,
 * `error`, and `stopped` are settled turn outcomes. `waiting` (blocked on a
 * person) is not progress, so it never animates: a spinner that keeps going
 * through an approval gate says "busy" when the system is waiting on you.
 */
export type OrbState = AgentActivity | "done" | "error" | "stopped";

export type OrbVariant = "orb" | "ring" | "dots" | "wave";
export type OrbSize = "xs" | "sm" | "md" | "lg";

const PX: Record<OrbSize, number> = { xs: 12, sm: 18, md: 28, lg: 48 };

export const orbLabel: Record<OrbState, string> = {
  idle: "Idle",
  thinking: "Thinking",
  searching: "Searching",
  calling: "Running a tool",
  delegating: "Delegating to another agent",
  composing: "Writing the answer",
  waiting: "Waiting for you",
  done: "Done",
  error: "Failed",
  stopped: "Stopped",
};

/** Orb state from a session or turn: the outcome once settled, else the live activity. */
export function orbStateOf(
  status: TurnStatus | "idle" | undefined,
  activity?: AgentActivity,
): OrbState {
  switch (status) {
    case "done":
      return "done";
    case "failed":
      return "error";
    case "stopped":
      return "stopped";
    case "waiting":
      return "waiting";
    case "queued":
      return activity ?? "thinking";
    case "working":
      return activity && activity !== "idle" ? activity : "thinking";
    default:
      return activity ?? "idle";
  }
}

/** Activity inferred from a turn when the runtime didn't say: running task, streaming text, or thinking. */
export function turnOrbState(turn: Turn): OrbState {
  if (turn.status !== "working" && turn.status !== "queued") return orbStateOf(turn.status);
  if (turn.activity) return orbStateOf(turn.status, turn.activity);
  const running = turn.tasks.find((t) => t.status === "in_progress");
  if (running?.subagent) return "delegating";
  if (running) return /search|look|find|read/i.test(running.title) ? "searching" : "calling";
  if (turn.text) return "composing";
  return "thinking";
}

const tone = (s: OrbState) => (s === "error" ? "bad" : s === "stopped" ? "muted" : undefined);

/**
 * The agent's ambient activity mark. Accent by default (follows --aui-accent).
 * Each state has its own motion: thinking breathes, searching sweeps, calling
 * ripples out, delegating sends a satellite, composing converges, and waiting
 * holds still. Reduced motion shows one static frame per state.
 *
 * `label` defaults to the state; pass "" when visible text already says it.
 */
export function AgentOrb({
  state,
  variant = "orb",
  size = "md",
  label,
  className,
}: {
  state: OrbState;
  variant?: OrbVariant;
  size?: OrbSize;
  label?: string;
  className?: string;
}) {
  const name = label ?? orbLabel[state];
  const style = { "--s": `${PX[size]}px` } as CSSProperties;
  const bars = variant === "dots" ? 3 : 4;
  return (
    <span
      className={`aui-orb ${className ?? ""}`}
      data-state={state}
      data-variant={variant}
      data-tone={tone(state)}
      style={style}
      {...(name ? { role: "img", "aria-label": name } : { "aria-hidden": true })}
    >
      {variant === "orb" && (
        <>
          <span className="aui-orb-halo" />
          <span className="aui-orb-sphere" />
          <span className="aui-orb-sat" />
        </>
      )}
      {variant === "ring" && <span className="aui-orb-ring" />}
      {(variant === "dots" || variant === "wave") && (
        <span className={variant === "dots" ? "aui-orb-dots" : "aui-orb-wave"}>
          {Array.from({ length: bars }, (_, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: fixed decorative marks
            <i key={i} style={{ "--i": i } as CSSProperties} />
          ))}
        </span>
      )}
    </span>
  );
}
