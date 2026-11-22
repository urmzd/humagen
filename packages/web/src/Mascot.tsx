import {
  BOT_AVATAR_OVERSCAN,
  BOT_AVATAR_RISE,
  drawBotAvatarFrame,
  restPose,
  warmBotAvatarPlastic,
} from "bot-avatars";
import {
  type CSSProperties,
  type ReactNode,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import type { OrbState } from "./AgentOrb.tsx";
import {
  Bubbles,
  Hourglass,
  Microscope,
  Notepad,
  Pencil,
  PropDefs,
  Remote,
  ThoughtCloud,
  Wrench,
} from "./MascotProps.tsx";

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
  | "spy"
  | "plain";

export const mascotPersonas: MascotPersona[] = [
  "detective",
  "writer",
  "engineer",
  "scientist",
  "inspector",
  "spy",
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
  if (h === "humagen" || h === "lead" || h.includes("orchestr")) return "spy";
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
  engineer: { kind: "oval", rx: 31, ry: 29 },
  scientist: { kind: "oval", rx: 30, ry: 31 },
  inspector: { kind: "oval", rx: 29, ry: 30 },
  spy: { kind: "oval", rx: 29, ry: 32 },
  plain: { kind: "oval", rx: 30, ry: 31 },
};

type Eyes = "dot" | "keen" | "happy" | "wide" | "suave";
const EYES: Record<MascotPersona, Eyes> = {
  detective: "keen",
  writer: "dot",
  engineer: "dot",
  scientist: "wide",
  inspector: "keen",
  spy: "suave",
  plain: "dot",
};

const CX = 52;
const BASE = 100; // where the body meets its feet
const UPPER = 15;
const FORE = 11;

/** Where each arm points, in degrees from the shoulder (0 is right, 90 is down). */
const REST: [number, number] = [140, -80];
const ARMS: Record<Pose, { left: [number, number]; right: [number, number] }> = {
  idle: { left: REST, right: [40, 80] },
  searching: { left: REST, right: [62, -97] },
  thinking: { left: REST, right: [105, -225] },
  calling: { left: REST, right: [30, -20] },
  composing: { left: [60, -60], right: [120, 80] },
  delegating: { left: REST, right: [-40, -30] },
  waiting: { left: [70, -50], right: [110, 50] },
};

/**
 * Fur is drawn once, not on every frame. An SVG filter runs again whenever
 * anything in its picture moves, so the furry parts (the body with its feet,
 * an upper arm, a forearm, a hand) are drawn to images once per color, shape
 * and size, and the moving parts move those images.
 */
/**
 * The 3D drawing is heavy, so it never runs all at once: each piece of work
 * is a small job, and jobs run in idle time a few at a time, with the page
 * free to respond between them. Pictures are handed out as blob URLs, which
 * the browser decodes once, rather than long data URLs copied into the page.
 */
type Job = () => void;
const jobs: Job[] = [];
let pumping = false;
function work(job: Job, soon = false) {
  if (soon) jobs.unshift(job);
  else jobs.push(job);
  if (!pumping) pump();
}
function pump() {
  pumping = true;
  const run = (deadline?: IdleDeadline) => {
    const start = performance.now();
    do {
      // One failing job (an old browser without Path2D.roundRect, say) leaves that
      // picture plain; it never stops the jobs after it.
      try {
        jobs.shift()?.();
      } catch {
        // Its promise stays pending and the mark keeps its plain shape.
      }
    } while (
      jobs.length &&
      performance.now() - start < 10 &&
      (!deadline || deadline.timeRemaining() > 4)
    );
    if (jobs.length) pump();
    else pumping = false;
  };
  if (typeof window.requestIdleCallback === "function")
    window.requestIdleCallback(run, { timeout: 400 });
  else setTimeout(run, 16);
}
function urlOf(canvas: HTMLCanvasElement): Promise<string> {
  return new Promise((resolve) =>
    canvas.toBlob((blob) => resolve(blob ? URL.createObjectURL(blob) : ""), "image/png"),
  );
}

const baked = new Map<string, Promise<string>>();

function furDefs(box: [number, number, number, number]) {
  const [x, y, w, h] = box;
  return `<defs>
<filter id="fur" filterUnits="userSpaceOnUse" x="${x}" y="${y}" width="${w}" height="${h}">
<feTurbulence type="fractalNoise" baseFrequency="0.9" numOctaves="3" seed="7"/>
<feDisplacementMap in="SourceGraphic" scale="3.2"/></filter>
<filter id="grain" filterUnits="userSpaceOnUse" x="${x}" y="${y}" width="${w}" height="${h}">
<feTurbulence type="fractalNoise" baseFrequency="2.4" numOctaves="2" seed="2"/>
<feColorMatrix type="matrix" values="0 0 0 0 0  0 0 0 0 0  0 0 0 0 0  0 0 0 -1.4 1.1"/>
<feComposite in2="SourceGraphic" operator="in"/></filter>
<radialGradient id="shade" cx="0.36" cy="0.3" r="0.85">
<stop offset="0" stop-color="#fff" stop-opacity="0.42"/>
<stop offset="0.45" stop-color="#fff" stop-opacity="0"/>
<stop offset="1" stop-color="#000" stop-opacity="0.45"/></radialGradient></defs>`;
}

function bake(key: string, box: [number, number, number, number], body: string, px: number) {
  const hit = baked.get(key);
  if (hit) return hit;
  const [x, y, w, h] = box;
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${Math.ceil(w * px)}" height="${Math.ceil(h * px)}" viewBox="${x} ${y} ${w} ${h}">${furDefs(box)}${body}</svg>`;
  const made = new Promise<string>((resolve, reject) => {
    const img = new Image();
    img.onload = () =>
      work(() => {
        const canvas = document.createElement("canvas");
        canvas.width = Math.ceil(w * px);
        canvas.height = Math.ceil(h * px);
        const ctx = canvas.getContext("2d");
        if (!ctx) return reject(new Error("no canvas"));
        ctx.drawImage(img, 0, 0, canvas.width, canvas.height);
        urlOf(canvas).then(resolve);
      });
    img.onerror = () => reject(new Error("fur could not be drawn"));
    img.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
  });
  baked.set(key, made);
  // A picture that could not be made is tried again next time, not kept as a failure.
  made.then(
    (url) => url || baked.delete(key),
    () => baked.delete(key),
  );
  made.catch(() => baked.delete(key));
  return made;
}

const BOX = {
  body: [0, 0, 130, 120] as [number, number, number, number],
  upper: [-8, -8, UPPER + 16, 16] as [number, number, number, number],
  fore: [-8, -11, FORE + 18, 21] as [number, number, number, number],
};

/**
 * One limb as one piece of the bot: fur at its edge, round shading across it
 * so it reads as a soft cylinder whatever its angle, the body's grain, and
 * its start faded so it grows out of the body or the upper arm with no seam.
 * The forearm carries the hand, a mitt with a thumb, in the same piece.
 */
function limbMarkup(opts: {
  len: number;
  width: number;
  limb: string;
  hand: boolean;
  fade: [number, number];
  sleeve?: string;
}) {
  const { len, width, limb, hand, fade, sleeve } = opts;
  const half = width / 2;
  const line = (stroke: string) =>
    `<path d="M0 0L${len} 0" stroke="${stroke}" stroke-width="${width}" stroke-linecap="round"/>`;
  const mitt = (fill: string) =>
    `<circle cx="${len}" cy="0" r="6.6" fill="${fill}"/><ellipse cx="${len - 2}" cy="-5.2" rx="2.7" ry="2.3" fill="${fill}"/>`;
  const arm = sleeve
    ? // A suit's sleeve is cloth, with a white cuff before the hand.
      `${line(sleeve)}<path d="M0 ${-half + 1.2}L${len} ${-half + 1.2}" stroke="#fff" stroke-opacity="0.12" stroke-width="1.4" stroke-linecap="round"/>${hand ? `<rect x="${len - 4.4}" y="${-half}" width="2.8" height="${width}" rx="1" fill="#f4f6f8"/>` : ""}`
    : `<g filter="url(#fur)">${line(limb)}</g>${line("url(#cyl)")}<g filter="url(#grain)" opacity="0.5">${line("#fff")}</g>`;
  const fist = hand
    ? `<g filter="url(#fur)">${mitt(limb)}</g>${mitt("url(#ball)")}<g filter="url(#grain)" opacity="0.5">${mitt("#fff")}</g>`
    : "";
  return `<defs>
<linearGradient id="cyl" gradientUnits="userSpaceOnUse" x1="0" y1="${-half}" x2="0" y2="${half}">
<stop offset="0" stop-color="#000" stop-opacity="0.3"/><stop offset="0.38" stop-color="#fff" stop-opacity="0.16"/>
<stop offset="1" stop-color="#000" stop-opacity="0.42"/></linearGradient>
<radialGradient id="ball" gradientUnits="userSpaceOnUse" cx="${len - 1.8}" cy="-2.4" r="8.5">
<stop offset="0" stop-color="#fff" stop-opacity="0.3"/><stop offset="0.45" stop-color="#fff" stop-opacity="0"/>
<stop offset="1" stop-color="#000" stop-opacity="0.42"/></radialGradient>
<linearGradient id="fade" gradientUnits="userSpaceOnUse" x1="${fade[0]}" y1="0" x2="${fade[1]}" y2="0">
<stop offset="0" stop-color="#fff" stop-opacity="0"/><stop offset="1" stop-color="#fff"/></linearGradient>
<mask id="grow" maskUnits="userSpaceOnUse" x="-20" y="-20" width="60" height="40">
<rect x="-20" y="-20" width="60" height="40" fill="url(#fade)"/></mask></defs>
<g mask="url(#grow)">${arm}</g>${fist}`;
}

function shapeMarkup(shape: Shape, cy: number, fill: string) {
  return shape.kind === "oval"
    ? `<ellipse cx="${CX}" cy="${cy}" rx="${shape.rx}" ry="${shape.ry}" fill="${fill}"/>`
    : `<rect x="${CX - shape.w / 2}" y="${cy - shape.h / 2}" width="${shape.w}" height="${shape.h}" rx="20" fill="${fill}"/>`;
}

/**
 * The body and limbs in the same 3D plush the old bots had: bot-avatars
 * builds a height field from the outline, lights it with a matcap and grows
 * its fur along the slope. Its canvas is 1.5 boxes wide, with the shape's
 * centre (design 50,50) at the middle and RISE of a box below it; a part is
 * placed so that centre lands where `at` says, in this grid's units.
 */
const SPAN = 100 * BOT_AVATAR_OVERSCAN;
function fabricPlace(at: [number, number]): [number, number, number, number] {
  return [at[0] - SPAN / 2, at[1] - SPAN / 2 - BOT_AVATAR_RISE * 100, SPAN, SPAN];
}

/** A CSS colour as #rrggbb, which the renderer reads (it does not read oklch). */
function hexOf(color: string): string {
  const c = document.createElement("canvas");
  c.width = c.height = 1;
  const ctx = c.getContext("2d");
  if (!ctx) return "#888888";
  ctx.fillStyle = color;
  ctx.fillRect(0, 0, 1, 1);
  const [r = 0, g = 0, b = 0] = ctx.getImageData(0, 0, 1, 1).data;
  return `#${[r, g, b].map((v) => v.toString(16).padStart(2, "0")).join("")}`;
}

const fabrics = new Map<string, Promise<string>>();

/** The surface the fur is drawn for: the page's theme, else the system's. Part of every cache key. */
function themeNow(): "light" | "dark" {
  const set = document.documentElement.dataset.theme;
  if (set === "light" || set === "dark") return set;
  return window.matchMedia?.("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

/** How many angles a turn is drawn at. */
export const TURN_FRAMES = 16;
const turns = new Map<string, Promise<string>>();

/**
 * The body at every angle of a full turn, side by side in one image: the
 * spin shows it frame by frame, so it turns in depth rather than flipping
 * like a card. It is drawn only when a bot is first about to spin, at a
 * modest size, one angle per job.
 */
function bakeTurn(key: string, path: () => Path2D, color: string, unit: number) {
  const hit = turns.get(key);
  if (hit) return hit;
  const box = Math.max(24, Math.round(100 * unit));
  const side = Math.round(box * BOT_AVATAR_OVERSCAN);
  const theme = themeNow();
  const made = new Promise<string>((resolve) => {
    const strip = document.createElement("canvas");
    strip.width = side * TURN_FRAMES;
    strip.height = side;
    const sctx = strip.getContext("2d");
    const frame = document.createElement("canvas");
    frame.width = frame.height = side;
    const fctx = frame.getContext("2d");
    if (!sctx || !fctx) return resolve("");
    const p = path();
    // One job builds the form, then one job draws each angle.
    let failed = false;
    work(() => {
      try {
        warmBotAvatarPlastic(key, p, box, undefined, true);
      } catch {
        failed = true;
      }
    });
    for (let i = 0; i < TURN_FRAMES; i++)
      work(() => {
        if (failed) {
          if (i === TURN_FRAMES - 1) resolve("");
          return;
        }
        try {
          fctx.clearRect(0, 0, side, side);
          drawBotAvatarFrame(
            fctx,
            box,
            { ...restPose("default"), yaw: (i / TURN_FRAMES) * Math.PI * 2 },
            {
              path: p,
              face: "eyes",
              faceX: 50,
              faceY: 50,
              faceScale: 0,
              color,
              ink: "#111111",
              shading: "fabric",
              still: true,
              typeKey: key,
              theme,
              dpr: 1,
            },
          );
          sctx.drawImage(frame, i * side, 0);
          if (i === TURN_FRAMES - 1) urlOf(strip).then(resolve);
        } catch {
          failed = true;
        }
        if (i === TURN_FRAMES - 1 && failed) resolve("");
      });
  });
  turns.set(key, made);
  // A picture that could not be made is tried again next time, not kept as a failure.
  made.then(
    (url) => url || turns.delete(key),
    () => turns.delete(key),
  );
  return made;
}

function bakeFabric(key: string, path: () => Path2D, color: string, unit: number) {
  const hit = fabrics.get(key);
  if (hit) return hit;
  const box = Math.max(24, Math.round(100 * unit));
  const side = Math.round(box * BOT_AVATAR_OVERSCAN);
  const theme = themeNow();
  const made = new Promise<string>((resolve) => {
    const canvas = document.createElement("canvas");
    canvas.width = canvas.height = side;
    const ctx = canvas.getContext("2d");
    if (!ctx) return resolve("");
    const draw = () => {
      ctx.clearRect(0, 0, side, side);
      drawBotAvatarFrame(ctx, box, restPose("default"), {
        path: path(),
        face: "eyes",
        faceX: 50,
        faceY: 50,
        faceScale: 0,
        color,
        ink: "#111111",
        shading: "fabric",
        still: true,
        typeKey: key,
        theme,
        dpr: 1,
      });
    };
    // Build the form and grow its pile, then draw it once with its fur.
    work(() => {
      // A throw resolves empty, so the cache forgets it and the next mark tries again.
      try {
        warmBotAvatarPlastic(key, path(), box, undefined, true);
        draw();
        urlOf(canvas).then(resolve);
      } catch {
        resolve("");
      }
    });
  });
  fabrics.set(key, made);
  // A picture that could not be made is tried again next time, not kept as a failure.
  made.then(
    (url) => url || fabrics.delete(key),
    () => fabrics.delete(key),
  );
  return made;
}

/** A rounded rectangle, drawn with arcs where Path2D.roundRect is missing. */
function rounded(p: Path2D, x: number, y: number, w: number, h: number, r: number) {
  if (typeof p.roundRect === "function") return p.roundRect(x, y, w, h, r);
  p.moveTo(x + r, y);
  p.arcTo(x + w, y, x + w, y + h, r);
  p.arcTo(x + w, y + h, x, y + h, r);
  p.arcTo(x, y + h, x, y, r);
  p.arcTo(x, y, x + w, y, r);
  p.closePath();
}

function bodyPath(shape: Shape, cy: number) {
  // Design units: the body's centre at 50,50, the feet below it as one form.
  const p = new Path2D();
  if (shape.kind === "oval") p.ellipse(50, 50, shape.rx, shape.ry, 0, 0, Math.PI * 2);
  else rounded(p, 50 - shape.w / 2, 50 - shape.h / 2, shape.w, shape.h, 20);
  const fy = 50 + (BASE + 3 - cy);
  p.ellipse(50 - 11, fy, 8, 4.5, 0, 0, Math.PI * 2);
  p.ellipse(50 + 11, fy, 8, 4.5, 0, 0, Math.PI * 2);
  return p;
}

function limbPath(len: number, width: number, hand: boolean) {
  // A capsule along x, centred at 50,50; the forearm's ends in a mitt with a thumb.
  const p = new Path2D();
  rounded(p, 50 - len / 2 - width / 2, 50 - width / 2, len + width, width, width / 2);
  if (hand) {
    p.moveTo(50 + len / 2 + 6.6, 50);
    p.ellipse(50 + len / 2, 50, 6.6, 6.6, 0, 0, Math.PI * 2);
    p.moveTo(50 + len / 2 - 2 + 2.7, 50 - 5.2);
    p.ellipse(50 + len / 2 - 2, 50 - 5.2, 2.7, 2.3, 0, 0, Math.PI * 2);
  }
  return p;
}

type Fur = Partial<Record<keyof typeof BOX, string>> & { cloth?: boolean; turn?: string };

/** The furry parts as images, once the ink is known; none until they are drawn. */
function useFur(
  ink: string | undefined,
  shape: Shape,
  cy: number,
  size: number,
  seen: boolean,
  sleeve?: string,
): Fur & { wantTurn: () => void } {
  const [fur, setFur] = useState<Fur>({});
  const turnKey = useRef<() => void>(() => {});
  useEffect(() => {
    // Nothing is drawn for a bot that has not come into view.
    if (!ink || !seen || typeof document === "undefined") return;
    // A new color, shape or size: the old spin strip no longer matches.
    setFur((f) => (f.turn ? { ...f, turn: undefined } : f));
    const limb = `color-mix(in oklab, ${ink} 88%, black)`;
    const px = Math.min(6, Math.max(2, (size / 120) * (window.devicePixelRatio || 1) * 1.5));
    const k = `${ink}|${sleeve ?? ""}|${JSON.stringify(shape)}|${px.toFixed(2)}|${themeNow()}`;
    const parts: Record<keyof typeof BOX, string> = {
      body: `<g filter="url(#fur)"><ellipse cx="${CX - 11}" cy="${BASE + 3}" rx="8" ry="4.5" fill="${limb}"/><ellipse cx="${CX + 11}" cy="${BASE + 3}" rx="8" ry="4.5" fill="${limb}"/></g>
<g filter="url(#fur)">${shapeMarkup(shape, cy, ink)}</g>${shapeMarkup(shape, cy, "url(#shade)")}
<g filter="url(#grain)" opacity="0.5">${shapeMarkup(shape, cy, "#fff")}</g>`,
      // The upper arm starts inside the body and fades in, so it grows out of it.
      upper: limbMarkup({
        len: UPPER,
        width: 11.5,
        limb,
        hand: false,
        fade: sleeve ? [-30, -20] : [-6, 3],
        sleeve,
      }),
      // The forearm fades in over the elbow, and ends in its hand.
      fore: limbMarkup({ len: FORE, width: 10.4, limb, hand: true, fade: [-5, 1.5], sleeve }),
    };
    let live = true;
    const keep = (name: keyof typeof BOX) => (url: string) =>
      live && url && setFur((f) => ({ ...f, [name]: url }));
    // Sharp enough for the size it is shown at, and no larger: the form's
    // cost grows with it.
    const unit = Math.min(2.2, Math.max(0.8, (size / 80) * (window.devicePixelRatio || 1)));
    const hex = hexOf(ink);
    const limbHex = hexOf(`color-mix(in oklab, ${ink} 90%, black)`);
    const fk = `${hex}|${JSON.stringify(shape)}|${unit.toFixed(2)}|${themeNow()}`;
    bakeFabric(`body|${fk}`, () => bodyPath(shape, cy), hex, unit).then(keep("body"));
    // The turn is drawn on first need, smaller than the still body.
    const turnUnit = Math.min(unit, 1.4);
    turnKey.current = () =>
      bakeTurn(`turn|${fk}|${turnUnit}`, () => bodyPath(shape, cy), hex, turnUnit).then(
        (turn) => live && turn && setFur((f) => ({ ...f, turn })),
      );
    if (sleeve)
      for (const name of ["upper", "fore"] as const)
        bake(`${name}|${k}`, BOX[name], parts[name], px).then(keep(name), () => {});
    else {
      bakeFabric(
        `upper|${limbHex}|${unit}|${themeNow()}`,
        () => limbPath(UPPER, 11.5, false),
        limbHex,
        unit,
      ).then(keep("upper"));
      bakeFabric(
        `fore|${limbHex}|${unit}|${themeNow()}`,
        () => limbPath(FORE, 10.4, true),
        limbHex,
        unit,
      ).then(keep("fore"));
    }
    return () => {
      live = false;
    };
  }, [ink, shape, cy, size, sleeve, seen]);
  const wantTurn = () => turnKey.current();
  return sleeve ? { ...fur, cloth: true, wantTurn } : { ...fur, wantTurn };
}

function FurImage({ url, box }: { url: string; box: [number, number, number, number] }) {
  const [x, y, w, h] = box;
  return <image href={url} x={x} y={y} width={w} height={h} preserveAspectRatio="none" />;
}

/** The playful moves of an idle bot, and how long each takes. */
const ACTS = { hop: 700, spin: 1000, wave: 1300, wiggle: 800 } as const;
type Act = keyof typeof ACTS;

const reducedMotion = () =>
  typeof window !== "undefined" &&
  !!window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;

/**
 * A playful move now and then while idle: a hop, a spin, a wave or a wiggle,
 * never the same twice in a row. `play` starts one at once, as a pointer
 * does. Nothing plays while the bot works or waits, in a hidden tab, or under
 * reduced motion.
 */
function useAct(
  on: boolean,
  every: [number, number],
  canSpin: boolean,
  /** Asks for the turn to be drawn, when a spin comes up before it is. */
  wantSpin: () => void,
) {
  const [act, setAct] = useState<Act>();
  const last = useRef<Act | undefined>(undefined);
  const busy = useRef(false);
  const ends = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const play = () => {
    if (!on || busy.current || reducedMotion()) return;
    if (!canSpin) wantSpin();
    const choices = (Object.keys(ACTS) as Act[]).filter(
      (a) => a !== last.current && (a !== "spin" || canSpin),
    );
    const next = choices[Math.floor(Math.random() * choices.length)] ?? "hop";
    last.current = next;
    busy.current = true;
    setAct(next);
    ends.current = setTimeout(() => {
      busy.current = false;
      setAct(undefined);
    }, ACTS[next]);
  };
  const playRef = useRef(play);
  playRef.current = play;
  const [low, high] = every;
  useEffect(() => {
    if (!on) return;
    let timer: ReturnType<typeof setTimeout>;
    const plan = () => {
      timer = setTimeout(
        () => {
          if (!document.hidden) playRef.current();
          plan();
        },
        low + Math.random() * (high - low),
      );
    };
    plan();
    return () => {
      clearTimeout(timer);
      clearTimeout(ends.current);
      busy.current = false;
      setAct(undefined);
    };
  }, [on, low, high]);
  return { act, play };
}

export function Mascot({
  persona = "plain",
  state = "idle",
  size = 96,
  color,
  className,
  fit,
  quiet,
}: {
  /** Frame the bot itself in a square of `size`, its tools drawn past the frame. */
  fit?: boolean;
  /** Hold still: an idle mark in a list does not fidget. */
  quiet?: boolean;
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
  const leftShoulder: [number, number] = [CX - half + 10, shoulderY];
  const rightShoulder: [number, number] = [CX + half - 10, shoulderY];
  const eyeY = cy - 4;
  // The agent gives its orders at the earpiece, the remote held out in its other hand.
  const arm =
    persona === "spy" && pose === "delegating"
      ? { left: [200, -20] as [number, number], right: [-40, -140] as [number, number] }
      : ARMS[pose];
  const hold = handProp(pose, persona);
  const ref = useRef<SVGSVGElement>(null);
  const [ink, setInk] = useState<string>();
  // biome-ignore lint/correctness/useExhaustiveDependencies: the ink is read again when the color changes.
  useLayoutEffect(() => {
    if (ref.current) setInk(getComputedStyle(ref.current).color);
  }, [color]);
  // Whether it is on screen: it is drawn once it first comes into view, and
  // it holds still while out of view.
  const [seen, setSeen] = useState(false);
  const [away, setAway] = useState(true);
  useEffect(() => {
    const el = ref.current;
    if (!el || typeof IntersectionObserver === "undefined") {
      setSeen(true);
      setAway(false);
      return;
    }
    const io = new IntersectionObserver(
      ([entry]) => {
        const on = !!entry?.isIntersecting;
        setAway(!on);
        if (on) setSeen(true);
      },
      { rootMargin: "200px" },
    );
    io.observe(el);
    return () => io.disconnect();
  }, []);
  const fur = useFur(ink, shape, cy, size, seen, persona === "spy" ? "#16181d" : undefined);
  // An idle bot plays now and then, and at once when pointed at; rarely in a list.
  const { act, play } = useAct(
    pose === "idle" && !away,
    fit ? [14000, 30000] : [4000, 9000],
    !!fur.turn,
    fur.wantTurn,
  );
  return (
    <svg
      ref={ref}
      aria-hidden="true"
      viewBox={fit ? "12 30 80 80" : "0 0 130 120"}
      width={fit ? size : (size * 130) / 120}
      height={size}
      data-quiet={quiet || undefined}
      data-act={act}
      data-away={away || undefined}
      onPointerEnter={(e) => {
        if (e.pointerType === "mouse") play();
      }}
      className={`hm-mascot ${className ?? ""}`}
      data-pose={pose}
      data-persona={persona}
      style={{ color, "--m-size": `${size}px` } as CSSProperties}
    >
      <defs>
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
        <PropDefs id={id} />
        <clipPath id={`${id}-frame`}>
          <rect
            x={fabricPlace([CX, cy])[0]}
            y={fabricPlace([CX, cy])[1]}
            width={SPAN}
            height={SPAN}
          />
        </clipPath>
      </defs>

      {/* The ground under it. */}
      <ellipse
        className="m-shadow"
        cx={CX}
        cy={BASE + 7}
        rx={half - 2}
        ry="3"
        fill="#000"
        opacity="0.22"
      />
      {/* What stands beside it. */}
      {pose === "searching" && <Microscope u={u} />}
      {pose === "waiting" && <Hourglass u={u} />}

      {/* The bot itself, body and arms together, so it hops and spins as one. */}
      <g className="m-actor">
        <g className="m-body">
          {/* The body and feet in fur, drawn once; a plain shape until then. */}
          {fur.body ? (
            <g className="m-still">
              <FurImage url={fur.body} box={fabricPlace([CX, cy])} />
            </g>
          ) : (
            <g>
              <g className="m-limb">
                <ellipse cx={CX - 11} cy={BASE + 3} rx="8" ry="4.5" fill="currentColor" />
                <ellipse cx={CX + 11} cy={BASE + 3} rx="8" ry="4.5" fill="currentColor" />
              </g>
              <BodyShape shape={shape} cy={cy} fill="currentColor" />
              <BodyShape shape={shape} cy={cy} fill={u("shade")} />
            </g>
          )}
          {fur.turn && (
            // The body drawn at every angle of a turn, shown frame by frame as it spins.
            <g className="m-turn" clipPath={`url(#${id}-frame)`}>
              <image
                className="m-strip"
                href={fur.turn}
                x={fabricPlace([CX, cy])[0]}
                y={fabricPlace([CX, cy])[1]}
                width={SPAN * TURN_FRAMES}
                height={SPAN}
                preserveAspectRatio="none"
              />
            </g>
          )}
          {/* What is on its front slides round the body as it turns, and hides behind it. */}
          <g className="m-front">
            <g className="m-no-hat">
              <Outfit persona={persona} u={u} top={top} cy={cy} half={half} />
            </g>
            <EyePair kind={EYES[persona]} y={eyeY} pose={pose} persona={persona} />
            {persona === "detective" && (pose === "idle" || pose === "thinking") && (
              <Pipe x={CX + 9} y={eyeY + 13} u={u} />
            )}
          </g>
          {/* A hat turns with the head and stays in sight. */}
          <g className="m-turn-hat m-only-hat">
            <Outfit persona={persona} u={u} top={top} cy={cy} half={half} />
          </g>
        </g>

        <g className="m-arm-l">
          <Arm
            at={leftShoulder}
            angles={arm.left}
            u={u}
            fur={fur}
            upper="m-left-upper"
            fore="m-left-fore"
            hold={
              pose === "composing" ? (
                <Notepad u={u} />
              ) : persona === "spy" && pose === "delegating" ? (
                <Remote u={u} signal />
              ) : undefined
            }
          />
        </g>
        <g className="m-arm-r">
          <Arm
            at={rightShoulder}
            angles={arm.right}
            u={u}
            fur={fur}
            upper="m-right-upper"
            fore="m-right-fore"
            hold={hold?.(u)}
          />
        </g>
      </g>

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
  fur,
  upper,
  fore,
  hold,
}: {
  at: [number, number];
  angles: [number, number];
  u: (n: string) => string;
  fur: Fur;
  upper: string;
  fore: string;
  hold?: ReactNode;
}) {
  const [a, b] = angles;
  return (
    <g transform={`translate(${at[0]} ${at[1]}) rotate(${a})`}>
      <g className={`m-upper ${upper}`}>
        {fur.upper ? (
          <FurImage url={fur.upper} box={fur.cloth ? BOX.upper : fabricPlace([UPPER / 2, 0])} />
        ) : (
          <path
            d={`M0 0L${UPPER} 0`}
            stroke="currentColor"
            strokeWidth="9"
            strokeLinecap="round"
            className="m-limb"
          />
        )}
        <g transform={`translate(${UPPER} 0) rotate(${b})`}>
          <g className={`m-fore ${fore}`}>
            {fur.fore ? (
              <FurImage url={fur.fore} box={fur.cloth ? BOX.fore : fabricPlace([FORE / 2, 0])} />
            ) : (
              <path
                d={`M0 0L${FORE} 0`}
                stroke="currentColor"
                strokeWidth="8.4"
                strokeLinecap="round"
                className="m-limb"
              />
            )}
            <g transform={`translate(${FORE} 0)`}>
              {/* What the hand holds stays upright, whatever the arm's angle. */}
              <g transform={`rotate(${-(a + b)})`}>{hold}</g>
              {/* The hand is part of the forearm's piece; a plain one until it is drawn. */}
              {!fur.fore && (
                <g>
                  <circle r="6" fill="currentColor" className="m-limb" />
                  <circle r="6" fill={u("shade")} />
                </g>
              )}
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
        {kind === "suave" && (
          // One brow level, one raised: calm, and a step ahead.
          <path
            d={
              x < CX
                ? `M${x - r} ${y - r - 2.4}L${x + r} ${y - r - 2.4}`
                : `M${x - r} ${y - r - 2.6}Q${x} ${y - r - 6} ${x + r} ${y - r - 3.6}`
            }
            stroke="#1b1d2a"
            strokeWidth="1.8"
            strokeLinecap="round"
            fill="none"
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
          <g className="m-hat" transform={`translate(0 ${top - 34})`}>
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
          <g className="m-hat" transform={`translate(0 ${top - 37}) rotate(-12 50 40)`}>
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
          <g className="m-hat" transform={`translate(0 ${top - 40})`}>
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
          <g className="m-hat" transform={`translate(0 ${top - 37})`}>
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
          <g className="m-hat" transform={`translate(0 ${top - 37})`}>
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
    case "spy":
      // Humagen runs the others, as a secret agent would: a tuxedo with satin
      // lapels, a white shirt and black bow tie, a pocket square, slicked hair
      // and an earpiece on its coiled wire.
      return (
        <g>
          <path
            d={`M${L + 1} ${cy + 4}Q${L - 1} ${cy + 26} ${L + 7} ${BASE + 1}L${R - 7} ${BASE + 1}Q${R + 1} ${cy + 26} ${R - 1} ${cy + 4}Q${CX} ${cy + 12} ${L + 1} ${cy + 4}Z`}
            fill="#16181d"
          />
          <path
            d={`M${CX - 11} ${cy + 9}L${CX} ${BASE - 4}L${CX + 11} ${cy + 9}Q${CX} ${cy + 13} ${CX - 11} ${cy + 9}Z`}
            fill="#f6f7f9"
          />
          <path d={`M${CX} ${cy + 20}v${BASE - cy - 26}`} stroke="#d5d9df" strokeWidth="0.6" />
          <circle cx={CX} cy={cy + 23} r="0.9" fill="#1b1d22" />
          <circle cx={CX} cy={cy + 28} r="0.9" fill="#1b1d22" />
          <path
            d={`M${CX - 11} ${cy + 9}L${CX - 3} ${BASE - 10}L${CX - 15} ${cy + 22}Z`}
            fill="#2b2f38"
          />
          <path
            d={`M${CX + 11} ${cy + 9}L${CX + 3} ${BASE - 10}L${CX + 15} ${cy + 22}Z`}
            fill="#2b2f38"
          />
          <path d={`M${L + 9} ${cy + 19}l7-1.2-1.8 3.6z`} fill="#f6f7f9" />
          <path d={`M${CX - 1} ${cy + 13}l-7-3.6v7.2z`} fill="#0b0c0f" />
          <path d={`M${CX + 1} ${cy + 13}l7-3.6v7.2z`} fill="#0b0c0f" />
          <rect x={CX - 1.8} y={cy + 11.2} width="3.6" height="3.6" rx="1" fill="#0b0c0f" />
          <g className="m-hat" transform={`translate(0 ${top - 34})`}>
            {/* Slicked back with a side part: a sweep of dark hair and its shine. */}
            <path
              d="M33 41C32 30 42 24 55 25C67 26 74 32 72 40C67 35 60 32.5 52 33.5L44 33C39 35 35 37.5 33 41Z"
              fill="#1e1a17"
            />
            <path d="M44 33L41 27" stroke="#0e0c0b" strokeWidth="1" strokeLinecap="round" />
            <path
              d="M47 29Q57 26.5 67 31.5"
              stroke="#6a6058"
              strokeWidth="1.1"
              fill="none"
              strokeLinecap="round"
            />
          </g>
          <circle cx={R - 2} cy={cy - 4} r="2.2" fill="#22252b" />
          <path
            d={`M${R - 2} ${cy - 2}c2 2-2 3 0 5s-2 3 0 5s-2 3 0 5`}
            stroke="#c9ced6"
            strokeWidth="0.8"
            fill="none"
          />
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
  if (pose === "composing") return (u: (n: string) => string) => <Pencil u={u} />;
  if (pose === "calling") return (u: (n: string) => string) => <Wrench u={u} />;
  if (persona === "spy" && pose === "idle") return (u: (n: string) => string) => <Remote u={u} />;
  return undefined;
}

/** A blob: picture as a data URL, so a serialized drawing still carries it. */
async function inlined(url: string): Promise<string> {
  if (!url.startsWith("blob:")) return url;
  const blob = await (await fetch(url)).blob();
  return new Promise((done, fail) => {
    const r = new FileReader();
    r.onload = () => done(String(r.result));
    r.onerror = () => fail(r.error);
    r.readAsDataURL(blob);
  });
}

/**
 * The mascot drawn inside `root` as a PNG of `px` square, at rest: what a
 * surface that shows only images, such as Slack, shows for the agent. It
 * waits for the fur to be drawn, and gives null where the page cannot draw.
 */
export async function mascotPicture(root: Element, px = 256): Promise<Blob | null> {
  const frame = () => new Promise((done) => requestAnimationFrame(() => done(undefined)));
  let svg: SVGSVGElement | null = null;
  let count = -1;
  // The fur is baked on idle time: wait until its pictures stop arriving.
  for (let tries = 0; tries < 60; tries++) {
    svg = root.querySelector("svg.hm-mascot");
    const now = svg
      ? [...svg.querySelectorAll("image")].filter((i) => i.getAttribute("href")).length
      : 0;
    if (now > 0 && now === count) break;
    count = now;
    await new Promise((done) => setTimeout(done, 50));
    await frame();
  }
  if (!svg) return null;
  const copy = svg.cloneNode(true) as SVGSVGElement;
  for (const image of copy.querySelectorAll("image")) {
    const href = image.getAttribute("href");
    if (href) image.setAttribute("href", await inlined(href));
  }
  copy.setAttribute("xmlns", "http://www.w3.org/2000/svg");
  copy.setAttribute("width", String(px));
  copy.setAttribute("height", String(px));
  copy.style.color = getComputedStyle(svg).color;
  const url = URL.createObjectURL(
    new Blob([new XMLSerializer().serializeToString(copy)], { type: "image/svg+xml" }),
  );
  try {
    const img = new Image();
    img.src = url;
    await img.decode();
    const canvas = document.createElement("canvas");
    canvas.width = canvas.height = px;
    const g = canvas.getContext("2d");
    if (!g) return null;
    g.drawImage(img, 0, 0, px, px);
    return await new Promise<Blob | null>((done) => canvas.toBlob(done, "image/png"));
  } catch {
    return null;
  } finally {
    URL.revokeObjectURL(url);
  }
}
