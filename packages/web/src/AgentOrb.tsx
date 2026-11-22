import type { AgentActivity, Turn, TurnStatus } from "@humagen/core";
import {
  BotAvatar,
  type BotAvatarGlasses,
  type BotAvatarHat,
  type BotAvatarType,
} from "bot-avatars";
import {
  type CSSProperties,
  forwardRef,
  type ReactNode,
  type RefObject,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
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
export function toRgb(css: string): string | undefined {
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

/** Every shape an agent may be drawn as, in the order a picker shows them. */
export const botShapes: readonly BotAvatarType[] = BOTS;

/**
 * How an agent chose to look: its bot's shape, its face, its finish and what
 * it wears, and the name its initials are taken from. Every field is
 * optional; one left out is the default, so an agent nobody styled is drawn
 * as it always was.
 */
export interface BotLook {
  shape?: BotAvatarType;
  face?: "eyes" | "mouth";
  finish?: "fabric" | "plastic" | "smooth" | "flat";
  hat?: BotAvatarHat;
  glasses?: BotAvatarGlasses;
  headphones?: boolean;
  bowTie?: boolean;
  /** The name people see, which initials are taken from. */
  label?: string;
}

/**
 * Where marks read each agent's look. The surface host configures it once
 * (the console from the looks the API serves), so a mark drawn anywhere, in a
 * shared component or a page, shows the same bot. Unconfigured, every agent
 * has the default look.
 */
let looks: {
  get: (name: string) => BotLook | undefined;
  subscribe: (changed: () => void) => () => void;
} = {
  get: () => undefined,
  subscribe: () => () => {},
};
const lookListeners = new Set<() => void>();

export function configureBotLooks(next: typeof looks | undefined): void {
  looks = next ?? { get: () => undefined, subscribe: () => () => {} };
  for (const l of lookListeners) l();
}

function useBotLook(name: string): BotLook | undefined {
  return useSyncExternalStore(
    (changed) => {
      lookListeners.add(changed);
      const off = looks.subscribe(changed);
      return () => {
        lookListeners.delete(changed);
        off();
      };
    },
    () => looks.get(name),
    () => looks.get(name),
  );
}

/** The shape an agent's name always draws: the same agent is the same bot everywhere. */
export function agentBot(name: string): BotAvatarType {
  let h = 2166136261;
  for (let i = 0; i < name.length; i++) h = Math.imul(h ^ name.charCodeAt(i), 16777619);
  return BOTS[(h >>> 0) % BOTS.length] ?? "circle";
}

/**
 * The small thing an agent holds while it works, drawn beside its bot: a
 * magnifier while it searches, a thought bubble while it thinks, a pencil
 * while it writes, two bubbles while it hands work to another agent, and an
 * hourglass while it waits on a person. A state without one (a tool call)
 * keeps the orb on the bot's corner.
 */
export type AgentProp = "magnifier" | "thought" | "pencil" | "bubbles" | "hourglass";

const PROPS: Partial<Record<OrbState, AgentProp>> = {
  searching: "magnifier",
  thinking: "thought",
  composing: "pencil",
  delegating: "bubbles",
  waiting: "hourglass",
};

/** The smallest mark a prop is drawn beside; a smaller one keeps just the bot. */
export const PROP_FROM = 24;
/** How long a prop takes to fade in or out, in milliseconds. */
export const PROP_FADE_MS = 150;

/** The prop a mark of this size holds in this state, if any. */
export function agentPropOf(state: OrbState, size: number): AgentProp | undefined {
  return size >= PROP_FROM ? PROPS[state] : undefined;
}

/** A prop that has just gone away stays for its fade out, then leaves. */
function useShownProp(prop: AgentProp | undefined) {
  const [held, setHeld] = useState(prop);
  useEffect(() => {
    if (prop) {
      setHeld(prop);
      return;
    }
    const t = setTimeout(() => setHeld(undefined), PROP_FADE_MS);
    return () => clearTimeout(t);
  }, [prop]);
  const shown = prop ?? held;
  return { shown, leaving: !prop && !!shown };
}

// Each prop on a 16-unit grid. `.aui-prop-move` is the part that loops.
const PROP_ART: Record<AgentProp, ReactNode> = {
  magnifier: (
    <g className="aui-prop-move">
      <circle cx="6.5" cy="6.5" r="4.25" />
      <path d="M9.6 9.6 14 14" />
    </g>
  ),
  thought: (
    <>
      <circle
        className="aui-prop-move"
        cx="2.6"
        cy="13.6"
        r="1.15"
        fill="currentColor"
        stroke="none"
      />
      <circle
        className="aui-prop-move aui-prop-late"
        cx="5.4"
        cy="10.4"
        r="1.6"
        fill="currentColor"
        stroke="none"
      />
      <path d="M9.4 8.4a2.3 2.3 0 0 1-.3-4.4 2.9 2.9 0 0 1 5.3-.4 2.2 2.2 0 0 1-.6 4.8z" />
    </>
  ),
  pencil: (
    <g className="aui-prop-move">
      <path d="M11 2.8 13.2 5 5.6 12.6 2.4 13.6 3.4 10.4z" />
      <path d="M9.6 4.2 11.8 6.4" />
    </g>
  ),
  bubbles: (
    <>
      <path
        className="aui-prop-move"
        d="M2.5 2h6a1.5 1.5 0 0 1 1.5 1.5v2.5A1.5 1.5 0 0 1 8.5 7.5H5l-2 1.6V7.5h-.5A1.5 1.5 0 0 1 1 6V3.5A1.5 1.5 0 0 1 2.5 2z"
      />
      <path
        className="aui-prop-front"
        d="M7.5 7.5h6A1.5 1.5 0 0 1 15 9v2.5a1.5 1.5 0 0 1-1.5 1.5H13v1.6L11 13H7.5A1.5 1.5 0 0 1 6 11.5V9a1.5 1.5 0 0 1 1.5-1.5z"
      />
    </>
  ),
  hourglass: (
    <>
      <path d="M3.5 1.8h9M3.5 14.2h9" />
      <path d="M4.8 1.8c0 3.6 6.4 4 6.4 6.2s-6.4 2.6-6.4 6.2M11.2 1.8c0 3.6-6.4 4-6.4 6.2s6.4 2.6 6.4 6.2" />
    </>
  ),
};

/**
 * A prop beside a mark, at the top right where it never covers the face.
 * It fades in and out and loops slowly, except the hourglass: anything
 * waiting on a person holds still. Reduced motion stops every loop.
 */
function PropMark({ prop, leaving, size }: { prop: AgentProp; leaving: boolean; size: number }) {
  return (
    <span
      aria-hidden
      className="aui-prop text-aui-muted"
      data-prop={prop}
      data-motion={prop === "hourglass" ? "still" : "loop"}
      data-leaving={leaving || undefined}
      style={{ "--prop-size": `${Math.round(size * 0.4)}px` } as CSSProperties}
    >
      <svg
        aria-hidden="true"
        viewBox="0 0 16 16"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.7"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        {PROP_ART[prop]}
      </svg>
    </span>
  );
}

/** Whether the person asked for less motion, kept current as they change it. */
function useReducedMotion(): boolean {
  return useSyncExternalStore(
    (changed) => {
      const media = typeof window === "undefined" ? undefined : window.matchMedia?.(REDUCE);
      media?.addEventListener?.("change", changed);
      return () => media?.removeEventListener?.("change", changed);
    },
    () => !!window.matchMedia?.(REDUCE)?.matches,
    () => false,
  );
}
const REDUCE = "(prefers-reduced-motion: reduce)";

/** Whether the element is on screen, watched only while `watch` holds; without an observer it counts as seen. */
function useOnScreen(ref: RefObject<Element | null>, watch: boolean): boolean {
  const [seen, setSeen] = useState(true);
  useEffect(() => {
    const el = ref.current;
    if (!watch || !el || typeof IntersectionObserver === "undefined") return;
    const o = new IntersectionObserver((entries) => {
      const last = entries[entries.length - 1];
      if (last) setSeen(last.isIntersecting);
    });
    o.observe(el);
    return () => o.disconnect();
  }, [ref, watch]);
  return seen;
}

/** The timers an idle glance is scheduled with; tests pass their own. */
export interface GlanceTimers {
  set(fn: () => void, ms: number): unknown;
  clear(id: unknown): void;
  /** 0 to 1. */
  random(): number;
}
const realTimers: GlanceTimers = {
  set: (fn, ms) => setTimeout(fn, ms),
  clear: (id) => clearTimeout(id as ReturnType<typeof setTimeout>),
  random: Math.random,
};

/** How often an idle mark moves and for how long, in milliseconds. */
export const GLANCE = { minGap: 12_000, maxGap: 25_000, minFor: 1_200, maxFor: 2_000 } as const;

/** 0 to 1 from an agent's name: where in the gap its first move falls, so a list does not move in unison. */
export function glanceOffset(name: string): number {
  let h = 2166136261;
  for (let i = 0; i < name.length; i++) h = Math.imul(h ^ name.charCodeAt(i), 16777619);
  return (Math.imul(h ^ (h >>> 15), 2246822507) >>> 0) / 2 ** 32;
}

/**
 * An idle mark's rare small move: every 12 to 25 seconds (the first wait set
 * by the agent's name) it is true for 1.2 to 2 seconds, long enough for a
 * blink or a glance, then false again. Nothing is scheduled while `on` is
 * false, while the mark is off screen, or when the person asked for reduced
 * motion, and the timer is cleared when the mark goes.
 */
export function useIdleGlance(
  ref: RefObject<Element | null>,
  on: boolean,
  name: string,
  timers: GlanceTimers = realTimers,
): boolean {
  const reduced = useReducedMotion();
  const seen = useOnScreen(ref, on && !reduced);
  const active = on && !reduced && seen;
  const [glancing, setGlancing] = useState(false);
  useEffect(() => {
    if (!active) return;
    let id: unknown;
    let first = true;
    const wait = () => {
      const r = first ? glanceOffset(name) : timers.random();
      first = false;
      id = timers.set(move, GLANCE.minGap + r * (GLANCE.maxGap - GLANCE.minGap));
    };
    const move = () => {
      setGlancing(true);
      id = timers.set(
        () => {
          setGlancing(false);
          wait();
        },
        GLANCE.minFor + timers.random() * (GLANCE.maxFor - GLANCE.minFor),
      );
    };
    wait();
    return () => {
      timers.clear(id);
      setGlancing(false);
    };
  }, [active, name, timers]);
  return active && glancing;
}

/**
 * An agent's face: a small bot in the agent's color, its shape drawn from
 * its name, which no person's mark looks like. It hops while the agent
 * works, sits still while it waits on a person, and from 32px looks toward
 * the pointer. A smaller idle mark holds still but blinks or glances now and
 * then (`useIdleGlance`). From 24px it holds a prop at its top right that
 * says what it is doing (`AgentProp`), or the orb on its corner for a state
 * without one. Where there is no canvas to draw on it is the agent's
 * initials in a thin ring, with the same prop or orb.
 */
export function AgentInitials({
  name,
  state = "idle",
  size = 28,
  className,
  look: given,
  lookOf,
}: {
  /** The agent's handle, or the name its default shape is drawn from. */
  name: string;
  state?: OrbState;
  size?: number;
  className?: string;
  /** A look to draw instead of the agent's own, as a picker previews one. */
  look?: BotLook;
  /** The handle its look is kept under, when `name` is a name shown instead. */
  lookOf?: string;
}) {
  const live = !["idle", "done", "error", "stopped", "waiting"].includes(state);
  const { ref, ink, dark } = useInk<HTMLSpanElement>();
  const kept = useBotLook(lookOf ?? name);
  const look = given ?? kept;
  // Under the pointer a mark comes alive: it moves and its eyes follow, whatever its size.
  const [hovered, setHovered] = useState(false);
  // A small idle mark holds still, but now and then makes a small move of
  // its own. A mark waiting on a person never moves.
  const glancing = useIdleGlance(
    ref,
    !!ink && !live && !hovered && state !== "waiting" && size < 32,
    lookOf ?? name,
  );
  const moving = live || hovered;
  const { shown: prop, leaving } = useShownProp(agentPropOf(state, size));
  if (ink)
    return (
      <span
        ref={ref}
        aria-hidden
        className={`aui-bot ${className ?? ""}`}
        data-live={live || undefined}
        data-hovered={hovered || undefined}
        data-glancing={glancing || undefined}
        onPointerEnter={(e) => {
          if (e.pointerType === "mouse") setHovered(true);
        }}
        onPointerLeave={() => setHovered(false)}
        style={{ "--initials-size": `${size}px` } as CSSProperties}
      >
        <BotAvatar
          type={look?.shape ?? agentBot(name)}
          face={look?.face}
          hat={look?.hat ?? "none"}
          glasses={look?.glasses ?? "none"}
          headphones={look?.headphones ?? false}
          bowTie={look?.bowTie ?? false}
          state={moving ? "working" : "default"}
          size={size}
          color={ink}
          theme={dark ? "dark" : "light"}
          shading={size < 24 ? "flat" : (look?.finish ?? "fabric")}
          interactive={size >= 32 || hovered}
          // Small marks hold still unless they work, are under the pointer or
          // take their rare glance: a list of agents does not fidget. Their
          // glance has no hop and only half the head turn.
          paused={!moving && !glancing && size < 32}
          jumpEvery={size < 32 ? 0 : undefined}
          turn={size < 32 ? 0.5 : undefined}
        />
        {prop && <PropMark key={prop} prop={prop} leaving={leaving} size={size} />}
        {live && !prop && size >= 24 && (
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
      {agentInitials(look?.label || name)}
      {prop && <PropMark key={prop} prop={prop} leaving={leaving} size={size} />}
      {live && !prop && size >= 20 && (
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

/**
 * An agent's bot held still, for a picture of it: the look given, in the
 * color given, with no motion and no pointer play, on a canvas the caller
 * reads (the ref). The console draws one to a PNG when a look is saved, so a
 * surface that shows images, such as Slack, shows the same bot.
 */
export const AgentStill = forwardRef<
  HTMLCanvasElement,
  { name: string; look?: BotLook; color: string; size?: number }
>(function AgentStill({ name, look, color, size = 256 }, ref) {
  return (
    <BotAvatar
      ref={ref}
      type={look?.shape ?? agentBot(name)}
      face={look?.face}
      hat={look?.hat ?? "none"}
      glasses={look?.glasses ?? "none"}
      headphones={look?.headphones ?? false}
      bowTie={look?.bowTie ?? false}
      state="default"
      size={size}
      color={color}
      theme="light"
      shading={look?.finish ?? "fabric"}
      interactive={false}
      paused
      seed={0.5}
    />
  );
});
