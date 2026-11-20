import type { AgentActivity, Turn, TurnStatus } from "@humagen/core";
import { BotAvatar, type BotAvatarType } from "bot-avatars";
import { type CSSProperties, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import { type OrbState as Motion, ThinkingOrb } from "thinking-orbs";

/**
 * What the orb depicts. The activities come from the runtime; `done`,
 * `error`, and `stopped` are settled turn outcomes. `waiting` (blocked on a
 * person) is not progress, so it never animates: a spinner that keeps going
 * through an approval gate says "busy" when the system is waiting on you.
 */
export type OrbState = AgentActivity | "done" | "error" | "stopped";

export type OrbVariant = "orb" | "ring" | "dots" | "wave";
export type OrbSize = "xs" | "sm" | "md" | "lg" | number;

const PX = { xs: 12, sm: 18, md: 28, lg: 48 } as const;

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

/**
 * The motion each live activity is drawn with (thinking-orbs). Thinking
 * scrambles and clicks back, searching sweeps a globe, a tool call sends
 * particles round, delegating wires a constellation, composing rolls a sash.
 * A state that is not here (idle, waiting, the outcomes) is a still mark.
 */
const MOTION: Partial<Record<OrbState, Motion>> = {
  thinking: "solving",
  searching: "searching",
  calling: "working",
  delegating: "connecting",
  composing: "composing",
};

/** The smallest footprint the drawn orb reads at; under it the mark is the plain dot. */
const DRAWN_FROM = 12;

// The page's theme and accent are attributes on the root (apps set them); a
// color read from CSS is read again when either changes.
const subscribeTheme = (changed: () => void) => {
  if (typeof MutationObserver === "undefined") return () => {};
  const o = new MutationObserver(changed);
  o.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["data-theme", "data-accent", "class", "style"],
  });
  return () => o.disconnect();
};
const themeKey = () => {
  const d = document.documentElement.dataset;
  return `${d.theme ?? ""}/${d.accent ?? ""}`;
};

let pixel: CanvasRenderingContext2D | null | undefined;
const resolved = new Map<string, string | undefined>();
/** Any CSS color as `rgb(r, g, b)`, which is what the canvas marks take. Undefined without a canvas. */
function toRgb(css: string): string | undefined {
  if (resolved.has(css)) return resolved.get(css);
  let out: string | undefined;
  try {
    pixel ??= document.createElement("canvas").getContext("2d", { willReadFrequently: true });
    if (pixel) {
      pixel.clearRect(0, 0, 1, 1);
      pixel.fillStyle = css;
      pixel.fillRect(0, 0, 1, 1);
      const [r, g, b, a] = pixel.getImageData(0, 0, 1, 1).data;
      if (a) out = `rgb(${r}, ${g}, ${b})`;
    }
  } catch {
    pixel = null;
  }
  resolved.set(css, out);
  return out;
}

/**
 * The element's own text color, as rgb: how a mark drawn on a canvas takes
 * the accent it sits in (--aui-accent, an agent's hue). Undefined until it is
 * read, and where there is no canvas to draw on.
 */
function useInk<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const theme = useSyncExternalStore(subscribeTheme, themeKey, () => "");
  const [ink, setInk] = useState<string>();
  // biome-ignore lint/correctness/useExhaustiveDependencies: read again when the theme changes
  useLayoutEffect(() => {
    if (ref.current) setInk(toRgb(getComputedStyle(ref.current).color));
  }, [theme]);
  return { ref, ink, dark: theme.startsWith("dark") };
}

const tone = (s: OrbState) => (s === "error" ? "bad" : s === "stopped" ? "muted" : undefined);

/**
 * The agent's ambient activity mark. Accent by default (follows --aui-accent).
 * Each state has its own motion: thinking breathes, searching sweeps, calling
 * ripples out, delegating sends a satellite, composing converges, and waiting
 * holds still. Reduced motion shows one static frame per state.
 *
 * `label` defaults to the state; pass "" when visible text already says it.
 * `size` is the full footprint in pixels, including room for motion and glow.
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
  const style = {
    "--orb-size": `${typeof size === "number" ? size : PX[size]}px`,
    // The ink a drawn orb takes is read from here.
    color: "var(--aui-accent)",
  } as CSSProperties;
  const bars = variant === "dots" ? 3 : 4;
  const px = typeof size === "number" ? size : PX[size];
  const motion = variant === "orb" && px >= DRAWN_FROM ? MOTION[state] : undefined;
  const { ref, ink, dark } = useInk<HTMLSpanElement>();
  if (motion && ink)
    return (
      <span
        ref={ref}
        className={`aui-orb-drawn ${className ?? ""}`}
        data-state={state}
        style={style}
        {...(name ? { role: "img", "aria-label": name } : { "aria-hidden": true })}
      >
        <ThinkingOrb
          aria-hidden
          state={motion}
          // Three tuned drawings; the nearest is fitted to the footprint, with
          // heavier dots where it is drawn smaller than it was tuned for.
          size={px >= 48 ? 64 : px >= 26 ? 32 : 20}
          dotSize={px < 20 ? Math.min(2, 24 / px) : 1}
          dots={px < 16 ? 0.7 : 1}
          color={ink}
          theme={dark ? "dark" : "light"}
          style={{ width: px, height: px, display: "block" }}
        />
      </span>
    );
  return (
    <span
      ref={ref}
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

/**
 * Two letters for an agent: the first letters of its first two words, or,
 * for a one-word name, its first letter and A for agent ("lead" is LA).
 */
export function agentInitials(name: string): string {
  const words = name
    .replace(/([a-z])([A-Z])/g, "$1 $2")
    .split(/[\s_\-./]+/)
    .filter((w) => /[a-z0-9]/i.test(w));
  const [first = "", second] = words;
  if (!first) return "A";
  return `${first[0]}${second ? second[0] : (first[1] ?? "")}`.toUpperCase();
}

const BOTS: readonly BotAvatarType[] = [
  "clover",
  "flower",
  "triangle",
  "square",
  "blob",
  "ghost",
  "circle",
  "drop",
  "star",
  "droid",
  "mech",
  "alien",
  "hexagon",
  "cat",
  "cloud",
  "pill",
  "pebble",
  "puddle",
];

/** The shape an agent's name always draws: the same agent is the same bot everywhere. */
export function agentBot(name: string): BotAvatarType {
  let h = 2166136261;
  for (let i = 0; i < name.length; i++) h = Math.imul(h ^ name.charCodeAt(i), 16777619);
  return BOTS[(h >>> 0) % BOTS.length] ?? "circle";
}

/**
 * An agent's face: a small bot in the agent's color, its shape drawn from
 * its name, which no person's mark looks like. It hops while the agent
 * works, sits still while it waits on a person or has nothing to do, and
 * from 32px looks toward the pointer. Where there is no canvas to draw on it
 * is the agent's initials in a thin ring, with the orb on its corner.
 */
export function AgentInitials({
  name,
  state = "idle",
  size = 28,
  className,
}: {
  name: string;
  state?: OrbState;
  size?: number;
  className?: string;
}) {
  const live = !["idle", "done", "error", "stopped", "waiting"].includes(state);
  const { ref, ink, dark } = useInk<HTMLSpanElement>();
  if (ink)
    return (
      <span
        ref={ref}
        aria-hidden
        className={`aui-bot ${className ?? ""}`}
        data-live={live || undefined}
        style={{ "--initials-size": `${size}px` } as CSSProperties}
      >
        <BotAvatar
          type={agentBot(name)}
          state={live ? "working" : "default"}
          size={size}
          color={ink}
          theme={dark ? "dark" : "light"}
          shading={size < 24 ? "flat" : "fabric"}
          interactive={size >= 32}
          // Small marks hold still unless they work: a list of agents does not fidget.
          paused={!live && size < 32}
        />
        {live && size >= 24 && (
          <AgentOrb
            state={state}
            size={Math.max(12, Math.round(size * 0.42))}
            label=""
            className="absolute -right-1 -bottom-1"
          />
        )}
      </span>
    );
  return (
    <span
      ref={ref}
      aria-hidden
      className={`aui-initials ${className ?? ""}`}
      data-live={live || undefined}
      style={{ "--initials-size": `${size}px` } as CSSProperties}
    >
      {agentInitials(name)}
      {live && size >= 20 && (
        <AgentOrb
          state={state}
          size={Math.max(10, Math.round(size * 0.42))}
          label=""
          className="absolute -right-1 -bottom-1"
        />
      )}
    </span>
  );
}
