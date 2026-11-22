import { readReview, type Turn, type TurnStatus } from "@humagen/core";
import {
  type KeyboardEvent,
  type PointerEvent,
  type RefObject,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";
import { AgentInitials } from "./AgentOrb.tsx";

export interface TurnRailEntry {
  /** Matches `data-turn-id` on the turn's element (AgentMessage stamps it). */
  id: string;
  /** Who said it: an agent's name, or a person's name when `kind` is person. */
  agent: string;
  /** An agent's answer (the default) or a person's message; picks the preview's mark. */
  kind?: "agent" | "person";
  /** What the preview card says: the prompt that started the turn, or the answer's first line. */
  preview: string;
  status: TurnStatus;
  /** How much happened in the turn; sets the tick's length. Any positive scale. */
  weight: number;
  /** When the turn started, ISO. */
  at?: string;
}

/** A rail entry from a turn: weight grows with answer length and work done. */
export function railEntry(turn: Turn, at?: string): TurnRailEntry {
  const review = readReview(turn.text);
  const text = (review.kind === "review" ? review.report.summary : turn.text)
    .replace(/\[\[[^\]]+\]\]/g, "")
    .trim();
  return {
    id: turn.id,
    agent: turn.agent.name,
    preview: text.split("\n").find((l) => l.trim()) ?? "",
    status: turn.status,
    weight: text.length + turn.tasks.length * 120 + (turn.attachments?.length ?? 0) * 200 + 40,
    at,
  };
}

const REVEAL_MS = 1400;
/** The height of one turn's slot on the rail, and of the whole rail at most. */
const SLOT_PX = 12;
const MAX_RAIL = "min(60vh, 32rem)";
const statusWord: Record<TurnStatus, string> = {
  queued: "Queued",
  working: "Working",
  waiting: "Needs input",
  done: "Done",
  failed: "Failed",
  stopped: "Stopped",
};

/** The slot a pointer at `y` is over, on a rail of `n` slots from `top` to `top + height`. */
export function slotAt(y: number, top: number, height: number, n: number): number {
  if (n <= 0 || height <= 0) return 0;
  return Math.min(n - 1, Math.max(0, Math.floor(((y - top) / height) * n)));
}

/** The wave's width in its own units; it is drawn about the middle of this. */
const WAVE_W = 24;

/**
 * The wave's outline: one closed, smooth shape about a center line, as wide
 * at each turn's slot as that turn's amplitude, and closed to a point at
 * both ends. Each side is one curve through every slot, so neighbours run
 * into each other with no step between them. The same number of commands
 * for the same number of turns, so a change of amplitude can be animated.
 */
export function wavePath(amps: readonly number[], slot = SLOT_PX, center = WAVE_W / 2): string {
  const n = amps.length;
  if (n === 0) return "";
  const height = n * slot;
  const f = (v: number) => Number(v.toFixed(2));
  // One side: from the top point, through each slot's middle, to the bottom point.
  const side = (sign: 1 | -1) => {
    const points = [
      { x: center, y: 0 },
      ...amps.map((a, i) => ({ x: center + sign * a, y: (i + 0.5) * slot })),
      { x: center, y: height },
    ];
    return points;
  };
  const curve = (points: { x: number; y: number }[]) =>
    points
      .slice(1)
      .map((p, i) => {
        const from = points[i] as { x: number; y: number };
        // Handles along the rail: the outline is upright at each slot, so it swells and never kinks.
        const reach = (p.y - from.y) / 2;
        return `C${f(from.x)} ${f(from.y + reach)} ${f(p.x)} ${f(p.y - reach)} ${f(p.x)} ${f(p.y)}`;
      })
      .join("");
  const down = side(1);
  const up = side(-1).reverse();
  return `M${f(center)} 0${curve(down)}${curve(up)}Z`;
}

/**
 * The conversation's wave, down its left edge: one connected shape, wider
 * where a turn did more, so a long conversation reads like a sound wave.
 * It swells about the turn in view or under the pointer, which is also the
 * brightest part of it. It rests faint and brightens while the reader
 * scrolls or reaches for it.
 *
 * It is one vertical slider. Hover shows the turn's prompt; press, or on a
 * touch screen press and drag, to scrub through the conversation, which
 * follows; arrows, Home and End step through it from the keyboard.
 */
export function TurnRail({
  turns,
  scroller,
}: {
  turns: TurnRailEntry[];
  scroller: RefObject<HTMLElement | null>;
}) {
  const paint = useId();
  const [active, setActive] = useState<string | null>(turns.at(-1)?.id ?? null);
  const [hovered, setHovered] = useState<number | null>(null);
  const [scrubbing, setScrubbing] = useState(false);
  const [scrolling, setScrolling] = useState(false);
  const ids = useRef<string[]>([]);
  ids.current = turns.map((t) => t.id);
  const rail = useRef<HTMLDivElement>(null);
  // After a jump, the target stays current while the scroll settles.
  const lockUntil = useRef(0);
  const lastJump = useRef(-1);
  // A touch waits to see which way it goes: down the rail scrubs, a tap jumps,
  // and sideways is left to the page (a swipe from the edge opens the drawer).
  const press = useRef<{ x: number; y: number; decided: boolean } | null>(null);

  useEffect(() => {
    const root = scroller.current;
    if (!root) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const onScroll = () => {
      setScrolling(true);
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => setScrolling(false), REVEAL_MS);
    };
    root.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      root.removeEventListener("scroll", onScroll);
      if (timer) clearTimeout(timer);
    };
  }, [scroller]);

  useEffect(() => {
    const root = scroller.current;
    if (!root || typeof IntersectionObserver === "undefined") return;
    const visible = new Set<string>();
    const observer = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          const id = e.target.getAttribute("data-turn-id");
          if (!id) continue;
          if (e.isIntersecting) visible.add(id);
          else visible.delete(id);
        }
        if (Date.now() < lockUntil.current) return;
        // Scrolled to the end: the last turn is current even if it starts lower down.
        const atEnd = root.scrollHeight - root.scrollTop - root.clientHeight < 8;
        const top = atEnd ? ids.current.at(-1) : ids.current.find((id) => visible.has(id));
        if (top) setActive(top);
      },
      { root, rootMargin: "0px 0px -55% 0px", threshold: 0 },
    );
    for (const t of turns) {
      const el = root.querySelector(`[data-turn-id="${CSS.escape(t.id)}"]`);
      if (el) observer.observe(el);
    }
    return () => observer.disconnect();
  }, [turns, scroller]);

  if (turns.length === 0) return null;

  const reduce = () => window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
  const jump = (i: number, smooth: boolean) => {
    const t = turns[i];
    if (!t) return;
    const el = scroller.current?.querySelector(`[data-turn-id="${CSS.escape(t.id)}"]`);
    lockUntil.current = Date.now() + 900;
    const root = scroller.current;
    if (root && el)
      root.scrollTo({
        top: root.scrollTop + el.getBoundingClientRect().top - root.getBoundingClientRect().top,
        behavior: smooth && !reduce() ? "smooth" : "auto",
      });
    setActive(t.id);
    lastJump.current = i;
  };

  const slotOf = (e: PointerEvent<HTMLDivElement>) => {
    const box = rail.current?.getBoundingClientRect();
    return box ? slotAt(e.clientY, box.top, box.height, turns.length) : 0;
  };

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    e.currentTarget.setPointerCapture?.(e.pointerId);
    const i = slotOf(e);
    setScrubbing(true);
    setHovered(i);
    if (e.pointerType === "touch") {
      press.current = { x: e.clientX, y: e.clientY, decided: false };
      return;
    }
    press.current = null;
    jump(i, e.pointerType === "mouse");
  };
  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    const i = slotOf(e);
    const p = press.current;
    if (scrubbing && p && !p.decided) {
      const dx = Math.abs(e.clientX - p.x);
      const dy = Math.abs(e.clientY - p.y);
      if (dx > 10 && dx > dy) {
        press.current = null;
        setScrubbing(false);
        setHovered(null);
        e.currentTarget.releasePointerCapture?.(e.pointerId);
        return;
      }
      if (dy <= 6) return;
      p.decided = true;
    }
    if (scrubbing) {
      if (i !== lastJump.current) {
        jump(i, false);
        // A light tick under the thumb for each turn passed, where the device has one.
        if (e.pointerType === "touch") navigator.vibrate?.(4);
      }
      setHovered(i);
    } else if (e.pointerType === "mouse") setHovered(i);
  };
  const onPointerEnd = (e: PointerEvent<HTMLDivElement>) => {
    const p = press.current;
    // A tap: no scrub, so the turn under it is the one to go to.
    if (scrubbing && p && !p.decided && e.type === "pointerup") jump(slotOf(e), true);
    press.current = null;
    setScrubbing(false);
    if (e.pointerType !== "mouse" || e.type === "pointerleave") setHovered(null);
  };

  const activeIndex = Math.max(
    0,
    turns.findIndex((t) => t.id === active),
  );
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const to =
      e.key === "ArrowDown" || e.key === "ArrowRight"
        ? Math.min(turns.length - 1, activeIndex + 1)
        : e.key === "ArrowUp" || e.key === "ArrowLeft"
          ? Math.max(0, activeIndex - 1)
          : e.key === "Home"
            ? 0
            : e.key === "End"
              ? turns.length - 1
              : null;
    if (to === null) return;
    e.preventDefault();
    jump(to, true);
    setHovered(to);
  };

  const logs = turns.map((t) => Math.log(t.weight + 1));
  const max = Math.max(...logs);
  const min = Math.min(...logs);
  const focus = hovered ?? activeIndex;
  // How near each turn is to the one in view or under the pointer: the wave swells there.
  const nearOf = (i: number) => Math.max(0, 1 - Math.abs(i - focus) / 3);
  const amps = turns.map((_, i) => {
    const norm = max > min ? ((logs[i] ?? min) - min) / (max - min) : 0.5;
    return 2 + norm * 6 + nearOf(i) * 3;
  });
  const current = turns[activeIndex];
  const shown = hovered !== null ? turns[hovered] : undefined;
  const lit = scrolling || scrubbing || hovered !== null;
  // The turn in view is neutral ink, so colour stays for meaning: a failure, a wait on a person.
  const tones = turns.map((t, i) =>
    t.status === "failed"
      ? { color: "var(--aui-bad)", opacity: 1 }
      : i === activeIndex
        ? { color: "var(--aui-fg)", opacity: 1 }
        : t.status === "waiting"
          ? { color: "var(--aui-warn)", opacity: 1 }
          : { color: "var(--aui-fg)", opacity: nearOf(i) > 0 && lit ? 0.45 : 0.25 },
  );

  return (
    <nav
      aria-label="Conversation wave"
      data-layer="wave"
      className={`aui-wave absolute top-1/2 left-0 z-(--aui-z-sticky) -translate-y-1/2 transition-opacity duration-(--aui-dur-page) focus-within:opacity-100 hover:opacity-100 ${lit ? "opacity-100" : "opacity-75"}`}
    >
      <div
        ref={rail}
        role="slider"
        tabIndex={0}
        aria-label="Turns in this conversation"
        aria-orientation="vertical"
        aria-valuemin={1}
        aria-valuemax={turns.length}
        aria-valuenow={activeIndex + 1}
        aria-valuetext={
          current
            ? `${activeIndex + 1} of ${turns.length}, ${current.agent}: ${current.preview || "no answer yet"}`
            : undefined
        }
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerEnd}
        onPointerCancel={onPointerEnd}
        onPointerLeave={onPointerEnd}
        onKeyDown={onKeyDown}
        onBlur={() => setHovered(null)}
        className="aui-wave-rail flex cursor-pointer touch-none select-none flex-col rounded-full outline-none focus-visible:ring-2 focus-visible:ring-aui-accent"
        style={{ height: `min(${turns.length * SLOT_PX}px, ${MAX_RAIL})` }}
      >
        <svg
          aria-hidden
          data-layer="wave-shape"
          width="100%"
          height="100%"
          viewBox={`0 0 ${WAVE_W} ${turns.length * SLOT_PX}`}
          preserveAspectRatio="none"
          className="block overflow-visible"
        >
          <defs>
            {/* Each turn's tone at its slot; between slots one tone runs into the next. */}
            <linearGradient
              id={paint}
              gradientUnits="userSpaceOnUse"
              x1={0}
              y1={0}
              x2={0}
              y2={turns.length * SLOT_PX}
            >
              {tones.map((tone, i) => (
                <stop
                  key={turns[i]?.id}
                  offset={(i + 0.5) / turns.length}
                  className="aui-wave-stop"
                  style={{ stopColor: tone.color, stopOpacity: tone.opacity }}
                />
              ))}
            </linearGradient>
          </defs>
          <path className="aui-wave-shape" d={wavePath(amps)} fill={`url(#${CSS.escape(paint)})`} />
        </svg>
      </div>
      {shown && hovered !== null && (
        <div
          role="tooltip"
          className="aui-glass pointer-events-none absolute left-full z-(--aui-z-popover) ml-2 flex w-64 max-w-[70vw] -translate-y-1/2 flex-col gap-0.5 rounded-aui px-2.5 py-2 text-aui-xs shadow-md"
          style={{ top: `${((hovered + 0.5) / turns.length) * 100}%` }}
        >
          <span className="flex items-center gap-1.5 text-aui-2xs text-aui-muted">
            {shown.kind === "person" ? (
              <span
                aria-hidden
                data-mark="person"
                className="inline-flex size-4 shrink-0 items-center justify-center rounded-full bg-aui-hover text-aui-2xs leading-none font-medium text-aui-fg"
              >
                {(shown.agent.trim()[0] ?? "?").toUpperCase()}
              </span>
            ) : (
              <AgentInitials name={shown.agent} size={16} />
            )}
            <span className="font-medium text-aui-fg">{shown.agent}</span>
            <span>| {statusWord[shown.status]}</span>
            <span className="ml-auto tabular-nums">
              {shown.at
                ? new Date(shown.at).toLocaleTimeString(undefined, {
                    hour: "numeric",
                    minute: "2-digit",
                  })
                : `${hovered + 1}/${turns.length}`}
            </span>
          </span>
          <span className="line-clamp-2 text-aui-fg">{shown.preview || "No answer yet"}</span>
        </div>
      )}
    </nav>
  );
}
