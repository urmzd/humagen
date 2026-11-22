import { type CSSProperties, type ReactNode, useId } from "react";
import type { OrbState } from "./AgentOrb.tsx";

/**
 * Humagen's own mascots: one rig (a furry body, eyes, two arms with elbows
 * and hands, feet) dressed as a persona, and posed by what the agent is
 * doing. The hands hold and use the props, so the work reads as the bot's
 * own: it turns a microscope's focus knob, writes on a notepad, swings a
 * wrench, taps its chin, conducts. Drawn in SVG on a 130 by 120 grid.
 */
export type MascotPersona =
  | "detective"
  | "writer"
  | "engineer"
  | "scientist"
  | "inspector"
  | "conductor"
  | "plain";

export const mascotPersonas: MascotPersona[] = [
  "detective",
  "writer",
  "engineer",
  "scientist",
  "inspector",
  "conductor",
  "plain",
];

type Pose = "idle" | "searching" | "thinking" | "calling" | "composing" | "delegating" | "waiting";

/** The pose for what the agent is doing. */
export function mascotPose(state: OrbState): Pose {
  switch (state) {
    case "searching":
    case "thinking":
    case "calling":
    case "composing":
    case "delegating":
    case "waiting":
      return state;
    default:
      return "idle";
  }
}

/** A persona from an agent's handle, for agents with no look of their own. */
export function mascotPersonaOf(handle: string): MascotPersona {
  const h = handle.toLowerCase();
  if (h === "humagen" || h === "lead" || h.includes("orchestr")) return "conductor";
  if (h.includes("research") || h.includes("investig") || h.includes("detect")) return "detective";
  if (h.includes("writ") || h.includes("editor") || h.includes("copy")) return "writer";
  if (h.includes("engineer") || h.includes("dev") || h.includes("build") || h.includes("ops"))
    return "engineer";
  if (h.includes("analy") || h.includes("data") || h.includes("scien")) return "scientist";
  if (h.includes("qa") || h.includes("review") || h.includes("audit") || h.includes("test"))
    return "inspector";
  return "plain";
}

type Shape = { kind: "oval"; rx: number; ry: number } | { kind: "box"; w: number; h: number };

const SHAPES: Record<MascotPersona, Shape> = {
  detective: { kind: "oval", rx: 27, ry: 33 },
  writer: { kind: "oval", rx: 30, ry: 30 },
  engineer: { kind: "box", w: 58, h: 58 },
  scientist: { kind: "oval", rx: 30, ry: 31 },
  inspector: { kind: "oval", rx: 29, ry: 30 },
  conductor: { kind: "oval", rx: 30, ry: 32 },
  plain: { kind: "oval", rx: 30, ry: 31 },
};

type Eyes = "dot" | "keen" | "happy" | "wide";
const EYES: Record<MascotPersona, Eyes> = {
  detective: "keen",
  writer: "dot",
  engineer: "dot",
  scientist: "wide",
  inspector: "keen",
  conductor: "happy",
  plain: "dot",
};

const CX = 52;
const BASE = 100; // where the body meets its feet
const UPPER = 14;
const FORE = 13;

/** Where each arm points, in degrees from the shoulder (0 is right, 90 is down). */
const ARMS: Record<Pose, { left: [number, number]; right: [number, number] }> = {
  idle: { left: [112, -14], right: [68, 14] },
  searching: { left: [104, -10], right: [20, 40] },
  thinking: { left: [108, -12], right: [120, -230] },
  calling: { left: [108, -12], right: [30, -20] },
  composing: { left: [60, -60], right: [120, 80] },
  delegating: { left: [110, -14], right: [-40, -30] },
  waiting: { left: [70, -50], right: [110, 50] },
};

export function Mascot({
  persona = "plain",
  state = "idle",
  size = 96,
  color,
  className,
}: {
  persona?: MascotPersona;
  state?: OrbState;
  size?: number;
  /** The bot's fur color; it is drawn in the current text color when not given. */
  color?: string;
  className?: string;
}) {
  const id = `m${useId().replace(/:/g, "")}`;
  const u = (n: string) => `url(#${id}-${n})`;
  const pose = mascotPose(state);
  const shape = SHAPES[persona];
  const top = shape.kind === "oval" ? BASE - shape.ry * 2 + 2 : BASE - shape.h;
  const cy = shape.kind === "oval" ? BASE - shape.ry + 2 : BASE - shape.h / 2;
  const half = shape.kind === "oval" ? shape.rx : shape.w / 2;
  const shoulderY = cy + 8;
  const leftShoulder: [number, number] = [CX - half + 5, shoulderY];
  const rightShoulder: [number, number] = [CX + half - 5, shoulderY];
  const eyeY = cy - 4;
  const arm = ARMS[pose];
  const hold = handProp(pose, persona);
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 130 120"
      width={(size * 130) / 120}
      height={size}
      className={`hm-mascot ${className ?? ""}`}
      data-pose={pose}
      data-persona={persona}
      style={{ color, "--m-size": `${size}px` } as CSSProperties}
    >
      <defs>
        <filter
          id={`${id}-fur`}
          filterUnits="userSpaceOnUse"
          x="-20"
          y="-20"
          width="170"
          height="160"
        >
          <feTurbulence type="fractalNoise" baseFrequency="0.9" numOctaves="3" seed="7" />
          <feDisplacementMap in="SourceGraphic" scale="3.2" />
        </filter>
        <filter id={`${id}-grain`} x="0" y="0" width="100%" height="100%">
          <feTurbulence type="fractalNoise" baseFrequency="2.4" numOctaves="2" seed="2" />
          <feColorMatrix type="matrix" values="0 0 0 0 0  0 0 0 0 0  0 0 0 0 0  0 0 0 -1.4 1.1" />
          <feComposite in2="SourceGraphic" operator="in" />
        </filter>
        <radialGradient id={`${id}-shade`} cx="0.36" cy="0.3" r="0.85">
          <stop offset="0" stopColor="#fff" stopOpacity="0.42" />
          <stop offset="0.45" stopColor="#fff" stopOpacity="0" />
          <stop offset="1" stopColor="#000" stopOpacity="0.45" />
        </radialGradient>
        <pattern id={`${id}-tweed`} width="3" height="3" patternUnits="userSpaceOnUse">
          <rect width="3" height="3" fill="#8a6a45" />
          <path d="M0 .5h3M0 2h3" stroke="#6b5032" strokeWidth="0.5" />
          <path d="M.5 0v3M2 0v3" stroke="#a88a5f" strokeWidth="0.45" opacity="0.7" />
        </pattern>
        <pattern id={`${id}-grey`} width="3" height="3" patternUnits="userSpaceOnUse">
          <rect width="3" height="3" fill="#6f747c" />
          <path d="M0 .5h3M0 2h3" stroke="#585c63" strokeWidth="0.5" />
          <path d="M.5 0v3M2 0v3" stroke="#878c94" strokeWidth="0.45" opacity="0.7" />
        </pattern>
        <pattern id={`${id}-stripe`} width="6" height="6" patternUnits="userSpaceOnUse">
          <rect width="6" height="6" fill="#e9e1cf" />
          <rect width="3" height="6" fill="#c2453a" />
        </pattern>
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
        <linearGradient id={`${id}-hat`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#ffd84d" />
          <stop offset="1" stopColor="#e2a514" />
        </linearGradient>
        <radialGradient id={`${id}-glow`} cx="0.5" cy="0.5" r="0.5">
          <stop offset="0" stopColor="#fff3c4" />
          <stop offset="1" stopColor="#ffcf4a" stopOpacity="0" />
        </radialGradient>
      </defs>

      {/* The ground under it. */}
      <ellipse cx={CX} cy={BASE + 7} rx={half - 2} ry="3" fill="#000" opacity="0.22" />
      {/* What stands beside it. */}
      {pose === "searching" && <Microscope u={u} />}
      {pose === "waiting" && <Hourglass u={u} />}

      <g className="m-body">
        {/* Feet. */}
        <g filter={u("fur")} className="m-limb">
          <ellipse cx={CX - 11} cy={BASE + 3} rx="8" ry="4.5" fill="currentColor" />
          <ellipse cx={CX + 11} cy={BASE + 3} rx="8" ry="4.5" fill="currentColor" />
        </g>
        {/* The body, in fur, shaded and grained. */}
        <g filter={u("fur")}>
          <BodyShape shape={shape} cy={cy} fill="currentColor" />
        </g>
        <BodyShape shape={shape} cy={cy} fill={u("shade")} />
        <g filter={u("grain")} opacity="0.5">
          <BodyShape shape={shape} cy={cy} fill="#fff" />
        </g>
        <Outfit persona={persona} u={u} top={top} cy={cy} half={half} />
        <EyePair kind={EYES[persona]} y={eyeY} pose={pose} persona={persona} />
        {persona === "detective" && (pose === "idle" || pose === "thinking") && (
          <Pipe x={CX + 9} y={eyeY + 13} u={u} />
        )}
      </g>

      <Arm
        at={leftShoulder}
        angles={arm.left}
        u={u}
        upper="m-left-upper"
        fore="m-left-fore"
        hold={pose === "composing" ? <Notepad /> : undefined}
      />
      <Arm
        at={rightShoulder}
        angles={arm.right}
        u={u}
        upper="m-right-upper"
        fore="m-right-fore"
        hold={hold?.(u)}
      />

      {pose === "thinking" && <ThoughtCloud u={u} />}
      {pose === "delegating" && <Bubbles u={u} />}
    </svg>
  );
}

function BodyShape({ shape, cy, fill }: { shape: Shape; cy: number; fill: string }) {
  return shape.kind === "oval" ? (
    <ellipse cx={CX} cy={cy} rx={shape.rx} ry={shape.ry} fill={fill} />
  ) : (
    <rect
      x={CX - shape.w / 2}
      y={cy - shape.h / 2}
      width={shape.w}
      height={shape.h}
      rx="20"
      fill={fill}
    />
  );
}

/** An arm with an elbow: the upper arm turns at the shoulder, the forearm at the elbow. */
function Arm({
  at,
  angles,
  u,
  upper,
  fore,
  hold,
}: {
  at: [number, number];
  angles: [number, number];
  u: (n: string) => string;
  upper: string;
  fore: string;
  hold?: ReactNode;
}) {
  const [a, b] = angles;
  return (
    <g transform={`translate(${at[0]} ${at[1]}) rotate(${a})`}>
      <g className={`m-upper ${upper}`}>
        <path
          d={`M0 0L${UPPER} 0`}
          stroke="currentColor"
          strokeWidth="9"
          strokeLinecap="round"
          filter={u("fur")}
          className="m-limb"
        />
        <g transform={`translate(${UPPER} 0) rotate(${b})`}>
          <g className={`m-fore ${fore}`}>
            <path
              d={`M0 0L${FORE} 0`}
              stroke="currentColor"
              strokeWidth="8.4"
              strokeLinecap="round"
              filter={u("fur")}
              className="m-limb"
            />
            <g transform={`translate(${FORE} 0)`}>
              {/* What the hand holds stays upright, whatever the arm's angle. */}
              <g transform={`rotate(${-(a + b)})`}>{hold}</g>
              <g filter={u("fur")} className="m-limb">
                <circle r="6.4" fill="currentColor" />
              </g>
              <circle r="6.4" fill={u("shade")} />
            </g>
          </g>
        </g>
      </g>
    </g>
  );
}

function EyePair({
  kind,
  y,
  pose,
  persona,
}: {
  kind: Eyes;
  y: number;
  pose: Pose;
  persona: MascotPersona;
}) {
  // Where it looks: at the work in front of it.
  const look: [number, number] =
    pose === "searching"
      ? [3, 1]
      : pose === "composing"
        ? [-1, 3]
        : pose === "thinking"
          ? [3, -3]
          : pose === "delegating"
            ? [3, -2]
            : pose === "calling"
              ? [3, 1]
              : [0, 0];
  const gap = persona === "detective" ? 9 : 10;
  const one = (x: number) => {
    if (kind === "happy")
      return (
        <path
          d={`M${x - 4} ${y + 1}Q${x} ${y - 5} ${x + 4} ${y + 1}`}
          fill="none"
          stroke="#1b1d2a"
          strokeWidth="2.4"
          strokeLinecap="round"
        />
      );
    const r = kind === "wide" ? 5.2 : 4.2;
    return (
      <g>
        <ellipse cx={x} cy={y} rx={r * 0.85} ry={r * 1.1} fill="#1b1d2a" />
        <circle cx={x + 1.3} cy={y - 1.6} r={r * 0.3} fill="#fff" />
        {kind === "keen" && (
          // A lid over the top of the eye: a keen, narrowed look.
          <path
            d={`M${x - r - 1} ${y - r * 0.6}Q${x} ${y - r * 0.1} ${x + r + 1} ${y - r * 0.9}L${x + r + 1} ${y - r - 3}L${x - r - 1} ${y - r - 3}Z`}
            fill="currentColor"
          />
        )}
      </g>
    );
  };
  return (
    <g className="m-eyes" transform={`translate(${look[0]} ${look[1]})`}>
      <g className="m-blink" style={{ transformOrigin: `${CX}px ${y}px` }}>
        {one(CX - gap)}
        {one(CX + gap)}
      </g>
    </g>
  );
}

function Outfit({
  persona,
  u,
  top,
  cy,
  half,
}: {
  persona: MascotPersona;
  u: (n: string) => string;
  top: number;
  cy: number;
  half: number;
}) {
  const L = CX - half;
  const R = CX + half;
  switch (persona) {
    case "detective":
      // A deerstalker with its two peaks and the flaps tied on top, and a tweed cape.
      return (
        <g>
          <path
            d={`M${L + 4} ${cy + 12}C${L + 10} ${cy + 4} ${R - 10} ${cy + 4} ${R - 4} ${cy + 12}L${R + 2} ${cy + 26}C${R - 14} ${cy + 33} ${L + 14} ${cy + 33} ${L - 2} ${cy + 26}Z`}
            fill={u("tweed")}
            stroke="#4f3a22"
            strokeWidth="0.8"
          />
          <path
            d={`M${CX - 7} ${cy + 9}L${CX} ${cy + 15}L${CX + 7} ${cy + 9}`}
            fill="none"
            stroke="#4f3a22"
            strokeWidth="1"
          />
          <g transform={`translate(0 ${top - 34})`}>
            <path
              d="M27 45C27 22 77 22 77 45Z"
              fill={u("tweed")}
              stroke="#4f3a22"
              strokeWidth="0.8"
            />
            <path
              d="M29 44C20 45 15 48 13 52C23 51 31 48 36 45Z"
              fill={u("tweed")}
              stroke="#4f3a22"
              strokeWidth="0.8"
            />
            <path
              d="M75 44C84 45 89 48 91 52C81 51 73 48 68 45Z"
              fill={u("tweed")}
              stroke="#4f3a22"
              strokeWidth="0.8"
            />
            <path d="M27 44.5h50" stroke="#4f3a22" strokeWidth="1.6" />
            <ellipse
              cx="47.5"
              cy="26.5"
              rx="4"
              ry="2"
              fill={u("tweed")}
              stroke="#4f3a22"
              strokeWidth="0.6"
            />
            <ellipse
              cx="56.5"
              cy="26.5"
              rx="4"
              ry="2"
              fill={u("tweed")}
              stroke="#4f3a22"
              strokeWidth="0.6"
            />
            <circle cx="52" cy="26.8" r="1.6" fill="#6b5032" />
          </g>
        </g>
      );
    case "writer":
      // A red beret, tilted, and a striped scarf with a tail.
      return (
        <g>
          <path
            d={`M${L + 3} ${cy + 13}Q${CX} ${cy + 21} ${R - 3} ${cy + 13}L${R - 3} ${cy + 20}Q${CX} ${cy + 28} ${L + 3} ${cy + 20}Z`}
            fill={u("stripe")}
            stroke="#8e2f27"
            strokeWidth="0.6"
          />
          <path
            d={`M${CX - 14} ${cy + 22}l-3 16 7 1 3-15z`}
            fill={u("stripe")}
            stroke="#8e2f27"
            strokeWidth="0.6"
          />
          <g transform={`translate(0 ${top - 37}) rotate(-12 50 40)`}>
            <ellipse cx="50" cy="40" rx="24" ry="8.5" fill="#c43b30" />
            <ellipse cx="50" cy="38" rx="20" ry="5" fill="#d9493d" />
            <path
              d="M48 32q1-4 3 0"
              stroke="#c43b30"
              strokeWidth="2.4"
              strokeLinecap="round"
              fill="none"
            />
          </g>
        </g>
      );
    case "engineer":
      // A yellow hard hat and dungaree straps.
      return (
        <g>
          <rect x={CX - 15} y={cy + 14} width="30" height={BASE - cy - 12} rx="4" fill="#2f5f9e" />
          <path
            d={`M${CX - 13} ${cy + 15}L${CX - 17} ${cy - 2}M${CX + 13} ${cy + 15}L${CX + 17} ${cy - 2}`}
            stroke="#2f5f9e"
            strokeWidth="4"
            strokeLinecap="round"
          />
          <circle cx={CX - 11} cy={cy + 17} r="1.4" fill="#e2c15a" />
          <circle cx={CX + 11} cy={cy + 17} r="1.4" fill="#e2c15a" />
          <g transform={`translate(0 ${top - 40})`}>
            <path
              d="M30 44C30 23 74 23 74 44Z"
              fill={u("hat")}
              stroke="#a57a0c"
              strokeWidth="0.8"
            />
            <path d="M50 26v18M54 26v18" stroke="#e8b52a" strokeWidth="1.6" />
            <rect
              x="23"
              y="42"
              width="58"
              height="5"
              rx="2.5"
              fill={u("hat")}
              stroke="#a57a0c"
              strokeWidth="0.8"
            />
          </g>
        </g>
      );
    case "scientist":
      // A lab coat's lapels and goggles pushed up on the forehead.
      return (
        <g>
          <path
            d={`M${L + 3} ${cy + 6}L${CX - 6} ${BASE + 1}L${L + 6} ${BASE + 1}Q${L} ${cy + 24} ${L + 3} ${cy + 6}Z`}
            fill="#f4f6f8"
            stroke="#b9c2cc"
            strokeWidth="0.8"
          />
          <path
            d={`M${R - 3} ${cy + 6}L${CX + 6} ${BASE + 1}L${R - 6} ${BASE + 1}Q${R} ${cy + 24} ${R - 3} ${cy + 6}Z`}
            fill="#f4f6f8"
            stroke="#b9c2cc"
            strokeWidth="0.8"
          />
          <rect x={R - 12} y={cy + 22} width="5" height="7" rx="1" fill="#3a78d8" />
          <g transform={`translate(0 ${top - 37})`}>
            <path d="M22 50Q52 42 82 50" stroke="#2c3038" strokeWidth="3" fill="none" />
            <circle
              cx="42"
              cy="47"
              r="7"
              fill="#9fd3f0"
              fillOpacity="0.75"
              stroke="#2c3038"
              strokeWidth="2.2"
            />
            <circle
              cx="62"
              cy="47"
              r="7"
              fill="#9fd3f0"
              fillOpacity="0.75"
              stroke="#2c3038"
              strokeWidth="2.2"
            />
            <path d="M39 44l3-2" stroke="#fff" strokeWidth="1.4" strokeLinecap="round" />
            <path d="M59 44l3-2" stroke="#fff" strokeWidth="1.4" strokeLinecap="round" />
          </g>
        </g>
      );
    case "inspector":
      // A flat cap and a red bow tie.
      return (
        <g>
          <path d={`M${CX - 1} ${cy + 15}l-8-4v9z`} fill="#c0392b" />
          <path d={`M${CX + 1} ${cy + 15}l8-4v9z`} fill="#c0392b" />
          <circle cx={CX} cy={cy + 15.5} r="2.2" fill="#9b2c21" />
          <g transform={`translate(0 ${top - 37})`}>
            <path
              d="M27 45C29 29 73 27 79 41L90 46C74 49 40 49 27 45Z"
              fill={u("grey")}
              stroke="#3f4349"
              strokeWidth="0.8"
            />
            <circle cx="53" cy="31" r="1.6" fill="#585c63" />
          </g>
        </g>
      );
    case "conductor":
      // Humagen leads the others: a tailcoat's lapels, a white collar, a bow tie.
      return (
        <g>
          <path
            d={`M${L + 3} ${cy + 6}L${CX - 4} ${BASE + 1}L${L + 6} ${BASE + 1}Q${L} ${cy + 24} ${L + 3} ${cy + 6}Z`}
            fill="#1d2027"
          />
          <path
            d={`M${R - 3} ${cy + 6}L${CX + 4} ${BASE + 1}L${R - 6} ${BASE + 1}Q${R} ${cy + 24} ${R - 3} ${cy + 6}Z`}
            fill="#1d2027"
          />
          <path
            d={`M${CX - 10} ${cy + 11}L${CX} ${cy + 17}L${CX + 10} ${cy + 11}L${CX + 8} ${cy + 8}L${CX} ${cy + 13}L${CX - 8} ${cy + 8}Z`}
            fill="#f4f6f8"
          />
          <path d={`M${CX - 1} ${cy + 15}l-7-4v8z`} fill="#111" />
          <path d={`M${CX + 1} ${cy + 15}l7-4v8z`} fill="#111" />
          <circle cx={CX} cy={cy + 15} r="2" fill="#111" />
        </g>
      );
    default:
      return null;
  }
}

function Pipe({ x, y, u }: { x: number; y: number; u: (n: string) => string }) {
  return (
    <g>
      <path
        d={`M${x - 4} ${y}q6 3 10 1`}
        stroke="#3b2a1a"
        strokeWidth="1.8"
        fill="none"
        strokeLinecap="round"
      />
      <path d={`M${x + 5} ${y - 3}h6l-1 7q-2 2-4 0z`} fill={u("wood")} />
      <circle className="m-smoke" cx={x + 9} cy={y - 7} r="1.8" fill="#cfd5dc" opacity="0.7" />
      <circle
        className="m-smoke m-late"
        cx={x + 11}
        cy={y - 12}
        r="1.3"
        fill="#cfd5dc"
        opacity="0.5"
      />
    </g>
  );
}

/** What the right hand holds in each pose; drawn upright, with the hand at 0,0. */
function handProp(pose: Pose, persona: MascotPersona) {
  if (pose === "composing")
    return () => (
      <g transform="rotate(25) translate(-16 -21)">
        <rect x="13" y="1.6" width="6" height="4.4" rx="1.6" fill="#ef8a9b" />
        <rect x="13" y="5.6" width="6" height="3.4" fill="#c9d0d7" />
        <rect x="13" y="9" width="6" height="14" fill="#f2b630" />
        <rect x="15" y="9" width="2" height="14" fill="#ffd45e" />
        <path d="M13 23h6l-3 6.6z" fill="#f0cf9e" />
        <path d="M15.1 27.6h1.8L16 29.6z" fill="#3b3b3b" />
      </g>
    );
  if (pose === "calling")
    return (u: (n: string) => string) => (
      <g transform="translate(-6 -22) scale(1.15)">
        <path
          d="M20.8 4.4a6.8 6.8 0 0 0-7.6 9.4l-8.6 8.6a3 3 0 0 0 4.2 4.2l8.6-8.6a6.8 6.8 0 0 0 9.4-7.6l-4.2 4.2-4.4-1-1-4.4z"
          fill={u("steel")}
          stroke="#4b545e"
          strokeWidth="0.7"
          strokeLinejoin="round"
        />
      </g>
    );
  if (persona === "conductor" && (pose === "delegating" || pose === "idle"))
    return () => (
      <g>
        <path d="M0 0L16 -16" stroke="#f4f6f8" strokeWidth="1.6" strokeLinecap="round" />
        <path d="M-1 1L3 -3" stroke="#6e3f1c" strokeWidth="3" strokeLinecap="round" />
      </g>
    );
  return undefined;
}

function Notepad() {
  return (
    <g transform="translate(-3 -9)">
      <rect width="24" height="21" rx="1.5" fill="#fbfbf7" stroke="#c9cdd2" strokeWidth="0.7" />
      <rect width="24" height="3" rx="1.2" fill="#c43b30" />
      <path
        className="m-ink"
        d="M4 8h15M4 12h12M4 16h14"
        stroke="#3b4048"
        strokeWidth="1"
        strokeLinecap="round"
        pathLength={1}
      />
    </g>
  );
}

function Microscope({ u }: { u: (n: string) => string }) {
  const edge = { stroke: "#2a2f36", strokeWidth: 0.5 };
  // Turned so its eyepiece leans toward the bot; its knob sits under the bot's hand.
  return (
    <g transform="translate(81.4 65.4) scale(1.4) translate(32 0) scale(-1 1)">
      <rect x="4" y="27" width="23" height="3.4" rx="1.5" fill={u("dark")} />
      <rect x="15.5" y="24.2" width="8.5" height="3.2" rx="1" fill={u("dark")} />
      <ellipse className="m-glow" cx="11.6" cy="21.6" rx="3.4" ry="2.6" fill={u("glow")} />
      <rect x="9.6" y="22.6" width="4" height="2.4" rx="0.7" fill={u("dark")} />
      <rect x="5" y="19.4" width="14" height="2.1" rx="0.7" fill={u("dark")} />
      <rect x="7.6" y="18.5" width="7.6" height="1" rx="0.3" fill="#bfe3ff" />
      <path
        d="M20.6 25.2C26.4 21.4 26.6 12.2 19.8 8.2L17.9 10.9C22.6 13.6 22.6 20 18.2 23.4Z"
        fill={u("body")}
        {...edge}
      />
      <g className="m-focus">
        <g transform="rotate(25 15 13)">
          <rect x="13.2" y="0.6" width="3.6" height="1.2" rx="0.4" fill={u("knob")} />
          <rect x="13.6" y="1.6" width="2.8" height="2.8" rx="0.5" fill={u("dark")} />
          <rect x="13" y="4.2" width="4" height="9.8" rx="1" fill={u("body")} {...edge} />
          <rect x="13" y="8.8" width="4" height="1.1" fill="#2a2f36" />
          <rect x="12.4" y="13.8" width="5.2" height="1.7" rx="0.6" fill={u("knob")} {...edge} />
          <rect x="13.9" y="15.4" width="2.2" height="2.7" rx="0.4" fill={u("dark")} />
          <rect x="13.9" y="16.3" width="2.2" height="0.6" fill="#d9a441" />
        </g>
      </g>
      <circle cx="21.6" cy="17.6" r="2.5" fill={u("knob")} {...edge} />
    </g>
  );
}

function Hourglass({ u }: { u: (n: string) => string }) {
  return (
    <g transform="translate(90 72) scale(1.05)">
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
    </g>
  );
}

function ThoughtCloud({ u }: { u: (n: string) => string }) {
  const edge = { stroke: "#2a2f36", strokeWidth: 0.5 };
  return (
    <g transform="translate(78 0) scale(1.45)">
      <circle className="m-trail" cx="3.4" cy="29" r="1.5" fill={u("cloud")} {...edge} />
      <circle className="m-trail m-late" cx="7.2" cy="24.6" r="2.4" fill={u("cloud")} {...edge} />
      <path
        d="M10.4 21.4C6.6 21.4 5.2 16.6 8.6 14.8 7.6 10.4 12.6 8 15.4 10.4 17 5.8 24 6.4 24.4 11 28.6 10.8 30.4 15.6 27.4 18 28.4 21 25.6 22.4 23.4 21.4Z"
        fill={u("cloud")}
        {...edge}
      />
      <circle className="m-dot" cx="13.2" cy="16.2" r="1.35" fill="#56606c" />
      <circle className="m-dot" cx="17.8" cy="16.2" r="1.35" fill="#56606c" />
      <circle className="m-dot" cx="22.4" cy="16.2" r="1.35" fill="#56606c" />
    </g>
  );
}

function Bubbles({ u }: { u: (n: string) => string }) {
  return (
    <g transform="translate(84 4) scale(1.35)">
      <path
        className="m-rock"
        d="M4.6 3h12a3 3 0 0 1 3 3v5a3 3 0 0 1-3 3H10l-4 3.2V14H4.6a3 3 0 0 1-3-3V6a3 3 0 0 1 3-3z"
        fill={u("cloud")}
        stroke="#2a2f36"
        strokeWidth="0.5"
      />
      <path
        d="M15 14.4h12a3 3 0 0 1 3 3v5a3 3 0 0 1-3 3h-1.4v3.2l-4-3.2H15a3 3 0 0 1-3-3v-5a3 3 0 0 1 3-3z"
        fill={u("accent")}
        stroke="#1f4fae"
        strokeWidth="0.5"
      />
      <circle className="m-dot" cx="17.4" cy="19.9" r="1.2" fill="#fff" />
      <circle className="m-dot" cx="21" cy="19.9" r="1.2" fill="#fff" />
      <circle className="m-dot" cx="24.6" cy="19.9" r="1.2" fill="#fff" />
    </g>
  );
}
