import type { AgentActivity, Turn, TurnStatus } from "@humagen/core";
import { autoInk, type BotAvatarGlasses, type BotAvatarHat, type BotAvatarType } from "bot-avatars";
import {
  type CSSProperties,
  forwardRef,
  type RefObject,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { type OrbState as Motion, ThinkingOrb } from "thinking-orbs";
import { Mascot, type MascotPersona, mascotPersonaOf } from "./Mascot.tsx";

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
  /** The mascot it is drawn as; without one, the persona its handle suggests. */
  character?: MascotPersona;
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
 * microscope while it searches or investigates, a thought bubble while it
 * thinks, a wrench while it runs a tool, a pencil while it writes, two
 * bubbles while it hands work to another agent, and an hourglass while it
 * waits on a person. A mascot never carries the orb: the bot's own motion
 * and its prop say what it is doing.
 */
export type AgentProp = "microscope" | "thought" | "wrench" | "pencil" | "bubbles" | "hourglass";

const PROPS: Partial<Record<OrbState, AgentProp>> = {
  searching: "microscope",
  thinking: "thought",
  calling: "wrench",
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

/**
 * Each prop drawn as a small object, on a 32-unit grid, with its own colors
 * and shading so it reads as the thing itself: a lab microscope, a cloud of
 * thought, a steel wrench, a yellow pencil, two chat bubbles, a wooden
 * hourglass. `id` keeps each mark's gradients its own.
 */
function PropArt({ prop, id }: { prop: AgentProp; id: string }) {
  const u = (name: string) => `url(#${id}-${name})`;
  const defs = (
    <defs>
      <linearGradient id={`${id}-dark`} x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" stopColor="#4a515c" />
        <stop offset="1" stopColor="#16191d" />
      </linearGradient>
      <linearGradient id={`${id}-body`} x1="0" y1="0" x2="1" y2="0">
        <stop offset="0" stopColor="#ffffff" />
        <stop offset="0.55" stopColor="#e4e9ef" />
        <stop offset="1" stopColor="#aab4bf" />
      </linearGradient>
      <linearGradient id={`${id}-steel`} x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" stopColor="#f4f6f8" />
        <stop offset="0.5" stopColor="#b8c1ca" />
        <stop offset="1" stopColor="#6f7a86" />
      </linearGradient>
      <radialGradient id={`${id}-knob`} cx="0.35" cy="0.35" r="0.75">
        <stop offset="0" stopColor="#ffffff" />
        <stop offset="1" stopColor="#7d8893" />
      </radialGradient>
      <radialGradient id={`${id}-cloud`} cx="0.4" cy="0.3" r="0.8">
        <stop offset="0" stopColor="#ffffff" />
        <stop offset="1" stopColor="#d5dde7" />
      </radialGradient>
      <linearGradient id={`${id}-wood`} x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" stopColor="#b9773f" />
        <stop offset="1" stopColor="#6e3f1c" />
      </linearGradient>
      <linearGradient id={`${id}-accent`} x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" stopColor="#6aa5ff" />
        <stop offset="1" stopColor="#2f6fe0" />
      </linearGradient>
      <radialGradient id={`${id}-glow`} cx="0.5" cy="0.5" r="0.5">
        <stop offset="0" stopColor="#fff3c4" />
        <stop offset="1" stopColor="#ffcf4a" stopOpacity="0" />
      </radialGradient>
      <radialGradient id={`${id}-palm`} cx="0.35" cy="0.3" r="0.85">
        <stop offset="0" stopColor="#ffffff" stopOpacity="0.4" />
        <stop offset="0.5" stopColor="#ffffff" stopOpacity="0" />
        <stop offset="1" stopColor="#000000" stopOpacity="0.35" />
      </radialGradient>
      {/* The bot's own fur on its arm and hand: a soft, broken edge. */}
      <filter id={`${id}-fur`} x="-30%" y="-30%" width="160%" height="160%">
        <feTurbulence type="fractalNoise" baseFrequency="1.6" numOctaves="2" seed="3" />
        <feDisplacementMap in="SourceGraphic" scale="1.4" />
      </filter>
    </defs>
  );
  const edge = { stroke: "#2a2f36", strokeWidth: 0.5 };
  // The bot's arm, from inside its body to its hand, in its own color.
  const hand = (from: [number, number], at: [number, number], className = "") => (
    <g
      className={`aui-prop-hand ${className}`}
      // A hand on a knob turns at the hand; a wave or a tap moves the whole arm.
      style={{
        transformOrigin:
          className === "aui-prop-twist" ? `${at[0]}px ${at[1]}px` : `${from[0]}px ${from[1]}px`,
      }}
    >
      <path
        d={`M${from[0]} ${from[1]}Q${(from[0] + at[0]) / 2} ${Math.max(from[1], at[1]) + 2} ${at[0]} ${at[1]}`}
        fill="none"
        stroke="currentColor"
        strokeWidth="4.2"
        strokeLinecap="round"
        filter={u("fur")}
      />
      <g filter={u("fur")}>
        <ellipse cx={at[0]} cy={at[1]} rx="3.9" ry="3.4" fill="currentColor" />
        <ellipse cx={at[0] - 3} cy={at[1] - 2} rx="1.6" ry="1.3" fill="currentColor" />
      </g>
      <ellipse cx={at[0]} cy={at[1]} rx="3.9" ry="3.4" fill={u("palm")} />
    </g>
  );
  switch (prop) {
    case "microscope":
      return (
        <>
          {defs}
          {/* Base, lamp, stage and the slide under the lens. */}
          <rect x="4" y="27" width="23" height="3.4" rx="1.5" fill={u("dark")} />
          <rect x="15.5" y="24.2" width="8.5" height="3.2" rx="1" fill={u("dark")} />
          <ellipse
            className="aui-prop-glow"
            cx="11.6"
            cy="21.6"
            rx="3.4"
            ry="2.6"
            fill={u("glow")}
          />
          <rect x="9.6" y="22.6" width="4" height="2.4" rx="0.7" fill={u("dark")} />
          <rect x="5" y="19.4" width="14" height="2.1" rx="0.7" fill={u("dark")} />
          <rect x="7.6" y="18.5" width="7.6" height="1" rx="0.3" fill="#bfe3ff" />
          {/* The arm, from the base up to the tube. */}
          <path
            d="M20.6 25.2C26.4 21.4 26.6 12.2 19.8 8.2L17.9 10.9C22.6 13.6 22.6 20 18.2 23.4Z"
            fill={u("body")}
            {...edge}
          />
          {/* The tube moves as it focuses: eyepiece, body, nosepiece, objective. */}
          <g className="aui-prop-move">
            <g transform="rotate(25 15 13)">
              <rect x="13.2" y="0.6" width="3.6" height="1.2" rx="0.4" fill={u("knob")} />
              <rect x="13.6" y="1.6" width="2.8" height="2.8" rx="0.5" fill={u("dark")} />
              <rect x="13" y="4.2" width="4" height="9.8" rx="1" fill={u("body")} {...edge} />
              <rect x="13" y="8.8" width="4" height="1.1" fill="#2a2f36" />
              <rect
                x="12.4"
                y="13.8"
                width="5.2"
                height="1.7"
                rx="0.6"
                fill={u("knob")}
                {...edge}
              />
              <rect x="13.9" y="15.4" width="2.2" height="2.7" rx="0.4" fill={u("dark")} />
              <rect x="13.9" y="16.3" width="2.2" height="0.6" fill="#d9a441" />
            </g>
          </g>
          {/* The focus knob turns, under the bot's hand. */}
          <g className="aui-prop-knob">
            <circle cx="21.6" cy="17.6" r="2.5" fill={u("knob")} {...edge} />
            <path d="M21.6 15.6v1.3" stroke="#2a2f36" strokeWidth="0.8" strokeLinecap="round" />
          </g>
          {hand([30, 13], [22.4, 18.2], "aui-prop-twist")}
        </>
      );
    case "thought":
      return (
        <>
          {defs}
          <circle className="aui-prop-trail" cx="3.4" cy="29" r="1.5" fill={u("cloud")} {...edge} />
          <circle
            className="aui-prop-trail aui-prop-late"
            cx="7.2"
            cy="24.6"
            r="2.4"
            fill={u("cloud")}
            {...edge}
          />
          <path
            d="M10.4 21.4C6.6 21.4 5.2 16.6 8.6 14.8 7.6 10.4 12.6 8 15.4 10.4 17 5.8 24 6.4 24.4 11 28.6 10.8 30.4 15.6 27.4 18 28.4 21 25.6 22.4 23.4 21.4Z"
            fill={u("cloud")}
            {...edge}
          />
          <circle className="aui-prop-dot" cx="13.2" cy="16.2" r="1.35" fill="#56606c" />
          <circle className="aui-prop-dot" cx="17.8" cy="16.2" r="1.35" fill="#56606c" />
          <circle className="aui-prop-dot" cx="22.4" cy="16.2" r="1.35" fill="#56606c" />
          {hand([0, 41], [7, 37], "aui-prop-tap")}
        </>
      );
    case "wrench":
      return (
        <>
          {defs}
          <g className="aui-prop-move">
            <path
              d="M20.8 4.4a6.8 6.8 0 0 0-7.6 9.4l-8.6 8.6a3 3 0 0 0 4.2 4.2l8.6-8.6a6.8 6.8 0 0 0 9.4-7.6l-4.2 4.2-4.4-1-1-4.4z"
              fill={u("steel")}
              stroke="#4b545e"
              strokeWidth="0.7"
              strokeLinejoin="round"
            />
            <circle cx="6.7" cy="24.5" r="1" fill="#4b545e" />
            {hand([2, 13], [9.4, 22.6])}
          </g>
        </>
      );
    case "pencil":
      return (
        <>
          {defs}
          <path
            className="aui-prop-ink"
            d="M6.4 28c.9-1.2 1.8 1 2.7-.1s1.8 1 2.7 0"
            fill="none"
            stroke="#3b3f46"
            strokeWidth="0.9"
            strokeLinecap="round"
            pathLength={1}
          />
          <g className="aui-prop-move">
            <g transform="rotate(40 16 16)">
              <rect x="13" y="1.6" width="6" height="4.4" rx="1.6" fill="#ef8a9b" />
              <rect x="13" y="5.6" width="6" height="3.4" fill={u("steel")} />
              <path d="M13 6.8h6M13 7.9h6" stroke="#7d8893" strokeWidth="0.4" />
              <rect x="13" y="9" width="6" height="14" fill="#f2b630" />
              <rect x="15" y="9" width="2" height="14" fill="#ffd45e" />
              <path d="M13 23h6l-3 6.6z" fill="#f0cf9e" />
              <path d="M15.1 27.6h1.8L16 29.6z" fill="#3b3b3b" />
            </g>
            {hand([2, 12], [12.6, 19.6])}
          </g>
        </>
      );
    case "bubbles":
      return (
        <>
          {defs}
          <path
            className="aui-prop-move"
            d="M4.6 3h12a3 3 0 0 1 3 3v5a3 3 0 0 1-3 3H10l-4 3.2V14H4.6a3 3 0 0 1-3-3V6a3 3 0 0 1 3-3z"
            fill={u("cloud")}
            {...edge}
          />
          <path
            d="M15 14.4h12a3 3 0 0 1 3 3v5a3 3 0 0 1-3 3h-1.4v3.2l-4-3.2H15a3 3 0 0 1-3-3v-5a3 3 0 0 1 3-3z"
            fill={u("accent")}
            stroke="#1f4fae"
            strokeWidth="0.5"
          />
          <circle className="aui-prop-dot" cx="17.4" cy="19.9" r="1.2" fill="#ffffff" />
          <circle className="aui-prop-dot" cx="21" cy="19.9" r="1.2" fill="#ffffff" />
          <circle className="aui-prop-dot" cx="24.6" cy="19.9" r="1.2" fill="#ffffff" />
          {hand([2, 44], [9, 32], "aui-prop-wave")}
        </>
      );
    case "hourglass":
      return (
        <>
          {defs}
          <path
            d="M10 5c0 7 5 8.5 5 11s-5 4-5 11h12c0-7-5-8.5-5-11s5-4 5-11z"
            fill="#dff1ff"
            fillOpacity="0.6"
            stroke="#8fb8d8"
            strokeWidth="0.6"
          />
          <path d="M11.6 9c.9 3 3.4 4.4 3.8 6h1.2c.4-1.6 2.9-3 3.8-6z" fill="#e6b25a" />
          <rect x="15.7" y="15" width="0.6" height="7.6" fill="#e6b25a" />
          <path d="M11 27c1-3.5 3.5-4.5 5-4.5s4 1 5 4.5z" fill="#e6b25a" />
          <rect x="7" y="5" width="1.6" height="22" fill={u("wood")} />
          <rect x="23.4" y="5" width="1.6" height="22" fill={u("wood")} />
          <rect x="5.6" y="2" width="20.8" height="3.2" rx="1" fill={u("wood")} />
          <rect x="5.6" y="26.8" width="20.8" height="3.2" rx="1" fill={u("wood")} />
          {hand([2, 14], [8.6, 3])}
        </>
      );
  }
}

/**
 * A prop at a mark's right, where it never covers the face: a thought or a
 * message floats above the head, a thing it uses stands beside it, the
 * microscope turned so its eyepiece leans toward the bot.
 * It fades in and out and loops slowly, except the hourglass: anything
 * waiting on a person holds still. Reduced motion stops every loop.
 */
function PropMark({ prop, leaving, size }: { prop: AgentProp; leaving: boolean; size: number }) {
  const id = useId().replace(/:/g, "");
  return (
    <span
      aria-hidden
      className="aui-prop"
      data-prop={prop}
      data-motion={prop === "hourglass" ? "still" : "loop"}
      data-anchor={prop === "thought" || prop === "bubbles" ? "above" : "beside"}
      data-leaving={leaving || undefined}
      style={{ "--prop-size": `${Math.round(size * 0.6)}px` } as CSSProperties}
    >
      <svg aria-hidden="true" viewBox="0 0 32 32">
        <PropArt prop={prop} id={`p${id}`} />
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

/** How long a small mark keeps moving after its motion stops, in milliseconds. */
export const SETTLE_MS = 1_400;

/**
 * True while `moving`, and for `SETTLE_MS` after it stops: a small mark that
 * loses the pointer, or whose agent stops working, finishes its move (lands
 * its hop and eases back to rest) instead of freezing mid-move. Moving again
 * within that time just keeps it moving. Under reduced motion it stops at
 * once, and the timer is cleared when the mark goes.
 */
export function useSettle(moving: boolean, timers: GlanceTimers = realTimers): boolean {
  const reduced = useReducedMotion();
  const [was, setWas] = useState(moving);
  const [settling, setSettling] = useState(false);
  // Set during render, so the frame the motion stops is never drawn paused.
  if (was !== moving) {
    setWas(moving);
    setSettling(!moving && !reduced);
  }
  useEffect(() => {
    if (!settling) return;
    const id = timers.set(() => setSettling(false), SETTLE_MS);
    return () => timers.clear(id);
  }, [settling, timers]);
  return moving || settling;
}

/** The longest a bot keeps moving to finish a blink, in milliseconds. */
export const FINISH_MS = 1_200;

/**
 * Whether the latest of these counts of face-ink pixels, one per frame, shows
 * the eyes open: near the most seen over the last 0.6 s and not closing. A
 * blink shrinks the eyes to a line for about 0.17 s, so the count drops well
 * below that most; a glance or a small turn moves it far less.
 */
export function eyesOpen(counts: readonly number[]): boolean {
  if (counts.length < 6) return false;
  const recent = counts.slice(-36);
  const most = Math.max(...recent);
  const now = counts[counts.length - 1] ?? 0;
  const before = counts[counts.length - 2] ?? 0;
  return most > 0 && now >= most * 0.8 && now >= before * 0.95;
}

/** The pixels of the face's ink on the bot's canvas. */
function inkPixels(canvas: HTMLCanvasElement, ink: [number, number, number]): number {
  const g = canvas.getContext("2d");
  if (!g || !canvas.width || !canvas.height) return 0;
  const d = g.getImageData(0, 0, canvas.width, canvas.height).data;
  let n = 0;
  for (let i = 0; i < d.length; i += 4) {
    if ((d[i + 3] ?? 0) < 200) continue;
    const r = (d[i] ?? 0) - ink[0];
    const gr = (d[i + 1] ?? 0) - ink[1];
    const b = (d[i + 2] ?? 0) - ink[2];
    if (r * r + gr * gr + b * b < 4900) n++;
  }
  return n;
}

const hexRgb = (hex: string): [number, number, number] => {
  const v = Number.parseInt(hex.replace("#", ""), 16);
  return [(v >> 16) & 255, (v >> 8) & 255, v & 255];
};

/**
 * True for a moment after `moving` stops: the bot keeps going until its eyes
 * are open, read from its canvas each frame, so it never holds still with
 * its eyes part closed. It gives up after `FINISH_MS`, and at once where
 * there is no canvas to read or under reduced motion.
 */
function useFinish(ref: RefObject<HTMLElement | null>, moving: boolean, color?: string): boolean {
  const reduced = useReducedMotion();
  const [was, setWas] = useState(moving);
  const [finishing, setFinishing] = useState(false);
  if (was !== moving) {
    setWas(moving);
    setFinishing(!moving && !reduced && !!color);
  }
  useEffect(() => {
    if (!finishing || !color) return;
    const canvas = ref.current?.querySelector("canvas");
    if (!canvas || typeof requestAnimationFrame === "undefined") {
      setFinishing(false);
      return;
    }
    const ink = hexRgb(autoInk(color));
    const counts: number[] = [];
    let frame = 0;
    const look = () => {
      try {
        counts.push(inkPixels(canvas, ink));
      } catch {
        setFinishing(false);
        return;
      }
      if (eyesOpen(counts)) setFinishing(false);
      else frame = requestAnimationFrame(look);
    };
    frame = requestAnimationFrame(look);
    const cap = setTimeout(() => setFinishing(false), FINISH_MS);
    return () => {
      cancelAnimationFrame(frame);
      clearTimeout(cap);
    };
  }, [finishing, color, ref]);
  return finishing;
}

/**
 * An agent's face: a small bot in the agent's color, its shape drawn from
 * its name, which no person's mark looks like. It hops while the agent
 * works, sits still while it waits on a person, and from 32px looks toward
 * the pointer. A smaller idle mark holds still but blinks or glances now and
 * then (`useIdleGlance`), and when the pointer leaves or the work stops it
 * finishes its move, on open eyes, before it holds still (`useSettle`).
 * From 24px it holds a prop at its top right that says what it is doing
 * (`AgentProp`); it never carries the orb. Where there is no canvas to draw
 * on it is the agent's initials in a thin ring, with the same prop.
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
  const { ref, ink } = useInk<HTMLSpanElement>();
  const kept = useBotLook(lookOf ?? name);
  const look = given ?? kept;
  // Under the pointer a mark comes alive: it moves and its eyes follow, whatever its size.
  const [hovered, setHovered] = useState(false);
  // A small idle mark holds still, but now and then makes a small move of
  // its own. A mark waiting on a person never moves.
  const moving = live || hovered;
  // When the pointer leaves or the work stops, the move in progress finishes.
  const stirring = useSettle(moving);
  const glancing = useIdleGlance(
    ref,
    !!ink && !stirring && state !== "waiting" && size < 32,
    lookOf ?? name,
  );
  // And it stops on open eyes, never mid-blink.
  const finishing = useFinish(ref, stirring || glancing, size < 32 ? ink : undefined);
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
        data-settling={(stirring && !moving) || finishing || undefined}
        onPointerEnter={(e) => {
          if (e.pointerType === "mouse") setHovered(true);
        }}
        onPointerLeave={() => setHovered(false)}
        style={{ "--initials-size": `${size}px` } as CSSProperties}
      >
        {/* Humagen's own mascot: a persona with arms that use its tools. */}
        <Mascot
          persona={look?.character ?? mascotPersonaOf(lookOf ?? name)}
          state={state}
          size={size}
          color={ink}
          fit
          quiet={!live && !hovered}
        />
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
    </span>
  );
}

/**
 * An agent's mascot held still, for a picture of it: its character, in the
 * color given, with no motion. The console reads it with `mascotPicture`
 * into a PNG when an appearance is saved, so a surface that shows images,
 * such as Slack, shows the same character.
 */
export const AgentStill = forwardRef<
  HTMLSpanElement,
  { name: string; look?: BotLook; color: string; size?: number }
>(function AgentStill({ name, look, color, size = 256 }, ref) {
  return (
    <span ref={ref} className="inline-flex">
      <Mascot
        persona={look?.character ?? mascotPersonaOf(name)}
        state="idle"
        size={size}
        color={color}
        quiet
        still
      />
    </span>
  );
});
